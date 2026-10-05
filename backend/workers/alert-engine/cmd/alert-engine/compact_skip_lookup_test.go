package main

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
)

type fakeCompactSkipSource struct {
	compactStreamBootstrapSource
	values map[string]string
	err    error
}

func (s *fakeCompactSkipSource) GetCompactSkipLedger(_ context.Context, key string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	value, ok := s.values[key]
	if !ok {
		return "", redis.Nil
	}
	return value, nil
}

func TestResolveBootstrapCompactSkipRequiresExactPayloadLedger(t *testing.T) {
	message := redis.XMessage{ID: "1700000000000-0", Values: map[string]any{"json": "not-json"}}
	record, err := streamMessageSkipLedgerRecord(message, "candles:v3", "group", streamFailureMalformedJSON)
	if err != nil {
		t.Fatal(err)
	}
	if record.Key != compactSkipLedgerKey("candles:v3", "group", message.ID) {
		t.Fatal("runtime and bootstrap ledger keys differ")
	}
	source := &fakeCompactSkipSource{values: map[string]string{record.Key: record.Value}}
	digest, skipped, err := resolveBootstrapCompactSkip(context.Background(), source, "candles:v3", "group", message)
	if err != nil || !skipped || digest != record.SkipDigest {
		t.Fatalf("digest=%q skipped=%v err=%v", digest, skipped, err)
	}
	message.Values["json"] = "changed"
	if _, skipped, err := resolveBootstrapCompactSkip(context.Background(), source, "candles:v3", "group", message); err == nil || skipped {
		t.Fatalf("changed payload skipped=%v err=%v", skipped, err)
	}
}

func TestResolveBootstrapCompactSkipFailsClosedOnLookupError(t *testing.T) {
	message := redis.XMessage{ID: "1-0", Values: map[string]any{"json": "bad"}}
	source := &fakeCompactSkipSource{err: errors.New("redis unavailable")}
	if _, skipped, err := resolveBootstrapCompactSkip(context.Background(), source, "stream", "group", message); err == nil || skipped {
		t.Fatalf("lookup failure skipped=%v err=%v", skipped, err)
	}
}
