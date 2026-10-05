package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
)

const (
	defaultOutboxMaxAttempts     = 8
	defaultOutboxRetryBase       = time.Second
	defaultOutboxRetryMax        = 5 * time.Minute
	defaultOutboxPublishDedupMax = 200_000
	defaultOutboxPublishDedupAge = 30 * 24 * time.Hour
	maxOutboxStoredErrorBytes    = 2048
)

type outboxErrorClass string

const (
	outboxErrorTransient outboxErrorClass = "transient"
	outboxErrorPermanent outboxErrorClass = "permanent"
	outboxErrorExhausted outboxErrorClass = "transient_exhausted"
)

type permanentOutboxError struct{ err error }

func (e permanentOutboxError) Error() string { return e.err.Error() }
func (e permanentOutboxError) Unwrap() error { return e.err }

func markOutboxPermanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentOutboxError{err: err}
}

type outboxRetryDecision struct {
	Class       outboxErrorClass
	DeadLetter  bool
	NextAttempt time.Time
}

func decideOutboxRetry(now time.Time, deliveryID int64, attempt, maxAttempts int, failure error) outboxRetryDecision {
	if maxAttempts <= 0 {
		maxAttempts = defaultOutboxMaxAttempts
	}
	if attempt < 1 {
		attempt = 1
	}
	var permanent permanentOutboxError
	if errors.As(failure, &permanent) {
		return outboxRetryDecision{Class: outboxErrorPermanent, DeadLetter: true, NextAttempt: now.UTC()}
	}
	if attempt >= maxAttempts {
		return outboxRetryDecision{Class: outboxErrorExhausted, DeadLetter: true, NextAttempt: now.UTC()}
	}
	return outboxRetryDecision{Class: outboxErrorTransient, NextAttempt: now.UTC().Add(outboxRetryDelay(deliveryID, attempt, defaultOutboxRetryBase, defaultOutboxRetryMax))}
}

func outboxRetryDelay(deliveryID int64, attempt int, base, maximum time.Duration) time.Duration {
	if base <= 0 {
		base = defaultOutboxRetryBase
	}
	if maximum < base {
		maximum = defaultOutboxRetryMax
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for step := 1; step < attempt && delay < maximum; step++ {
		if delay > maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	// Stable ±20% jitter avoids synchronized retries while keeping unit tests
	// and incident reconstruction deterministic for a delivery/attempt pair.
	hash := fnv.New32a()
	_, _ = fmt.Fprintf(hash, "%d:%d", deliveryID, attempt)
	percent := int(hash.Sum32()%41) - 20
	delay += time.Duration(int64(delay) * int64(percent) / 100)
	if delay > maximum {
		delay = maximum
	}
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	return delay
}

func boundedOutboxError(err error) string {
	if err == nil {
		return ""
	}
	value := strings.TrimSpace(err.Error())
	if len(value) <= maxOutboxStoredErrorBytes {
		return value
	}
	value = value[:maxOutboxStoredErrorBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

var publishOutboxOnceScript = redis.NewScript(`
local existing = redis.call('HGET', KEYS[2], ARGV[1])
if existing then
  local separator = string.find(existing, '|', 1, true)
  if not separator then
    return redis.error_reply('outbox dedup index is corrupt')
  end
  local stream_id = string.sub(existing, 1, separator - 1)
  local payload_digest = string.sub(existing, separator + 1)
  if payload_digest ~= ARGV[7] then
    return redis.error_reply('outbox logical delivery payload conflict')
  end
  local retained = redis.call('XRANGE', KEYS[1], stream_id, stream_id, 'COUNT', 1)
  if #retained > 0 then
    return stream_id
  end
end
local max_len = tonumber(ARGV[3])
local stream_id
if max_len and max_len > 0 then
  stream_id = redis.call('XADD', KEYS[1], 'MAXLEN', '~', max_len, '*', 'json', ARGV[2])
else
  stream_id = redis.call('XADD', KEYS[1], '*', 'json', ARGV[2])
end
redis.call('HSET', KEYS[2], ARGV[1], stream_id .. '|' .. ARGV[7])
redis.call('ZADD', KEYS[3], ARGV[4], ARGV[1])

local cutoff = tonumber(ARGV[4]) - tonumber(ARGV[6])
local expired = redis.call('ZRANGEBYSCORE', KEYS[3], '-inf', cutoff, 'LIMIT', 0, 1000)
for _, member in ipairs(expired) do
  redis.call('HDEL', KEYS[2], member)
  redis.call('ZREM', KEYS[3], member)
end

local max_entries = tonumber(ARGV[5])
local excess = redis.call('ZCARD', KEYS[3]) - max_entries
if excess > 0 then
  local oldest = redis.call('ZRANGE', KEYS[3], 0, excess - 1)
  for _, member in ipairs(oldest) do
    redis.call('HDEL', KEYS[2], member)
    redis.call('ZREM', KEYS[3], member)
  end
end
return stream_id
`)

// publishOutboxStreamOnce closes the XADD -> PostgreSQL-marker crash window.
// A retry with the same logical delivery key receives the original stream ID
// instead of appending a duplicate. The bounded Redis index is an optimization
// safety net; payload remains durable in PostgreSQL until marked complete.
func publishOutboxStreamOnce(ctx context.Context, client *redis.Client, stream, logicalDeliveryKey, payload string, maxLen, dedupMax int64, dedupAge time.Duration, now time.Time) (string, error) {
	stream = strings.TrimSpace(stream)
	logicalDeliveryKey = strings.TrimSpace(logicalDeliveryKey)
	if client == nil || stream == "" || logicalDeliveryKey == "" || payload == "" {
		return "", errors.New("invalid idempotent outbox publish request")
	}
	if dedupMax <= 0 {
		dedupMax = defaultOutboxPublishDedupMax
	}
	if dedupAge <= 0 {
		dedupAge = defaultOutboxPublishDedupAge
	}
	indexKey := "outbox:published:" + stream
	ageKey := indexKey + ":age"
	payloadDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	result, err := publishOutboxOnceScript.Run(ctx, client, []string{stream, indexKey, ageKey},
		logicalDeliveryKey, payload, maxLen, now.UTC().UnixMilli(), dedupMax, dedupAge.Milliseconds(), payloadDigest).Text()
	if err != nil {
		return "", fmt.Errorf("idempotent outbox publish: %w", err)
	}
	return result, nil
}
