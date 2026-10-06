package engine

import (
	"math"
	"shortlong/arbitrage/internal/market"
	"time"
)

type Execution struct {
	RequestedUSDT   float64 `json:"requestedUsdt"`
	FilledUSDT      float64 `json:"filledUsdt"`
	CapacityUSDT    float64 `json:"capacityUsdt"`
	FillPct         float64 `json:"fillPct"`
	Full            bool    `json:"full"`
	Status          string  `json:"status"`
	BuyLevels       int     `json:"buyLevels"`
	SellLevels      int     `json:"sellLevels"`
	BuySlippagePct  float64 `json:"buySlippagePct"`
	SellSlippagePct float64 `json:"sellSlippagePct"`
	DepthAt         int64   `json:"depthAt"`
}
type Metrics struct {
	Kind             string  `json:"kind"`
	ValuePct         float64 `json:"valuePct"`
	CashUSDT         float64 `json:"cashUsdt"`
	GrossUSDT        float64 `json:"grossUsdt"`
	CostsUSDT        float64 `json:"costsUsdt"`
	FundingUSDT      float64 `json:"fundingUsdt"`
	FundingEvents    int     `json:"fundingEvents"`
	HorizonHours     int     `json:"horizonHours"`
	NextAt           int64   `json:"nextAt,omitempty"`
	AnnualFundingPct float64 `json:"annualFundingPct,omitempty"`
	MakerSide        string  `json:"makerSide,omitempty"`
	Condition        string  `json:"condition"`
	URL              string  `json:"url,omitempty"`
	Network          string  `json:"network,omitempty"`
	Pool             string  `json:"pool,omitempty"`
	PoolAddress      string  `json:"poolAddress,omitempty"`
	TVLUSD           float64 `json:"tvlUsd,omitempty"`
	HistoricalAPY    float64 `json:"historicalApy,omitempty"`
	LPRangePct       float64 `json:"lpRangePct,omitempty"`
	PriceLower       float64 `json:"priceLower,omitempty"`
	PriceUpper       float64 `json:"priceUpper,omitempty"`
	HedgeQuantity    float64 `json:"hedgeQuantity,omitempty"`
	ResidualQuantity float64 `json:"residualQuantity,omitempty"`
	ObservedAt       int64   `json:"observedAt"`
	ReferenceUSD     float64 `json:"referenceUsd,omitempty"`
	DiscountPct      float64 `json:"discountPct,omitempty"`
	Eligibility      string  `json:"eligibility,omitempty"`
}

func KnownStrategy(s string) bool {
	switch s {
	case "spot-perpetual", "perpetual-perpetual", "spot-spot", "cex-cex", "funding", "xemm", "redemption", "hedged-lp":
		return true
	}
	return false
}
func IsSpecial(s string) bool {
	return s == "cex-cex" || s == "funding" || s == "xemm" || s == "redemption" || s == "hedged-lp"
}
func supportedLegs(b, s market.Book, strategy string) bool {
	switch strategy {
	case "spot-perpetual":
		return b.Instrument.Market == "spot" && s.Instrument.Market == "perpetual"
	case "perpetual-perpetual":
		return b.Instrument.Market == "perpetual" && s.Instrument.Market == "perpetual"
	case "spot-spot":
		return b.Instrument.Market == "spot" && s.Instrument.Market == "spot"
	case "cex-cex":
		return b.Instrument.Market == "spot" && s.Instrument.Market == "spot"
	case "funding":
		return s.Instrument.Market == "perpetual"
	case "xemm":
		return b.Instrument.Market == "spot"
	}
	return false
}

// Exact prefix integration of the known book, never an extrapolated price.
func BudgetQuantity(levels []market.Level, budget float64) (quantity float64, covered bool) {
	if !market.Finite(budget) || budget <= 0 {
		return 0, false
	}
	left := budget
	for _, l := range levels {
		take := math.Min(l.Quantity, left/l.Price)
		quantity += take
		left -= take * l.Price
		if left <= budget*1e-10 {
			return quantity, true
		}
	}
	return quantity, false
}
func totalQuantity(levels []market.Level) float64 {
	q := 0.0
	for _, l := range levels {
		q += l.Quantity
	}
	return q
}
func usedLevels(levels []market.Level, q float64) int {
	for n, l := range levels {
		q -= l.Quantity
		if q <= 1e-10 {
			return n + 1
		}
	}
	return len(levels)
}
func leg(b market.Book, price, q float64) Leg {
	return Leg{Instrument: b.Instrument, Price: price, Notional: q * price, BookAt: b.QuoteAt(), Bids: b.Bids, Asks: b.Asks, Funding: b.Funding}
}
func SizedExecution(b, s market.Book, amount float64) (Leg, Leg, float64, *Execution, bool) {
	wanted, covered := BudgetQuantity(b.Asks, amount)
	rounded := commonQuantity(wanted, b.Instrument.Step, s.Instrument.Step)
	qty := commonQuantity(math.Min(wanted, totalQuantity(s.Bids)), b.Instrument.Step, s.Instrument.Step)
	if qty <= 0 {
		return Leg{}, Leg{}, 0, nil, false
	}
	bp, bok := market.VWAP(b.Asks, qty)
	sp, sok := market.VWAP(s.Bids, qty)
	if !bok || !sok {
		return Leg{}, Leg{}, 0, nil, false
	}
	capacity := commonQuantity(math.Min(totalQuantity(b.Asks), totalQuantity(s.Bids)), b.Instrument.Step, s.Instrument.Step)
	cp, cok := market.VWAP(b.Asks, capacity)
	if !cok {
		cp = bp
	}
	full := covered && qty+1e-10 >= rounded
	status := "ready"
	if !full {
		status = "depth-insufficient"
	}
	if qty < b.Instrument.MinQuantity || qty < s.Instrument.MinQuantity || qty*bp < b.Instrument.MinNotional || qty*sp < s.Instrument.MinNotional {
		full = false
		status = "below-minimum"
	}
	ex := &Execution{RequestedUSDT: amount, FilledUSDT: qty * bp, CapacityUSDT: capacity * cp, FillPct: math.Min(100, qty*bp/amount*100), Full: full, Status: status, BuyLevels: usedLevels(b.Asks, qty), SellLevels: usedLevels(s.Bids, qty), BuySlippagePct: (bp/b.Asks[0].Price - 1) * 100, SellSlippagePct: (1 - sp/s.Bids[0].Price) * 100, DepthAt: min(b.QuoteAt(), s.QuoteAt())}
	return leg(b, bp, qty), leg(s, sp, qty), qty, ex, true
}
func CalculateSized(b, s market.Book, q Query, now time.Time) (Opportunity, bool) {
	if b.Instrument.Identity() != s.Instrument.Identity() || b.Instrument.ID == s.Instrument.ID || !supportedLegs(b, s, q.Strategy) || !b.Fresh(now, time.Duration(q.MaxAgeMS)*time.Millisecond) || !s.Fresh(now, time.Duration(q.MaxAgeMS)*time.Millisecond) {
		return Opportunity{}, false
	}
	if b.Instrument.Exchange == s.Instrument.Exchange && q.Strategy != "funding" {
		return Opportunity{}, false
	}
	if q.Strategy == "xemm" {
		return makerBuy(b, s, q, now)
	}
	quote := (s.Bids[0].Price/b.Asks[0].Price - 1) * 100
	if q.Strategy != "funding" && quote <= 0 {
		return Opportunity{}, false
	}
	buy, sell, qty, execution, ok := SizedExecution(b, s, q.Amount)
	if !ok {
		return Opportunity{}, false
	}
	o := Opportunity{ID: RouteID(b.Instrument.ID, s.Instrument.ID), Asset: b.Instrument.Asset, Strategy: q.Strategy, Buy: buy, Sell: sell, Quantity: qty, Execution: execution, QuoteSpreadPct: quote, EntrySpreadPct: (sell.Price/buy.Price - 1) * 100, DepthUSDT: execution.CapacityUSDT, AgeMS: now.UnixMilli() - execution.DepthAt, CalculatedAt: now.UnixMilli()}
	if IsSpecial(q.Strategy) {
		o.ID = RouteID(q.Strategy+":"+b.Instrument.ID, s.Instrument.ID)
	}
	if o.EntrySpreadPct <= 0 && q.Strategy != "funding" {
		execution.Status = "spread-lost"
	}
	if q.Strategy == "cex-cex" {
		fees := buy.Notional*fee(b.Instrument, q) + sell.Notional*fee(s.Instrument, q)
		gross := sell.Notional - buy.Notional
		o.EntryFees = fees
		o.NetEntryPct = (gross - fees) / buy.Notional * 100
		o.Metrics = &Metrics{Kind: "cex-cex", ValuePct: o.NetEntryPct, CashUSDT: gross - fees, GrossUSDT: gross, CostsUSDT: fees, ObservedAt: execution.DepthAt, Condition: "Одновременные taker-сделки; средства на двух биржах. Комиссии — публичная оценка; исполнение не гарантировано."}
		if gross <= fees && execution.Full {
			execution.Status = "costs-exceed-edge"
		}
	}
	if q.Strategy == "funding" {
		bf, bn, bok := 0.0, 0, true
		if b.Instrument.Market == "perpetual" {
			bf, bn, bok = funding(b.Funding, now, q.HorizonHours)
		}
		sf, sn, sok := funding(s.Funding, now, q.HorizonHours)
		if !bok || !sok {
			return Opportunity{}, false
		}
		cash := sell.Notional*sf - buy.Notional*bf
		if cash <= 0 {
			return Opportunity{}, false
		}
		costs := 2 * (buy.Notional*fee(b.Instrument, q) + sell.Notional*fee(s.Instrument, q))
		annual := s.Funding.Rate / float64(s.Funding.IntervalMS) * 86400000 * 365 * 100
		next := s.Funding.NextAt
		if b.Funding != nil && b.Instrument.Market == "perpetual" {
			annual -= b.Funding.Rate / float64(b.Funding.IntervalMS) * 86400000 * 365 * 100
			next = min(next, b.Funding.NextAt)
		}
		o.FundingEstimate = &cash
		o.FundingEvents = bn + sn
		o.Metrics = &Metrics{Kind: "funding", ValuePct: (cash - costs) / buy.Notional * 100, CashUSDT: cash - costs, FundingUSDT: cash, CostsUSDT: costs, FundingEvents: bn + sn, HorizonHours: q.HorizonHours, NextAt: next, AnnualFundingPct: annual, ObservedAt: min(s.Funding.ObservedAt, now.UnixMilli()), Condition: "Прогноз при неизменных ставках. Четыре комиссии оценены; изменение базиса и маржа не включены."}
		if cash <= costs && execution.Full {
			execution.Status = "costs-exceed-edge"
		}
	}
	return o, true
}
func makerBuy(b, s market.Book, q Query, now time.Time) (Opportunity, bool) {
	price := b.Bids[0].Price
	qty := commonQuantity(math.Min(q.Amount/price, totalQuantity(s.Bids)), b.Instrument.Step, s.Instrument.Step)
	if qty <= 0 {
		return Opportunity{}, false
	}
	hedge, ok := market.VWAP(s.Bids, qty)
	if !ok || hedge <= price {
		return Opportunity{}, false
	}
	full := qty+1e-10 >= commonQuantity(q.Amount/price, b.Instrument.Step, s.Instrument.Step)
	status := "maker-conditional"
	if !full {
		status = "depth-insufficient"
	}
	if qty < b.Instrument.MinQuantity || qty < s.Instrument.MinQuantity || qty*price < b.Instrument.MinNotional || qty*hedge < s.Instrument.MinNotional {
		full = false
		status = "below-minimum"
	}
	// Until an account-specific maker tariff is provided, conservatively use the public taker tariff. No invented rebates.
	costs := qty * (price*fee(b.Instrument, q) + hedge*fee(s.Instrument, q))
	gross := qty * (hedge - price)
	if gross <= costs {
		return Opportunity{}, false
	}
	capacityQty := MatchedQuantity([]market.Level{{Price: price, Quantity: totalQuantity(s.Bids)}}, s.Bids, fee(b.Instrument, q), fee(s.Instrument, q))
	capacity := commonQuantity(capacityQty, b.Instrument.Step, s.Instrument.Step) * price
	ex := &Execution{RequestedUSDT: q.Amount, FilledUSDT: qty * price, CapacityUSDT: capacity, FillPct: math.Min(100, qty*price/q.Amount*100), Full: full, Status: status, SellLevels: usedLevels(s.Bids, qty), SellSlippagePct: (1 - hedge/s.Bids[0].Price) * 100, DepthAt: min(b.QuoteAt(), s.QuoteAt())}
	return Opportunity{ID: RouteID("xemm:buy:"+b.Instrument.ID, s.Instrument.ID), Asset: b.Instrument.Asset, Strategy: "xemm", Buy: leg(b, price, qty), Sell: leg(s, hedge, qty), Quantity: qty, EntrySpreadPct: (hedge/price - 1) * 100, QuoteSpreadPct: (s.Bids[0].Price/price - 1) * 100, Execution: ex, DepthUSDT: capacity, CalculatedAt: now.UnixMilli(), Metrics: &Metrics{Kind: "xemm", MakerSide: "buy", ValuePct: (gross - costs) / (qty * price) * 100, CashUSDT: gross - costs, GrossUSDT: gross, CostsUSDT: costs, ObservedAt: ex.DepthAt, Condition: "Maker-покупка по текущему bid, затем taker-хедж. Исполнение и очередь maker неизвестны; maker-комиссия консервативно оценена taker-тарифом."}}, true
}
