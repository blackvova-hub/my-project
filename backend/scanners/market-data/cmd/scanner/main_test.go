package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type recordingPublisher struct {
	events [][]byte
}

func (p *recordingPublisher) Publish(_ context.Context, event []byte) error {
	p.events = append(p.events, append([]byte(nil), event...))
	return nil
}

func (p *recordingPublisher) Close() error { return nil }

func TestLiquidationSidesMatchBybitSemantics(t *testing.T) {
	store := NewLiquidationStore()
	minute := int64(1_800_000)
	key := NewInstrumentKey(ExchangeBybit, MarketTypePerpetual, "BTCUSDT")

	store.Add(key, minute, 100, "Buy")
	store.Add(key, minute+1, 40, "Sell")

	bucket := store.Get(key, minute)
	if bucket.Total != 140 {
		t.Fatalf("total = %v, want 140", bucket.Total)
	}
	if bucket.Long != 100 {
		t.Fatalf("long liquidations = %v, want 100 for Bybit Buy side", bucket.Long)
	}
	if bucket.Short != 40 {
		t.Fatalf("short liquidations = %v, want 40 for Bybit Sell side", bucket.Short)
	}
}

func TestTradeWindowCompleteness(t *testing.T) {
	const (
		start = int64(1_800_000)
		limit = 1000
	)
	tests := []struct {
		name   string
		count  int
		oldest int64
		want   bool
	}{
		{name: "response below limit is complete", count: 50, oldest: start + 10, want: true},
		{name: "full response reaches prior minute", count: limit, oldest: start - 1, want: true},
		{name: "full response stays inside minute", count: limit, oldest: start + 1, want: false},
		{name: "boundary trade alone is not proof", count: limit, oldest: start, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTradeWindowComplete(tt.count, limit, tt.oldest, start); got != tt.want {
				t.Fatalf("isTradeWindowComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBybitRESTRecoveryOrdersTradesBeforeBuildingCluster(t *testing.T) {
	const minute = int64(1_800_000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/market/recent-trade" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[
			{"symbol":"BTCUSDT","price":"102","size":"3","side":"Buy","time":"1802500"},
			{"symbol":"BTCUSDT","price":"101","size":"2","side":"Buy","time":"1801000"},
			{"symbol":"BTCUSDT","price":"100","size":"1","side":"Buy","time":"1800100"}
		]}}`))
	}))
	defer server.Close()
	client := NewBybitClient(Config{BaseURL: server.URL, Category: "linear", RPS: 100, Burst: 100, HTTPTimeout: time.Second})
	stats, err := client.FetchDeltaForMinute(context.Background(), "BTCUSDT", minute, 1000)
	if err != nil {
		t.Fatalf("FetchDeltaForMinute() error = %v", err)
	}
	if !stats.Complete || stats.ClusterUSD != 608 || stats.ClusterCount != 3 || math.Abs(stats.PriceImpactPct-2) > 0.000001 {
		t.Fatalf("REST recovery did not preserve chronological cluster semantics: %+v", stats)
	}
	if stats.MedianUSD != 202 || stats.LargestSide != 1 {
		t.Fatalf("REST recovery did not build trade-size aggregates: %+v", stats)
	}
}

func TestLiquidationConnectionState(t *testing.T) {
	store := NewLiquidationStore()
	if store.IsConnected() {
		t.Fatal("new store must start disconnected")
	}
	store.SetConnected(true)
	if !store.IsConnected() {
		t.Fatal("store must report connected after SetConnected(true)")
	}
	if store.CoversMinute(0) {
		t.Fatal("connection established mid-run must not claim coverage of an older minute")
	}
	if !store.CoversMinute(store.connectedAtMs) {
		t.Fatal("connection must cover minutes beginning at or after its connection time")
	}
}

func TestBinanceStreamWaitsForAckAndStoresPerSymbolLiquidation(t *testing.T) {
	const liquidationTime = int64(1_800_010)
	serverErrors := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer conn.Close()
		var request struct {
			Method string   `json:"method"`
			Params []string `json:"params"`
			ID     int64    `json:"id"`
		}
		if err := conn.ReadJSON(&request); err != nil {
			serverErrors <- err
			return
		}
		if request.Method != "SUBSCRIBE" || request.ID == 0 ||
			!containsString(request.Params, "btcusdt@aggTrade") ||
			!containsString(request.Params, "btcusdt@forceOrder") ||
			containsString(request.Params, "!forceOrder@arr") {
			serverErrors <- fmt.Errorf("unexpected Binance subscription: %+v", request)
			return
		}
		if err := conn.WriteJSON(map[string]any{"result": nil, "id": request.ID}); err != nil {
			serverErrors <- err
			return
		}
		if err := conn.WriteJSON(map[string]any{
			"e": "forceOrder",
			"o": map[string]any{
				"s": "BTCUSDT", "S": "SELL", "q": "2", "z": "2", "ap": "100", "p": "99", "T": liquidationTime,
			},
		}); err != nil {
			serverErrors <- err
			return
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	adapter := NewBinanceAdapter(Config{
		Exchange: string(ExchangeBinance), MarketType: string(MarketTypePerpetual),
		WSURL: "ws" + strings.TrimPrefix(server.URL, "http"), HTTPTimeout: time.Second,
	})
	trades := NewTradeStore()
	liquidations := NewLiquidationStore()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- adapter.RunStreams(ctx, []string{"BTCUSDT"}, trades, liquidations)
	}()

	deadline := time.After(2 * time.Second)
	for {
		bucket := liquidations.Get(adapter.Instrument("BTCUSDT"), liquidationTime)
		if bucket.Total == 200 && bucket.Long == 200 && bucket.Short == 0 {
			break
		}
		select {
		case err := <-serverErrors:
			cancel()
			t.Fatalf("test websocket server: %v", err)
		case err := <-errCh:
			cancel()
			t.Fatalf("RunStreams() returned before liquidation: %v", err)
		case <-deadline:
			cancel()
			t.Fatalf("Binance liquidation was not stored: %+v", bucket)
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(time.Second):
		t.Fatal("RunStreams() did not stop after cancellation")
	}
}

func TestParseBinanceAllMarketLiquidationArray(t *testing.T) {
	events := parseBinanceForceOrderEvents([]byte(`[
		{"e":"forceOrder","o":{"s":"BTCUSDT","S":"SELL","q":"2","z":"2","ap":"100","T":1800010}},
		{"e":"forceOrder","o":{"s":"ETHUSDT","S":"BUY","q":"3","z":"3","ap":"50","T":1800020}}
	]`))
	if len(events) != 2 || events[0].Order.Symbol != "BTCUSDT" || events[1].Order.Symbol != "ETHUSDT" {
		t.Fatalf("all-market force-order events = %+v", events)
	}
}

func TestParseBinanceForceOrderWithoutInnerEventType(t *testing.T) {
	events := parseBinanceForceOrderEvents([]byte(
		`{"o":{"s":"BTCUSDT","S":"SELL","q":"2","z":"2","ap":"100","T":1800010}}`,
	))
	if len(events) != 1 || events[0].Order.Symbol != "BTCUSDT" {
		t.Fatalf("force-order event without inner type = %+v", events)
	}
}

func TestParseBinanceForceOrderWithEventTime(t *testing.T) {
	events := parseBinanceForceOrderEvents([]byte(
		`{"e":"forceOrder","E":1785072908343,"o":{"s":"BTCUSDT","S":"SELL","q":"2","z":"2","ap":"100","T":1785072908340}}`,
	))
	if len(events) != 1 {
		t.Fatalf("force-order events = %+v", events)
	}
	if events[0].Event != "forceOrder" || events[0].EventTime != 1785072908343 {
		t.Fatalf("force-order event metadata = %+v", events[0])
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestScanOncePublishesEverySuccessfulBufferedResult(t *testing.T) {
	const targetMinuteMs = int64(1_800_000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v5/market/kline":
			_, _ = fmt.Fprintf(w, `{"retCode":0,"result":{"list":[["%d","1","2","0.5","1.5","10","15"]]}}`, targetMinuteMs)
		case "/v5/market/recent-trade":
			_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[]}}`))
		case "/v5/market/open-interest":
			_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[{"openInterest":"10","timestamp":"1800000"}]}}`))
		case "/v5/market/account-ratio":
			_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[{"buyRatio":"0.5","sellRatio":"0.5"}]}}`))
		case "/v5/market/tickers":
			_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[{"markPrice":"1.5","indexPrice":"1.5","fundingRate":"0.0001"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := Config{
		Exchange:         string(ExchangeBybit),
		MarketType:       string(MarketTypePerpetual),
		ShardTotal:       1,
		BaseURL:          server.URL,
		Category:         "linear",
		Workers:          24,
		RPS:              10000,
		Burst:            10000,
		TradeLimit:       1000,
		OIInterval:       "5min",
		HTTPTimeout:      2 * time.Second,
		OrderbookEnabled: false,
	}
	adapter := NewBybitAdapter(cfg)
	pub := &recordingPublisher{}
	symbols := make([]string, 60)
	for i := range symbols {
		symbols[i] = fmt.Sprintf("COIN%dUSDT", i)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := scanOnce(ctx, cfg, adapter, pub, NewCVDStore(), NewTradeStore(), nil, NewRatioStore(), symbols, targetMinuteMs)
	if err != nil {
		t.Fatalf("scanOnce() error = %v", err)
	}
	if len(pub.events) != 1 {
		t.Fatalf("compact batches = %d, want 1", len(pub.events))
	}
	var compact compactBatch
	if err := json.Unmarshal(pub.events[0], &compact); err != nil {
		t.Fatalf("decode compact batch: %v", err)
	}
	if !compact.Coverage || len(compact.CandleRows) != len(symbols) || len(compact.Catalog) != len(symbols) {
		t.Fatalf("incomplete compact batch: coverage=%v rows=%d catalog=%d", compact.Coverage, len(compact.CandleRows), len(compact.Catalog))
	}
	outputs, err := expandCompactCandleBatch(compact)
	if err != nil || len(outputs) != len(symbols) {
		t.Fatalf("expand compact batch: outputs=%d err=%v", len(outputs), err)
	}
	output := outputs[0]
	if output.SchemaVersion != schemaVersion || output.Exchange != "bybit" || output.MarketType != "perpetual" {
		t.Fatalf("missing canonical identity: %+v", output)
	}
	if metric := output.Metrics["markPrice"]; !metric.Valid || metric.Value != 1.5 {
		t.Fatalf("invalid metric metadata: %+v", metric)
	}
	if output.QuoteVolumeUsd != 15 {
		t.Fatalf("native Bybit quote volume = %v, want 15", output.QuoteVolumeUsd)
	}
	if metric := output.Metrics["openInterest"]; !metric.Valid || output.OpenInterestTimestamp != targetMinuteMs {
		t.Fatalf("Bybit OI source timestamp = %d metric=%+v, want %d", output.OpenInterestTimestamp, metric, targetMinuteMs)
	}
}

func TestBinanceAdapterPublishesCanonicalMetrics(t *testing.T) {
	const targetMinuteMs = int64(1_800_000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/fapi/v1/klines":
			_, _ = fmt.Fprintf(w, `[[%d,"100","110","90","105","20",0,"2100",0,0,0,0]]`, targetMinuteMs)
		case "/fapi/v1/aggTrades":
			_, _ = w.Write([]byte(`[{"a":1,"p":"105","q":"2","f":10,"l":11,"T":1800010,"m":false}]`))
		case "/futures/data/openInterestHist":
			_, _ = w.Write([]byte(`[{"sumOpenInterest":"1234","timestamp":1800000}]`))
		case "/futures/data/globalLongShortAccountRatio":
			_, _ = w.Write([]byte(`[{"longAccount":"0.55","shortAccount":"0.45","timestamp":1800000}]`))
		case "/fapi/v1/depth":
			_, _ = w.Write([]byte(`{"bids":[["104","3"]],"asks":[["106","4"]]}`))
		case "/fapi/v1/premiumIndex":
			_, _ = w.Write([]byte(`[{"symbol":"BTCUSDT","markPrice":"105.5","indexPrice":"105.4","lastFundingRate":"0.0002","time":1800050}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := Config{
		Exchange:         string(ExchangeBinance),
		MarketType:       string(MarketTypePerpetual),
		BaseURL:          server.URL,
		Workers:          1,
		RPS:              1000,
		Burst:            1000,
		TradeLimit:       1000,
		OIInterval:       "5min",
		HTTPTimeout:      2 * time.Second,
		OrderbookEnabled: true,
	}
	adapter := NewBinanceAdapter(cfg)
	trades := NewTradeStore()
	trades.connected = true
	trades.connectedAtMs = targetMinuteMs
	trades.Add(adapter.Instrument("BTCUSDT"), targetMinuteMs+10, 105, 2, "Buy", 2)
	output, err := fetchMetricsOnce(context.Background(), cfg, adapter, NewCVDStore(), trades, nil, NewRatioStore(), nil, "BTCUSDT", targetMinuteMs)
	if err != nil {
		t.Fatalf("fetchMetricsOnce() error = %v", err)
	}
	if output.Instrument.String() != "binance:perpetual:BTCUSDT" {
		t.Fatalf("instrument = %q", output.Instrument.String())
	}
	if output.QuoteVolumeUsd != 2100 || output.Metrics["quoteVolumeUsd"].Value != 2100 {
		t.Fatalf("native Binance quote volume was not preserved: %+v", output.Metrics["quoteVolumeUsd"])
	}
	if output.Metrics["openInterest"].Timestamp != targetMinuteMs || output.OpenInterestTimestamp != targetMinuteMs {
		t.Fatalf("Binance OI source timestamp was not preserved: metric=%+v flat=%d", output.Metrics["openInterest"], output.OpenInterestTimestamp)
	}
	for _, name := range []string{"open", "close", "volume", "delta", "cvd", "openInterest", "fundingRate", "markPrice", "indexPrice", "orderbookSpread", "longRatio", "shortRatio"} {
		metric, ok := output.Metrics[name]
		if !ok || !metric.Valid || metric.Timestamp <= 0 || !strings.HasPrefix(metric.Source, "binance:") {
			t.Fatalf("metric %s = %+v", name, metric)
		}
	}
	if metric := output.Metrics["liquidations"]; metric.Valid || metric.Source != "binance:websocket_snapshot" {
		t.Fatalf("binance liquidation provenance = %+v", metric)
	}
}

func TestBybitSymbolDiscoveryKeepsOnlyPerpetuals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[{"symbol":"BTCUSDT","status":"Trading","contractType":"LinearPerpetual"},{"symbol":"BTCUSDT17JUL26","status":"Trading","contractType":"LinearFutures"}]}}`))
	}))
	defer server.Close()
	adapter := NewBybitAdapter(Config{
		Exchange: "bybit", MarketType: "perpetual", BaseURL: server.URL, Category: "linear",
		RPS: 100, Burst: 100, HTTPTimeout: time.Second,
	})
	symbols, err := adapter.FetchAllSymbolsFromAPI(context.Background())
	if err != nil {
		t.Fatalf("FetchAllSymbolsFromAPI() error = %v", err)
	}
	if len(symbols) != 1 || symbols[0] != "BTCUSDT" {
		t.Fatalf("symbols = %v, want only BTCUSDT perpetual", symbols)
	}
}

func TestBinanceRetryDeadlineUsesBanTimestamp(t *testing.T) {
	want := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Millisecond)
	body := fmt.Sprintf(`{"code":-1003,"msg":"IP banned until %d"}`, want.UnixMilli())
	got := binanceRetryDeadline(http.StatusTeapot, "", body)
	if got.Before(want) {
		t.Fatalf("deadline = %s, want at least %s", got, want)
	}
}

func TestBinancePremiumIndexUsesSingleBulkSnapshot(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fapi/v1/premiumIndex" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"symbol":"BTCUSDT","markPrice":"100","indexPrice":"99","lastFundingRate":"0.001"},{"symbol":"ETHUSDT","markPrice":"50","indexPrice":"49","lastFundingRate":"0.002"}]`))
	}))
	defer server.Close()
	adapter := NewBinanceAdapter(Config{
		Exchange: "binance", MarketType: "perpetual", BaseURL: server.URL,
		RPS: 100, Burst: 100, HTTPTimeout: time.Second,
	})
	if _, _, _, err := adapter.FetchTickers(context.Background(), "BTCUSDT"); err != nil {
		t.Fatalf("BTC ticker: %v", err)
	}
	if _, _, _, err := adapter.FetchTickers(context.Background(), "ETHUSDT"); err != nil {
		t.Fatalf("ETH ticker: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("premium index calls = %d, want one bulk request", got)
	}
}

func TestBinanceAdapterRejectsNonFiniteExchangeData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[[1800000,"NaN","110","90","105","20",0,0,0,0,0,0]]`))
	}))
	defer server.Close()
	adapter := NewBinanceAdapter(Config{
		Exchange: "binance", MarketType: "perpetual", BaseURL: server.URL,
		RPS: 100, Burst: 100, HTTPTimeout: time.Second,
	})
	if _, _, _, _, _, _, _, err := adapter.FetchKline1m(context.Background(), "BTCUSDT", 1_800_000); err == nil {
		t.Fatal("non-finite Binance kline value must be rejected")
	}
}

func TestInstrumentKeySeparatesExchangesAndMarkets(t *testing.T) {
	bybit := NewInstrumentKey(ExchangeBybit, MarketTypePerpetual, "btcusdt")
	binance := NewInstrumentKey(ExchangeBinance, MarketTypePerpetual, "BTCUSDT")
	spot := NewInstrumentKey(ExchangeBybit, MarketTypeSpot, "BTCUSDT")
	if bybit.String() == binance.String() || bybit.String() == spot.String() {
		t.Fatalf("instrument keys must be venue-specific: %q %q %q", bybit, binance, spot)
	}
}

func TestAdapterFactoryRejectsUnsupportedVenueAndMarket(t *testing.T) {
	for _, cfg := range []Config{
		{Exchange: "kraken", MarketType: "perpetual"},
		{Exchange: "bybit", MarketType: "spot"},
		{Exchange: "binance", MarketType: "spot"},
	} {
		if _, err := newMarketAdapter(cfg); err == nil {
			t.Fatalf("newMarketAdapter(%+v) unexpectedly succeeded", cfg)
		}
	}
}

func TestTradeStoreSeparatesExchanges(t *testing.T) {
	store := NewTradeStore()
	minute := int64(1_800_000)
	bybit := NewInstrumentKey(ExchangeBybit, MarketTypePerpetual, "BTCUSDT")
	binance := NewInstrumentKey(ExchangeBinance, MarketTypePerpetual, "BTCUSDT")
	store.Add(bybit, minute+1, 100, 2, "Buy", 1)
	store.Add(binance, minute+2, 100, 3, "Sell", 1)

	bybitStats, _ := store.Get(bybit, minute)
	binanceStats, _ := store.Get(binance, minute)
	if bybitStats.Delta != 2 || binanceStats.Delta != -3 {
		t.Fatalf("trade data crossed exchanges: bybit=%+v binance=%+v", bybitStats, binanceStats)
	}
}

func TestTradeStoreRequiresAFullMinuteAfterConnect(t *testing.T) {
	store := NewTradeStore()
	minute := time.Now().UTC().Truncate(time.Minute).UnixMilli()
	store.connected = true
	store.connectedAtMs = minute + 30_000
	if store.CoversMinute(minute) {
		t.Fatal("a stream connected mid-minute must not claim full coverage")
	}
	if !store.CoversMinute(minute + ringMinuteMs) {
		t.Fatal("the next complete minute must be covered")
	}
}

func TestDeltaUnavailableIsNotCountedAsTransportOrScannerError(t *testing.T) {
	stats := &ScanStats{}
	stats.addDeltaUnavailable()
	snapshot := stats.snapshot()
	if snapshot.DeltaUnavailable != 1 || snapshot.DeltaErr != 0 || snapshot.ErrOther != 0 {
		t.Fatalf("unexpected warm-up stats: %+v", snapshot)
	}
}

func TestStoresKeepLargestTradeAndLiquidationAggregates(t *testing.T) {
	key := NewInstrumentKey(ExchangeBybit, MarketTypePerpetual, "ETHUSDT")
	minute := int64(1_800_000)
	trades := NewTradeStore()
	trades.Add(key, minute+1, 100, 2, "Buy", 1)
	trades.Add(key, minute+2, 100, 5, "Sell", 1)
	tradeStats, _ := trades.Get(key, minute)
	if tradeStats.LargestUSD != 500 || tradeStats.Count != 2 {
		t.Fatalf("unexpected trade aggregates: %+v", tradeStats)
	}
	liquidations := NewLiquidationStore()
	liquidations.Add(key, minute+1, 100_000, "Buy")
	liquidations.Add(key, minute+2, 250_000, "Sell")
	bucket := liquidations.Get(key, minute)
	if bucket.Total != 350_000 || bucket.Count != 2 || bucket.Largest != 250_000 {
		t.Fatalf("unexpected liquidation aggregates: %+v", bucket)
	}
}

func TestTradeStoreBuildsBoundedAggressiveTradeAggregates(t *testing.T) {
	key := NewInstrumentKey(ExchangeBybit, MarketTypePerpetual, "BTCUSDT")
	minute := int64(1_800_000)
	trades := NewTradeStore()
	trades.Add(key, minute+100, 100, 1, "Buy", 1)    // $100
	trades.Add(key, minute+1_000, 101, 2, "Buy", 1)  // $202
	trades.Add(key, minute+2_500, 102, 3, "Buy", 1)  // $306
	trades.Add(key, minute+2_600, 102, 5, "Sell", 1) // $510
	trades.Add(key, minute+5_800, 103, 1, "Buy", 1)  // separate cluster

	stats, _ := trades.Get(key, minute)
	if stats.LargestUSD != 510 || stats.LargestSide != -1 {
		t.Fatalf("largest public trade observation = (%v,%v), want (510,sell)", stats.LargestUSD, stats.LargestSide)
	}
	if stats.ClusterUSD != 608 || stats.ClusterCount != 3 || stats.ClusterSide != 1 {
		t.Fatalf("largest same-side 3s cluster = usd=%v count=%v side=%v, want 608/3/buy", stats.ClusterUSD, stats.ClusterCount, stats.ClusterSide)
	}
	if math.Abs(stats.PriceImpactPct-2) > 0.000001 {
		t.Fatalf("cluster observed price impact = %v, want 2%%", stats.PriceImpactPct)
	}
	if stats.MedianUSD != 202 {
		t.Fatalf("median public observation size = %v, want 202", stats.MedianUSD)
	}
}

func TestOutOfOrderTradeDoesNotCorruptLastTradeOrCluster(t *testing.T) {
	stats := TradeStats{}
	stats.addTrade(2_000, 102, 1, "Buy", 1)
	stats.addTrade(1_000, 99, 10, "Sell", 1)
	if stats.Timestamp != 2_000 || stats.LastPrice != 102 || stats.LastSide != 1 {
		t.Fatalf("out-of-order observation corrupted latest trade: %+v", stats)
	}
	if stats.ClusterSide != 1 || stats.ClusterUSD != 102 {
		t.Fatalf("out-of-order observation corrupted active cluster: %+v", stats)
	}
}

func TestP2MedianUsesConstantMemoryAndTracksDistribution(t *testing.T) {
	var estimator p2Median
	for value := 1.0; value <= 1001; value++ {
		estimator.Add(value)
	}
	if got := estimator.Value(); math.Abs(got-501) > 5 {
		t.Fatalf("streaming median = %v, want approximately 501", got)
	}
	if len(estimator.initial) != 5 || len(estimator.q) != 5 {
		t.Fatalf("median estimator storage unexpectedly grew: %+v", estimator)
	}
}

func TestBinanceLiquidationSideMapsClosedPosition(t *testing.T) {
	tests := []struct {
		orderSide string
		want      string
		ok        bool
	}{
		{orderSide: "SELL", want: "Buy", ok: true},
		{orderSide: "buy", want: "Sell", ok: true},
		{orderSide: "unknown", ok: false},
	}
	for _, test := range tests {
		got, ok := binanceLiquidationCanonicalSide(test.orderSide)
		if got != test.want || ok != test.ok {
			t.Fatalf("side %q = (%q, %v), want (%q, %v)", test.orderSide, got, ok, test.want, test.ok)
		}
	}
}

func TestRound6NeverLeaksNonFiniteJSONValues(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got := round6(value); got != 0 {
			t.Fatalf("round6(%v) = %v, want 0", value, got)
		}
	}
}

func TestStoresRejectNonFiniteExchangeValues(t *testing.T) {
	key := NewInstrumentKey(ExchangeBinance, MarketTypePerpetual, "BTCUSDT")
	minute := int64(1_800_000)
	trades := NewTradeStore()
	trades.Add(key, minute+1, math.NaN(), 1, "Buy", 1)
	if stats, _ := trades.Get(key, minute); stats.Count != 0 || stats.Delta != 0 {
		t.Fatalf("non-finite trade reached store: %+v", stats)
	}
	liquidations := NewLiquidationStore()
	liquidations.Add(key, minute+1, math.Inf(1), "Buy")
	if bucket := liquidations.Get(key, minute); bucket.Total != 0 {
		t.Fatalf("non-finite liquidation reached store: %+v", bucket)
	}
}

type testLiquidationFeed struct{ exchange Exchange }

func (f testLiquidationFeed) Exchange() Exchange { return f.exchange }
func (f testLiquidationFeed) Source() string     { return string(f.exchange) + ":test" }
func (f testLiquidationFeed) LoadInstruments(context.Context) ([]liquidationInstrument, error) {
	return nil, nil
}
func (f testLiquidationFeed) Consume(context.Context, *liquidationCatalog, *LiquidationStore) error {
	return nil
}

func TestLiquidationOnlyVenueMappings(t *testing.T) {
	if got := normalizeSymbol("BTC_USDT"); got != "BTCUSDT" {
		t.Fatalf("Gate symbol = %q, want BTCUSDT", got)
	}
	okxLong, okxLongOK := okxCanonicalLiquidationSide("long", "sell")
	okxShort, okxShortOK := okxCanonicalLiquidationSide("short", "buy")
	bitgetLong, bitgetLongOK := bitgetCanonicalLiquidationSide("buy")
	gateLong, gateLongOK := gateCanonicalLiquidationSide(-2)
	gateShort, gateShortOK := gateCanonicalLiquidationSide(2)
	tests := []struct {
		name   string
		got    string
		gotOK  bool
		want   string
		wantOK bool
	}{
		{name: "OKX long", got: okxLong, gotOK: okxLongOK, want: "Buy", wantOK: true},
		{name: "OKX short", got: okxShort, gotOK: okxShortOK, want: "Sell", wantOK: true},
		{name: "Bitget long", got: bitgetLong, gotOK: bitgetLongOK, want: "Buy", wantOK: true},
		{name: "Gate forced sell", got: gateLong, gotOK: gateLongOK, want: "Buy", wantOK: true},
		{name: "Gate forced buy", got: gateShort, gotOK: gateShortOK, want: "Sell", wantOK: true},
	}
	for _, test := range tests {
		if test.got != test.want || test.gotOK != test.wantOK {
			t.Errorf("%s = (%q, %v), want (%q, %v)", test.name, test.got, test.gotOK, test.want, test.wantOK)
		}
	}
}

func TestPublishLiquidationOnlyMinuteCarriesOnlyCanonicalLiquidationData(t *testing.T) {
	feed := testLiquidationFeed{exchange: ExchangeOKX}
	catalog := newLiquidationCatalog([]liquidationInstrument{{CanonicalSymbol: "BTCUSDT", NativeSymbol: "BTC-USDT-SWAP", ContractValue: 0.01}})
	store := NewLiquidationStore()
	store.SetConnected(true)
	minute := time.UnixMilli(store.connectedAtMs).UTC().Truncate(time.Minute).Add(time.Minute).UnixMilli()
	store.Add(NewInstrumentKey(ExchangeOKX, MarketTypePerpetual, "BTCUSDT"), minute+1, 250, "Buy")
	publisher := &recordingPublisher{}
	if err := publishLiquidationMinute(context.Background(), publisher, feed, catalog, store, minute); err != nil {
		t.Fatalf("publishLiquidationMinute() error = %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("events = %d, want 1", len(publisher.events))
	}
	var compact compactBatch
	if err := json.Unmarshal(publisher.events[0], &compact); err != nil {
		t.Fatalf("decode compact liquidation batch: %v", err)
	}
	if !compact.Coverage || len(compact.LiquidationRows) != 1 || len(compact.Catalog) != 1 {
		t.Fatalf("invalid compact liquidation batch: %+v", compact)
	}
	events, err := expandCompactLiquidationBatch(compact)
	if err != nil || len(events) != 1 {
		t.Fatalf("expand compact liquidation batch: events=%d err=%v", len(events), err)
	}
	event := events[0]
	metric := event.Metrics["liquidations"]
	if !event.LiquidationOnly || event.Exchange != "okx" || event.Symbol != "BTCUSDT" || event.Liquidations != 250 || !metric.Valid || metric.Source != "okx:test" {
		t.Fatalf("liquidation-only event = %+v metric=%+v", event, metric)
	}
	if event.Close != 0 || event.Delta != 0 || event.OpenInterest != 0 {
		t.Fatalf("liquidation-only event leaked market metrics: %+v", event)
	}
}

func TestPublishLiquidationOnlyMinuteMarksInterruptedCoverageInvalid(t *testing.T) {
	feed := testLiquidationFeed{exchange: ExchangeBitget}
	catalog := newLiquidationCatalog([]liquidationInstrument{{CanonicalSymbol: "ETHUSDT", NativeSymbol: "ETHUSDT", ContractValue: 1}})
	publisher := &recordingPublisher{}
	if err := publishLiquidationMinute(context.Background(), publisher, feed, catalog, NewLiquidationStore(), 1_800_000); err != nil {
		t.Fatalf("publishLiquidationMinute() error = %v", err)
	}
	var batch compactBatch
	if err := json.Unmarshal(publisher.events[0], &batch); err != nil {
		t.Fatalf("decode batch: %v", err)
	}
	events, err := expandCompactLiquidationBatch(batch)
	if err != nil || len(events) != 1 {
		t.Fatalf("expand batch: events=%d err=%v", len(events), err)
	}
	event := events[0]
	if event.Metrics["liquidations"].Valid || !containsString(event.InvalidMetrics, "liquidations") {
		t.Fatalf("interrupted minute must be invalid: %+v", event)
	}
}
