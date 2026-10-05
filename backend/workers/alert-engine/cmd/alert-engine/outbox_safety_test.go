package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
)

func TestOutboxRetryDecisionBacksOffAndDeadLetters(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	first := decideOutboxRetry(now, 7, 1, 3, errors.New("temporary"))
	if first.Class != outboxErrorTransient || first.DeadLetter || !first.NextAttempt.After(now) {
		t.Fatalf("first retry=%+v", first)
	}
	exhausted := decideOutboxRetry(now, 7, 3, 3, errors.New("temporary"))
	if exhausted.Class != outboxErrorExhausted || !exhausted.DeadLetter {
		t.Fatalf("exhausted retry=%+v", exhausted)
	}
	permanent := decideOutboxRetry(now, 7, 1, 3, markOutboxPermanent(errors.New("bad payload")))
	if permanent.Class != outboxErrorPermanent || !permanent.DeadLetter {
		t.Fatalf("permanent retry=%+v", permanent)
	}
}

func TestOutboxRetryDelayIsDeterministicBoundedAndGrows(t *testing.T) {
	previous := time.Duration(0)
	for attempt := 1; attempt <= 20; attempt++ {
		first := outboxRetryDelay(42, attempt, time.Second, time.Minute)
		second := outboxRetryDelay(42, attempt, time.Second, time.Minute)
		if first != second || first <= 0 || first > time.Minute {
			t.Fatalf("attempt=%d delay=%s second=%s", attempt, first, second)
		}
		if attempt > 1 && first < previous/2 {
			t.Fatalf("retry collapsed attempt=%d previous=%s current=%s", attempt, previous, first)
		}
		previous = first
	}
}

func TestBoundedOutboxErrorPreservesUTF8(t *testing.T) {
	value := boundedOutboxError(errors.New(strings.Repeat("я", maxOutboxStoredErrorBytes)))
	if len(value) > maxOutboxStoredErrorBytes || !utf8.ValidString(value) {
		t.Fatalf("bounded error bytes=%d valid=%v", len(value), utf8.ValidString(value))
	}
}

func TestPublishOutboxStreamOnceIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	first, err := publishOutboxStreamOnce(ctx, client, "alerts:test", "signal:17", `{"deliveryId":17}`, 100, 100, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := publishOutboxStreamOnce(ctx, client, "alerts:test", "signal:17", `{"deliveryId":17}`, 100, 100, time.Hour, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("idempotent IDs differ: %s != %s", first, second)
	}
	if length := client.XLen(ctx, "alerts:test").Val(); length != 1 {
		t.Fatalf("stream length=%d want=1", length)
	}
	if _, err := publishOutboxStreamOnce(ctx, client, "alerts:test", "signal:17", `{"deliveryId":17,"changed":true}`, 100, 100, time.Hour, now.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "payload conflict") {
		t.Fatalf("changed payload was not rejected: %v", err)
	}
	if err := client.XDel(ctx, "alerts:test", first).Err(); err != nil {
		t.Fatal(err)
	}
	republished, err := publishOutboxStreamOnce(ctx, client, "alerts:test", "signal:17", `{"deliveryId":17}`, 100, 100, time.Hour, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if republished == first || client.XLen(ctx, "alerts:test").Val() != 1 {
		t.Fatalf("trimmed delivery was not safely republished old=%s new=%s", first, republished)
	}
}
