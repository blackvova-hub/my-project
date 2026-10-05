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

func TestCompactPermanentDLQThenCheckpointSkipIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	now := time.Now().UTC().Truncate(time.Second)
	suffix := strconv.FormatInt(now.UnixNano(), 36)
	stream, group, consumer := "codex:test:compact-skip:"+suffix, "codex-test-compact-skip-"+suffix, "consumer-v3"
	defer client.Del(ctx, stream, streamDLQName(stream))
	message := addAndReadPending(t, ctx, client, stream, group, consumer, map[string]any{"json": "malformed"})
	ledger, err := streamMessageSkipLedgerRecord(message, stream, group, streamFailureMalformedJSON)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Del(ctx, ledger.Key)
	history := newSnapshotTestHistory()
	checkpoint := compactStreamHighWater{MessageID: "0-0", ObservedAt: now, ReadyAt: now, Bootstrapped: true}
	if err := history.SetStreamBootstrapCheckpoint(stream, checkpoint, now); err != nil {
		t.Fatal(err)
	}
	if err := history.RegisterStreamMessages(stream, []string{message.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := movePendingStreamMessageToDLQWithConfig(ctx, client, stream, group, message, streamFailurePermanent, streamFailureMalformedJSON, errors.New("decode failed"), 1, streamDLQConfig{ExpectedConsumer: consumer}); err != nil {
		t.Fatal(err)
	}
	if err := history.MarkStreamMessageSkipped(stream, message.ID, ledger.SkipDigest, now); err != nil {
		t.Fatal(err)
	}
	pending, err := client.XPending(ctx, stream, group).Result()
	if err != nil || pending.Count != 0 {
		t.Fatalf("permanent message remains pending: %+v err=%v", pending, err)
	}
	water, exists := history.StreamHighWater(stream)
	if !exists || water.MessageID != message.ID || water.SkipDigest != ledger.SkipDigest || water.Skipped != 1 {
		t.Fatalf("skip checkpoint=%+v exists=%v", water, exists)
	}
}
