package db

import (
	"strings"
	"testing"
)

func TestOutboxRetryMigrationKeepsPayloadAndAddsBoundedScheduling(t *testing.T) {
	raw, err := migrationsFS.ReadFile("migrations/057_outbox_retry_scheduling.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(raw))
	for _, required := range []string{
		"next_attempt_at", "dead_lettered_at", "error_class",
		"stream_attempts", "telegram_attempts", "publish_attempts",
		"idx_signal_delivery_outbox_ready", "idx_important_event_outbox_ready",
		"idx_important_event_outbox_event_pending",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration lacks %s", required)
		}
	}
	if strings.Contains(sql, "delete from") {
		t.Fatal("retry migration must retain dead-letter payloads for audit/replay")
	}
}
