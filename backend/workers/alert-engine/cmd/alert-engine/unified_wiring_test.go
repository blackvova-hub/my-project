package main

import (
	"strings"
	"testing"
)

func TestEngineAlwaysCreatesProductionSparseHistory(t *testing.T) {
	rules := NewRulesCache()
	if engine := NewEngine(Config{}, nil, rules); engine.unifiedHistory == nil {
		t.Fatal("production sparse history was not initialized")
	}
	engine := NewEngine(Config{MaxInstruments: 123}, nil, rules)
	if engine.unifiedHistory == nil || engine.unifiedHistory.maxInstruments != 123 {
		t.Fatal("production sparse history capacity was not configured")
	}
}

func TestMergeRuleMapsKeepsAllSlotsForSameInstrument(t *testing.T) {
	merged := mergeRuleMaps(
		map[string][]Rule{"bybit:perpetual:BTCUSDT": {{ID: 1, ScannerSlot: "SLOT_1"}}},
		map[string][]Rule{"bybit:perpetual:BTCUSDT": {{ID: 2, ScannerSlot: "SLOT_2"}}},
		map[string][]Rule{"bybit:perpetual:ETHUSDT": {{ID: 3, ScannerSlot: "SLOT_3"}}},
	)
	if got := len(merged["bybit:perpetual:BTCUSDT"]); got != 2 {
		t.Fatalf("BTC rules=%d want=2", got)
	}
	if got := len(merged["bybit:perpetual:ETHUSDT"]); got != 1 {
		t.Fatalf("ETH rules=%d want=1", got)
	}
}

func TestUnifiedRulesQueryIsSingleSnapshotAndKeepsSlot3ProGate(t *testing.T) {
	query, args := enabledRulesQuery("", true)
	if len(args) != 0 {
		t.Fatalf("unified query args=%v want none", args)
	}
	if strings.Count(strings.ToUpper(query), "SELECT ") != 1 {
		t.Fatalf("unified loader is not one SELECT: %q", query)
	}
	if !strings.Contains(query, "a.scanner_slot IN ('SLOT_1','SLOT_2','SLOT_3')") || !strings.Contains(query, "lower(u.plan) = 'pro'") {
		t.Fatal("unified query lost slot membership or the SLOT_3 Pro gate")
	}
	if !strings.Contains(query, "a.scanner_slot <> 'SLOT_3'") {
		t.Fatal("Pro gate incorrectly applies to SLOT_1/SLOT_2")
	}
}
