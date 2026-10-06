package engine

import (
	"shortlong/arbitrage/internal/market"
	"testing"
	"time"
)

func TestSimplePricesCommonTopVolumeAndNoHiddenBudget(t *testing.T) {
	now := time.Now()
	buy := book("binance", "spot", 100, 99, now)
	sell := book("bybit", "perpetual", 102, 101, now)
	buy.Asks = []market.Level{{Price: 100, Quantity: .5}, {Price: 110, Quantity: 100}}
	sell.Bids = []market.Level{{Price: 101, Quantity: .4}, {Price: 90, Quantity: 100}}
	buy.Instrument.Step = .003
	sell.Instrument.Step = .005
	q := Defaults()
	q.Amount = 1000000
	q.MinSpread = 50
	o, ok := Calculate(buy, sell, q, now)
	if !ok {
		t.Fatal("hidden size/spread filter still active")
	}
	closeTo(t, o.Quantity, .39)
	closeTo(t, o.DepthUSDT, 39)
	closeTo(t, o.Buy.Price, 100)
	closeTo(t, o.Sell.Price, 101)
	closeTo(t, o.EntrySpreadPct, 1)
	if o.ScenarioPnL != nil || o.FundingEstimate != nil || o.EntryFees != 0 {
		t.Fatal("simple result contains a profit scenario")
	}
	sell.Bids[0].Price = 100
	if _, ok = Calculate(buy, sell, q, now); ok {
		t.Fatal("zero gap accepted")
	}
	sell.Bids[0].Price = 101
	sell.Instrument.MinNotional = 50
	if _, ok = Calculate(buy, sell, q, now); ok {
		t.Fatal("minimum order ignored")
	}
}
func TestSimpleSearchAndFavoritesUseCompleteUniverse(t *testing.T) {
	now := time.Now()
	books := []market.Book{}
	for _, asset := range []string{"BTC", "1INCH", "NEWTOKEN"} {
		for _, v := range []struct {
			e, k     string
			ask, bid float64
		}{{"binance", "spot", 100, 99}, {"bybit", "perpetual", 102, 101}} {
			b := book(v.e, v.k, v.ask, v.bid, now)
			b.Instrument.Asset = asset
			b.Instrument.Native = asset + "USDT"
			b.Instrument.ID = b.Instrument.Key()
			books = append(books, b)
		}
	}
	q := Defaults()
	q.Search = "INCH"
	rows := Scan(books, q, now)
	if len(rows) != 1 || rows[0].Asset != "1INCH" {
		t.Fatal(rows)
	}
	q.Search = ""
	q.Routes = []string{RouteID(books[4].Instrument.ID, books[5].Instrument.ID)}
	rows = Scan(books, q, now)
	if len(rows) != 1 || rows[0].Asset != "NEWTOKEN" {
		t.Fatal(rows)
	}
	q.Limit = 0
	q.Routes = nil
	if len(Scan(books, q, now)) != 3 {
		t.Fatal("unlimited internal scan truncated")
	}
}
func TestSameTickerDifferentTokensCannotCreateArbitrage(t *testing.T) {
	now := time.Now()
	b := book("bitget", "spot", .07, .069, now)
	s := book("okx", "perpetual", .2, .19, now)
	b.Instrument.Asset = "UP"
	s.Instrument.Asset = "UP"
	b.Instrument.MinNotional = 0
	s.Instrument.MinNotional = 0
	if _, ok := Calculate(b, s, Defaults(), now); ok {
		t.Fatal("Superform was matched to Unitas")
	}
	b.Instrument.Exchange = "gateio"
	b.Instrument.ID = b.Instrument.Key()
	if _, ok := Calculate(b, s, Defaults(), now); !ok {
		t.Fatal("reviewed Unitas identity failed to match")
	}
}
