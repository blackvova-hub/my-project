package analytics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func fixtureClient(t *testing.T, exchange string, responses map[string]string) *exchangeClient {
	t.Helper()
	x := newExchange(exchange, Credentials{"fixture-key", "fixture-secret"})
	x.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("exchange mutation attempted")
		}
		body, ok := responses[r.URL.Path]
		if !ok {
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
		}
		if exchange == "bybit" {
			body = `{"retCode":0,"result":` + body + `}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	return x
}

func TestBybitImportPreservesSignedWalletEquation(t *testing.T) {
	x := fixtureClient(t, "bybit", map[string]string{
		"/v5/execution/list":          `{"list":[{"execType":"Trade","symbol":"BTCUSDT","execId":"a","side":"Buy","execTime":"1000","execPrice":"100","execQty":"1","execFee":"0.1","closedSize":"0","isMaker":true},{"execType":"Trade","symbol":"BTCUSDT","execId":"b","side":"Sell","execTime":"2000","execPrice":"110","execQty":"1","execFee":"0.1","closedSize":"1","isMaker":false}]}`,
		"/v5/account/transaction-log": `{"list":[{"id":"tx","symbol":"BTCUSDT","currency":"USDT","category":"linear","type":"TRADE","transactionTime":"2000","cashFlow":"10.00000000","fee":"0.10","funding":"-2","bonusChange":"-8"},{"id":"transfer","currency":"USDT","type":"TRANSFER_IN","transactionTime":"1500","cashFlow":"100","fee":"0"},{"id":"spot","currency":"USDT","category":"spot","type":"TRADE","transactionTime":"1800","cashFlow":"-25","fee":"0"}]}`,
		"/v5/account/wallet-balance":  `{"list":[{"totalEquity":"108","totalWalletBalance":"107.9","totalPerpUPL":"0.1","totalInitialMargin":"0","coin":[{"coin":"USDT","usdValue":"108"}]}]}`,
		"/v5/position/list":           `{"list":[]}`,
		"/v5/market/tickers":          `{"list":[{"lastPrice":"50000"}]}`,
	})
	r, err := x.sync(context.Background(), Connection{ID: "one", Exchange: "bybit"}, time.UnixMilli(1), time.UnixMilli(3000))
	if err != nil {
		t.Fatal(err)
	}
	fills := []Fill{}
	ledger := []Ledger{}
	net := 0.0
	for _, e := range r.Events {
		if e.Kind == "fill" {
			fills = append(fills, e.Normalized.(Fill))
		} else {
			l := e.Normalized.(Ledger)
			ledger = append(ledger, l)
			net += l.Amount
			if l.Category == "rewards" {
				t.Fatal("noncash bonus counted twice")
			}
		}
	}
	closeTo(t, net, 82.9)
	trades := Reconstruct(fills, ledger)
	if len(trades) != 1 || !trades[0].Complete {
		t.Fatal(trades)
	}
	closeTo(t, trades[0].Net, 7.8)
	closeTo(t, r.Snapshot.Equity, 108)
}

func TestBinanceImportFindsSymbolsFromIncomeAndKeepsTransfersSeparate(t *testing.T) {
	x := fixtureClient(t, "binance", map[string]string{
		"/fapi/v1/income":       `[{"symbol":"BTCUSDT","incomeType":"REALIZED_PNL","tranId":11,"income":"10","asset":"USDT","time":2000},{"symbol":"BTCUSDT","incomeType":"COMMISSION","tranId":12,"income":"-0.2","asset":"USDT","time":2000},{"symbol":"","incomeType":"TRANSFER","tranId":13,"income":"100","asset":"USDT","time":1500}]`,
		"/fapi/v3/account":      `{"totalMarginBalance":"109.8","totalWalletBalance":"109.8","totalUnrealizedProfit":"0","totalInitialMargin":"0","assets":[{"asset":"USDT","marginBalance":"109.8"}]}`,
		"/fapi/v3/positionRisk": `[]`,
		"/fapi/v1/userTrades":   `[{"id":1,"symbol":"BTCUSDT","side":"BUY","positionSide":"BOTH","time":1000,"price":"100","qty":"1","commission":"0.1","commissionAsset":"USDT","realizedPnl":"0","maker":true},{"id":2,"symbol":"BTCUSDT","side":"SELL","positionSide":"BOTH","time":2000,"price":"110","qty":"1","commission":"0.1","commissionAsset":"USDT","realizedPnl":"10","maker":false}]`,
		"/fapi/v1/ticker/price": `{"price":"50000"}`,
	})
	r, err := x.sync(context.Background(), Connection{ID: "two", Exchange: "binance"}, time.UnixMilli(1), time.UnixMilli(3000))
	if err != nil {
		t.Fatal(err)
	}
	fills := []Fill{}
	ledger := []Ledger{}
	for _, e := range r.Events {
		if e.Kind == "fill" {
			fills = append(fills, e.Normalized.(Fill))
		} else {
			ledger = append(ledger, e.Normalized.(Ledger))
		}
	}
	trades := Reconstruct(fills, ledger)
	if len(trades) != 1 || !trades[0].Complete {
		t.Fatal(trades)
	}
	closeTo(t, trades[0].Net, 9.8)
	if ledger[2].Category != "transfer" {
		t.Fatal("transfer counted as profit")
	}
}

func TestBybitReversalConservesBothPositionLanes(t *testing.T) {
	first := fill("a", "BUY", 1, 100, 1, 1000)
	first.PositionSide = "LONG"
	reversal := fill("b", "SELL", 2, 110, 2, 2000)
	reversal.PositionSide = "LONG"
	q := 1.0
	reversal.ClosedQuantity = &q
	last := fill("c", "BUY", 1, 90, 1, 3000)
	last.PositionSide = "SHORT"
	last.ClosedQuantity = &q
	fills := append([]Fill{first}, splitBybitReversal(reversal)...)
	fills = append(fills, last)
	trades := Reconstruct(fills, nil)
	if len(trades) != 2 {
		t.Fatal(trades)
	}
	closeTo(t, trades[0].Net+trades[1].Net, 26)
	if !trades[0].Complete || !trades[1].Complete {
		t.Fatal("complete reversal was excluded")
	}
}

func TestBybitForcedClosuresDoNotLeavePhantomPositions(t *testing.T) {
	for _, tc := range []struct{ execution, transaction string }{
		{"BustTrade", "LIQUIDATION"}, {"AdlTrade", "ADL"}, {"Delivery", "DELIVERY"},
		{"BlockTrade", "TRADE"}, {"FutureSpread", "TRADE"},
	} {
		t.Run(tc.execution, func(t *testing.T) {
			x := fixtureClient(t, "bybit", map[string]string{
				"/v5/execution/list":          fmt.Sprintf(`{"list":[{"execType":"Trade","symbol":"BTCUSDT","execId":"open","side":"Buy","execTime":"1000","execPrice":"100","execQty":"1","execFee":"0.1","closedSize":"0"},{"execType":%q,"symbol":"BTCUSDT","execId":"close","side":"Sell","execTime":"2000","execPrice":"90","execQty":"1","execFee":"0.1","closedSize":"1"},{"execType":"Funding","symbol":"BTCUSDT","execId":"funding","side":"Sell","execTime":"1500","execPrice":"100","execQty":"1","execFee":"2","closedSize":"0"}]}`, tc.execution),
				"/v5/account/transaction-log": fmt.Sprintf(`{"list":[{"id":"closed-pnl","symbol":"BTCUSDT","currency":"USDT","category":"linear","type":%q,"transactionTime":"2000","cashFlow":"-10","fee":"0.1"}]}`, tc.transaction),
				"/v5/account/wallet-balance":  `{"list":[{"totalEquity":"89.8","totalWalletBalance":"89.8","coin":[]}]}`,
				"/v5/position/list":           `{"list":[]}`,
				"/v5/market/tickers":          `{"list":[]}`,
			})
			r, err := x.sync(context.Background(), Connection{ID: "one", Exchange: "bybit"}, time.UnixMilli(1), time.UnixMilli(3000))
			if err != nil {
				t.Fatal(err)
			}
			var fills []Fill
			var ledger []Ledger
			for _, e := range r.Events {
				if e.Kind == "fill" {
					fills = append(fills, e.Normalized.(Fill))
				} else {
					ledger = append(ledger, e.Normalized.(Ledger))
				}
			}
			if len(fills) != 2 {
				t.Fatalf("expected entry and forced exit, got %d fills", len(fills))
			}
			trades := Reconstruct(fills, ledger)
			if len(trades) != 1 || trades[0].ClosedAt == nil || !trades[0].Complete || trades[0].Remaining != 0 {
				t.Fatalf("forced closure not reconstructed: %+v", trades)
			}
			closeTo(t, trades[0].Net, -10.2)
			if len(ledger) != 2 || ledger[0].Category != "trading" {
				t.Fatalf("forced closure missing from trading ledger: %+v", ledger)
			}
		})
	}
}
