package engine

import (
	"math"
	"net/url"
	"shortlong/arbitrage/internal/market"
	"testing"
	"time"
)

func TestAvailabilityMatchesProfitableDepthNotBudget(t *testing.T) {
	now := time.Now()
	b := book("binance", "spot", 100, 99, now)
	s := book("bybit", "spot", 105, 104, now)
	b.Asks = []market.Level{{Price: 100, Quantity: .5}, {Price: 102, Quantity: 1}, {Price: 106, Quantity: 100}}
	s.Bids = []market.Level{{Price: 104, Quantity: .8}, {Price: 103, Quantity: 1}, {Price: 101, Quantity: 100}}
	q := Defaults()
	q.Strategy = "spot-spot"
	q.Budget = true
	q.Amount = 10
	o, ok := Calculate(b, s, q, now)
	if !ok {
		t.Fatal("missing route")
	}
	closeTo(t, o.Quantity, 1.5)
	closeTo(t, o.DepthUSDT, 152)
	closeTo(t, o.Buy.Price, 152/1.5)
	q.Amount = 1000000
	again, _ := Calculate(b, s, q, now)
	closeTo(t, again.DepthUSDT, o.DepthUSDT)
	q.MinAvailable = 152
	if len(Scan([]market.Book{b, s}, q, now)) != 1 {
		t.Fatal("inclusive threshold rejected")
	}
	q.MinAvailable = 152.01
	if len(Scan([]market.Book{b, s}, q, now)) != 0 {
		t.Fatal("threshold not applied")
	}
}
func TestAvailabilityFeesStepsAndStaleness(t *testing.T) {
	now := time.Now()
	b := book("binance", "spot", 100, 99, now)
	s := book("bybit", "spot", 101, 100.1, now)
	q := Defaults()
	q.Budget = true
	q.Strategy = "cex-cex"
	if _, ok := Calculate(b, s, q, now); ok {
		t.Fatal("fees exceed edge")
	}
	q.Strategy = "spot-spot"
	b.Instrument.Step = .003
	s.Instrument.Step = .005
	b.Asks[0].Quantity = .019
	o, ok := Calculate(b, s, q, now)
	if ok {
		t.Fatal("below min notional")
	}
	b.Instrument.MinNotional = 0
	s.Instrument.MinNotional = 0
	o, ok = Calculate(b, s, q, now)
	if !ok {
		t.Fatal("steps rejected")
	}
	closeTo(t, o.Quantity, .015)
	s.SourceAt = now.Add(-11 * time.Second).UnixMilli()
	if _, ok := Calculate(b, s, q, now); ok {
		t.Fatal("stale depth")
	}
}
func TestBudgetIntegrationAndPartialHedge(t *testing.T) {
	now := time.Now()
	b := book("binance", "spot", 100, 99, now)
	s := book("bybit", "perpetual", 110, 109, now)
	b.Asks = []market.Level{{Price: 100, Quantity: .5}, {Price: 120, Quantity: 1}}
	s.Bids[0].Quantity = .7
	buy, sell, qty, e, ok := SizedExecution(b, s, 100)
	if !ok || e.Full || e.Status != "depth-insufficient" {
		t.Fatal(e)
	}
	closeTo(t, qty, .7)
	closeTo(t, buy.Notional, 74)
	closeTo(t, sell.Notional, 76.3)
	closeTo(t, e.CapacityUSDT, 74)
	q, covered := BudgetQuantity(b.Asks, 110)
	closeTo(t, q, 1)
	if !covered {
		t.Fatal("exact budget")
	}
}
func TestFundingIntervalsSignsAndBasisNotProfit(t *testing.T) {
	now := time.Now()
	b := book("binance", "perpetual", 100, 99, now)
	s := book("bybit", "perpetual", 99, 98, now)
	b.Funding = &market.Funding{Rate: -.001, NextAt: now.Add(time.Hour).UnixMilli(), IntervalMS: 3600000, ObservedAt: now.UnixMilli()}
	s.Funding = &market.Funding{Rate: .002, NextAt: now.Add(4 * time.Hour).UnixMilli(), IntervalMS: 8 * 3600000, ObservedAt: now.UnixMilli()}
	q := Defaults()
	q.Strategy = "funding"
	q.Amount = 100
	q.HorizonHours = 24
	o, ok := Calculate(b, s, q, now)
	if !ok {
		t.Fatal("negative basis must not hide carry")
	}
	closeTo(t, o.Metrics.FundingUSDT, 2.988)
	closeTo(t, o.Metrics.CostsUSDT, .396)
	closeTo(t, o.Metrics.CashUSDT, 2.592)
	if o.Metrics.FundingEvents != 27 || o.ScenarioPnL != nil {
		t.Fatal("incorrect events or fabricated basis convergence")
	}
	s.Funding = nil
	if _, ok := Calculate(b, s, q, now); ok {
		t.Fatal("unknown funding became zero")
	}
	b.Funding.Rate = .001
	s.Funding = &market.Funding{Rate: -.001, NextAt: now.Add(time.Hour).UnixMilli(), IntervalMS: 3600000, ObservedAt: now.UnixMilli()}
	if _, ok := Calculate(b, s, q, now); ok {
		t.Fatal("negative carry route")
	}
}
func TestXEMMBothMakerSidesAreConditional(t *testing.T) {
	now := time.Now()
	b := book("binance", "spot", 100, 99, now)
	s := book("bybit", "spot", 103, 102, now)
	q := Defaults()
	q.Strategy = "xemm"
	q.Amount = 100
	rows := Scan([]market.Book{b, s}, q, now)
	sides := map[string]bool{}
	ids := map[string]bool{}
	for _, o := range rows {
		if o.Execution.Status != "maker-conditional" || ids[o.ID] {
			t.Fatal("maker presented as instant or duplicate")
		}
		ids[o.ID] = true
		sides[o.Metrics.MakerSide] = true
	}
	if !sides["buy"] || !sides["sell"] {
		t.Fatal(sides)
	}
	// Own BBO quantity is not a queue fill guarantee or a cap on a maker quote.
	b.Bids[0].Quantity = .001
	o, ok := makerBuy(b, s, q, now)
	if !ok || o.Quantity < 1 {
		t.Fatal("maker quantity incorrectly limited by own bid")
	}
}
func TestAlternativeUSDAnchorEligibilityAndFreshness(t *testing.T) {
	now := time.Now()
	b := book("binance", "spot", .98, .97, now)
	b.Instrument.Asset = "USDC"
	b.Instrument.MinNotional = 0
	b.Instrument.Step = .01
	b.Asks[0].Quantity = 1000
	a := market.AlternativeData{USDTUSD: market.USDQuote{Bid: .99, Ask: .991, ObservedAt: now.UnixMilli()}}
	q := Defaults()
	q.Strategy = "redemption"
	q.Amount = 100
	rows := ScanAlternatives([]market.Book{b}, q, a, now)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	o := rows[0]
	closeTo(t, o.Sell.Price, 1/.991)
	if o.Metrics.Eligibility != "unverified" || o.Execution.Status != "redemption-conditional" {
		t.Fatal("issuer route falsely executable")
	}
	q.MinAvailable = o.DepthUSDT + 1
	if len(ScanAlternatives([]market.Book{b}, q, a, now)) != 0 {
		t.Fatal("alternative threshold")
	}
	a.USDTUSD.ObservedAt = now.Add(-31 * time.Second).UnixMilli()
	if len(ScanAlternatives([]market.Book{b}, q, a, now)) != 0 {
		t.Fatal("stale USD anchor")
	}
}
func TestV3InventoryUsesTicksAndConservesCapital(t *testing.T) {
	p := market.LPPool{PriceUSDC: 2000, TickPriceUSDC: 1999.98, Tick: 0, TickSpacing: 60, Token0: "base"}
	x, y, lower, upper, ok := LPInventory(p, 1000, 10)
	if !ok {
		t.Fatal("inventory rejected")
	}
	closeTo(t, x*p.PriceUSDC+y, 1000)
	if x <= 0 || y <= 0 || lower >= p.PriceUSDC || upper <= p.PriceUSDC {
		t.Fatal("invalid bounds")
	}
	for _, bound := range []float64{lower, upper} {
		tick := math.Log(bound/p.TickPriceUSDC) / math.Log(1.0001)
		closeTo(t, tick/60, math.Round(tick/60))
	}
	p.TickPriceUSDC = 0
	if _, _, _, _, ok = LPInventory(p, 1000, 10); ok {
		t.Fatal("unknown tick anchor accepted")
	}
}
func TestMinAvailableParseRejectsNonfiniteAndNegative(t *testing.T) {
	for _, value := range []string{"-1", "NaN", "Inf", "1000001", "oops"} {
		if _, err := Parse(url.Values{"minAvailable": {value}}); err == nil {
			t.Fatal(value)
		}
	}
	q, err := Parse(url.Values{"minAvailable": {"100.5"}})
	if err != nil {
		t.Fatal(err)
	}
	closeTo(t, q.MinAvailable, 100.5)
}
func TestHedgedLPAvailabilityIndependentOfCapitalAndAPYNotCombined(t *testing.T) {
	now := time.Now()
	hedge := book("bybit", "perpetual", 2001, 2000, now)
	hedge.Instrument.Asset = "ETH"
	hedge.Funding = &market.Funding{Rate: -.001, NextAt: now.Add(time.Hour).UnixMilli(), IntervalMS: 8 * 3600000, ObservedAt: now.UnixMilli()}
	pool := market.LPPool{ID: "reviewed", Address: "0x123", Network: "Ethereum", Asset: "ETH", Token0: "base", PriceUSDC: 2000, TickPriceUSDC: 1999.98, TickSpacing: 60, ObservedAt: now.UnixMilli(), YieldAt: now.UnixMilli(), TVLUSD: 100000, HistoricalAPY: 5}
	a := market.AlternativeData{USDTUSD: market.USDQuote{Bid: 1, Ask: 1, ObservedAt: now.UnixMilli()}, USDCUSD: market.USDQuote{Bid: 1, Ask: 1, ObservedAt: now.UnixMilli()}, Pools: []market.LPPool{pool}}
	q := Defaults()
	q.Strategy = "hedged-lp"
	q.Amount = 100
	q.MinAvailable = 500
	q.LPRangePct = 10
	rows := ScanAlternatives([]market.Book{hedge}, q, a, now)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	o := rows[0]
	closeTo(t, o.DepthUSDT, 1000)
	closeTo(t, o.Metrics.ValuePct, 5)
	if o.Metrics.FundingUSDT >= 0 || o.Metrics.ResidualQuantity < 0 || o.Execution.Status != "lp-conditional" {
		t.Fatal(o.Metrics, o.Execution)
	}
	q.Amount = 200
	again := ScanAlternatives([]market.Book{hedge}, q, a, now)
	closeTo(t, again[0].DepthUSDT, o.DepthUSDT)
	a.Pools[0].ObservedAt = now.Add(-61 * time.Second).UnixMilli()
	if len(ScanAlternatives([]market.Book{hedge}, q, a, now)) != 0 {
		t.Fatal("old chain price")
	}
}
