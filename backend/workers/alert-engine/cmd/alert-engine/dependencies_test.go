package main

import "testing"

func TestResolveHistoryPlanKeepsOnlyRuleDependencies(t *testing.T) {
	rules := []Rule{{
		Exchange:      "bybit",
		MarketType:    "perpetual",
		Symbol:        "BTCUSDT",
		WindowMinutes: 120,
		Conditions: []Condition{
			{Indicator: "price:open:high"},
			{Indicator: "longShortRatio"},
		},
	}}

	plan := resolveHistoryPlan(rules, "bybit", "perpetual", "BTCUSDT", false)
	want := historyPlan{"open": 121, "high": 1, "longRatio": 121, "shortRatio": 121}
	assertHistoryPlan(t, plan, want)
}

func TestResolveHistoryPlanExpandsCombinedLiquidationsWithoutFullCandleHistory(t *testing.T) {
	rules := []Rule{{
		Exchange:      "bybit",
		MarketType:    "perpetual",
		Symbol:        "*",
		WindowMinutes: 30,
		Conditions:    []Condition{{Indicator: "liquidationsCombined", ThresholdAmount: 1}},
	}}

	for _, venue := range combinedLiquidationVenues {
		plan := resolveHistoryPlan(rules, venue, "perpetual", "ETHUSDT", false)
		want := historyPlan{"liquidations": 30}
		if venue == "bybit" {
			want["close"] = 1
		}
		assertHistoryPlan(t, plan, want)
	}
}

func TestResolveHistoryPlanRejectsUnrelatedRules(t *testing.T) {
	rules := []Rule{{
		Exchange:      "binance",
		MarketType:    "perpetual",
		Symbol:        "ETHUSDT",
		WindowMinutes: 720,
		Conditions:    []Condition{{Indicator: "openInterest"}},
	}}

	if plan := resolveHistoryPlan(rules, "bybit", "perpetual", "BTCUSDT", false); len(plan) != 0 {
		t.Fatalf("unrelated rule allocated history: %#v", plan)
	}
}

func TestResolveHistoryPlanUsesIndependentMetricDepths(t *testing.T) {
	rules := []Rule{
		{Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT", WindowMinutes: 1440, Conditions: []Condition{{Indicator: "price"}}},
		{Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT", WindowMinutes: 60, Conditions: []Condition{{Indicator: "quoteVolumeUsd"}}},
	}
	plan := resolveHistoryPlan(rules, "bybit", "perpetual", "BTCUSDT", false)
	assertHistoryPlan(t, plan, historyPlan{"close": 1441, "quoteVolumeUsd": 61})
}

func TestImportantHistoryPlanMatchesDetectorLookback(t *testing.T) {
	plan := resolveHistoryPlan(nil, "bybit", "perpetual", "BTCUSDT", true)

	if got, want := plan["close"], importantHistoryMinutes+16; got != want {
		t.Fatalf("close capacity=%d want=%d", got, want)
	}
	if got, want := plan["quoteVolumeUsd"], importantHistoryMinutes+15; got != want {
		t.Fatalf("quote volume capacity=%d want=%d", got, want)
	}
	for _, metric := range []string{"openInterest", "openInterestTimestamp"} {
		if got, want := plan[metric], 6; got != want {
			t.Fatalf("%s capacity=%d want=%d", metric, got, want)
		}
	}
	for _, metric := range []string{"markPrice", "largestTradeUsd", "medianTradeUsd", "tradeClusterUsd", "tradeClusterSide", "tradeClusterCount", "tradePriceImpactPct"} {
		if got, want := plan[metric], 1; got != want {
			t.Fatalf("%s capacity=%d want=%d", metric, got, want)
		}
	}
}

func assertHistoryPlan(t *testing.T, got, want historyPlan) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("plan length=%d want=%d plan=%#v", len(got), len(want), got)
	}
	for metric, capacity := range want {
		if got[metric] != capacity {
			t.Fatalf("%s capacity=%d want=%d plan=%#v", metric, got[metric], capacity, got)
		}
	}
}
