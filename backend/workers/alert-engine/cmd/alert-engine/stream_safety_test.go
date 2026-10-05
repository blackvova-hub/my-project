package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestBuildStreamDLQRecordBoundsPayloadAndError(t *testing.T) {
	message := redis.XMessage{ID: "1700000000000-0", Values: map[string]any{
		"json": strings.Repeat("payload-", 64),
		"type": "candle",
	}}
	failure := errors.New(strings.Repeat("failure-", 32))
	cfg := streamDLQConfig{MaxLen: 10, RawMaxBytes: 80, ErrorMaxBytes: 24}

	record, err := buildStreamDLQRecord(message, streamFailurePermanent, streamFailureMalformedJSON, failure, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.RawValues) > cfg.RawMaxBytes || !record.RawTruncated {
		t.Fatalf("raw length=%d truncated=%v", len(record.RawValues), record.RawTruncated)
	}
	if record.RawSize <= int64(len(record.RawValues)) {
		t.Fatalf("raw size=%d stored=%d", record.RawSize, len(record.RawValues))
	}
	rawFull, err := json.Marshal(message.Values)
	if err != nil {
		t.Fatal(err)
	}
	rawDigest := sha256.Sum256(rawFull)
	if record.RawSHA256 != hex.EncodeToString(rawDigest[:]) {
		t.Fatalf("raw sha=%q", record.RawSHA256)
	}
	if len(record.Error) > cfg.ErrorMaxBytes || !record.ErrorTruncated {
		t.Fatalf("error length=%d truncated=%v", len(record.Error), record.ErrorTruncated)
	}
	if record.ErrorSize != int64(len(failure.Error())) {
		t.Fatalf("error size=%d", record.ErrorSize)
	}
}

func TestBuildStreamDLQRecordKeepsCompletePayloadBelowCap(t *testing.T) {
	message := redis.XMessage{ID: "1-0", Values: map[string]any{"json": `{"ok":true}`}}
	record, err := buildStreamDLQRecord(message, streamFailurePermanent, streamFailureMalformedJSON, errors.New("bad"), streamDLQConfig{MaxLen: 10, RawMaxBytes: 4096, ErrorMaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if record.RawTruncated || int64(len(record.RawValues)) != record.RawSize {
		t.Fatalf("complete raw was truncated: size=%d stored=%d", record.RawSize, len(record.RawValues))
	}
	rawFull, err := json.Marshal(message.Values)
	if err != nil {
		t.Fatal(err)
	}
	if string(record.RawValues) != string(rawFull) {
		t.Fatal("stored raw differs from canonical source values")
	}
}

func TestStreamSafetyConfigDefaultsAndBounds(t *testing.T) {
	t.Setenv("STREAM_MAX_DELIVERIES", "0")
	t.Setenv("STREAM_DLQ_MAXLEN", "0")
	t.Setenv("STREAM_DLQ_RAW_MAX_BYTES", strconv.Itoa(streamDLQHardRawMaxBytes+1))
	t.Setenv("STREAM_DLQ_ERROR_MAX_BYTES", strconv.Itoa(streamDLQHardErrorMaxBytes+1))
	t.Setenv("STREAM_READ_COUNT", "0")
	cfg := loadConfig()
	if cfg.StreamMaxDeliveries != defaultStreamMaxDeliveries {
		t.Fatalf("max deliveries=%d", cfg.StreamMaxDeliveries)
	}
	if cfg.StreamDLQMaxLen != defaultStreamDLQMaxLen {
		t.Fatalf("dlq maxlen=%d", cfg.StreamDLQMaxLen)
	}
	if cfg.StreamDLQRawMaxBytes != streamDLQHardRawMaxBytes {
		t.Fatalf("raw max=%d", cfg.StreamDLQRawMaxBytes)
	}
	if cfg.StreamDLQErrorMaxBytes != streamDLQHardErrorMaxBytes {
		t.Fatalf("error max=%d", cfg.StreamDLQErrorMaxBytes)
	}
	if cfg.StreamReadCount != defaultStreamReadCount {
		t.Fatalf("compact read count=%d", cfg.StreamReadCount)
	}
}

func TestStreamSafetyAtomicDLQAndHeartbeatIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	testSuffix := strconv.FormatInt(time.Now().UnixNano(), 36)

	t.Run("dlq write failure leaves source pending", func(t *testing.T) {
		stream, group, consumer := "codex:test:safety:"+testSuffix+":write-failure", "codex-test-safety-"+testSuffix, "consumer-a"
		defer client.Del(ctx, stream, streamDLQName(stream))
		message := addAndReadPending(t, ctx, client, stream, group, consumer, map[string]any{"json": `{"x":1}`})
		if err := client.Set(ctx, streamDLQName(stream), "wrong-type", 0).Err(); err != nil {
			t.Fatal(err)
		}
		_, err := movePendingStreamMessageToDLQWithConfig(ctx, client, stream, group, message, streamFailurePermanent, streamFailureMalformedJSON, errors.New("bad"), 1, streamDLQConfig{MaxLen: 10, RawMaxBytes: 128, ErrorMaxBytes: 64})
		if err == nil {
			t.Fatal("DLQ XADD against a wrong-type key unexpectedly succeeded")
		}
		pending, pendingErr := client.XPending(ctx, stream, group).Result()
		if pendingErr != nil || pending.Count != 1 {
			t.Fatalf("source was acknowledged after DLQ failure: pending=%+v err=%v", pending, pendingErr)
		}
	})

	t.Run("dlq is exactly bounded and records truncation metadata", func(t *testing.T) {
		stream, group, consumer := "codex:test:safety:"+testSuffix+":bounded", "codex-test-safety-"+testSuffix, "consumer-a"
		defer client.Del(ctx, stream, streamDLQName(stream))
		cfg := streamDLQConfig{MaxLen: 2, RawMaxBytes: 64, ErrorMaxBytes: 16}
		for index := 0; index < 3; index++ {
			message := addAndReadPending(t, ctx, client, stream, group, consumer, map[string]any{"json": strings.Repeat(strconv.Itoa(index), 256)})
			if _, err := movePendingStreamMessageToDLQWithConfig(ctx, client, stream, group, message, streamFailurePermanent, streamFailureMalformedJSON, errors.New(strings.Repeat("e", 100)), 1, cfg); err != nil {
				t.Fatal(err)
			}
		}
		entries, err := client.XRange(ctx, streamDLQName(stream), "-", "+").Result()
		if err != nil || len(entries) != 2 {
			t.Fatalf("bounded DLQ entries=%d err=%v", len(entries), err)
		}
		last := entries[len(entries)-1].Values
		if last["raw_truncated"] != "1" || last["error_truncated"] != "1" || last["failure_reason"] != string(streamFailureMalformedJSON) {
			t.Fatalf("DLQ metadata=%+v", last)
		}
		if len(last["raw_values"].(string)) > cfg.RawMaxBytes || len(last["error"].(string)) > cfg.ErrorMaxBytes {
			t.Fatalf("bounded fields exceeded configured caps: %+v", last)
		}
	})

	t.Run("heartbeat prevents active reclaim without increasing deliveries", func(t *testing.T) {
		stream, group, consumer := "codex:test:safety:"+testSuffix+":heartbeat", "codex-test-safety-"+testSuffix, "consumer-a"
		defer client.Del(ctx, stream, streamDLQName(stream))
		message := addAndReadPending(t, ctx, client, stream, group, consumer, map[string]any{"json": `{"x":1}`})
		guard, err := startStreamBatchGuard(ctx, client, stream, group, consumer, []string{message.ID}, 180*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		defer guard.Close()
		if _, err := startStreamBatchGuard(ctx, client, stream, group, consumer, []string{message.ID}, 180*time.Millisecond); !errors.Is(err, errStreamMessageBusy) {
			t.Fatalf("second owner error=%v", err)
		}
		time.Sleep(500 * time.Millisecond)
		claimed, _, err := client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: stream, Group: group, Consumer: "consumer-b", MinIdle: 180 * time.Millisecond, Start: "0-0", Count: 1}).Result()
		if err != nil && err != redis.Nil {
			t.Fatal(err)
		}
		if len(claimed) != 0 {
			t.Fatalf("active message was reclaimed: %+v", claimed)
		}
		deliveries, err := streamDeliveryCount(ctx, client, stream, group, message.ID)
		if err != nil || deliveries != 1 {
			t.Fatalf("heartbeat changed delivery count=%d err=%v", deliveries, err)
		}
	})
}

func addAndReadPending(t *testing.T, ctx context.Context, client *redis.Client, stream, group, consumer string, values map[string]any) redis.XMessage {
	t.Helper()
	if err := client.XGroupCreateMkStream(ctx, stream, group, "0-0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		t.Fatal(err)
	}
	id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: values}).Result()
	if err != nil {
		t.Fatal(err)
	}
	read, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: group, Consumer: consumer, Streams: []string{stream, ">"}, Count: 1}).Result()
	if err != nil || len(read) != 1 || len(read[0].Messages) != 1 {
		t.Fatalf("read pending: streams=%+v err=%v", read, err)
	}
	if read[0].Messages[0].ID != id {
		t.Fatalf("read id=%s want=%s", read[0].Messages[0].ID, id)
	}
	return read[0].Messages[0]
}
