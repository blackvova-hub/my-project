package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRulesCacheSeparatesExchanges(t *testing.T) {
	cache := NewRulesCache()
	cache.Set(map[string][]Rule{
		instrumentCacheKey("bybit", "perpetual", "BTCUSDT"):   {{ID: 1, Exchange: "bybit", MarketType: "perpetual"}},
		instrumentCacheKey("binance", "perpetual", "BTCUSDT"): {{ID: 2, Exchange: "binance", MarketType: "perpetual"}},
		instrumentCacheKey("binance", "perpetual", "*"):       {{ID: 3, Exchange: "binance", MarketType: "perpetual"}},
	})
	bybit := cache.GetForInstrument("bybit", "perpetual", "BTCUSDT")
	binance := cache.GetForInstrument("binance", "perpetual", "BTCUSDT")
	if len(bybit) != 1 || bybit[0].ID != 1 {
		t.Fatalf("bybit rules crossed venues: %+v", bybit)
	}
	if len(binance) != 2 || binance[0].ID != 2 || binance[1].ID != 3 {
		t.Fatalf("binance rules = %+v", binance)
	}
}

func TestPrepareCandleEventRejectsStreamVenueMismatch(t *testing.T) {
	event := CandleEvent{Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT", TS: 60_000}
	if prepareCandleEventForStream(&event, "candles:v3:binance:perpetual") {
		t.Fatal("Bybit event must not be accepted from Binance stream")
	}
	bybit := CandleEvent{Symbol: "BTCUSDT", MarketType: "perpetual", TS: 60_000}
	if !prepareCandleEventForStream(&bybit, "candles:v3:bybit:perpetual") || bybit.Exchange != "bybit" {
		t.Fatalf("Bybit event was not normalized: %+v", bybit)
	}
	for _, test := range []struct {
		exchange string
		stream   string
	}{
		{exchange: "okx", stream: "liquidations:v3:okx:perpetual"},
		{exchange: "bitget", stream: "liquidations:v3:bitget:perpetual"},
		{exchange: "gateio", stream: "liquidations:v3:gateio:perpetual"},
	} {
		event := CandleEvent{Exchange: test.exchange, MarketType: "perpetual", Symbol: "BTCUSDT", TS: 60_000, LiquidationOnly: true}
		if !prepareCandleEventForStream(&event, test.stream) {
			t.Errorf("%s event rejected from its stream", test.exchange)
		}
		mismatch := event
		mismatch.Exchange = "bybit"
		if prepareCandleEventForStream(&mismatch, test.stream) {
			t.Errorf("Bybit event accepted from %s stream", test.exchange)
		}
	}
}

func TestParseStreamListDeduplicatesFiveExchanges(t *testing.T) {
	streams := parseStreamList("candles:v3:bybit:perpetual, candles:v3:binance:perpetual, liquidations:v3:okx:perpetual, liquidations:v3:bitget:perpetual, liquidations:v3:gateio:perpetual, candles:v3:bybit:perpetual")
	if len(streams) != 5 || streams[0] != "candles:v3:bybit:perpetual" || streams[4] != "liquidations:v3:gateio:perpetual" {
		t.Fatalf("streams = %#v", streams)
	}
}

func TestImportantEventBroadcastIsGlobalAndVersioned(t *testing.T) {
	event := importantEvent{EventType: "price_shock", Family: "price_shock", Exchange: "binance", MarketType: "perpetual", Symbol: "BTCUSDT", Direction: "up", EventAt: time.Unix(1_800_000_000, 0).UTC(), Metadata: map[string]any{"factorNames": []string{"price", "volume"}}}
	payload := importantEventBroadcastPayload("event-id", "updated", 3, event)
	if payload["audience"] != "all" || payload["userId"] != int64(0) || payload["operation"] != "updated" || payload["occurrenceCount"] != 3 {
		t.Fatalf("unexpected global payload: %#v", payload)
	}
}

func TestImportantSampleFreshness(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	if !importantSampleFresh(now.Add(-time.Minute).UnixMilli(), now) {
		t.Fatal("recent closed candle must be accepted")
	}
	if importantSampleFresh(now.Add(-importantMaxSampleAge-time.Second).UnixMilli(), now) {
		t.Fatal("stale candle must be rejected")
	}
}

func TestSignalDeliveryRetryContinuesOnlyPendingSideEffects(t *testing.T) {
	publish, telegram := pendingSignalDeliverySteps(false, false)
	if !publish || !telegram {
		t.Fatal("new delivery must publish to stream and process Telegram")
	}
	publish, telegram = pendingSignalDeliverySteps(true, false)
	if publish || !telegram {
		t.Fatal("retry after Redis success must skip Redis and continue Telegram")
	}
	publish, telegram = pendingSignalDeliverySteps(true, true)
	if publish || telegram {
		t.Fatal("completed delivery must not repeat side effects")
	}
}

func TestSignalDeliveryIDIsIncludedForConsumerDeduplication(t *testing.T) {
	raw, err := json.Marshal(AlertEvent{DeliveryID: 42, UserID: 7, RuleID: 9, Symbol: "BTCUSDT"})
	if err != nil {
		t.Fatalf("marshal alert: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode alert: %v", err)
	}
	if payload["deliveryId"] != float64(42) {
		t.Fatalf("deliveryId = %#v, want 42", payload["deliveryId"])
	}
}

func TestCombinedLiquidationAlertCarriesVenueBreakdown(t *testing.T) {
	raw, err := json.Marshal(AlertEvent{
		Liquidations:        290,
		BybitLiquidations:   120,
		BinanceLiquidations: 80,
		OKXLiquidations:     40,
		BitgetLiquidations:  30,
		GateIOLiquidations:  20,
		CombinedExchanges:   true,
	})
	if err != nil {
		t.Fatalf("marshal alert: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode alert: %v", err)
	}
	if payload["liquidationsUsd"] != float64(290) ||
		payload["bybitLiquidationsUsd"] != float64(120) ||
		payload["binanceLiquidationsUsd"] != float64(80) ||
		payload["okxLiquidationsUsd"] != float64(40) ||
		payload["bitgetLiquidationsUsd"] != float64(30) ||
		payload["gateioLiquidationsUsd"] != float64(20) ||
		payload["combinedExchanges"] != true {
		t.Fatalf("combined liquidation payload = %#v", payload)
	}
}

func TestImportantEventRussianCurrencyUnitsAreReadable(t *testing.T) {
	if got := formatUSD(1_500_000); got != "$1.50 млн" {
		t.Fatalf("formatUSD million = %q", got)
	}
	if got := formatUSD(2_500); got != "$2 тыс." {
		t.Fatalf("formatUSD thousand = %q", got)
	}
	if got := exchangeLabel("multiple"); got != "Binance и Bybit" {
		t.Fatalf("multiple exchange label = %q", got)
	}
}
