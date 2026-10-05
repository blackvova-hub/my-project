package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type failingCompactPublisher struct{}

func (failingCompactPublisher) Publish(context.Context, []byte) error {
	return errors.New("compact unavailable")
}
func (failingCompactPublisher) Close() error { return nil }

func TestCompactTransportIsTheProductionDefault(t *testing.T) {
	if cfg := loadConfig(); cfg.CompactRedisStream == "" {
		t.Fatal("production compact stream must always be configured")
	}
}

func TestCompactRetentionCutoffUsesRedisServerTime(t *testing.T) {
	got, err := retentionMinID("1700000000000-7", 26*time.Hour)
	if err != nil {
		t.Fatalf("retention min id: %v", err)
	}
	if want := "1699906400000-0"; got != want {
		t.Fatalf("retention min id=%s, want %s", got, want)
	}
	if _, err := retentionMinID("invalid", 26*time.Hour); err == nil {
		t.Fatal("invalid Redis stream id was accepted")
	}
}

func TestCompactCandleBatchRoundTripPreservesValuesAndValidity(t *testing.T) {
	if len(compactMetricLayout) == 0 || len(compactMetricLayout) > 64 {
		t.Fatalf("metric layout length=%d must fit uint64 validity mask", len(compactMetricLayout))
	}
	out := CandleOut{
		SchemaVersion: schemaVersion, Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT",
		TS: 1_800_000, ShardIndex: 0, ShardTotal: 2, OpenInterestTimestamp: 1_799_000,
		Metrics: make(map[string]MetricValue, len(compactMetricLayout)),
	}
	for index, name := range compactMetricLayout {
		value := float64(index) + 0.25
		setCompactMetricValue(&out, name, value)
		out.Metrics[name] = MetricValue{Value: value, Valid: index%7 != 0, Timestamp: out.TS, Source: "test"}
	}
	batch, err := buildCompactCandleBatch([]CandleOut{out}, []string{"BTCUSDT", "ETHUSDT"}, out.TS, 0, 2, "bybit", "perpetual")
	if err != nil {
		t.Fatalf("build compact batch: %v", err)
	}
	if batch.TransportVersion != compactTransportVersion || batch.CanonicalVersion != schemaVersion || batch.Coverage {
		t.Fatalf("unexpected batch header: %+v", batch)
	}
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal compact batch: %v", err)
	}
	encoded := string(raw)
	for _, forbidden := range []string{"schemaVersion", "metrics", "source", "timestamp"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("compact payload repeats %q: %s", forbidden, encoded)
		}
	}
	expanded, err := expandCompactCandleBatch(batch)
	if err != nil {
		t.Fatalf("expand compact batch: %v", err)
	}
	if len(expanded) != 1 || expanded[0].OpenInterestTimestamp != out.OpenInterestTimestamp {
		t.Fatalf("expanded=%+v", expanded)
	}
	for index, name := range compactMetricLayout {
		got := compactMetricValue(expanded[0], name)
		want := float64(index) + 0.25
		if got != want {
			t.Fatalf("metric %s=%v, want %v", name, got, want)
		}
		if expanded[0].Metrics[name].Valid != (index%7 != 0) {
			t.Fatalf("metric %s validity=%v", name, expanded[0].Metrics[name].Valid)
		}
	}
}

func TestCompactLiquidationBatchDistinguishesZeroFromMissing(t *testing.T) {
	catalog := []string{"BTCUSDT", "ETHUSDT"}
	rows := []compactLiquidationRow{{Symbol: "BTCUSDT", Values: [5]float64{100, 70, 30, 4, 50}}}
	covered, err := buildCompactLiquidationBatch(rows, catalog, 1_800_000, true, "okx:test", "okx", "perpetual")
	if err != nil {
		t.Fatalf("build covered batch: %v", err)
	}
	expanded, err := expandCompactLiquidationBatch(covered)
	if err != nil {
		t.Fatalf("expand covered batch: %v", err)
	}
	if len(expanded) != 2 || expanded[1].Symbol != "ETHUSDT" || expanded[1].Liquidations != 0 || !expanded[1].Metrics["liquidations"].Valid {
		t.Fatalf("covered zero was not reconstructed: %+v", expanded)
	}
	missing, err := buildCompactLiquidationBatch(rows, catalog, 1_800_000, false, "okx:test", "okx", "perpetual")
	if err != nil {
		t.Fatalf("build uncovered batch: %v", err)
	}
	expanded, err = expandCompactLiquidationBatch(missing)
	if err != nil {
		t.Fatalf("expand uncovered batch: %v", err)
	}
	for _, event := range expanded {
		if event.Metrics["liquidations"].Valid {
			t.Fatalf("coverage=false symbol=%s must be invalid", event.Symbol)
		}
	}
}

func TestCompactPublishFailureFailsLiquidationMinute(t *testing.T) {
	feed := testLiquidationFeed{exchange: ExchangeOKX}
	catalog := newLiquidationCatalog([]liquidationInstrument{{CanonicalSymbol: "BTCUSDT", NativeSymbol: "BTC-USDT-SWAP", ContractValue: 1}})
	store := NewLiquidationStore()
	store.SetConnected(true)
	minute := time.UnixMilli(store.connectedAtMs).UTC().Truncate(time.Minute).Add(time.Minute).UnixMilli()
	if err := publishLiquidationMinute(context.Background(), failingCompactPublisher{}, feed, catalog, store, minute); err == nil {
		t.Fatal("production compact publish failure was ignored")
	}
}
