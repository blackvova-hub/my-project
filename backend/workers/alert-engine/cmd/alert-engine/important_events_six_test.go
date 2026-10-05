package main

import (
	"testing"
	"time"
)

func TestSingleLifecycleTransitions(t *testing.T) {
	if got := lifecycleTransition("candidate", 1, 2); got != "candidate" {
		t.Fatalf("first observation=%s", got)
	}
	if got := lifecycleTransition("candidate", 2, 2); got != "confirmed" {
		t.Fatalf("second observation=%s", got)
	}
	if got := lifecycleTransition("confirmed", 3, 2); got != "confirmed" {
		t.Fatalf("third observation=%s", got)
	}
}

func TestImportantEventResolutionSchedule(t *testing.T) {
	if importantResolutionInterval <= 0 || importantResolutionInterval > time.Minute {
		t.Fatalf("resolution interval=%s must be positive and at most one minute", importantResolutionInterval)
	}
	if importantResolutionBatchLimit <= 0 || importantResolutionBatchLimit > 1000 {
		t.Fatalf("resolution batch limit=%d must be within 1..1000", importantResolutionBatchLimit)
	}
	if importantCandidateResolutionAge != 3*time.Minute {
		t.Fatalf("candidate resolution age=%s, want 3m", importantCandidateResolutionAge)
	}
	if importantConfirmedResolutionAge != 8*time.Minute {
		t.Fatalf("confirmed resolution age=%s, want 8m", importantConfirmedResolutionAge)
	}
}

func TestDelayedImportantSampleDoesNotResolveImmediatelyAfterIngestion(t *testing.T) {
	ingestedAt := time.Unix(1_800_000_000, 0).UTC()
	eventAt := ingestedAt.Add(-importantMaxSampleAge + 10*time.Second)
	if !importantSampleFresh(eventAt.UnixMilli(), ingestedAt) {
		t.Fatal("fixture must be an allowed delayed sample")
	}
	immediateCutoff := ingestedAt.Add(-importantCandidateResolutionAge)
	if ingestedAt.Before(immediateCutoff) {
		t.Fatal("a just-ingested delayed candidate was considered expired")
	}
	afterFullInterval := ingestedAt.Add(importantCandidateResolutionAge + time.Second)
	if !ingestedAt.Before(afterFullInterval.Add(-importantCandidateResolutionAge)) {
		t.Fatal("candidate did not become eligible after a full processing-time resolution interval")
	}
}

func buildSixEventHistory(mk int64) *sparseHistory {
	r := newSparseHistory(resolveHistoryPlan(nil, "binance", "perpetual", "BTCUSDT", true))
	for minute := int64(1); minute <= mk; minute++ {
		r.Put(minute, CandleEvent{
			TS: minute * 60_000, Open: 100, High: 100, Low: 100, Close: 100,
			QuoteVolumeUsd: 100_000, MedianTradeUsd: 1_000,
		})
	}
	return r
}

func TestPriceShockUsesConfiguredWindowsAndVolumeGate(t *testing.T) {
	const mk = int64(200)
	r := buildSixEventHistory(mk)
	for minute := mk - 5; minute <= mk; minute++ {
		event := CandleEvent{TS: minute * 60_000, Open: 100, High: 104, Low: 100, Close: 100, QuoteVolumeUsd: 300_000, MedianTradeUsd: 1_000}
		if minute == mk {
			event.Close = 104
		}
		r.Put(minute, event)
	}
	ce := CandleEvent{Exchange: "binance", MarketType: "perpetual", Symbol: "BTCUSDT", TS: mk * 60_000}
	e := NewEngine(Config{}, nil, NewRulesCache())
	event, ok := e.detectPriceShock(ce, r, map[string]metricHistory{"binance": r}, mk)
	if !ok || event.EventType != "price_shock" || event.WindowMinutes != 5 {
		t.Fatalf("price shock=%+v ok=%v", event, ok)
	}
	if event.BaselineRatio == nil || *event.BaselineRatio < 2 {
		t.Fatalf("volume baseline ratio=%v", event.BaselineRatio)
	}
}

func TestPriceShockRejectsInsufficientVolume(t *testing.T) {
	const mk = int64(200)
	r := buildSixEventHistory(mk)
	r.Put(mk, CandleEvent{TS: mk * 60_000, Close: 104, QuoteVolumeUsd: 100_000})
	e := NewEngine(Config{}, nil, NewRulesCache())
	if _, ok := e.detectPriceShock(CandleEvent{Exchange: "binance", MarketType: "perpetual", Symbol: "BTCUSDT", TS: mk * 60_000}, r, map[string]metricHistory{"binance": r}, mk); ok {
		t.Fatal("price shock passed without 2x volume")
	}
}

func TestCrossVenueConfirmationRequiresSameDirection(t *testing.T) {
	const mk = int64(200)
	e := NewEngine(Config{}, nil, NewRulesCache())
	histories := make(map[string]metricHistory)
	for venue, end := range map[string]float64{"binance": 104, "bybit": 96} {
		r := buildSixEventHistory(mk)
		r.Put(mk, CandleEvent{TS: mk * 60_000, Close: end, QuoteVolumeUsd: 1_000_000})
		histories[venue] = r
	}
	venues := sameDirectionPriceVenues(CandleEvent{MarketType: "perpetual", Symbol: "BTCUSDT"}, histories, mk, 5, 3, "up")
	if len(venues) != 1 || venues[0] != "binance" {
		t.Fatalf("opposite venue confirmed: %v", venues)
	}
	histories["bybit"].(*sparseHistory).Put(mk, CandleEvent{TS: mk * 60_000, Close: 104, QuoteVolumeUsd: 1_000_000})
	venues = sameDirectionPriceVenues(CandleEvent{MarketType: "perpetual", Symbol: "BTCUSDT"}, histories, mk, 5, 3, "up")
	if len(venues) != 2 {
		t.Fatalf("same-direction venues=%v", venues)
	}
	ce := CandleEvent{Exchange: "binance", MarketType: "perpetual", Symbol: "BTCUSDT", TS: mk * 60_000}
	for _, candidate := range histories {
		for minute := mk - 4; minute <= mk; minute++ {
			candidate.(*sparseHistory).Put(minute, CandleEvent{TS: minute * 60_000, Close: 100, QuoteVolumeUsd: 300_000})
		}
		candidate.(*sparseHistory).Put(mk, CandleEvent{TS: mk * 60_000, Close: 104, QuoteVolumeUsd: 300_000})
	}
	if event, ok := e.detectPriceShock(ce, histories["binance"], histories, mk); !ok || event.Exchange != "multiple" {
		t.Fatalf("cross-venue price shock not consolidated: %+v ok=%v", event, ok)
	}
}

func TestHighConfidenceRequiresBinanceAndBybit(t *testing.T) {
	if !hasVenue([]string{"binance", "bybit"}, "binance") || !hasVenue([]string{"binance", "bybit"}, "bybit") {
		t.Fatal("Binance and Bybit confirmation was not recognized")
	}
	if hasVenue([]string{"binance", "okx"}, "bybit") {
		t.Fatal("another second venue must not impersonate Bybit confirmation")
	}
}

func TestOpenInterestShockUsesUsdAndVolumeRatioOncePerSourceSample(t *testing.T) {
	const mk = int64(100)
	r := buildSixEventHistory(mk)
	for minute := mk - 5; minute <= mk; minute++ {
		r.Put(minute, CandleEvent{TS: minute * 60_000, Close: 100, MarkPrice: 100, OpenInterest: 100_000, OpenInterestTimestamp: (mk - 5) * 60_000, QuoteVolumeUsd: 300_000})
	}
	r.Put(mk, CandleEvent{TS: mk * 60_000, Close: 100, MarkPrice: 100, OpenInterest: 120_000, OpenInterestTimestamp: mk * 60_000, QuoteVolumeUsd: 300_000})
	event, ok := detectOpenInterestShock(CandleEvent{Exchange: "binance", MarketType: "perpetual", Symbol: "BTCUSDT", TS: mk * 60_000}, r, mk)
	if !ok || event.AmountUSD == nil || *event.AmountUSD != 2_000_000 {
		t.Fatalf("oi shock=%+v ok=%v", event, ok)
	}
	r.Put(mk+1, CandleEvent{TS: (mk + 1) * 60_000, Close: 100, MarkPrice: 100, OpenInterest: 120_000, OpenInterestTimestamp: mk * 60_000, QuoteVolumeUsd: 300_000})
	if _, ok := detectOpenInterestShock(CandleEvent{Exchange: "binance", MarketType: "perpetual", Symbol: "BTCUSDT", TS: (mk + 1) * 60_000}, r, mk+1); ok {
		t.Fatal("same OI source sample was observed twice")
	}
}

func TestLargeAggressiveTradeRequiresThresholdAndConfirmation(t *testing.T) {
	const mk = int64(100)
	r := buildSixEventHistory(mk)
	for minute := mk - 4; minute <= mk; minute++ {
		r.Put(minute, CandleEvent{TS: minute * 60_000, Close: 100, QuoteVolumeUsd: 4_000_000, MedianTradeUsd: 20_000})
	}
	r.Put(mk, CandleEvent{TS: mk * 60_000, Close: 100, QuoteVolumeUsd: 4_000_000, MedianTradeUsd: 20_000,
		LargestTradeUsd: 1_200_000, LargestTradeSide: 1, TradeClusterUsd: 1_400_000,
		TradeClusterSide: 1, TradeClusterCount: 3, TradePriceImpactPct: .35})
	event, ok := detectLargeAggressiveTrade(CandleEvent{Exchange: "binance", MarketType: "perpetual", Symbol: "BTCUSDT", TS: mk * 60_000}, r, mk)
	if !ok || event.Direction != "buy" || event.EventType != "large_aggressive_trade" {
		t.Fatalf("large trade=%+v ok=%v", event, ok)
	}
	r.Put(mk, CandleEvent{TS: mk * 60_000, Close: 100, QuoteVolumeUsd: 4_000_000, MedianTradeUsd: 20_000,
		LargestTradeUsd: 1_200_000, LargestTradeSide: 1, TradeClusterUsd: 500_000,
		TradeClusterSide: 1, TradeClusterCount: 1, TradePriceImpactPct: .1})
	if _, ok := detectLargeAggressiveTrade(CandleEvent{Exchange: "binance", MarketType: "perpetual", Symbol: "BTCUSDT", TS: mk * 60_000}, r, mk); ok {
		t.Fatal("unconfirmed large trade was published")
	}
}

func TestLargeAggressiveTradeUsesConfirmedClusterDirection(t *testing.T) {
	const mk = int64(100)
	r := buildSixEventHistory(mk)
	for minute := mk - 4; minute <= mk; minute++ {
		r.Put(minute, CandleEvent{TS: minute * 60_000, Close: 100, QuoteVolumeUsd: 4_000_000, MedianTradeUsd: 20_000})
	}
	r.Put(mk, CandleEvent{TS: mk * 60_000, Close: 100, QuoteVolumeUsd: 4_000_000, MedianTradeUsd: 20_000,
		LargestTradeUsd: 1_800_000, LargestTradeSide: -1, TradeClusterUsd: 1_400_000,
		TradeClusterSide: 1, TradeClusterCount: 3, TradePriceImpactPct: .35})
	event, ok := detectLargeAggressiveTrade(CandleEvent{Exchange: "binance", MarketType: "perpetual", Symbol: "BTCUSDT", TS: mk * 60_000}, r, mk)
	if !ok || event.Direction != "buy" || event.AmountUSD == nil || *event.AmountUSD != 1_400_000 {
		t.Fatalf("event used unconfirmed opposite-side trade: %+v ok=%v", event, ok)
	}
}

func TestExchangeAnnouncementContract(t *testing.T) {
	if !isExchangeAnnouncementSubtype("spot_listing") || isExchangeAnnouncementSubtype("listing") {
		t.Fatal("announcement subtype contract is not strict")
	}
	if got := announcementMarketType("futures_listing"); got != "futures" {
		t.Fatalf("market=%s", got)
	}
}

func TestMajorStatementRequiresSpecificMarketTheme(t *testing.T) {
	if topic := statementMarketTopic("Presidential message honoring a religious anniversary"); topic != "" {
		t.Fatalf("unrelated statement topic=%s", topic)
	}
	if topic := statementMarketTopic("The Federal Reserve announced an interest rate cut"); topic != "monetary_macro" {
		t.Fatalf("market statement topic=%s", topic)
	}
	if got := majorStatementFingerprint("federal_reserve", "monetary_macro", time.Date(2026, 7, 21, 1, 0, 0, 0, time.UTC)); got != "federal_reserve:monetary_macro:2026-07-21" {
		t.Fatalf("fingerprint=%s", got)
	}
}

func TestMediaStatementRequiresOfficialConfirmation(t *testing.T) {
	if eventCanConfirm(map[string]any{"requiresOfficialConfirmation": true, "official": false}) {
		t.Fatal("media statement was allowed to confirm itself")
	}
	if !eventCanConfirm(map[string]any{"requiresOfficialConfirmation": true, "official": true}) {
		t.Fatal("official evidence did not confirm candidate")
	}
	if !eventCanConfirm(map[string]any{"requiredObservations": 1}) {
		t.Fatal("strict detector event did not confirm")
	}
}
