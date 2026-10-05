package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRuleRefreshReplayBackfillsExpanded1440Window(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	stream := "candles:v3:bybit:perpetual"
	rules := NewRulesCache()
	setPriceRule := func(window int) {
		rules.Set(map[string][]Rule{"bybit:perpetual:BTCUSDT": {{
			ID: 1, Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT", WindowMinutes: window,
			Conditions: []Condition{{Indicator: "price", Direction: DirUp, ThresholdPct: 1}},
		}}})
	}
	setPriceRule(1)
	history := newSparseEngineState(rules, false, 10)
	if err := history.setBootstrapDependencyFingerprint(history.currentDependencyFingerprint()); err != nil {
		t.Fatal(err)
	}
	setPriceRule(1440)
	startMinute := now.Add(-1440 * time.Minute)
	messages := []redis.XMessage{
		compactReplayCandleMessage(t, startMinute, 100, 0),
		compactReplayCandleMessage(t, now, 106, 0),
	}
	engine := &Engine{
		cfg:   Config{MarketStreams: []string{stream}, MarketConsumerGroup: "group", MaxInstruments: 10},
		rules: rules, unifiedHistory: history, compactOwner: &fakeCompactBootstrapOwner{},
		outcomeScheduler: newOutcomeScheduler(time.Minute, 10),
	}
	if err := engine.rebootstrapRulesFrom(context.Background(), &compactBootstrapPageSource{messages: messages}); err != nil {
		t.Fatal(err)
	}
	key := instrumentCacheKey("bybit", "perpetual", "BTCUSDT")
	record := history.instruments[key]
	if record == nil || record.history.Capacity("close") != 1441 {
		t.Fatalf("replayed record=%+v", record)
	}
	if value, ok := record.history.Get(minuteKey(startMinute.UnixMilli()), "close"); !ok || value != 100 {
		t.Fatalf("oldest replayed close=(%v,%v)", value, ok)
	}
	if err := history.requireBootstrapDependenciesStable(); err != nil {
		t.Fatalf("replayed dependencies are not stable: %v", err)
	}
}

func compactReplayCandleMessage(t *testing.T, minute time.Time, closeValue float64, sequence int) redis.XMessage {
	t.Helper()
	catalog, version := compactCatalog([]string{"BTCUSDT"})
	values := make([]float64, len(compactMetricLayout))
	var validity uint64
	for index, metric := range compactMetricLayout {
		if metric == "close" {
			values[index] = closeValue
			validity |= uint64(1) << index
		}
	}
	batch := compactWireBatch{
		TransportVersion: compactTransportVersion, CanonicalVersion: compactCanonicalVersion,
		Kind: "c", Exchange: "bybit", MarketType: "perpetual", Minute: minute.UnixMilli(),
		ShardTotal: 1, Coverage: true, CatalogVersion: version, Catalog: catalog,
		LayoutVersion: compactMetricLayoutV1, LayoutHash: compactMetricLayoutHash,
		CandleRows: []compactWireCandleRow{{Symbol: "BTCUSDT", Values: values, Validity: validity}},
	}
	batch.BatchID = compactBatchID(batch)
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	return redis.XMessage{ID: fmt.Sprintf("%d-%d", minute.UnixMilli(), sequence), Values: map[string]any{"json": string(raw)}}
}
