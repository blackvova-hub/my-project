package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const compactReplayPastTolerance = time.Hour

// processCompactStreamBatch registers the complete Redis delivery before any
// message is applied. This preserves a contiguous checkpoint even when a
// later entry finishes before an earlier retry or poison entry.
func (e *Engine) processCompactStreamBatch(ctx context.Context, db *pgxpool.Pool, stream string, messages []redis.XMessage, reclaimed bool, consumer string, deduper *compactBatchDeduper) error {
	if e == nil || len(messages) == 0 {
		return nil
	}
	e.streamProcessing.Lock()
	defer e.streamProcessing.Unlock()
	if e.compactCheckpointingEnabled() {
		if err := e.unifiedHistory.RegisterStreamMessages(stream, streamMessageIDs(messages)); err != nil {
			return fmt.Errorf("register compact stream messages: %w", err)
		}
	}
	guard, err := startStreamBatchGuard(ctx, e.rdb, stream, e.cfg.MarketConsumerGroup, consumer, streamMessageIDs(messages), time.Duration(max1(e.cfg.PendingMinIdleSeconds))*time.Second)
	if err != nil {
		if errors.Is(err, errStreamMessageBusy) {
			e.streamSafety.ownershipSkipped.Add(uint64(len(messages)))
			return nil
		}
		e.streamSafety.guardFailed.Add(uint64(len(messages)))
		return fmt.Errorf("start compact stream guard: %w", err)
	}
	defer guard.Close()
	acknowledge := make([]string, 0, len(messages))
	for _, message := range messages {
		shouldAck, err := e.processCompactStreamMessage(guard.MessageContext(message.ID), db, stream, message, reclaimed, consumer, deduper, guard)
		if err != nil {
			return err
		}
		if shouldAck {
			acknowledge = append(acknowledge, message.ID)
		}
	}
	if len(acknowledge) > 0 {
		if err := e.assertCompactOwner(ctx); err != nil {
			return err
		}
		for _, messageID := range acknowledge {
			e.ackStreamMessage(ctx, stream, e.cfg.MarketConsumerGroup, consumer, messageID, guard)
		}
	}
	return nil
}

func (e *Engine) processCompactStreamMessage(ctx context.Context, db *pgxpool.Pool, stream string, message redis.XMessage, reclaimed bool, consumer string, deduper *compactBatchDeduper, guard *streamBatchGuard) (bool, error) {
	if guard == nil || !guard.Owns(message.ID) {
		e.streamSafety.ownershipSkipped.Add(1)
		return false, nil
	}
	raw, ok := compactMessageJSON(message.Values["json"])
	if !ok {
		return false, e.archiveCompactStreamMessage(ctx, stream, message, streamFailurePermanent, streamFailureMissingJSON, errors.New("compact message json field is missing or empty"), guard.DeliveryCount(message.ID), consumer, guard)
	}
	decoded, err := decodeCompactBatch(raw)
	if err != nil {
		return false, e.archiveCompactStreamMessage(ctx, stream, message, streamFailurePermanent, streamFailureMalformedJSON, fmt.Errorf("decode compact batch: %w", err), guard.DeliveryCount(message.ID), consumer, guard)
	}
	now := time.Now().UTC()
	if err := validateCompactReplayMinute(decoded.Minute, now); err != nil {
		return false, e.archiveCompactStreamMessage(ctx, stream, message, streamFailurePermanent, streamFailureInvalidTimestamp, err, guard.DeliveryCount(message.ID), consumer, guard)
	}
	if err := validateCompactBatchStreamIdentity(stream, &decoded); err != nil {
		return false, e.archiveCompactStreamMessage(ctx, stream, message, streamFailurePermanent, streamFailureVenueMismatch, err, guard.DeliveryCount(message.ID), consumer, guard)
	}
	dedupResult := deduper.Observe(decoded.BatchID, raw, now)
	if dedupResult == compactBatchConflict {
		return false, e.archiveCompactStreamMessage(ctx, stream, message, streamFailurePermanent, streamFailureConflictingDuplicate, fmt.Errorf("compact batch %s has conflicting payload bytes", decoded.BatchID), guard.DeliveryCount(message.ID), consumer, guard)
	}
	for index := range decoded.Events {
		if !prepareCandleEventForStream(&decoded.Events[index], stream) {
			return false, e.archiveCompactStreamMessage(ctx, stream, message, streamFailurePermanent, streamFailureVenueMismatch, fmt.Errorf("compact venue mismatch: exchange=%s market_type=%s", decoded.Exchange, decoded.MarketType), guard.DeliveryCount(message.ID), consumer, guard)
		}
	}
	if e.unifiedHistory != nil {
		var observeErr error
		if e.compactCheckpointingEnabled() {
			observeErr = e.unifiedHistory.ObserveStreamBatch(stream, message.ID, decoded, now)
		} else {
			observeErr = e.unifiedHistory.ObserveBatch(decoded, now)
		}
		if err := observeErr; err != nil {
			if errors.Is(err, errUnifiedInstrumentCapacity) {
				deduper.Forget(decoded.BatchID, raw)
				return false, e.retryOrArchiveCompactCapacity(ctx, stream, message, reclaimed, consumer, guard, err)
			}
			if errors.Is(err, errCompactStreamMessageGap) || errors.Is(err, errCompactStreamNotBootstrapped) || errors.Is(err, errCompactBootstrapRulesChanged) || errors.Is(err, ErrUnifiedSnapshotInvalidState) || errors.Is(err, ErrUnifiedSnapshotTooLarge) {
				return false, fmt.Errorf("compact checkpoint invariant stream=%s id=%s: %w", stream, message.ID, err)
			}
			return false, e.archiveCompactStreamMessage(ctx, stream, message, streamFailurePermanent, streamFailureCoverageConflict, err, guard.DeliveryCount(message.ID), consumer, guard)
		}
	}
	if err := e.processProductionBatch(ctx, db, decoded, now); err != nil {
		deduper.Forget(decoded.BatchID, raw)
		if errors.Is(err, errCompactBootstrapRulesChanged) {
			return false, err
		}
		deliveries := guard.DeliveryCount(message.ID)
		if reclaimed {
			observed, countErr := streamDeliveryCount(ctx, e.rdb, stream, e.cfg.MarketConsumerGroup, message.ID)
			if countErr != nil {
				e.streamSafety.retries.Add(1)
				e.logStreamSafety("retry_count_failed", stream, message.ID, "production_handle", deliveries, countErr)
				return false, nil
			}
			deliveries = observed
			if deliveries >= int64(e.cfg.StreamMaxDeliveries) {
				return false, e.archiveCompactStreamMessage(ctx, stream, message, streamFailureTransient, streamFailureHandleExhausted, err, deliveries, consumer, guard)
			}
		}
		e.streamSafety.retries.Add(1)
		e.logStreamSafety("retry_pending", stream, message.ID, "production_handle", deliveries, err)
		return false, nil
	}
	return true, nil
}

func (e *Engine) retryOrArchiveCompactCapacity(ctx context.Context, stream string, message redis.XMessage, reclaimed bool, consumer string, guard *streamBatchGuard, failure error) error {
	deliveries := guard.DeliveryCount(message.ID)
	if reclaimed {
		observed, err := streamDeliveryCount(ctx, e.rdb, stream, e.cfg.MarketConsumerGroup, message.ID)
		if err != nil {
			e.streamSafety.retries.Add(1)
			e.logStreamSafety("retry_count_failed", stream, message.ID, string(streamFailureCapacityExhausted), deliveries, err)
			return nil
		}
		deliveries = observed
	}
	if reclaimed && deliveries >= int64(e.cfg.StreamMaxDeliveries) {
		return e.archiveCompactStreamMessage(ctx, stream, message, streamFailureTransient, streamFailureCapacityExhausted, failure, deliveries, consumer, guard)
	}
	e.streamSafety.retries.Add(1)
	e.logStreamSafety("retry_pending", stream, message.ID, string(streamFailureCapacityExhausted), deliveries, failure)
	return nil
}

func (e *Engine) archiveCompactStreamMessage(ctx context.Context, stream string, message redis.XMessage, class streamFailureClass, reason streamFailureReason, failure error, deliveries int64, consumer string, guard *streamBatchGuard) error {
	if err := e.assertCompactOwner(ctx); err != nil {
		return err
	}
	var ledger streamSkipLedgerRecord
	var err error
	if e.compactCheckpointingEnabled() {
		ledger, err = streamMessageSkipLedgerRecord(message, stream, e.cfg.MarketConsumerGroup, reason)
		if err != nil {
			return fmt.Errorf("build compact skip ledger: %w", err)
		}
	}
	if !e.archiveStreamMessage(ctx, stream, e.cfg.MarketConsumerGroup, consumer, message, class, reason, failure, deliveries, guard) {
		return nil
	}
	if e.compactCheckpointingEnabled() {
		if err := e.unifiedHistory.MarkStreamMessageSkipped(stream, message.ID, ledger.SkipDigest, time.Now().UTC()); err != nil {
			return fmt.Errorf("mark compact stream message skipped after durable DLQ: %w", err)
		}
	}
	return nil
}

func validateCompactReplayMinute(minute int64, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	observed := time.UnixMilli(minute).UTC()
	if minute <= 0 || minute%60_000 != 0 {
		return errors.New("compact batch minute is missing or unaligned")
	}
	if observed.After(now.Add(unifiedSnapshotFutureTolerance)) {
		return errors.New("compact batch minute is too far in the future")
	}
	oldest := now.Truncate(time.Minute).Add(-compactHistoryRetentionMinutes*time.Minute - compactReplayPastTolerance)
	if observed.Before(oldest) {
		return errors.New("compact batch minute is older than retained replay window")
	}
	return nil
}

func (e *Engine) compactCheckpointingEnabled() bool {
	return e != nil && e.unifiedHistory != nil && e.compactOwner != nil
}

func validateCompactBatchStreamIdentity(stream string, batch *compactDecodedBatch) error {
	if batch == nil {
		return errors.New("nil compact batch")
	}
	lowerStream := strings.ToLower(strings.TrimSpace(stream))
	if (strings.Contains(lowerStream, "liquidation") && batch.Kind != "l") || (strings.Contains(lowerStream, "candle") && batch.Kind != "c") {
		return errors.New("compact batch kind does not match stream")
	}
	identity := CandleEvent{Exchange: batch.Exchange, MarketType: batch.MarketType}
	if !prepareCandleEventForStream(&identity, stream) {
		return errors.New("compact batch venue does not match stream")
	}
	return nil
}
