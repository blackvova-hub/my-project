package marketapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLiquidationHistoryPreservesObservedEventsAndVenueScope(t *testing.T) {
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	cache := newLiquidationCache()
	at := now.Add(-time.Minute).UnixMilli()
	cache.set("BTCUSDT", "bybit", at, 10)
	cache.set("BTCUSDT", "bybit", at, 12)
	cache.set("BTCUSDT", "binance", at, 20)
	cache.set("BTCUSDT", "bybit", now.Add(-25*time.Hour).UnixMilli(), 100)
	cache.set("BTCUSDT", "bybit", now.Add(time.Minute).UnixMilli(), 100)
	cache.set("ETHUSDT", "bybit", at, 100)
	all := cache.history("BTCUSDT", "all", now)
	if len(all) != 1 || all[0].Time != at/1000 || all[0].Value != 32 {
		t.Fatalf("history=%+v", all)
	}
	own := cache.history("BTCUSDT", "bybit", now)
	if len(own) != 1 || own[0].Value != 12 {
		t.Fatalf("venue history=%+v", own)
	}
	if len(cache.history("SOLUSDT", "all", now)) != 0 {
		t.Fatal("invented missing events")
	}
}
func TestLiquidationHistoryRejectsInvalidParameters(t *testing.T) {
	api := &API{liqCache: newLiquidationCache()}
	for _, url := range []string{"/market/liquidations/history?symbol=bad!&exchange=bybit", "/market/liquidations/history?symbol=BTCUSDT&exchange=unknown"} {
		response := httptest.NewRecorder()
		api.handleLiquidationHistory(response, httptest.NewRequest("GET", url, nil))
		if response.Code != 400 {
			t.Fatalf("status=%d", response.Code)
		}
	}
}
