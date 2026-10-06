package exchanges

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBybitAndOKXTradFiCannotMatchCryptoTickers(t *testing.T) {
	for _, venue := range []string{"bybit", "okx"} {
		t.Run(venue, func(t *testing.T) {
			rows := []map[string]any{}
			for _, asset := range []string{"BTC", "ON", "EWT", "BNC"} {
				var row map[string]any
				if venue == "bybit" {
					classification := "stock"
					if asset == "BTC" {
						classification = "innovation"
					}
					row = map[string]any{"symbol": asset + "USDT", "baseCoin": asset, "quoteCoin": "USDT", "status": "Trading", "contractType": "LinearPerpetual", "settleCoin": "USDT", "symbolType": classification, "lotSizeFilter": map[string]string{"qtyStep": "0.01", "minOrderQty": "0.01"}}
				} else {
					category := "3"
					if asset == "BTC" {
						category = "1"
					}
					row = map[string]any{"instId": asset + "-USDT-SWAP", "state": "live", "ctType": "linear", "settleCcy": "USDT", "ctValCcy": asset, "ctVal": "0.01", "lotSz": "1", "minSz": "1", "instCategory": category}
				}
				rows = append(rows, row)
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body any = map[string]any{"code": "0", "data": rows}
				if venue == "bybit" {
					body = map[string]any{"retCode": 0, "result": map[string]any{"list": rows}}
				}
				json.NewEncoder(w).Encode(body)
			}))
			defer s.Close()
			c := New(venue, "*")
			c.BasePerpetual = s.URL
			list, err := c.Catalog(context.Background(), "perpetual")
			if err != nil || len(list) != 1 || list[0].Asset != "BTC" {
				t.Fatal(list, err)
			}
		})
	}
}
