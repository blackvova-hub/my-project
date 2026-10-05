package db

import (
	"strings"
	"testing"
)

func TestAlertWindow24hMigrationContract(t *testing.T) {
	migration, err := migrationsFS.ReadFile("migrations/056_alert_window_24h.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(migration)
	for _, required := range []string{
		"pg_constraint",
		"conrelid = 'public.alerts'::regclass",
		"ADD CONSTRAINT alerts_window_minutes_24h_check",
		"CHECK (window_minutes BETWEEN 1 AND 1440) NOT VALID",
		"VALIDATE CONSTRAINT alerts_window_minutes_24h_check",
		"DROP CONSTRAINT IF EXISTS alerts_window_minutes_check",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration is missing %q", required)
		}
	}
	if strings.Contains(strings.ToUpper(sql), "UPDATE ALERTS") {
		t.Fatal("migration must not rewrite existing alert rows")
	}

	baseline, err := migrationsFS.ReadFile("baseline/001_base_current.sql")
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	if !strings.Contains(string(baseline), "CONSTRAINT alerts_window_minutes_24h_check CHECK (window_minutes BETWEEN 1 AND 1440)") {
		t.Fatal("baseline must contain the 24-hour alerts constraint")
	}
}

func TestAlertWindow24hReconciliationMigrationContract(t *testing.T) {
	migration, err := migrationsFS.ReadFile("migrations/062_reconcile_alert_window_24h.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(migration)
	for _, required := range []string{
		"pg_constraint",
		"conrelid = 'public.alerts'::regclass",
		"ADD CONSTRAINT alerts_window_minutes_24h_check",
		"CHECK (window_minutes BETWEEN 1 AND 1440) NOT VALID",
		"VALIDATE CONSTRAINT alerts_window_minutes_24h_check",
		"DROP CONSTRAINT IF EXISTS alerts_window_minutes_check",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("reconciliation migration is missing %q", required)
		}
	}
}
