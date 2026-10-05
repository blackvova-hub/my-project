package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pendingSignalFanoutDelivery struct {
	deliveryID       int64
	alert            AlertEvent
	cooldownExchange string
	ruleInstrument   string
	inserted         bool
}

// waitSignalOutboxDeliveryBatch polls all fanout deliveries in one query per
// tick. The fanout loop durably enqueues every eligible user first, so a
// terminal failure cannot suppress later users and the wait cost is bounded
// by one shared timeout rather than timeout multiplied by rule count.
func waitSignalOutboxDeliveryBatch(ctx context.Context, db *pgxpool.Pool, deliveries []pendingSignalFanoutDelivery, timeout time.Duration) map[int64]error {
	results := make(map[int64]error, len(deliveries))
	pending := make(map[int64]struct{}, len(deliveries))
	lastErrors := make(map[int64]string, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.deliveryID > 0 {
			pending[delivery.deliveryID] = struct{}{}
		}
	}
	if db == nil {
		for id := range pending {
			results[id] = errors.New("signal outbox database is nil")
		}
		return results
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for len(pending) > 0 {
		ids := make([]int64, 0, len(pending))
		for id := range pending {
			ids = append(ids, id)
		}
		rows, err := db.Query(waitCtx, `
			SELECT id, stream_published_at IS NOT NULL, telegram_processed_at IS NOT NULL,
			       dead_lettered_at IS NOT NULL, last_error, error_class
			FROM signal_delivery_outbox
			WHERE id = ANY($1::bigint[])
		`, ids)
		if err != nil {
			for id := range pending {
				results[id] = err
			}
			return results
		}
		for rows.Next() {
			var id int64
			var streamPublished, telegramProcessed, deadLettered bool
			var deliveryError, errorClass pgtype.Text
			if err := rows.Scan(&id, &streamPublished, &telegramProcessed, &deadLettered, &deliveryError, &errorClass); err != nil {
				rows.Close()
				for pendingID := range pending {
					results[pendingID] = err
				}
				return results
			}
			if deliveryError.Valid {
				lastErrors[id] = deliveryError.String
			}
			if deadLettered {
				results[id] = fmt.Errorf("signal outbox delivery %d dead-lettered class=%s: %s", id, errorClass.String, lastErrors[id])
				delete(pending, id)
			} else if streamPublished && telegramProcessed {
				results[id] = nil
				delete(pending, id)
			}
		}
		rowErr := rows.Err()
		rows.Close()
		if rowErr != nil {
			for id := range pending {
				results[id] = rowErr
			}
			return results
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-waitCtx.Done():
			for id := range pending {
				if lastErrors[id] != "" {
					results[id] = fmt.Errorf("signal outbox delivery %d timed out: %s: %w", id, lastErrors[id], waitCtx.Err())
				} else {
					results[id] = fmt.Errorf("signal outbox delivery %d timed out: %w", id, waitCtx.Err())
				}
			}
			return results
		case <-ticker.C:
		}
	}
	return results
}
