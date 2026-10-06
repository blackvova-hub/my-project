package service

import (
	"encoding/json"
	"net/http/httptest"
	"shortlong/arbitrage/internal/engine"
	"shortlong/arbitrage/internal/market"
	"shortlong/arbitrage/internal/verification"
	"testing"
	"time"
)

func TestVerificationUsesFreshSelectedRouteAndIndependentBudget(t *testing.T) {
	now := time.Now()
	i := market.Instrument{Exchange: "binance", Market: "spot", Native: "ETHUSDT", Asset: "ETH", Step: .001, PriceScale: 1, QuantityScale: 1, TakerFee: .001}
	i.ID = i.Key()
	b := market.Book{Instrument: i, Bids: []market.Level{{Price: 99, Quantity: 10}}, Asks: []market.Level{{Price: 100, Quantity: 10}}, Valid: true, ReceivedAt: now.UnixMilli()}
	sell := b
	sell.Instrument.Exchange = "bybit"
	sell.Instrument.ID = sell.Instrument.Key()
	sell.Bids = []market.Level{{Price: 105, Quantity: 10}}
	sell.Asks = []market.Level{{Price: 106, Quantity: 10}}
	s := New(nil)
	s.books = []market.Book{b, sell}
	s.at = now.UnixMilli()
	s.transport = "live"
	id := engine.RouteID(b.Instrument.ID, sell.Instrument.ID)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/verification?id="+id+"&strategy=spot-spot&amount=200&minAvailable=999999", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var report verification.Report
	if json.Unmarshal(w.Body.Bytes(), &report) != nil {
		t.Fatal(w.Body.String())
	}
	if report.Amount != 200 || len(report.Paths) != 2 || report.Paths[0].Calculation.Execution.FilledUSDT != 200 || report.Paths[0].NetProfit != nil {
		t.Fatal(report)
	}
	for _, query := range []string{"id=invalid", "id=" + id + "&amount=NaN", "id=" + id + "&amount=0"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/verification?"+query, nil))
		if w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	s.at = now.Add(-4 * time.Second).UnixMilli()
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/verification?id="+id+"&strategy=spot-spot", nil))
	if w.Code != 400 {
		t.Fatal("stale transport accepted")
	}
}
