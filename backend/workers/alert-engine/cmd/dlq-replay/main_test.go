package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestParseAndResolveCompleteDLQAuditRecord(t *testing.T) {
	values := map[string]any{"json": `{"symbol":"BTCUSDT"}`}
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	message := redis.XMessage{ID: "1800000000000-0", Values: map[string]any{
		"schema_version": "1", "source_stream": "candles:v3", "source_id": "1700000000000-0",
		"raw_values": string(raw), "raw_size": len(raw), "raw_sha256": hex.EncodeToString(digest[:]), "raw_truncated": "0",
	}}
	record, err := parseDLQAuditRecord("candles:v3:dlq", message)
	if err != nil {
		t.Fatal(err)
	}
	resolved, source, err := resolveReplayValues(context.Background(), nil, record)
	if err != nil {
		t.Fatal(err)
	}
	if source != "dlq_audit" || resolved["json"] != values["json"] {
		t.Fatalf("resolved=%+v source=%s", resolved, source)
	}
}

func TestParseDLQAuditRecordRejectsTampering(t *testing.T) {
	message := redis.XMessage{ID: "2-0", Values: map[string]any{
		"schema_version": "1", "source_stream": "source", "source_id": "1-0",
		"raw_values": "{}", "raw_size": "2", "raw_sha256": "not-a-sha", "raw_truncated": "0",
	}}
	if _, err := parseDLQAuditRecord("source:dlq", message); err == nil {
		t.Fatal("invalid SHA metadata was accepted")
	}
	validDigest := sha256.Sum256([]byte("{}"))
	message.Values["raw_sha256"] = hex.EncodeToString(validDigest[:])
	if _, err := parseDLQAuditRecord("different:dlq", message); err == nil {
		t.Fatal("mismatched DLQ/source identity was accepted")
	}
}

func TestParseOptionsIsDryRunByDefault(t *testing.T) {
	options, err := parseOptions([]string{"--dlq-stream", "source:dlq", "--id", "1-0"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Execute {
		t.Fatal("replay must be dry-run unless --execute is explicit")
	}
	options, err = parseOptions([]string{"--dlq-stream", "source:dlq", "--id", "1-0", "--execute"})
	if err != nil || !options.Execute {
		t.Fatalf("explicit execute not recognized: execute=%v err=%v", options.Execute, err)
	}
}
