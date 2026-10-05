package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestApplyMigrationsSmoke(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	pool, err := NewPool(dsn)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	checks := []string{
		"SELECT period_start,period_end,summary FROM news_summaries LIMIT 0",
		"SELECT exchange,market,symbol,backfill_to,live_to FROM candle_archive_series LIMIT 0",
		"SELECT id,user_id,request_key,request,status,progress,lease_token,result FROM backtest_jobs LIMIT 0",
		"SELECT job_id,ordinal,trade FROM backtest_trades LIMIT 0",
		"SELECT market,symbol,timeframe,last_used_at,refreshed_at FROM backtest_candle_series LIMIT 0",
		"SELECT primary_exchange FROM users LIMIT 0",
		"SELECT exchange, market_type FROM alerts LIMIT 0",
		"SELECT exchange, market_type FROM signals LIMIT 0",
		"SELECT signal_id, payload, stream_published_at, telegram_processed_at, claim_token, claim_expires_at, next_attempt_at, dead_lettered_at, error_class, stream_attempts, telegram_attempts FROM signal_delivery_outbox LIMIT 0",
		"SELECT 1 FROM community_posts LIMIT 1",
		"SELECT event_id, operation, version, payload, published_at, claim_token, claim_expires_at, next_attempt_at, dead_lettered_at, error_class, publish_attempts FROM important_event_outbox LIMIT 0",
		"SELECT provider, chain, address, last_scanned_block, cursor_token, last_scanned_at FROM onchain_scan_cursors LIMIT 0",
		"SELECT last_ingested_at FROM important_events LIMIT 0",
		"SELECT user_id, config, created_at, updated_at FROM user_chart_configurations LIMIT 0",
	}
	for _, q := range checks {
		_, err := pool.Exec(ctx, q)
		if err != nil {
			t.Fatalf("smoke query failed for %q: %v", q, err)
		}
	}
	for _, index := range []string{"uq_alerts_user_slot_exchange_market", "uq_signals_rule_instrument_tf_ts", "idx_important_events_confirmed_identity", "idx_important_events_resolution_clock", "idx_important_event_outbox_pending", "idx_signal_delivery_outbox_pending", "idx_signal_delivery_outbox_ready", "idx_important_event_outbox_ready", "idx_important_event_outbox_event_pending", "idx_onchain_scan_cursors_updated"} {
		var resolved *string
		if err := pool.QueryRow(ctx, "SELECT to_regclass($1)::text", index).Scan(&resolved); err != nil {
			t.Fatalf("check index %q: %v", index, err)
		}
		if resolved == nil {
			t.Fatalf("expected index %q to exist", index)
		}
	}

	var constraintValidated bool
	var constraintDefinition string
	if err := pool.QueryRow(ctx, `
		SELECT convalidated, pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = 'alerts_window_minutes_24h_check'
		  AND conrelid = 'public.alerts'::regclass
	`).Scan(&constraintValidated, &constraintDefinition); err != nil {
		t.Fatalf("check alerts window constraint: %v", err)
	}
	if !constraintValidated {
		t.Fatal("alerts window constraint must be validated")
	}
	if !strings.Contains(constraintDefinition, "1440") {
		t.Fatalf("unexpected alerts window constraint: %s", constraintDefinition)
	}
}
