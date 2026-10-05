package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestDLQSkipLedgerAtomicIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	stream, group := "codex:test:skip-ledger:"+suffix, "codex-test-skip-ledger-"+suffix
	defer func() {
		_ = client.Del(ctx, stream, streamDLQName(stream)).Err()
	}()

	message := addAndReadPending(t, ctx, client, stream, group, "consumer-a", map[string]any{"json": `{"invalid":true}`})
	ledger, err := streamMessageSkipLedgerRecord(message, stream, group, streamFailureMalformedJSON)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Del(ctx, ledger.Key)
	if _, err := movePendingStreamMessageToDLQWithConfig(ctx, client, stream, group, message, streamFailurePermanent, streamFailureMalformedJSON, errors.New("invalid compact payload"), 1, streamDLQConfig{ExpectedConsumer: "consumer-a"}); err != nil {
		t.Fatal(err)
	}
	value, err := client.Get(ctx, ledger.Key).Result()
	if err != nil || value != ledger.Value {
		t.Fatalf("ledger value=%q want=%q err=%v", value, ledger.Value, err)
	}
	ttl, err := client.PTTL(ctx, ledger.Key).Result()
	if err != nil || ttl < streamSkipLedgerMinTTL || ttl > streamSkipLedgerTTL {
		t.Fatalf("ledger TTL=%s err=%v", ttl, err)
	}
	dlq, err := client.XRangeN(ctx, streamDLQName(stream), "-", "+", 1).Result()
	if err != nil || len(dlq) != 1 || dlq[0].Values["skip_digest"] != ledger.SkipDigest {
		t.Fatalf("DLQ skip metadata=%+v err=%v", dlq, err)
	}
}

func TestDLQOwnerChangeLeavesMessagePendingIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	stream, group := "codex:test:owner-change:"+suffix, "codex-test-owner-change-"+suffix
	defer func() {
		_ = client.Del(ctx, stream, streamDLQName(stream)).Err()
	}()

	message := addAndReadPending(t, ctx, client, stream, group, "consumer-a", map[string]any{"json": `{"invalid":true}`})
	ledger, err := streamMessageSkipLedgerRecord(message, stream, group, streamFailureMalformedJSON)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Del(ctx, ledger.Key)
	claimed, err := client.XClaimJustID(ctx, &redis.XClaimArgs{Stream: stream, Group: group, Consumer: "consumer-b", MinIdle: 0, Messages: []string{message.ID}}).Result()
	if err != nil || len(claimed) != 1 {
		t.Fatalf("change owner ids=%+v err=%v", claimed, err)
	}
	if _, err := movePendingStreamMessageToDLQWithConfig(ctx, client, stream, group, message, streamFailurePermanent, streamFailureMalformedJSON, errors.New("invalid"), 1, streamDLQConfig{ExpectedConsumer: "consumer-a"}); err == nil {
		t.Fatal("stale owner archived a message owned by another consumer")
	}
	pending, err := client.XPending(ctx, stream, group).Result()
	if err != nil || pending.Count != 1 {
		t.Fatalf("message was not left pending: %+v err=%v", pending, err)
	}
	if exists := client.Exists(ctx, ledger.Key).Val(); exists != 0 {
		t.Fatal("owner mismatch wrote skip ledger")
	}
	if length := client.XLen(ctx, streamDLQName(stream)).Val(); length != 0 {
		t.Fatal("owner mismatch wrote DLQ audit")
	}
}
