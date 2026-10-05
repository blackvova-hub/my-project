package main

import (
	"strings"
	"testing"
)

func TestSignalOutboxClaimSkipsBackoffAndDeadLetters(t *testing.T) {
	for _, required := range []string{"dead_lettered_at IS NULL", "next_attempt_at <= now()", "FOR UPDATE SKIP LOCKED", "ORDER BY candidate_row.next_attempt_at, candidate_row.id", "stream_attempts", "telegram_attempts"} {
		if !strings.Contains(signalOutboxClaimSQL, required) {
			t.Fatalf("signal claim lacks %q", required)
		}
	}
}

func TestImportantOutboxClaimPreservesPerEventOrderWithoutBlockingOtherEvents(t *testing.T) {
	for _, required := range []string{"dead_lettered_at IS NULL", "next_attempt_at <= now()", "NOT EXISTS", "earlier.event_id=candidate_row.event_id", "earlier.id < candidate_row.id", "earlier.published_at IS NULL", "FOR UPDATE SKIP LOCKED"} {
		if !strings.Contains(importantOutboxClaimSQL, required) {
			t.Fatalf("important claim lacks %q", required)
		}
	}
	if strings.Contains(importantOutboxClaimSQL, "earlier.dead_lettered_at IS NULL") {
		t.Fatal("a dead-lettered earlier version must keep later versions blocked for manual resolution")
	}
}
