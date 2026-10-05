package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeCompactBootstrapOwner struct {
	calls  int
	failAt int
}

func (o *fakeCompactBootstrapOwner) AssertOwned(context.Context) error {
	o.calls++
	if o.failAt > 0 && o.calls >= o.failAt {
		return errors.New("owner lost")
	}
	return nil
}

type fakeCompactBootstrapSourceOwnerTest struct {
	bounds map[string]compactStreamBounds
	calls  []string
}

func (s *fakeCompactBootstrapSourceOwnerTest) CaptureBounds(_ context.Context, stream string) (compactStreamBounds, error) {
	s.calls = append(s.calls, "capture:"+stream)
	return s.bounds[stream], nil
}

func (s *fakeCompactBootstrapSourceOwnerTest) RangePage(_ context.Context, stream, _, _ string, _ int64) (compactStreamPage, error) {
	s.calls = append(s.calls, "range:"+stream)
	return compactStreamPage{FirstID: s.bounds[stream].FirstID}, nil
}

func (s *fakeCompactBootstrapSourceOwnerTest) LookupSkipDigests(context.Context, string, string, []redis.XMessage) (map[string]string, error) {
	return map[string]string{}, nil
}

func (s *fakeCompactBootstrapSourceOwnerTest) SetGroupStart(_ context.Context, stream, _, _ string) error {
	s.calls = append(s.calls, "group:"+stream)
	return nil
}

func TestCompactBootstrapRequiresOwnerAndLoadedRulesBeforeSourceReads(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	source := &fakeCompactBootstrapSourceOwnerTest{bounds: map[string]compactStreamBounds{"a": {FirstID: "0-0", UpperID: "0-0"}}}
	history := newSnapshotTestHistory()
	if _, err := BootstrapCompactStreams(context.Background(), source, nil, history, []string{"a"}, "group", now); !errors.Is(err, errCompactBootstrapOwnerRequired) {
		t.Fatalf("nil owner error=%v", err)
	}
	if len(source.calls) != 0 {
		t.Fatalf("source called before owner validation: %v", source.calls)
	}

	emptyRules := NewRulesCache()
	emptyHistory := newSparseEngineState(emptyRules, false, 10)
	if _, err := BootstrapCompactStreams(context.Background(), source, &fakeCompactBootstrapOwner{}, emptyHistory, []string{"a"}, "group", now); !errors.Is(err, errCompactBootstrapRulesNotReady) {
		t.Fatalf("unloaded rules error=%v", err)
	}
	if len(source.calls) != 0 {
		t.Fatalf("source called before rules validation: %v", source.calls)
	}
}

func TestCompactBootstrapCapturesAllBoundsBeforeGroupMoves(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	source := &fakeCompactBootstrapSourceOwnerTest{bounds: map[string]compactStreamBounds{
		"a": {FirstID: "0-0", UpperID: "0-0"},
		"b": {FirstID: "0-0", UpperID: "0-0"},
	}}
	report, err := BootstrapCompactStreams(context.Background(), source, &fakeCompactBootstrapOwner{}, newSnapshotTestHistory(), []string{"a", "b"}, "group", now)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"capture:a", "capture:b", "group:a", "group:b"}; !reflect.DeepEqual(source.calls, want) {
		t.Fatalf("calls=%v want=%v", source.calls, want)
	}
	if report.Ready {
		t.Fatal("empty fresh streams became ready before the 26-hour window")
	}
}

func TestCompactBootstrapOwnerLossDoesNotMoveGroupOrCheckpoint(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	source := &fakeCompactBootstrapSourceOwnerTest{bounds: map[string]compactStreamBounds{
		"a": {FirstID: "0-0", UpperID: "0-0"},
		"b": {FirstID: "0-0", UpperID: "0-0"},
	}}
	history := newSnapshotTestHistory()
	owner := &fakeCompactBootstrapOwner{failAt: 4}
	if _, err := BootstrapCompactStreams(context.Background(), source, owner, history, []string{"a", "b"}, "group", now); err == nil {
		t.Fatal("owner loss was accepted")
	}
	if want := []string{"capture:a", "capture:b"}; !reflect.DeepEqual(source.calls, want) {
		t.Fatalf("calls=%v want=%v", source.calls, want)
	}
	if _, ok := history.StreamHighWater("a"); ok {
		t.Fatal("checkpoint advanced after owner loss")
	}
}

func TestCompactBootstrapRejectsSnapshotOffsetOlderThanRetention(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	history := newSnapshotTestHistory()
	checkpoint := compactStreamHighWater{MessageID: "5-0", BatchMinute: 60_000, BatchID: "old", ObservedAt: now, ReadyAt: now, Bootstrapped: true}
	if err := history.SetStreamBootstrapCheckpoint("a", checkpoint, now); err != nil {
		t.Fatal(err)
	}
	source := &fakeCompactBootstrapSourceOwnerTest{bounds: map[string]compactStreamBounds{"a": {FirstID: "6-0", UpperID: "6-0"}}}
	fingerprint := history.currentDependencyFingerprint()
	_, err := bootstrapOneCompactStream(context.Background(), source, &fakeCompactBootstrapOwner{}, history, "a", "group", source.bounds["a"], now, fingerprint)
	if !errors.Is(err, errCompactStreamRewound) {
		t.Fatalf("stale offset error=%v", err)
	}
	if len(source.calls) != 0 {
		t.Fatalf("source mutated after stale offset: %v", source.calls)
	}
}
