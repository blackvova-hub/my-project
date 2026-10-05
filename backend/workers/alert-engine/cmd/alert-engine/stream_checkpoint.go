package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const compactStreamMaxPending = 100_000

var (
	errCompactStreamNotBootstrapped = errors.New("compact stream is not bootstrapped")
	errCompactStreamMessageGap      = errors.New("compact stream message was not registered in delivery order")
	errCompactStreamRewound         = errors.New("compact stream upper bound is behind snapshot offset")
)

type compactPendingApplication struct {
	water   compactStreamHighWater
	applied bool
	skipped bool
}

type compactStreamProgress struct {
	order   []string
	pending map[string]compactPendingApplication
}

// RegisterStreamMessages must be called with the complete, ordered message-ID
// list returned for one stream by XREADGROUP/XAUTOCLAIM before any message in
// that list is processed. The explicit queue is what prevents a later reclaim
// from moving the durable checkpoint across an earlier processing gap.
func (h *sparseEngineState) RegisterStreamMessages(stream string, messageIDs []string) error {
	if h == nil || len(messageIDs) == 0 {
		return nil
	}
	stream = strings.TrimSpace(stream)
	if stream == "" || len(stream) > 512 {
		return fmt.Errorf("%w: invalid stream", ErrUnifiedSnapshotInvalidState)
	}
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	checkpoint, ok := h.streamHighWater[stream]
	if !ok || !checkpoint.Bootstrapped {
		return fmt.Errorf("%w: %s", errCompactStreamNotBootstrapped, stream)
	}
	progress := h.streamProgress[stream]
	if progress == nil {
		progress = &compactStreamProgress{pending: make(map[string]compactPendingApplication)}
		h.streamProgress[stream] = progress
	}
	previous := ""
	for _, messageID := range messageIDs {
		if _, _, valid := parseRedisStreamID(messageID); !valid {
			return fmt.Errorf("%w: invalid redis stream id %q", ErrUnifiedSnapshotInvalidState, messageID)
		}
		if previous != "" {
			comparison, _ := compareRedisStreamIDs(messageID, previous)
			if comparison <= 0 {
				return fmt.Errorf("%w: delivery IDs are not strictly increasing", errCompactStreamMessageGap)
			}
		}
		previous = messageID
		comparison, _ := compareRedisStreamIDs(messageID, checkpoint.MessageID)
		if comparison <= 0 {
			continue
		}
		if _, exists := progress.pending[messageID]; exists {
			continue
		}
		if len(progress.pending) >= compactStreamMaxPending {
			return fmt.Errorf("%w: pending stream checkpoint budget exceeded", ErrUnifiedSnapshotTooLarge)
		}
		position := sort.Search(len(progress.order), func(index int) bool {
			order, _ := compareRedisStreamIDs(progress.order[index], messageID)
			return order >= 0
		})
		progress.order = append(progress.order, "")
		copy(progress.order[position+1:], progress.order[position:])
		progress.order[position] = messageID
		progress.pending[messageID] = compactPendingApplication{}
	}
	return nil
}

func (h *sparseEngineState) requireRegisteredStreamMessage(stream, messageID string, batch compactDecodedBatch) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	checkpoint, ok := h.streamHighWater[stream]
	if !ok || !checkpoint.Bootstrapped {
		return fmt.Errorf("%w: %s", errCompactStreamNotBootstrapped, stream)
	}
	comparison, valid := compareRedisStreamIDs(messageID, checkpoint.MessageID)
	if !valid {
		return fmt.Errorf("%w: invalid redis stream id %q", ErrUnifiedSnapshotInvalidState, messageID)
	}
	if comparison < 0 {
		return nil
	}
	if comparison == 0 {
		if checkpoint.SkipDigest != "" || checkpoint.BatchID != batch.BatchID || checkpoint.BatchMinute != batch.Minute {
			return fmt.Errorf("stream checkpoint id %s changed batch identity", messageID)
		}
		return nil
	}
	progress := h.streamProgress[stream]
	if progress == nil {
		return fmt.Errorf("%w: stream=%s id=%s", errCompactStreamMessageGap, stream, messageID)
	}
	if _, ok := progress.pending[messageID]; !ok {
		return fmt.Errorf("%w: stream=%s id=%s", errCompactStreamMessageGap, stream, messageID)
	}
	return nil
}

func (h *sparseEngineState) markStreamMessageApplied(stream, messageID string, batch compactDecodedBatch, now time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	checkpoint := h.streamHighWater[stream]
	comparison, valid := compareRedisStreamIDs(messageID, checkpoint.MessageID)
	if !valid {
		return fmt.Errorf("%w: invalid redis stream checkpoint", ErrUnifiedSnapshotInvalidState)
	}
	if comparison <= 0 {
		return nil
	}
	progress := h.streamProgress[stream]
	if progress == nil {
		return fmt.Errorf("%w: stream=%s id=%s", errCompactStreamMessageGap, stream, messageID)
	}
	pending, ok := progress.pending[messageID]
	if !ok {
		return fmt.Errorf("%w: stream=%s id=%s", errCompactStreamMessageGap, stream, messageID)
	}
	pending.applied = true
	pending.water = compactStreamHighWater{
		MessageID: messageID, BatchMinute: batch.Minute, BatchID: batch.BatchID,
		ObservedAt: now.UTC(), ReadyAt: checkpoint.ReadyAt, Bootstrapped: true, Skipped: checkpoint.Skipped,
	}
	pending.skipped = false
	progress.pending[messageID] = pending
	h.advanceStreamCheckpointLocked(stream, progress)
	return nil
}

// MarkStreamMessageSkipped advances the contiguous checkpoint only after the
// caller has atomically persisted the exact DLQ skip ledger and acknowledged
// the source entry. A later registered entry can never jump over an unmarked
// poison message.
func (h *sparseEngineState) MarkStreamMessageSkipped(stream, messageID, skipDigest string, now time.Time) error {
	if h == nil {
		return nil
	}
	stream = strings.TrimSpace(stream)
	messageID = strings.TrimSpace(messageID)
	skipDigest = strings.ToLower(strings.TrimSpace(skipDigest))
	if stream == "" || !validSHA256Hex(skipDigest) {
		return fmt.Errorf("%w: invalid skipped stream message", ErrUnifiedSnapshotInvalidState)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	checkpoint, ok := h.streamHighWater[stream]
	if !ok || !checkpoint.Bootstrapped {
		return fmt.Errorf("%w: %s", errCompactStreamNotBootstrapped, stream)
	}
	comparison, valid := compareRedisStreamIDs(messageID, checkpoint.MessageID)
	if !valid {
		return fmt.Errorf("%w: invalid redis stream id %q", ErrUnifiedSnapshotInvalidState, messageID)
	}
	if comparison < 0 {
		return nil
	}
	if comparison == 0 {
		if checkpoint.SkipDigest != skipDigest {
			return fmt.Errorf("stream checkpoint id %s changed skip identity", messageID)
		}
		return nil
	}
	progress := h.streamProgress[stream]
	if progress == nil {
		return fmt.Errorf("%w: stream=%s id=%s", errCompactStreamMessageGap, stream, messageID)
	}
	pending, ok := progress.pending[messageID]
	if !ok {
		return fmt.Errorf("%w: stream=%s id=%s", errCompactStreamMessageGap, stream, messageID)
	}
	pending.applied = true
	pending.skipped = true
	pending.water = compactStreamHighWater{MessageID: messageID, SkipDigest: skipDigest, ObservedAt: now, ReadyAt: checkpoint.ReadyAt, Bootstrapped: true}
	progress.pending[messageID] = pending
	h.advanceStreamCheckpointLocked(stream, progress)
	return nil
}

func (h *sparseEngineState) advanceStreamCheckpointLocked(stream string, progress *compactStreamProgress) {
	for len(progress.order) > 0 {
		nextID := progress.order[0]
		next := progress.pending[nextID]
		if !next.applied {
			break
		}
		current := h.streamHighWater[stream]
		next.water.Skipped = current.Skipped
		if next.skipped {
			next.water.Skipped++
		}
		h.streamHighWater[stream] = next.water
		delete(progress.pending, nextID)
		progress.order = progress.order[1:]
	}
	if len(progress.order) == 0 {
		delete(h.streamProgress, stream)
	}
}

func (h *sparseEngineState) SetStreamBootstrapCheckpoint(stream string, checkpoint compactStreamHighWater, now time.Time) error {
	if h == nil {
		return fmt.Errorf("%w: nil history", ErrUnifiedSnapshotInvalidState)
	}
	stream = strings.TrimSpace(stream)
	if err := validateCompactStreamHighWater(stream, checkpoint); err != nil {
		return err
	}
	if err := validateSnapshotNotFuture(checkpoint.ObservedAt, now); err != nil {
		return fmt.Errorf("%w: checkpoint observation time: %v", ErrUnifiedSnapshotInvalidState, err)
	}
	if err := validateSnapshotReadyAt(checkpoint.ReadyAt, now); err != nil {
		return fmt.Errorf("%w: checkpoint readiness time: %v", ErrUnifiedSnapshotInvalidState, err)
	}
	h.snapshotMu.Lock()
	defer h.snapshotMu.Unlock()
	h.mu.Lock()
	h.streamHighWater[stream] = checkpoint
	delete(h.streamProgress, stream)
	h.mu.Unlock()
	return nil
}

func (h *sparseEngineState) CompactStreamsReady(streams []string, now time.Time) bool {
	if h == nil || len(streams) == 0 {
		return false
	}
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := make(map[string]struct{}, len(streams))
	for _, rawStream := range streams {
		stream := strings.TrimSpace(rawStream)
		if stream == "" {
			return false
		}
		if _, duplicate := seen[stream]; duplicate {
			continue
		}
		seen[stream] = struct{}{}
		checkpoint, ok := h.streamHighWater[stream]
		if !ok || !checkpoint.Bootstrapped || now.UTC().Before(checkpoint.ReadyAt) {
			return false
		}
	}
	return len(seen) > 0
}
