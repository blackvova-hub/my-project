package main

import (
	"testing"
	"time"
)

func TestOutboxDispatcherFallbackStaysBelowOneSecond(t *testing.T) {
	if outboxFallbackInterval <= 0 || outboxFallbackInterval >= 1_000_000_000 {
		t.Fatalf("fallback interval=%s must be positive and below one second", outboxFallbackInterval)
	}
}

func TestOutboxClaimLeaseCoversExternalIOTimeout(t *testing.T) {
	const telegramHTTPTimeout = 8 * time.Second
	if outboxClaimLease <= telegramHTTPTimeout {
		t.Fatalf("claim lease=%s must exceed Telegram HTTP timeout=%s", outboxClaimLease, telegramHTTPTimeout)
	}
}

func TestNewOutboxClaimTokenIsUnique(t *testing.T) {
	first, err := newOutboxClaimToken()
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	second, err := newOutboxClaimToken()
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if first == "" || second == "" {
		t.Fatal("claim tokens must not be empty")
	}
	if first == second {
		t.Fatalf("claim tokens must be unique: %q", first)
	}
}
