package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/redis/go-redis/v9"
)

var errStreamMessageNotOwner = errors.New("stream message pending ownership changed")

var ackOwnedStreamMessageScript = redis.NewScript(`
local pending = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[2], ARGV[2], 1)
if #pending == 0 then
  return 0
end
if pending[1][2] ~= ARGV[3] then
  return -1
end
return redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
`)

type streamSafetyCounters struct {
	acked            atomic.Uint64
	permanent        atomic.Uint64
	retries          atomic.Uint64
	exhausted        atomic.Uint64
	dlqStored        atomic.Uint64
	dlqFailed        atomic.Uint64
	ownershipSkipped atomic.Uint64
	guardFailed      atomic.Uint64
}

type streamSafetySnapshot struct {
	Acked            uint64
	Permanent        uint64
	Retries          uint64
	Exhausted        uint64
	DLQStored        uint64
	DLQFailed        uint64
	OwnershipSkipped uint64
	GuardFailed      uint64
}

func (c *streamSafetyCounters) snapshot() streamSafetySnapshot {
	if c == nil {
		return streamSafetySnapshot{}
	}
	return streamSafetySnapshot{
		Acked: c.acked.Load(), Permanent: c.permanent.Load(), Retries: c.retries.Load(),
		Exhausted: c.exhausted.Load(), DLQStored: c.dlqStored.Load(), DLQFailed: c.dlqFailed.Load(),
		OwnershipSkipped: c.ownershipSkipped.Load(), GuardFailed: c.guardFailed.Load(),
	}
}

func streamMessageIDs(messages []redis.XMessage) []string {
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		if message.ID != "" {
			ids = append(ids, message.ID)
		}
	}
	return ids
}

func (e *Engine) ackStreamMessage(ctx context.Context, stream, group, consumer, messageID string, guard *streamBatchGuard) bool {
	if guard != nil && !guard.Owns(messageID) {
		e.streamSafety.ownershipSkipped.Add(1)
		return false
	}
	acked, err := ackOwnedStreamMessage(ctx, e.rdb, stream, group, consumer, messageID)
	if err != nil {
		e.logStreamSafety("ack_failed", stream, messageID, "", 0, err)
		return false
	}
	if acked {
		e.streamSafety.acked.Add(1)
		if guard != nil {
			guard.Complete(messageID)
		}
	}
	return acked
}

func ackOwnedStreamMessage(ctx context.Context, client *redis.Client, stream, group, consumer, messageID string) (bool, error) {
	if client == nil || stream == "" || group == "" || consumer == "" || messageID == "" {
		return false, errors.New("invalid owned stream ACK identity")
	}
	result, err := ackOwnedStreamMessageScript.Run(ctx, client, []string{stream}, group, messageID, consumer).Int64()
	if err != nil {
		return false, fmt.Errorf("owned stream ACK: %w", err)
	}
	if result < 0 {
		return false, errStreamMessageNotOwner
	}
	return result == 1, nil
}

func (e *Engine) archiveStreamMessage(ctx context.Context, stream, group, consumer string, message redis.XMessage, class streamFailureClass, reason streamFailureReason, failure error, deliveries int64, guard *streamBatchGuard) bool {
	if guard != nil && !guard.Owns(message.ID) {
		e.streamSafety.ownershipSkipped.Add(1)
		return false
	}
	if class == streamFailurePermanent {
		e.streamSafety.permanent.Add(1)
	} else {
		e.streamSafety.exhausted.Add(1)
	}
	_, err := movePendingStreamMessageToDLQWithConfig(ctx, e.rdb, stream, group, message, class, reason, failure, deliveries, streamDLQConfig{
		MaxLen: e.cfg.StreamDLQMaxLen, RawMaxBytes: e.cfg.StreamDLQRawMaxBytes,
		ErrorMaxBytes: e.cfg.StreamDLQErrorMaxBytes, ExpectedConsumer: consumer,
	})
	if err != nil {
		e.streamSafety.dlqFailed.Add(1)
		e.logStreamSafety("dlq_failed", stream, message.ID, string(reason), deliveries, err)
		return false
	}
	e.streamSafety.dlqStored.Add(1)
	if guard != nil {
		guard.Complete(message.ID)
	}
	e.logStreamSafety("dlq_stored", stream, message.ID, string(reason), deliveries, nil)
	return true
}

func (e *Engine) logStreamSafety(action, stream, messageID, reason string, deliveries int64, failure error) {
	snapshot := e.streamSafety.snapshot()
	errorText := ""
	if failure != nil {
		bounded, truncated := boundedCopy([]byte(failure.Error()), defaultStreamDLQErrorMaxBytes)
		errorText = string(bounded)
		if truncated {
			errorText += "...[truncated]"
		}
	}
	log.Printf("stream_safety action=%q stream=%q message_id=%q reason=%q deliveries=%d error=%q acked=%d permanent=%d retries=%d exhausted=%d dlq_stored=%d dlq_failed=%d ownership_skipped=%d guard_failed=%d",
		action, stream, messageID, reason, deliveries, errorText,
		snapshot.Acked, snapshot.Permanent, snapshot.Retries, snapshot.Exhausted,
		snapshot.DLQStored, snapshot.DLQFailed, snapshot.OwnershipSkipped, snapshot.GuardFailed)
}
