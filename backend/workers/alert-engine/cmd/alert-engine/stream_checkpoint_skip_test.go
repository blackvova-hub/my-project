package main

import (
	"strings"
	"testing"
	"time"
)

func TestStreamCheckpointDoesNotJumpSkippedGapAndPersistsV4(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	history := newSnapshotTestHistory()
	base := compactStreamHighWater{MessageID: "0-0", ObservedAt: now, ReadyAt: now, Bootstrapped: true}
	if err := history.SetStreamBootstrapCheckpoint(snapshotTestStream, base, now); err != nil {
		t.Fatal(err)
	}
	ids := []string{"10-1", "10-2", "10-3"}
	if err := history.RegisterStreamMessages(snapshotTestStream, ids); err != nil {
		t.Fatal(err)
	}
	digest2, digest3 := strings.Repeat("2", 64), strings.Repeat("3", 64)
	if err := history.MarkStreamMessageSkipped(snapshotTestStream, ids[1], digest2, now); err != nil {
		t.Fatal(err)
	}
	if err := history.MarkStreamMessageSkipped(snapshotTestStream, ids[2], digest3, now); err != nil {
		t.Fatal(err)
	}
	if water, _ := history.StreamHighWater(snapshotTestStream); water.MessageID != "0-0" {
		t.Fatalf("checkpoint jumped gap: %+v", water)
	}
	digest1 := strings.Repeat("1", 64)
	if err := history.MarkStreamMessageSkipped(snapshotTestStream, ids[0], digest1, now); err != nil {
		t.Fatal(err)
	}
	water, _ := history.StreamHighWater(snapshotTestStream)
	if water.MessageID != "10-3" || water.SkipDigest != digest3 || water.Skipped != 3 {
		t.Fatalf("checkpoint=%+v", water)
	}
	if err := history.MarkStreamMessageSkipped(snapshotTestStream, ids[2], strings.Repeat("a", 64), now); err == nil {
		t.Fatal("same ID accepted changed skip digest")
	}
}

func TestUnifiedSnapshotRoundTripsSkippedCheckpointV4(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	history := newSnapshotTestHistory()
	base := compactStreamHighWater{MessageID: "0-0", ObservedAt: now, ReadyAt: now, Bootstrapped: true}
	if err := history.SetStreamBootstrapCheckpoint(snapshotTestStream, base, now); err != nil {
		t.Fatal(err)
	}
	if err := history.RegisterStreamMessages(snapshotTestStream, []string{"11-0"}); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("b", 64)
	if err := history.MarkStreamMessageSkipped(snapshotTestStream, "11-0", digest, now); err != nil {
		t.Fatal(err)
	}
	store := newUnifiedSnapshotStore(t.TempDir() + "/state.snapshot")
	if err := store.Save(history, now); err != nil {
		t.Fatal(err)
	}
	restored := newSnapshotTestHistory()
	if err := store.Load(restored, now); err != nil {
		t.Fatal(err)
	}
	water, ok := restored.StreamHighWater(snapshotTestStream)
	if !ok || water.MessageID != "11-0" || water.SkipDigest != digest || water.Skipped != 1 {
		t.Fatalf("restored=%+v ok=%v", water, ok)
	}
}
