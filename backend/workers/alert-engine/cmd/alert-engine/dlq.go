package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	streamDLQSuffix = ":dlq"

	defaultStreamMaxDeliveries          = 5
	defaultStreamDLQMaxLen        int64 = 10_000
	defaultStreamDLQRawMaxBytes         = 64 << 10
	defaultStreamDLQErrorMaxBytes       = 2 << 10
	streamDLQHardRawMaxBytes            = 1 << 20
	streamDLQHardErrorMaxBytes          = 16 << 10
	defaultStreamReadCount              = 10
)

var movePendingToDLQScript = redis.NewScript(`
local pending = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[2], ARGV[2], 1)
if #pending == 0 then
  return redis.error_reply('SOURCE_NOT_PENDING')
end
if ARGV[17] ~= '' and pending[1][2] ~= ARGV[17] then
  return redis.error_reply('SOURCE_OWNER_CHANGED')
end
local dlq_id = redis.call('XADD', KEYS[2], 'MAXLEN', '=', ARGV[13], '*',
  'schema_version', ARGV[15],
  'source_stream', KEYS[1],
  'source_group', ARGV[1],
  'source_id', ARGV[2],
  'source_consumer', pending[1][2],
  'failure_class', ARGV[3],
  'failure_reason', ARGV[4],
  'error', ARGV[5],
  'error_size', ARGV[6],
  'error_sha256', ARGV[7],
  'error_truncated', ARGV[8],
  'delivery_count', pending[1][4],
  'delivery_count_observed', ARGV[16],
  'raw_values', ARGV[9],
  'raw_size', ARGV[10],
  'raw_sha256', ARGV[11],
  'raw_truncated', ARGV[12],
  'skip_digest', ARGV[20],
  'archived_at_unix_ms', ARGV[14])
redis.call('SET', KEYS[3], ARGV[18], 'PX', ARGV[19])
local acknowledged = redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
if acknowledged ~= 1 then
  return redis.error_reply('SOURCE_ACK_FAILED')
end
return dlq_id
`)

type streamFailureClass string

type streamFailureReason string

const (
	streamFailurePermanent streamFailureClass = "permanent"
	streamFailureTransient streamFailureClass = "transient_exhausted"
)

const (
	streamFailureMissingJSON          streamFailureReason = "missing_json"
	streamFailureMalformedJSON        streamFailureReason = "malformed_json"
	streamFailureVenueMismatch        streamFailureReason = "venue_mismatch"
	streamFailureConflictingDuplicate streamFailureReason = "conflicting_duplicate"
	streamFailureCoverageConflict     streamFailureReason = "coverage_conflict"
	streamFailureUnifiedConflict      streamFailureReason = "unified_conflict"
	streamFailureHandleExhausted      streamFailureReason = "handle_exhausted"
	streamFailureCapacityExhausted    streamFailureReason = "unified_capacity_exhausted"
)

type streamDLQConfig struct {
	MaxLen           int64
	RawMaxBytes      int
	ErrorMaxBytes    int
	ExpectedConsumer string
}

func (c streamDLQConfig) normalized() streamDLQConfig {
	if c.MaxLen <= 0 {
		c.MaxLen = defaultStreamDLQMaxLen
	}
	if c.RawMaxBytes <= 0 {
		c.RawMaxBytes = defaultStreamDLQRawMaxBytes
	} else if c.RawMaxBytes > streamDLQHardRawMaxBytes {
		c.RawMaxBytes = streamDLQHardRawMaxBytes
	}
	if c.ErrorMaxBytes <= 0 {
		c.ErrorMaxBytes = defaultStreamDLQErrorMaxBytes
	} else if c.ErrorMaxBytes > streamDLQHardErrorMaxBytes {
		c.ErrorMaxBytes = streamDLQHardErrorMaxBytes
	}
	return c
}

type streamDLQRecord struct {
	RawValues      []byte
	RawSize        int64
	RawSHA256      string
	RawTruncated   bool
	Error          string
	ErrorSize      int64
	ErrorSHA256    string
	ErrorTruncated bool
}

func streamDLQName(sourceStream string) string {
	return strings.TrimSpace(sourceStream) + streamDLQSuffix
}

func streamDeliveryCount(ctx context.Context, client *redis.Client, stream, group, messageID string) (int64, error) {
	if client == nil || strings.TrimSpace(stream) == "" || strings.TrimSpace(group) == "" || strings.TrimSpace(messageID) == "" {
		return 0, errors.New("invalid pending delivery identity")
	}
	entries, err := client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: stream, Group: group, Start: messageID, End: messageID, Count: 1}).Result()
	if err != nil {
		return 0, err
	}
	if len(entries) != 1 || entries[0].ID != messageID {
		return 0, errors.New("source message is not pending")
	}
	return entries[0].RetryCount, nil
}

func movePendingStreamMessageToDLQ(ctx context.Context, client *redis.Client, sourceStream, group string, message redis.XMessage, class streamFailureClass, failure error, deliveryCount int64) (string, error) {
	return movePendingStreamMessageToDLQWithConfig(ctx, client, sourceStream, group, message, class, streamFailureUnifiedConflict, failure, deliveryCount, streamDLQConfig{})
}

func movePendingStreamMessageToDLQWithConfig(ctx context.Context, client *redis.Client, sourceStream, group string, message redis.XMessage, class streamFailureClass, reason streamFailureReason, failure error, deliveryCount int64, cfg streamDLQConfig) (string, error) {
	if client == nil || strings.TrimSpace(sourceStream) == "" || strings.TrimSpace(group) == "" || message.ID == "" || failure == nil || strings.TrimSpace(string(reason)) == "" {
		return "", errors.New("invalid DLQ request")
	}
	if class != streamFailurePermanent && class != streamFailureTransient {
		return "", errors.New("invalid DLQ failure class")
	}
	cfg = cfg.normalized()
	record, err := buildStreamDLQRecord(message, class, reason, failure, cfg)
	if err != nil {
		return "", err
	}
	ledger, err := buildStreamSkipLedgerRecord(sourceStream, group, message.ID, record.RawSHA256, reason)
	if err != nil {
		return "", fmt.Errorf("build stream skip ledger: %w", err)
	}
	result, err := movePendingToDLQScript.Run(ctx, client,
		[]string{sourceStream, streamDLQName(sourceStream), ledger.Key},
		group,
		message.ID,
		string(class),
		string(reason),
		record.Error,
		strconv.FormatInt(record.ErrorSize, 10),
		record.ErrorSHA256,
		boolFlag(record.ErrorTruncated),
		string(record.RawValues),
		strconv.FormatInt(record.RawSize, 10),
		record.RawSHA256,
		boolFlag(record.RawTruncated),
		strconv.FormatInt(cfg.MaxLen, 10),
		strconv.FormatInt(time.Now().UTC().UnixMilli(), 10),
		"1",
		strconv.FormatInt(deliveryCount, 10),
		cfg.ExpectedConsumer,
		ledger.Value,
		strconv.FormatInt(ledger.TTL.Milliseconds(), 10),
		ledger.SkipDigest,
	).Text()
	if err != nil {
		return "", fmt.Errorf("move stream message to DLQ: %w", err)
	}
	return result, nil
}

func buildStreamDLQRecord(message redis.XMessage, _ streamFailureClass, _ streamFailureReason, failure error, cfg streamDLQConfig) (streamDLQRecord, error) {
	if failure == nil {
		return streamDLQRecord{}, errors.New("DLQ failure is nil")
	}
	cfg = cfg.normalized()
	raw, err := json.Marshal(message.Values)
	if err != nil {
		return streamDLQRecord{}, fmt.Errorf("marshal source stream values: %w", err)
	}
	rawSum := sha256.Sum256(raw)
	errorBytes := []byte(failure.Error())
	errorSum := sha256.Sum256(errorBytes)
	storedRaw, rawTruncated := boundedCopy(raw, cfg.RawMaxBytes)
	storedError, errorTruncated := boundedCopy(errorBytes, cfg.ErrorMaxBytes)
	return streamDLQRecord{
		RawValues:      storedRaw,
		RawSize:        int64(len(raw)),
		RawSHA256:      hex.EncodeToString(rawSum[:]),
		RawTruncated:   rawTruncated,
		Error:          string(storedError),
		ErrorSize:      int64(len(errorBytes)),
		ErrorSHA256:    hex.EncodeToString(errorSum[:]),
		ErrorTruncated: errorTruncated,
	}, nil
}

func boundedCopy(value []byte, limit int) ([]byte, bool) {
	if limit < 0 {
		limit = 0
	}
	truncated := len(value) > limit
	if truncated {
		value = value[:limit]
	}
	result := make([]byte, len(value))
	copy(result, value)
	return result, truncated
}

func boolFlag(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
