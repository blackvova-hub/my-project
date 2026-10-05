package main

import (
	"testing"
	"time"
)

func TestDuplicateCompactPayloadStillAdvancesContiguousCheckpoint(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	stream := snapshotTestStream
	history := newSnapshotTestHistory()
	if err := history.SetStreamBootstrapCheckpoint(stream, compactStreamHighWater{MessageID: "0-0", ObservedAt: now, ReadyAt: now, Bootstrapped: true}, now); err != nil {
		t.Fatal(err)
	}
	ids := []string{"1800000000000-0", "1800000000000-1"}
	if err := history.RegisterStreamMessages(stream, ids); err != nil {
		t.Fatal(err)
	}
	batch := testDecodedCandleBatch([]string{"BTCUSDT"}, 1_800_000_000_000, true)
	if err := history.ObserveStreamBatch(stream, ids[0], batch, now); err != nil {
		t.Fatal(err)
	}
	// Transport dedup may suppress repeated evaluation, but the stream
	// checkpoint must still apply the duplicate source ID in order.
	if err := history.ObserveStreamBatch(stream, ids[1], batch, now); err != nil {
		t.Fatal(err)
	}
	checkpoint, exists := history.StreamHighWater(stream)
	if !exists || checkpoint.MessageID != ids[1] || checkpoint.BatchID != batch.BatchID {
		t.Fatalf("checkpoint=%+v exists=%v", checkpoint, exists)
	}
}
