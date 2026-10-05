package main

import (
	"testing"
	"time"
)

func TestUnifiedOutcomeRegularRuleWaitsForCoverageAndMatchesExpectedMath(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	cache := NewRulesCache()
	rule := Rule{ID: 11, UserID: 7, Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT", WindowMinutes: 1, ScannerSlot: "SLOT_2", Conditions: []Condition{{Indicator: "price", Direction: DirUp, ThresholdPct: 5}}}
	cache.Set(map[string][]Rule{instrumentCacheKey("bybit", "perpetual", "BTCUSDT"): {rule}})
	history := newSparseEngineState(cache, false, 10)

	if outcomes, ready := history.EvaluateReady("perpetual", "BTCUSDT", 1_800_000, "cooldown:", now); ready || len(outcomes) != 0 {
		t.Fatalf("evaluation without coverage outcomes=%+v ready=%v", outcomes, ready)
	}
	first := outcomeBatch("bybit", "c", "BTCUSDT", 1_740_000, 100, 0)
	second := outcomeBatch("bybit", "c", "BTCUSDT", 1_800_000, 106, 0)
	if err := history.ObserveBatch(first, now); err != nil {
		t.Fatal(err)
	}
	if err := history.ObserveBatch(second, now); err != nil {
		t.Fatal(err)
	}
	outcomes, ready := history.EvaluateReady("perpetual", "BTCUSDT", second.Minute, "cooldown:", now)
	if !ready || len(outcomes) != 1 {
		t.Fatalf("outcomes=%+v ready=%v", outcomes, ready)
	}
	outcome := outcomes[0]
	if !outcome.Available || !outcome.Matched || outcome.Alert.ChangePercent != 6 || outcome.Alert.PriceNow != 106 || outcome.Alert.PriceThen != 100 {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
	if outcome.CooldownKey != coinCooldownKey("cooldown:", rule.UserID, "bybit", "perpetual", "BTCUSDT") {
		t.Fatalf("cooldown key=%q", outcome.CooldownKey)
	}
	if outcome.SignalIdentity == "" {
		t.Fatal("signal dedup identity is empty")
	}
}

func TestUnifiedOutcomeCombinedWaitsForFiveVenueCoverage(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	cache := NewRulesCache()
	rule := Rule{ID: 22, UserID: 9, Exchange: "bybit", MarketType: "perpetual", Symbol: "*", WindowMinutes: 1, ScannerSlot: "SLOT_1", Conditions: []Condition{{Indicator: "liquidationsCombined", ThresholdAmount: 14}}}
	cache.Set(map[string][]Rule{instrumentCacheKey("bybit", "perpetual", wildcardSymbol): {rule}})
	history := newSparseEngineState(cache, false, 20)
	minute := int64(1_800_000)
	for index, venue := range combinedLiquidationVenues {
		kind := "l"
		if venue == "bybit" || venue == "binance" {
			kind = "c"
		}
		if err := history.ObserveBatch(outcomeBatch(venue, kind, "BTCUSDT", minute, 100, float64(index+1)), now); err != nil {
			t.Fatalf("observe %s: %v", venue, err)
		}
		outcomes, ready := history.EvaluateReady("perpetual", "BTCUSDT", minute, "cooldown:", now)
		if index < len(combinedLiquidationVenues)-1 && (ready || len(outcomes) != 0) {
			t.Fatalf("combined became ready after %s outcomes=%+v ready=%v", venue, outcomes, ready)
		}
	}
	outcomes, ready := history.EvaluateReady("perpetual", "BTCUSDT", minute, "cooldown:", now)
	if !ready || len(outcomes) != 1 {
		t.Fatalf("combined outcomes=%+v ready=%v", outcomes, ready)
	}
	outcome := outcomes[0]
	if !outcome.Available || !outcome.Matched || outcome.Alert.Liquidations != 15 || outcome.Alert.BybitLiquidations != 1 || outcome.Alert.GateIOLiquidations != 5 {
		t.Fatalf("unexpected combined outcome: %+v", outcome)
	}
	wantCooldown := coinCooldownKey("cooldown:", rule.UserID, "bybit+binance+okx+bitget+gateio", "perpetual", "BTCUSDT")
	if outcome.CooldownKey != wantCooldown {
		t.Fatalf("cooldown key=%q want=%q", outcome.CooldownKey, wantCooldown)
	}
}

func TestUnifiedOutcomeCompletesMissingCoverageWithoutImplicitZero(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	cache := NewRulesCache()
	rule := Rule{ID: 33, Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT", WindowMinutes: 1, Conditions: []Condition{{Indicator: "price", Direction: DirBoth, ThresholdPct: 1}}}
	cache.Set(map[string][]Rule{instrumentCacheKey("bybit", "perpetual", "BTCUSDT"): {rule}})
	history := newSparseEngineState(cache, false, 10)
	batch := outcomeBatch("bybit", "c", "BTCUSDT", 1_800_000, 0, 0)
	batch.Coverage = false
	// A partial candle batch describes missing coverage by omitting the row.
	// A present row remains covered even when another catalog row is absent.
	batch.Events = nil
	if err := history.ObserveBatch(batch, now); err != nil {
		t.Fatal(err)
	}
	outcomes, ready := history.EvaluateReady("perpetual", "BTCUSDT", batch.Minute, "cooldown:", now)
	if !ready || len(outcomes) != 1 || outcomes[0].Available || outcomes[0].Matched || outcomes[0].Reason != "coverage_missing:bybit" {
		t.Fatalf("missing coverage outcomes=%+v ready=%v", outcomes, ready)
	}
}

func TestProductionSparseOutcomeIsEvaluationReadyAt1440Minutes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC().Truncate(time.Minute)
	cache := NewRulesCache()
	rule := Rule{ID: 1440, UserID: 7, Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT", WindowMinutes: 1440, ScannerSlot: "SLOT_1", Conditions: []Condition{{Indicator: "price", Direction: DirUp, ThresholdPct: 5}}}
	cache.Set(map[string][]Rule{instrumentCacheKey("bybit", "perpetual", "BTCUSDT"): {rule}})
	history := newSparseEngineState(cache, false, 10)
	start := outcomeBatch("bybit", "c", "BTCUSDT", now.Add(-1440*time.Minute).UnixMilli(), 100, 0)
	end := outcomeBatch("bybit", "c", "BTCUSDT", now.UnixMilli(), 106, 0)
	if err := history.ObserveBatch(start, now.Add(-1440*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := history.ObserveBatch(end, now); err != nil {
		t.Fatal(err)
	}
	outcomes, ready := history.EvaluateReady("perpetual", "BTCUSDT", end.Minute, "cooldown:", now)
	if !ready || len(outcomes) != 1 || !outcomes[0].Available || !outcomes[0].Matched {
		t.Fatalf("1440-minute outcome=%+v ready=%v", outcomes, ready)
	}
	key := instrumentCacheKey("bybit", "perpetual", "BTCUSDT")
	if record := history.instruments[key]; record == nil || record.history.Capacity("close") != 1441 {
		t.Fatalf("1440-minute close history is not allocated with lookback: record=%+v", record)
	}
}

func outcomeBatch(exchange, kind, symbol string, minute int64, closeValue, liquidations float64) compactDecodedBatch {
	catalog, version := compactCatalog([]string{symbol})
	metrics := map[string]MetricValue{
		"close":        {Value: closeValue, Valid: true},
		"liquidations": {Value: liquidations, Valid: true},
	}
	event := CandleEvent{Exchange: exchange, MarketType: "perpetual", Symbol: symbol, TS: minute, Close: closeValue, Liquidations: liquidations, Metrics: metrics, LiquidationOnly: kind == "l"}
	return compactDecodedBatch{BatchID: kind + ":" + exchange + ":" + symbol, Kind: kind, Exchange: exchange, MarketType: "perpetual", Minute: minute, ShardTotal: 1, Coverage: true, CatalogVersion: version, Catalog: catalog, Events: []CandleEvent{event}}
}
