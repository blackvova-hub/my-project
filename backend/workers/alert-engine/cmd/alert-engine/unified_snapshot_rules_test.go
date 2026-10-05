package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUnifiedSnapshotRejectsDifferentRuleDependenciesWithoutMutation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	source, _ := snapshotTestFixture(t, now)
	path := filepath.Join(t.TempDir(), "state.snapshot")
	store := newUnifiedSnapshotStore(path)
	if err := store.Save(source, now); err != nil {
		t.Fatal(err)
	}

	target := newSnapshotTestHistory()
	target.rules.Set(map[string][]Rule{"bybit:perpetual:*": {{
		ID: 8, Exchange: "bybit", MarketType: "perpetual", Symbol: "*", WindowMinutes: 1440,
		Conditions: []Condition{{Indicator: "openInterest"}},
	}}})
	if err := store.Load(target, now); !errors.Is(err, ErrUnifiedSnapshotRulesMismatch) {
		t.Fatalf("load error=%v want rules mismatch", err)
	}
	if stats := target.Stats(); stats.Instruments != 0 || stats.Series != 0 {
		t.Fatalf("mismatched snapshot partially mutated target: %+v", stats)
	}
}

func TestUnifiedSnapshotRuleChangeWaitsForReplayBeforeCheckpoint(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	source, first := snapshotTestFixture(t, now)
	path := filepath.Join(t.TempDir(), "state.snapshot")
	store := newUnifiedSnapshotStore(path)
	if err := store.Save(source, now); err != nil {
		t.Fatal(err)
	}
	history := newSnapshotTestHistory()
	if err := store.Load(history, now); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	history.rules.Set(map[string][]Rule{"bybit:perpetual:*": {{
		ID: 9, Exchange: "bybit", MarketType: "perpetual", Symbol: "*", WindowMinutes: 60,
		Conditions: []Condition{{Indicator: "openInterest"}},
	}}})
	second := snapshotCandleBatch(first.Minute+60_000, true, []string{"BTCUSDT"})
	second.BatchID = "after-rule-change"
	messageID := streamIDAt(time.UnixMilli(second.Minute))
	if err := history.RegisterStreamMessages(snapshotTestStream, []string{messageID}); err != nil {
		t.Fatal(err)
	}
	if err := history.ObserveStreamBatch(snapshotTestStream, messageID, second, now.Add(time.Minute)); err != nil {
		t.Fatalf("observe after rules refresh: %v", err)
	}
	if err := store.Save(history, now.Add(time.Minute)); !errors.Is(err, errCompactBootstrapRulesChanged) {
		t.Fatalf("save error=%v want rules replay", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("partial rules refresh replaced the last replay-complete snapshot")
	}
}

func TestUnifiedSnapshotRefusesHistoryThatDoesNotMatchRulePlan(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	history, _ := snapshotTestFixture(t, now)
	key := instrumentCacheKey("bybit", "perpetual", "BTCUSDT")
	history.mu.Lock()
	record := history.instruments[key]
	history.mu.Unlock()
	if record == nil {
		t.Fatal("fixture instrument is missing")
	}
	record.history.mu.Lock()
	delete(record.history.series, "close")
	record.history.mu.Unlock()
	err := newUnifiedSnapshotStore(filepath.Join(t.TempDir(), "state.snapshot")).Save(history, now)
	if !errors.Is(err, ErrUnifiedSnapshotInvalidState) {
		t.Fatalf("save error=%v want invalid state", err)
	}
}
