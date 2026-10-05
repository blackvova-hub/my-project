package db

import (
	"strings"
	"testing"
)

func TestImportantEventIngestionClockMigration(t *testing.T) {
	raw, err := migrationsFS.ReadFile("migrations/058_important_event_ingestion_clock.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(raw))
	if !strings.Contains(sql, "last_ingested_at") || !strings.Contains(sql, "idx_important_events_resolution_clock") {
		t.Fatal("migration lacks the processing-time resolution clock or index")
	}
}
