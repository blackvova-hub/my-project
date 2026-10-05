package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestReplayDryRunAndExecuteIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	source, dlq := "codex:test:replay:"+suffix, "codex:test:replay:"+suffix+":dlq"
	defer client.Del(ctx, source, dlq)
	values := map[string]any{"json": `{"symbol":"BTCUSDT"}`, "kind": "candle"}
	sourceID, err := client.XAdd(ctx, &redis.XAddArgs{Stream: source, Values: values}).Result()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"json": `{"symbol":"BTCUSDT"}`, "kind": "candle"})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	dlqID, err := client.XAdd(ctx, &redis.XAddArgs{Stream: dlq, Values: map[string]any{
		"schema_version": "1", "source_stream": source, "source_id": sourceID,
		"raw_values": string(raw), "raw_size": len(raw), "raw_sha256": hex.EncodeToString(digest[:]), "raw_truncated": "0",
	}}).Result()
	if err != nil {
		t.Fatal(err)
	}
	var dryOutput bytes.Buffer
	if err := run(ctx, &dryOutput, &dryOutput, []string{"--redis-addr", address, "--dlq-stream", dlq, "--id", dlqID}); err != nil {
		t.Fatal(err)
	}
	if length := client.XLen(ctx, source).Val(); length != 1 {
		t.Fatalf("dry-run changed source length=%d", length)
	}
	if !strings.Contains(dryOutput.String(), `"mode":"dry-run"`) {
		t.Fatalf("dry-run output=%s", dryOutput.String())
	}
	var executeOutput bytes.Buffer
	if err := run(ctx, &executeOutput, &executeOutput, []string{"--redis-addr", address, "--dlq-stream", dlq, "--id", dlqID, "--execute"}); err != nil {
		t.Fatal(err)
	}
	if length := client.XLen(ctx, source).Val(); length != 2 {
		t.Fatalf("execute source length=%d", length)
	}
	if length := client.XLen(ctx, dlq).Val(); length != 1 {
		t.Fatalf("execute changed DLQ audit length=%d", length)
	}
	latest, err := client.XRevRangeN(ctx, source, "+", "-", 1).Result()
	if err != nil || len(latest) != 1 {
		t.Fatalf("replayed source=%+v err=%v", latest, err)
	}
	if latest[0].Values[replayReferenceField] != dlq+"@"+dlqID || latest[0].Values[replaySourceIDField] != sourceID {
		t.Fatalf("replay reference=%+v", latest[0].Values)
	}
}

func TestReplayTruncatedAuditReadsExactOriginalSourceIDIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	source, dlq := "codex:test:replay-truncated:"+suffix, "codex:test:replay-truncated:"+suffix+":dlq"
	defer client.Del(ctx, source, dlq)
	values := map[string]any{"json": strings.Repeat("x", 512)}
	sourceID, err := client.XAdd(ctx, &redis.XAddArgs{Stream: source, Values: values}).Result()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"json": strings.Repeat("x", 512)})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	dlqID, err := client.XAdd(ctx, &redis.XAddArgs{Stream: dlq, Values: map[string]any{
		"schema_version": "1", "source_stream": source, "source_id": sourceID,
		"raw_values": string(raw[:32]), "raw_size": len(raw), "raw_sha256": hex.EncodeToString(digest[:]), "raw_truncated": "1",
	}}).Result()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(ctx, &output, &output, []string{"--redis-addr", address, "--dlq-stream", dlq, "--id", dlqID}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"payload_from":"source_id"`) {
		t.Fatalf("truncated replay output=%s", output.String())
	}
}
