package main

import (
	"strings"
	"testing"
)

func TestStreamSkipLedgerBindsIdentityPayloadAndReason(t *testing.T) {
	payloadSHA := strings.Repeat("a", 64)
	left, err := buildStreamSkipLedgerRecord("candles:v3", "group", "1-0", payloadSHA, streamFailureMalformedJSON)
	if err != nil {
		t.Fatal(err)
	}
	right, err := buildStreamSkipLedgerRecord("candles:v3", "group", "1-0", payloadSHA, streamFailureVenueMismatch)
	if err != nil {
		t.Fatal(err)
	}
	if left.Key != right.Key {
		t.Fatal("ledger identity key must not depend on failure reason")
	}
	if left.Value == right.Value || left.SkipDigest == right.SkipDigest {
		t.Fatal("ledger value/digest did not bind the failure reason")
	}
	if left.TTL < streamSkipLedgerMinTTL {
		t.Fatalf("ledger TTL=%s is shorter than retention safety floor", left.TTL)
	}
	if digest, ok := validateStreamSkipLedgerValue(left.Value, payloadSHA); !ok || digest != left.SkipDigest {
		t.Fatalf("ledger validation digest=%q ok=%v", digest, ok)
	}
	if _, ok := validateStreamSkipLedgerValue(left.Value, strings.Repeat("b", 64)); ok {
		t.Fatal("ledger accepted a different payload digest")
	}
}

func TestStreamSkipLedgerRejectsMalformedIdentity(t *testing.T) {
	if _, err := buildStreamSkipLedgerRecord("", "group", "1-0", strings.Repeat("a", 64), streamFailureMalformedJSON); err == nil {
		t.Fatal("empty stream identity was accepted")
	}
	if _, ok := validateStreamSkipLedgerValue("v1:not-a-hash:also-bad", strings.Repeat("a", 64)); ok {
		t.Fatal("malformed ledger value was accepted")
	}
}
