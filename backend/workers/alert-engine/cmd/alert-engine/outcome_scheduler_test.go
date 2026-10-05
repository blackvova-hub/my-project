package main

import (
	"fmt"
	"testing"
	"time"
)

func TestProductionSchedulerRetainsReadyWorkUntilDeliverySucceeds(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	rules := NewRulesCache()
	rules.Set(map[string][]Rule{"bybit:perpetual:BTCUSDT": {{
		ID: 1, UserID: 2, Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT",
		WindowMinutes: 1, ScannerSlot: "SLOT_1", Conditions: []Condition{{Indicator: "close", Direction: DirUp, ThresholdPct: 1}},
	}}})
	history := newSparseEngineState(rules, false, 10)
	previous := outcomeBatch("bybit", "c", "BTCUSDT", now.Add(-time.Minute).UnixMilli(), 100, 0)
	batch := outcomeBatch("bybit", "c", "BTCUSDT", now.UnixMilli(), 102, 0)
	if err := history.ObserveBatch(previous, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := history.ObserveBatch(batch, now); err != nil {
		t.Fatal(err)
	}
	scheduler := newOutcomeScheduler(time.Minute, 10)
	failed := true
	deliveries := 0
	deliver := func([]unifiedRuleOutcome) error {
		deliveries++
		if failed {
			return errorsForSchedulerTest
		}
		return nil
	}
	if err := scheduler.ObserveProductionBatch(now, batch, history, "SLOT_1", "cooldown:", deliver); err == nil {
		t.Fatal("delivery failure was ignored")
	}
	failed = false
	if err := scheduler.ObserveProductionBatch(now, batch, history, "SLOT_1", "cooldown:", deliver); err != nil {
		t.Fatal(err)
	}
	if deliveries != 2 || scheduler.units != 0 {
		t.Fatalf("deliveries=%d units=%d", deliveries, scheduler.units)
	}
}

var errorsForSchedulerTest = fmt.Errorf("delivery unavailable")
