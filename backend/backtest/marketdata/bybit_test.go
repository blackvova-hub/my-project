package marketdata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKlineCategoryIntervalAndReversePagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("category") != "spot" || q.Get("interval") != "5" || q.Get("end") != "899999" || q.Get("limit") != "1000" {
			t.Errorf("bad query: %s", r.URL)
		}
		fmt.Fprint(w, `{"retCode":0,"result":{"list":[["600000","100","101","99","100","12"],["300000","100","101","99","100","12"],["0","100","101","99","100","12"]]}}`)
	}))
	defer server.Close()
	c, err := NewBybit(server.URL, nil).Candles(context.Background(), "spot", "BTCUSDT", "5m", 0, 900000)
	if err != nil || len(c) != 3 || c[0].Time != 0 || c[2].Time != 600000 {
		t.Fatal(c, err)
	}
}
func TestProviderErrorsAreNotCachedAsCandles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"retCode":10001,"retMsg":"invalid symbol"}`)
	}))
	defer server.Close()
	if _, err := NewBybit(server.URL, nil).Candles(context.Background(), "linear", "BTCUSDT", "1h", 0, 3600000); err == nil {
		t.Fatal("provider error accepted")
	}
}
func TestCatalogFiltersUSDTPerpetualAndPaginates(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.URL.Query().Get("cursor") == "" {
			fmt.Fprint(w, `{"retCode":0,"result":{"list":[{"symbol":"BTCUSDT","quoteCoin":"USDT","status":"Trading","contractType":"LinearPerpetual"},{"symbol":"ABCUSDT","quoteCoin":"USDT","status":"Trading","contractType":"LinearFutures"}],"nextPageCursor":"next"}}`)
		} else {
			fmt.Fprint(w, `{"retCode":0,"result":{"list":[{"symbol":"ETHUSDT","quoteCoin":"USDT","status":"Trading","contractType":"LinearPerpetual"}]}}`)
		}
	}))
	defer server.Close()
	symbols, err := NewBybit(server.URL, nil).Symbols(context.Background(), "linear")
	if err != nil || len(symbols) != 2 || count != 2 {
		t.Fatal(symbols, count, err)
	}
}
