package exchanges

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"shortlong/arbitrage/internal/market"
	"testing"
	"time"
)

func TestPublicDepthNormalizesAllVenueMarkets(t *testing.T) {
	for _, exchange := range market.Exchanges {
		for _, kind := range []string{"spot", "perpetual"} {
			t.Run(exchange+"/"+kind, func(t *testing.T) {
				now := time.Now().UnixMilli()
				row := map[string]any{"bids": [][]string{{"990", "2"}, {"980", "3"}}, "asks": [][]string{{"1000", "4"}, {"1010", "5"}}, "ts": fmt.Sprint(now), "time": now * 1000000, "E": now, "current": float64(now) / 1000}
				var body any = row
				switch exchange {
				case "bybit":
					body = map[string]any{"retCode": 0, "result": map[string]any{"b": row["bids"], "a": row["asks"], "cts": now}}
				case "okx":
					body = map[string]any{"code": "0", "data": []any{row}}
				case "bitget", "kucoin":
					body = map[string]any{"code": "00000", "data": row}
					if exchange == "kucoin" {
						body = map[string]any{"code": "200000", "data": row}
					}
				case "gateio":
					if kind == "perpetual" {
						row["bids"] = []any{map[string]any{"p": "990", "s": 2}, map[string]any{"p": "980", "s": 3}}
						row["asks"] = []any{map[string]any{"p": "1000", "s": 4}, map[string]any{"p": "1010", "s": 5}}
					}
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" {
						t.Error("private request")
					}
					json.NewEncoder(w).Encode(body)
				}))
				defer server.Close()
				c := New(exchange, "BTC")
				c.BaseSpot = server.URL
				c.BasePerpetual = server.URL
				i := market.Instrument{Exchange: exchange, Market: kind, Asset: "BTC", Native: "BTCUSDT", PriceScale: 10, QuantityScale: .001, Step: .001, MinQuantity: .001}
				i.ID = i.Key()
				b, err := c.Depth(context.Background(), i)
				if err != nil {
					t.Fatal(err)
				}
				if b.Asks[0].Price != 100 || b.Asks[0].Quantity != .004 || len(b.Bids) != 2 || b.SourceAt != now || b.DepthSource != "rest-50" {
					t.Fatal(b)
				}
			})
		}
	}
}
func TestPublicDepthRejectsCrossedAndAPIError(t *testing.T) {
	for _, body := range []string{`{"retCode":10001,"retMsg":"bad symbol"}`, `{"retCode":0,"result":{"b":[["101","1"]],"a":[["100","1"]]}}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		c := New("bybit", "BTC")
		c.BaseSpot = s.URL
		i := market.Instrument{Exchange: "bybit", Market: "spot", Asset: "BTC", Native: "BTCUSDT", PriceScale: 1, QuantityScale: 1, Step: .001}
		i.ID = i.Key()
		if _, err := c.Depth(context.Background(), i); err == nil {
			t.Fatal("invalid depth accepted")
		}
		s.Close()
	}
}
