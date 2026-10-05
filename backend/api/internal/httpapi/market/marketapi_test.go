package marketapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchBinanceDistributionAndMovers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fapi/v1/ticker/24hr" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"symbol":"BTCUSDT","priceChangePercent":"2.5","lastPrice":"65000","quoteVolume":"1000"},
			{"symbol":"ETHUSDT","priceChangePercent":"-1.25","lastPrice":"3500","quoteVolume":"900"},
			{"symbol":"BTCUSDC","priceChangePercent":"99","lastPrice":"65000","quoteVolume":"2000"}
		]`))
	}))
	defer server.Close()
	t.Setenv("BINANCE_FUTURES_BASE_URL", server.URL)

	a := &API{httpClient: server.Client()}
	trend, movers, err := a.fetchBinanceDistributionAndMovers(context.Background(), true)
	if err != nil {
		t.Fatalf("fetch Binance market overview: %v", err)
	}
	if trend.Total != 2 || trend.Up != 1 || trend.Down != 1 {
		t.Fatalf("unexpected trend: %+v", trend)
	}
	if len(movers) != 2 || movers[0].Symbol != "BTCUSDT" || movers[0].ChangePct != 2.5 {
		t.Fatalf("unexpected movers: %+v", movers)
	}
}

func TestMergeSeriesSeedsRealPreviousValuesFromProviderChanges(t *testing.T) {
	a := &API{}
	capSeries, volumeSeries := a.mergeSeries(1_100, 450, 10, true, -10, true)

	if len(capSeries) != 2 || math.Abs(capSeries[0]-1_000) > 0.0001 || capSeries[1] != 1_100 {
		t.Fatalf("unexpected market cap series: %#v", capSeries)
	}
	if len(volumeSeries) != 2 || math.Abs(volumeSeries[0]-500) > 0.0001 || volumeSeries[1] != 450 {
		t.Fatalf("unexpected volume series: %#v", volumeSeries)
	}
}

func TestValueBeforeChangeRejectsInvalidDenominator(t *testing.T) {
	if _, ok := valueBeforeChange(100, -100); ok {
		t.Fatal("expected -100% change to be rejected")
	}
}

func TestWriteJSONDoesNotReturnEmptySuccessOnEncodingError(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusOK, map[string]float64{"value": math.NaN()})

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "market_overview_encode_failed") {
		t.Fatalf("expected encoding error body, got %q", recorder.Body.String())
	}
}

func TestOpenInterestChangeUsesLatestPointBeforeTarget(t *testing.T) {
	target := time.UnixMilli(2_000_000)
	points := []openInterestPoint{
		{OpenInterest: "90", Timestamp: "1999999"},
		{OpenInterest: "80", Timestamp: "1900000"},
		{OpenInterest: "999", Timestamp: "2000001"},
	}
	change := openInterestChange(100, points, target)
	if change == nil || math.Abs(*change-11.111111) > 0.0001 {
		t.Fatalf("unexpected OI change: %v", change)
	}
}

func TestPopulateBinanceDerivativesUsesBinanceMarketData(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/fapi/v1/premiumIndex":
			_, _ = w.Write([]byte(`{"markPrice":"2","lastFundingRate":"0.0001","nextFundingTime":123456789}`))
		case "/fapi/v1/openInterest":
			_, _ = w.Write([]byte(`{"openInterest":"100"}`))
		case "/futures/data/openInterestHist":
			_, _ = fmt.Fprintf(w, `[
				{"sumOpenInterest":"50","timestamp":%d},
				{"sumOpenInterest":"80","timestamp":%d},
				{"sumOpenInterest":"90","timestamp":%d}
			]`, now.Add(-25*time.Hour).UnixMilli(), now.Add(-5*time.Hour).UnixMilli(), now.Add(-2*time.Hour).UnixMilli())
		default:
			t.Fatalf("unexpected Binance path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("BINANCE_FUTURES_BASE_URL", server.URL)

	a := &API{httpClient: server.Client()}
	result := derivativesDTO{Source: "Binance"}
	a.populateBinanceDerivatives(context.Background(), &result, "ARBUSDT", now)

	if result.OpenInterest.USD != 200 {
		t.Fatalf("unexpected Binance OI USD: %v", result.OpenInterest.USD)
	}
	if result.OpenInterest.Change1h == nil || math.Abs(*result.OpenInterest.Change1h-11.111111) > 0.0001 {
		t.Fatalf("unexpected Binance OI 1h change: %v", result.OpenInterest.Change1h)
	}
	if result.OpenInterest.Change4h == nil || math.Abs(*result.OpenInterest.Change4h-25) > 0.0001 {
		t.Fatalf("unexpected Binance OI 4h change: %v", result.OpenInterest.Change4h)
	}
	if result.OpenInterest.Change24h == nil || math.Abs(*result.OpenInterest.Change24h-100) > 0.0001 {
		t.Fatalf("unexpected Binance OI 24h change: %v", result.OpenInterest.Change24h)
	}
	if result.Funding.Rate == nil || *result.Funding.Rate != 0.0001 || result.Funding.NextFundingTime != 123456789 {
		t.Fatalf("unexpected Binance funding: %+v", result.Funding)
	}
}

func TestHandleDerivativesRejectsUnknownExchange(t *testing.T) {
	a := &API{liqCache: newLiquidationCache()}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/market/derivatives?symbol=BTCUSDT&exchange=unknown", nil)
	a.handleDerivatives(recorder, request)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid_exchange") {
		t.Fatalf("expected invalid exchange response, got %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestLiquidationCacheAggregatesAndSortsVenues(t *testing.T) {
	now := time.UnixMilli(2_000_000_000_000)
	cache := newLiquidationCache()
	cache.set("BTCUSDT", "bybit", now.Add(-time.Hour).UnixMilli(), 25)
	cache.set("BTCUSDT", "okx", now.Add(-2*time.Hour).UnixMilli(), 75)
	cache.set("BTCUSDT", "binance", now.Add(-25*time.Hour).UnixMilli(), 1000)

	total, venues := cache.totals("BTCUSDT", now)
	if total != 100 || len(venues) != 2 {
		t.Fatalf("unexpected liquidation total=%v venues=%+v", total, venues)
	}
	if venues[0].Exchange != "okx" || venues[0].USD != 75 {
		t.Fatalf("venues are not sorted descending: %+v", venues)
	}
}

func TestObserveCompactLiquidationAndCandleBatches(t *testing.T) {
	a := &API{liqCache: newLiquidationCache()}
	a.observeLiquidationBatch(compactMarketBatch{
		Kind: "l", Exchange: "okx", Minute: 1_900_000,
		LiquidationRows: []compactLiquidationRow{{Symbol: "BTCUSDT", Values: []float64{40, 10, 30}}},
	})
	values := make([]float64, 36)
	values[35] = 60
	a.observeLiquidationBatch(compactMarketBatch{
		Kind: "c", Exchange: "bybit", Minute: 1_960_000,
		CandleRows: []compactMarketRow{{Symbol: "BTCUSDT", Values: values}},
	})

	total, venues := a.liqCache.totals("BTCUSDT", time.UnixMilli(2_000_000))
	if total != 100 || len(venues) != 2 {
		t.Fatalf("unexpected compact liquidation aggregate total=%v venues=%+v", total, venues)
	}
}
