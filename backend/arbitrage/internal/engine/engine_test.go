package engine

import (
	"math"
	"shortlong/arbitrage/internal/market"
	"testing"
	"time"
)

func book(e, k string, ask, bid float64, now time.Time) market.Book {
	i := market.Instrument{Exchange: e, Market: k, Native: "BTCUSDT", Asset: "BTC", PriceScale: 1, QuantityScale: 1, Step: 0.001, MinQuantity: 0.001, MinNotional: 5, TakerFee: 0.001}
	i.ID = i.Key()
	return market.Book{Instrument: i, Bids: []market.Level{{Price: bid, Quantity: 20}}, Asks: []market.Level{{Price: ask, Quantity: 20}}, ReceivedAt: now.UnixMilli(), SourceAt: now.UnixMilli(), Valid: true}
}
func closeTo(t *testing.T, a, b float64) {
	t.Helper()
	if math.Abs(a-b) > 1e-7 {
		t.Fatalf("got %g want %g", a, b)
	}
}
func TestHedgedScenarioFourFeesAndFunding(t *testing.T) {
	now := time.Unix(1800000000, 0)
	b := book("binance", "spot", 100, 99, now)
	s := book("bybit", "perpetual", 102, 101, now)
	s.Funding = &market.Funding{Rate: .0001, NextAt: now.Add(8 * time.Hour).UnixMilli(), IntervalMS: 8 * 3600000, ObservedAt: now.UnixMilli()}
	q := Defaults()
	o, ok := Calculate(b, s, q, now)
	if !ok {
		t.Fatal("route rejected")
	}
	closeTo(t, o.Quantity, 10)
	closeTo(t, o.EntrySpreadPct, 1)
	closeTo(t, o.EntryFees, 2.01)
	closeTo(t, o.ExitFeesEstimate, 2)
	closeTo(t, *o.FundingEstimate, .303)
	closeTo(t, *o.ScenarioPnL, 6.293)
	if o.FundingEvents != 3 {
		t.Fatal(o.FundingEvents)
	}
	q.ExitSpread = .5
	o, _ = Calculate(b, s, q, now)
	closeTo(t, *o.ScenarioPnL, 1.288)
}
func TestIndependentFundingSchedulesAndSigns(t *testing.T) {
	now := time.Unix(1800000000, 0)
	b := book("binance", "perpetual", 100, 99, now)
	s := book("bybit", "perpetual", 102, 101, now)
	b.Funding = &market.Funding{Rate: .0002, NextAt: now.Add(time.Hour).UnixMilli(), IntervalMS: 3600000, ObservedAt: now.UnixMilli()}
	s.Funding = &market.Funding{Rate: -.0001, NextAt: now.Add(4 * time.Hour).UnixMilli(), IntervalMS: 8 * 3600000, ObservedAt: now.UnixMilli()}
	q := Defaults()
	q.Strategy = "perpetual-perpetual"
	o, ok := Calculate(b, s, q, now)
	if !ok {
		t.Fatal("rejected")
	}
	closeTo(t, *o.FundingEstimate, -5.103)
	if o.FundingEvents != 27 {
		t.Fatal(o.FundingEvents)
	}
}
func TestMissingOrStaleFundingIsUnknownNotZero(t *testing.T) {
	now := time.Now()
	b := book("binance", "spot", 100, 99, now)
	s := book("bybit", "perpetual", 102, 101, now)
	q := Defaults()
	for _, f := range []*market.Funding{nil, {Rate: 0, NextAt: now.Add(time.Hour).UnixMilli(), IntervalMS: 3600000, ObservedAt: now.Add(-3 * time.Minute).UnixMilli()}, {Rate: 0, NextAt: now.Add(-time.Second).UnixMilli(), IntervalMS: 3600000, ObservedAt: now.UnixMilli()}} {
		s.Funding = f
		o, ok := Calculate(b, s, q, now)
		if !ok || o.FundingEstimate != nil || o.ScenarioPnL != nil {
			t.Fatal("unknown funding became a scenario")
		}
	}
}
func TestDepthAndCommonLotRounding(t *testing.T) {
	now := time.Now()
	b := book("binance", "spot", 100, 99, now)
	s := book("bybit", "perpetual", 102, 101, now)
	q := Defaults()
	b.Instrument.Step = .003
	s.Instrument.Step = .005
	o, ok := Calculate(b, s, q, now)
	if !ok {
		t.Fatal("rejected")
	}
	closeTo(t, o.Quantity, 9.99)
	b.Asks = []market.Level{{Price: 100, Quantity: 1}, {Price: 102, Quantity: 20}}
	s.Bids[0].Price = 104
	s.Asks[0].Price = 105
	o, ok = Calculate(b, s, q, now)
	if !ok || o.Buy.Notional > q.Amount+1e-7 || o.Buy.Price <= 100 {
		t.Fatal("bad VWAP budget")
	}
	s.Bids[0].Quantity = 0.001
	if _, ok = Calculate(b, s, q, now); ok {
		t.Fatal("insufficient depth admitted")
	}
}
func TestLargeContractLotsRemainHedged(t *testing.T) {
	closeTo(t, commonQuantity(2500000, .001, 1000000), 2000000)
	closeTo(t, commonQuantity(123456789, 1, 10000), 123450000)
	if commonQuantity(500000, .001, 1000000) != 0 {
		t.Fatal("budget below contract lot admitted")
	}
}
func TestFreshnessAndIdentity(t *testing.T) {
	now := time.Now()
	b := book("binance", "spot", 100, 99, now)
	s := book("bybit", "perpetual", 102, 101, now)
	q := Defaults()
	for _, change := range []func(*market.Book){func(b *market.Book) { b.Valid = false }, func(b *market.Book) { b.ReceivedAt = now.Add(-11 * time.Second).UnixMilli() }, func(b *market.Book) { b.SourceAt = now.Add(-11 * time.Second).UnixMilli() }, func(b *market.Book) { b.SourceAt = now.Add(3 * time.Second).UnixMilli() }, func(b *market.Book) { b.Instrument.Asset = "ETH" }, func(b *market.Book) { b.Bids[0].Price = b.Asks[0].Price }} {
		copy := s
		copy.Bids = append([]market.Level{}, s.Bids...)
		change(&copy)
		if _, ok := Calculate(b, copy, q, now); ok {
			t.Fatal("invalid route admitted")
		}
	}
}
func TestQuoteAgeUsesOlderExchangeTimestamp(t *testing.T) {
	now := time.Now()
	b := book("binance", "spot", 100, 99, now)
	s := book("bybit", "perpetual", 102, 101, now)
	b.SourceAt = now.Add(-20 * time.Second).UnixMilli()
	q := Defaults()
	q.MaxAgeMS = 30000
	o, ok := Calculate(b, s, q, now)
	if !ok || o.Buy.BookAt != b.SourceAt || o.AgeMS != 20000 {
		t.Fatal("receipt timestamp hid quote age", o, ok)
	}
	q.MaxAgeMS = 10000
	if _, ok = Calculate(b, s, q, now); ok {
		t.Fatal("default age filter ignored source time")
	}
}
func TestSpotCycleAndSameExchange(t *testing.T) {
	now := time.Now()
	b := book("binance", "spot", 100, 99, now)
	s := book("bybit", "spot", 102, 101, now)
	q := Defaults()
	q.Strategy = "spot-spot"
	o, ok := Calculate(b, s, q, now)
	if !ok || o.ScenarioPnL != nil || o.FundingEstimate != nil {
		t.Fatal("wrong spot result")
	}
	closeTo(t, *o.SpotCyclePnL, 7.99)
	s.Instrument.Exchange = "binance"
	s.Instrument.Native = "BTCUSDT2"
	s.Instrument.ID = s.Instrument.Key()
	if _, ok := Calculate(b, s, q, now); ok {
		t.Fatal("same exchange admitted")
	}
	q.InterExchange = false
	if _, ok := Calculate(b, s, q, now); !ok {
		t.Fatal("explicit same exchange rejected")
	}
}
func TestParseRejectsNonFiniteAndInvalidFilters(t *testing.T) {
	for _, raw := range []map[string][]string{{"amount": {"NaN"}}, {"feeSpot": {"Inf"}}, {"minSpread": {"abc"}}, {"exchanges": {"binance,binance"}}, {"maxAgeMs": {"999"}}, {"strategy": {"unknown"}}, {"horizonHours": {"169"}}, {"assets": {"BTC/USDT"}}, {"interExchange": {"perhaps"}}} {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("accepted %v", raw)
		}
	}
	q, err := Parse(map[string][]string{"assets": {"btc, eth"}, "feeSpot": {"0"}, "exchanges": {"binance,bybit"}})
	if err != nil || len(q.Assets) != 2 || q.FeeSpot == nil || *q.FeeSpot != 0 {
		t.Fatal(q, err)
	}
}
func BenchmarkScanSixExchanges(b *testing.B) {
	now := time.Now()
	books := []market.Book{}
	for _, a := range []string{"BTC", "ETH", "SOL", "XRP", "DOGE", "ADA", "AVAX", "LINK", "DOT", "LTC", "BCH", "TRX", "TON", "SUI", "APT", "ARB", "OP", "NEAR", "UNI", "AAVE", "ATOM", "ETC", "FIL", "ICP", "INJ", "SEI", "PEPE", "SHIB", "BONK", "FLOKI"} {
		for _, e := range market.Exchanges {
			for _, k := range []string{"spot", "perpetual"} {
				v := book(e, k, 100, 99, now)
				v.Instrument.Asset = a
				v.Instrument.Native = a + "USDT"
				v.Instrument.ID = v.Instrument.Key()
				books = append(books, v)
			}
		}
	}
	q := Defaults()
	q.MinSpread = -2
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Scan(books, q, now)
	}
}
