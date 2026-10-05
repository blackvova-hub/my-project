package db

import (
	"regexp"
	"strings"
	"testing"
)

func TestOutboxDeadLetterConstraintsRejectNullErrorClass(t *testing.T) {
	deadLetterInvariant := regexp.MustCompile(`dead_lettered_at\s+is\s+null\s+or\s+\(?\s*error_class\s+is\s+not\s+null\s+and\s+error_class\s+in`)
	tests := []struct {
		name string
		path string
	}{
		{name: "migration 057", path: "migrations/057_outbox_retry_scheduling.sql"},
		{name: "baseline", path: "baseline/001_base_current.sql"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := migrationsFS.ReadFile(test.path)
			if err != nil {
				t.Fatal(err)
			}
			normalized := strings.ToLower(string(raw))
			if matches := deadLetterInvariant.FindAllStringIndex(normalized, -1); len(matches) != 2 {
				t.Fatalf("%s must enforce error_class IS NOT NULL in both signal and important-event dead-letter constraints; matches=%d", test.path, len(matches))
			}
		})
	}
}
