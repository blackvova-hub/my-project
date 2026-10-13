package chartconfigapi

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrderflowAdapterValidatesIdentityAndNeverLeaksToken(t *testing.T) {
	query := OrderflowQuery{Layer: "footprint", Symbol: "BTCUSDT", Exchange: "bybit", Market: "linear", Interval: "1h", From: 1000, To: 2000}
	dataset := OrderflowDataset{Layer: query.Layer, Symbol: query.Symbol, Exchange: query.Exchange, Market: query.Market, Source: "isolated-provider", Coverage: []string{"bybit"}, Unit: "USDT", UpdatedAt: 2000, Cells: []OrderflowCell{{Time: 1000, PriceLow: 100, PriceHigh: 101, Value: 10, Buy: 7, Sell: 3}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer isolated-token" {
			t.Error("server credential missing")
		}
		if strings.Contains(r.URL.String(), "isolated-token") {
			t.Error("credential exposed in URL")
		}
		if r.URL.Query().Get("symbol") != "BTCUSDT" {
			t.Error("identity query missing")
		}
		json.NewEncoder(w).Encode(dataset)
	}))
	defer server.Close()
	provider := &HTTPOrderflowProvider{BaseURL: server.URL, Token: "isolated-token", Layers: map[string]bool{"footprint": true}, Client: server.Client()}
	result, err := provider.Fetch(context.Background(), query)
	if err != nil || len(result.Cells) != 1 {
		t.Fatalf("provider: %v", err)
	}
	if _, err = provider.Fetch(context.Background(), OrderflowQuery{Layer: "large-trades"}); err == nil {
		t.Fatal("unsupported source accepted")
	}
	for _, change := range []func(*OrderflowDataset){func(d *OrderflowDataset) { d.Symbol = "ETHUSDT" }, func(d *OrderflowDataset) { d.Unit = "" }, func(d *OrderflowDataset) { d.Cells[0].Value = math.NaN() }, func(d *OrderflowDataset) { d.Cells[0].Time = 3000 }} {
		copy := dataset
		copy.Cells = append([]OrderflowCell(nil), dataset.Cells...)
		change(&copy)
		if validateOrderflow(copy, query) == nil {
			t.Fatal("invalid source accepted")
		}
	}
	heatmap := dataset
	heatmap.Layer = "liquidation-heatmap"
	heatQuery := query
	heatQuery.Layer = heatmap.Layer
	if validateOrderflow(heatmap, heatQuery) == nil {
		t.Fatal("estimated liquidation levels mislabeled as actual liquidations")
	}
	heatmap.Estimated = true
	if err := validateOrderflow(heatmap, heatQuery); err != nil {
		t.Fatal(err)
	}
}
