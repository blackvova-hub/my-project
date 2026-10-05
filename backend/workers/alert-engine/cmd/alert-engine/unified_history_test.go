package main

import (
	"errors"
	"testing"
	"time"
)

func TestRulesCacheSnapshotGenerationAndIsolation(t *testing.T) {
	cache := NewRulesCache()
	cache.Set(map[string][]Rule{"bybit:perpetual:BTCUSDT": {{ID: 1, Symbol: "BTCUSDT"}}})
	rules, generation := cache.Snapshot()
	if generation == 0 || len(rules) != 1 {
		t.Fatalf("snapshot rules=%+v generation=%d", rules, generation)
	}
	rules[0].Symbol = "MUTATED"
	again, sameGeneration := cache.Snapshot()
	if sameGeneration != generation || again[0].Symbol != "BTCUSDT" {
		t.Fatal("rules snapshot exposed cache-owned storage")
	}
	cache.Set(map[string][]Rule{})
	_, nextGeneration := cache.Snapshot()
	if nextGeneration <= generation {
		t.Fatalf("generation did not advance: %d -> %d", generation, nextGeneration)
	}
}

func TestSparseEngineStateAllocatesOnlyMatchingDependencies(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	cache := NewRulesCache()
	cache.Set(map[string][]Rule{"bybit:perpetual:BTCUSDT": {{
		ID: 1, Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT", WindowMinutes: 2,
		Conditions: []Condition{{Indicator: "price"}},
	}}})
	engine := newSparseEngineState(cache, false, 10)
	batch := testDecodedCandleBatch([]string{"BTCUSDT", "ETHUSDT"}, 1_800_000, true)
	if err := engine.ObserveBatch(batch, now); err != nil {
		t.Fatalf("observe batch: %v", err)
	}
	stats := engine.Stats()
	if stats.Instruments != 1 || stats.Series != 1 || stats.ValueSlots != 3 {
		t.Fatalf("unexpected sparse stats: %+v", stats)
	}
	key := instrumentCacheKey("bybit", "perpetual", "BTCUSDT")
	if value, ok := engine.Get(key, minuteKey(batch.Minute), "close"); !ok || value != 100 {
		t.Fatalf("stored close=(%v,%v)", value, ok)
	}
	if engine.HasInstrument(instrumentCacheKey("bybit", "perpetual", "ETHUSDT")) {
		t.Fatal("unrelated catalog symbol allocated history")
	}
}

func TestSparseEngineStateWithNoRulesAllocatesNoInstrumentHistory(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	history := newSparseEngineState(NewRulesCache(), false, 10)
	if err := history.ObserveBatch(testDecodedCandleBatch([]string{"BTCUSDT", "ETHUSDT"}, 1_800_000, true), now); err != nil {
		t.Fatal(err)
	}
	if stats := history.Stats(); stats.Instruments != 0 || stats.Series != 0 || stats.ValueSlots != 0 {
		t.Fatalf("ruleless slot allocated market history: %+v", stats)
	}
}

func TestSparseEngineStateReleasesRemovedRuleImmediately(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	cache := NewRulesCache()
	cache.Set(map[string][]Rule{"bybit:perpetual:*": {{
		ID: 1, Exchange: "bybit", MarketType: "perpetual", Symbol: "*", WindowMinutes: 60,
		Conditions: []Condition{{Indicator: "openInterest"}},
	}}})
	engine := newSparseEngineState(cache, false, 10)
	batch := testDecodedCandleBatch([]string{"BTCUSDT"}, 1_800_000, true)
	if err := engine.ObserveBatch(batch, now); err != nil {
		t.Fatal(err)
	}
	cache.Set(map[string][]Rule{})
	if err := engine.ObserveBatch(batch, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if stats := engine.Stats(); stats.Instruments != 0 || stats.Series != 0 {
		t.Fatalf("removed rule retained history: %+v", stats)
	}
}

func TestSparseEngineStateResizesOnlyChangedDependencyOnRuleRefresh(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	cache := NewRulesCache()
	setWindow := func(window int) {
		cache.Set(map[string][]Rule{"bybit:perpetual:BTCUSDT": {{
			ID: 1, Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT", WindowMinutes: window,
			Conditions: []Condition{{Indicator: "price"}},
		}}})
	}
	setWindow(2)
	history := newSparseEngineState(cache, false, 10)
	observe := func(minute int64) {
		t.Helper()
		if err := history.ObserveBatch(testDecodedCandleBatch([]string{"BTCUSDT"}, minute, true), now.Add(time.Duration(minute-1_800_000)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	capacity := func() int {
		history.mu.Lock()
		defer history.mu.Unlock()
		return history.instruments["bybit:perpetual:BTCUSDT"].history.Capacity("close")
	}
	observe(1_800_000)
	if got := capacity(); got != 3 {
		t.Fatalf("initial capacity=%d", got)
	}
	setWindow(5)
	observe(1_860_000)
	if got := capacity(); got != 6 {
		t.Fatalf("grown capacity=%d", got)
	}
	setWindow(1)
	observe(1_920_000)
	if got := capacity(); got != 2 {
		t.Fatalf("shrunk capacity=%d", got)
	}
}

func TestSparseEngineStatePreservesCoverageFalseAsInvalid(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	cache := NewRulesCache()
	cache.Set(map[string][]Rule{"okx:perpetual:*": {{
		ID: 1, Exchange: "okx", MarketType: "perpetual", Symbol: "*", WindowMinutes: 2,
		Conditions: []Condition{{Indicator: "liquidations", ThresholdAmount: 1}},
	}}})
	engine := newSparseEngineState(cache, false, 10)
	catalog, version := compactCatalog([]string{"BTCUSDT"})
	batch := compactDecodedBatch{
		BatchID: "missing", Kind: "l", Exchange: "okx", MarketType: "perpetual", Minute: 1_800_000,
		ShardTotal: 1, Coverage: false, CatalogVersion: version, Catalog: catalog,
		Events: []CandleEvent{{Exchange: "okx", MarketType: "perpetual", Symbol: "BTCUSDT", TS: 1_800_000, LiquidationOnly: true,
			Metrics: map[string]MetricValue{"liquidations": {Value: 0, Valid: false}}}},
	}
	if err := engine.ObserveBatch(batch, now); err != nil {
		t.Fatal(err)
	}
	key := instrumentCacheKey("okx", "perpetual", "BTCUSDT")
	if _, ok := engine.Get(key, minuteKey(batch.Minute), "liquidations"); ok {
		t.Fatal("coverage=false was stored as valid zero")
	}
}

func TestSparseEngineStateRejectsCapacityWithoutEvictingActiveHistory(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	cache := NewRulesCache()
	cache.Set(map[string][]Rule{
		"bybit:perpetual:BTCUSDT": {{ID: 1, Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT", WindowMinutes: 1, Conditions: []Condition{{Indicator: "price", ThresholdPct: 1}}}},
		"bybit:perpetual:ETHUSDT": {{ID: 2, Exchange: "bybit", MarketType: "perpetual", Symbol: "ETHUSDT", WindowMinutes: 1, Conditions: []Condition{{Indicator: "price", ThresholdPct: 1}}}},
	})
	history := newSparseEngineState(cache, false, 1)
	btc := testDecodedCandleBatch([]string{"BTCUSDT"}, 1_800_000, true)
	if err := history.ObserveBatch(btc, now); err != nil {
		t.Fatal(err)
	}
	eth := testDecodedCandleBatch([]string{"ETHUSDT"}, 1_860_000, true)
	if err := history.ObserveBatch(eth, now.Add(time.Minute)); !errors.Is(err, errUnifiedInstrumentCapacity) {
		t.Fatalf("capacity error=%v", err)
	}
	if !history.HasInstrument("bybit:perpetual:BTCUSDT") {
		t.Fatal("capacity rejection evicted the active BTC history")
	}
	if history.HasInstrument("bybit:perpetual:ETHUSDT") {
		t.Fatal("capacity rejection partially admitted ETH history")
	}
	if status := history.coverage.Status("c", "bybit", "perpetual", eth.Minute, "ETHUSDT", now.Add(time.Minute)); status != coveragePending {
		t.Fatalf("rejected batch leaked ready coverage: %s", status)
	}
}

func testDecodedCandleBatch(symbols []string, minute int64, coverage bool) compactDecodedBatch {
	catalog, version := compactCatalog(symbols)
	events := make([]CandleEvent, 0, len(catalog))
	for _, symbol := range catalog {
		events = append(events, CandleEvent{Exchange: "bybit", MarketType: "perpetual", Symbol: symbol, TS: minute,
			Metrics: map[string]MetricValue{"close": {Value: 100, Valid: coverage}, "openInterest": {Value: 10, Valid: coverage}}})
	}
	return compactDecodedBatch{BatchID: "test", Kind: "c", Exchange: "bybit", MarketType: "perpetual", Minute: minute, ShardTotal: 1, Coverage: coverage, CatalogVersion: version, Catalog: catalog, Events: events}
}
