package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const signalOutboxClaimSQL = `
	WITH candidate AS (
		SELECT candidate_row.id
		FROM signal_delivery_outbox AS candidate_row
		WHERE (candidate_row.stream_published_at IS NULL OR candidate_row.telegram_processed_at IS NULL)
		  AND candidate_row.dead_lettered_at IS NULL
		  AND candidate_row.next_attempt_at <= now()
		  AND (candidate_row.claim_expires_at IS NULL OR candidate_row.claim_expires_at <= now())
		ORDER BY candidate_row.next_attempt_at, candidate_row.id
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	)
	UPDATE signal_delivery_outbox AS outbox
	SET claim_token=$1,
	    claim_expires_at=now() + ($2::bigint * interval '1 second')
	FROM candidate
	WHERE outbox.id=candidate.id
	RETURNING outbox.id, outbox.payload,
	          outbox.stream_published_at IS NOT NULL,
	          outbox.telegram_processed_at IS NOT NULL,
	          outbox.stream_attempts, outbox.telegram_attempts
`

const importantOutboxClaimSQL = `
	WITH candidate AS (
		SELECT candidate_row.id
		FROM important_event_outbox AS candidate_row
		WHERE candidate_row.published_at IS NULL
		  AND candidate_row.dead_lettered_at IS NULL
		  AND candidate_row.next_attempt_at <= now()
		  AND (candidate_row.claim_expires_at IS NULL OR candidate_row.claim_expires_at <= now())
		  AND NOT EXISTS (
			SELECT 1
			FROM important_event_outbox AS earlier
			WHERE earlier.event_id=candidate_row.event_id
			  AND earlier.id < candidate_row.id
			  AND earlier.published_at IS NULL
		  )
		ORDER BY candidate_row.next_attempt_at, candidate_row.id
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	)
	UPDATE important_event_outbox AS outbox
	SET claim_token=$1,
	    claim_expires_at=now() + ($2::bigint * interval '1 second')
	FROM candidate
	WHERE outbox.id=candidate.id
	RETURNING outbox.id, outbox.payload, outbox.publish_attempts
`

func scheduleSignalDeliveryFailure(ctx context.Context, db *pgxpool.Pool, item pendingSignalDelivery, deliveryErr error) error {
	if deliveryErr == nil {
		deliveryErr = errors.New("unknown signal outbox failure")
	}
	column := "telegram_attempts"
	attempt := item.telegramAttempts + 1
	if !item.streamPublished {
		column = "stream_attempts"
		attempt = item.streamAttempts + 1
	}
	decision := decideOutboxRetry(time.Now().UTC(), item.id, attempt, defaultOutboxMaxAttempts, deliveryErr)
	query := `
		UPDATE signal_delivery_outbox
		SET attempts=attempts+1, telegram_attempts=telegram_attempts+1,
		    last_error=$3, error_class=$4, next_attempt_at=$5,
		    dead_lettered_at=CASE WHEN $6 THEN now() ELSE NULL END,
		    claim_token=NULL, claim_expires_at=NULL
		WHERE id=$1 AND claim_token=$2
	`
	if column == "stream_attempts" {
		query = `
			UPDATE signal_delivery_outbox
			SET attempts=attempts+1, stream_attempts=stream_attempts+1,
			    last_error=$3, error_class=$4, next_attempt_at=$5,
			    dead_lettered_at=CASE WHEN $6 THEN now() ELSE NULL END,
			    claim_token=NULL, claim_expires_at=NULL
			WHERE id=$1 AND claim_token=$2
		`
	}
	tag, updateErr := db.Exec(ctx, query, item.id, item.claimToken, boundedOutboxError(deliveryErr), string(decision.Class), decision.NextAttempt, decision.DeadLetter)
	if updateErr != nil {
		return fmt.Errorf("signal delivery failed: %v; record failure: %w", deliveryErr, updateErr)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("signal delivery failed: %v; claim lost for delivery %d", deliveryErr, item.id)
	}
	return deliveryErr
}

func scheduleImportantDeliveryFailure(ctx context.Context, db *pgxpool.Pool, item pendingImportantEventDelivery, deliveryErr error) error {
	if deliveryErr == nil {
		deliveryErr = errors.New("unknown important-event outbox failure")
	}
	attempt := item.publishAttempts + 1
	decision := decideOutboxRetry(time.Now().UTC(), item.id, attempt, defaultOutboxMaxAttempts, deliveryErr)
	tag, updateErr := db.Exec(ctx, `
		UPDATE important_event_outbox
		SET attempts=attempts+1, publish_attempts=publish_attempts+1,
		    last_error=$3, error_class=$4, next_attempt_at=$5,
		    dead_lettered_at=CASE WHEN $6 THEN now() ELSE NULL END,
		    claim_token=NULL, claim_expires_at=NULL
		WHERE id=$1 AND claim_token=$2
	`, item.id, item.claimToken, boundedOutboxError(deliveryErr), string(decision.Class), decision.NextAttempt, decision.DeadLetter)
	if updateErr != nil {
		return fmt.Errorf("important-event delivery failed: %v; record failure: %w", deliveryErr, updateErr)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("important-event delivery failed: %v; claim lost for delivery %d", deliveryErr, item.id)
	}
	return deliveryErr
}
