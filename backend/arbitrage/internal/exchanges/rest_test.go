package exchanges

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"shortlong/arbitrage/internal/market"
	"testing"
	"time"
)

func TestCatalogSixExchangesBothMarkets(t *testing.T) {
	cases := []struct {
		exchange, kind, body, native string
		scale, step                  float64
	}{
		{"binance", "spot", `{"symbols":[{"symbol":"BTCUSDT","baseAsset":"BTC","quoteAsset":"USDT","status":"TRADING","isSpotTradingAllowed":true,"filters":[{"filterType":"LOT_SIZE","stepSize":"0.001","minQty":"0.001"},{"filterType":"NOTIONAL","minNotional":"5"}]}]}`, "BTCUSDT", 1, .001},
		{"binance", "perpetual", `{"symbols":[{"symbol":"BTCUSDT","baseAsset":"BTC","quoteAsset":"USDT","marginAsset":"USDT","contractType":"PERPETUAL","status":"TRADING","filters":[{"filterType":"LOT_SIZE","stepSize":"0.001","minQty":"0.001"}]}]}`, "BTCUSDT", 1, .001},
		{"bybit", "spot", `{"retCode":0,"result":{"list":[{"symbol":"BTCUSDT","baseCoin":"BTC","quoteCoin":"USDT","status":"Trading","lotSizeFilter":{"basePrecision":"0.000001","minOrderQty":"0.000001","minOrderAmt":"5"}}]}}`, "BTCUSDT", 1, .000001},
		{"bybit", "perpetual", `{"retCode":0,"result":{"list":[{"symbol":"1000PEPEUSDT","baseCoin":"1000PEPE","quoteCoin":"USDT","settleCoin":"USDT","contractType":"LinearPerpetual","status":"Trading","fundingInterval":480,"lotSizeFilter":{"qtyStep":"1","minOrderQty":"1"}}]}}`, "1000PEPEUSDT", 1000, 1000},
		{"okx", "spot", `{"code":"0","data":[{"instId":"BTC-USDT","baseCcy":"BTC","quoteCcy":"USDT","state":"live","lotSz":"0.000001","minSz":"0.00001"}]}`, "BTC-USDT", 1, .000001},
		{"okx", "perpetual", `{"code":"0","data":[{"instId":"BTC-USDT-SWAP","state":"live","ctType":"linear","settleCcy":"USDT","ctValCcy":"BTC","ctVal":"0.01","ctMult":"1","lotSz":"0.01","minSz":"0.01"}]}`, "BTC-USDT-SWAP", .01, .0001},
		{"bitget", "spot", `{"code":"00000","data":[{"symbol":"BTCUSDT","baseCoin":"BTC","quoteCoin":"USDT","status":"online","quantityPrecision":"6","minTradeAmount":"0.00001","minTradeUSDT":"5","takerFeeRate":"0.001"}]}`, "BTCUSDT", 1, .000001},
		{"bitget", "perpetual", `{"code":"00000","data":[{"symbol":"BTCUSDT","baseCoin":"BTC","quoteCoin":"USDT","symbolStatus":"normal","symbolType":"perpetual","sizeMultiplier":"0.001","minTradeNum":"0.001","fundInterval":"8","takerFeeRate":"0.0006"}]}`, "BTCUSDT", 1, .001},
		{"gateio", "spot", `[{"id":"BTC_USDT","base":"BTC","quote":"USDT","trade_status":"tradable","amount_precision":6,"min_quote_amount":"5"}]`, "BTC_USDT", 1, .000001},
		{"gateio", "perpetual", `[{"name":"BTC_USDT","status":"trading","quanto_multiplier":"0.0001","order_size_min":1,"taker_fee_rate":"0.0005","funding_interval":28800}]`, "BTC_USDT", .0001, .0001},
		{"kucoin", "spot", `{"code":"200000","data":[{"symbol":"BTC-USDT","baseCurrency":"BTC","quoteCurrency":"USDT","enableTrading":true,"baseIncrement":"0.000001","baseMinSize":"0.00001","minFunds":"5"}]}`, "BTC-USDT", 1, .000001},
		{"kucoin", "perpetual", `{"code":"200000","data":[{"symbol":"XBTUSDTM","baseCurrency":"XBT","quoteCurrency":"USDT","settleCurrency":"USDT","status":"Open","isInverse":false,"expireDate":null,"multiplier":0.001,"lotSize":1,"takerFeeRate":0.0006,"fundingRateGranularity":28800000}]}`, "XBTUSDTM", .001, .001},
	}
	for _, c := range cases {
		t.Run(c.exchange+"/"+c.kind, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, c.body) }))
			defer srv.Close()
			client := New(c.exchange, market.DefaultAssets)
			client.BaseSpot = srv.URL
			client.BasePerpetual = srv.URL
			list, err := client.Catalog(context.Background(), c.kind)
			if err != nil || len(list) != 1 {
				t.Fatal(list, err)
			}
			i := list[0]
			if i.Native != c.native || i.QuantityScale != c.scale || i.Step != c.step || i.ID != i.Key() || i.TradeURL == "" {
				t.Fatal(i)
			}
		})
	}
}
func TestBybitCataloguePaginationAndRejectedAPI(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("cursor") == "" {
			_, _ = fmt.Fprint(w, `{"retCode":0,"result":{"list":[],"nextPageCursor":"next"}}`)
		} else {
			_, _ = fmt.Fprint(w, `{"retCode":0,"result":{"list":[{"symbol":"BTCUSDT","baseCoin":"BTC","quoteCoin":"USDT","status":"Trading","lotSizeFilter":{"basePrecision":"0.001"}}]}}`)
		}
	}))
	defer server.Close()
	c := New("bybit", market.DefaultAssets)
	c.BaseSpot = server.URL
	v, err := c.Catalog(context.Background(), "spot")
	if err != nil || len(v) != 1 || calls != 2 {
		t.Fatal(v, err, calls)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, `{"retCode":"bad","result":{}}`) }))
	defer bad.Close()
	c.BaseSpot = bad.URL
	if _, err = c.Catalog(context.Background(), "spot"); err == nil {
		t.Fatal("invalid retCode accepted")
	}
}
func TestMissingAndMalformedFundingRemainUnknown(t *testing.T) {
	next := time.Now().Add(time.Hour).UnixMilli()
	for _, rate := range []string{"", "nonsense", "NaN", "0"} {
		t.Run(rate, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"retCode":0,"result":{"list":[{"symbol":"BTCUSDT","fundingRate":%q,"nextFundingTime":"%d"}]}}`, rate, next)
			}))
			defer srv.Close()
			c := New("bybit", market.DefaultAssets)
			c.BasePerpetual = srv.URL
			i := instrument("bybit")
			i.FundingIntervalMS = 8 * 3600000
			v, err := c.Funding(context.Background(), []market.Instrument{i})
			if err != nil {
				t.Fatal(err)
			}
			if rate == "0" {
				if len(v) != 1 || v[i.ID].Rate != 0 {
					t.Fatal(v)
				}
			} else if len(v) != 0 {
				t.Fatal("unknown rate became a zero", v)
			}
		})
	}
}
func TestUnknownFeesUseAssumptionNotFreeTrading(t *testing.T) {
	for _, v := range []any{nil, "bad", "NaN", "-1", "0.5"} {
		if got := publicFee(Record{"rate": v}, "rate", .0006); got != .0006 {
			t.Fatal(v, got)
		}
	}
	if got := publicFee(Record{"rate": "0"}, "rate", .0006); got != 0 {
		t.Fatal("known zero fee discarded", got)
	}
}
