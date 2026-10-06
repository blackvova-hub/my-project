package engine

import (
	"math"
	"shortlong/arbitrage/internal/market"
	"time"
)

// Selling an owned spot inventory at maker ask, then buying the same base
// quantity on the hedge venue. No assumed spot borrowing or negative inventory.
func makerSell(b, s market.Book, q Query, now time.Time) (Opportunity, bool) {
	if b.Instrument.Identity() != s.Instrument.Identity() || b.Instrument.Exchange == s.Instrument.Exchange || b.Instrument.Market != "spot" || s.Instrument.Market != "spot" || !b.Fresh(now, time.Duration(q.MaxAgeMS)*time.Millisecond) || !s.Fresh(now, time.Duration(q.MaxAgeMS)*time.Millisecond) {
		return Opportunity{}, false
	}
	price := s.Asks[0].Price
	wanted, covered := BudgetQuantity(b.Asks, q.Amount)
	quantity := commonQuantity(wanted, b.Instrument.Step, s.Instrument.Step)
	if quantity <= 0 {
		return Opportunity{}, false
	}
	bp, ok := market.VWAP(b.Asks, quantity)
	if !ok || bp >= price {
		return Opportunity{}, false
	}
	costs := quantity * (bp*fee(b.Instrument, q) + price*fee(s.Instrument, q))
	gross := quantity * (price - bp)
	if gross <= costs {
		return Opportunity{}, false
	}
	status := "maker-conditional"
	full := covered
	if !full {
		status = "depth-insufficient"
	}
	if quantity < b.Instrument.MinQuantity || quantity < s.Instrument.MinQuantity || quantity*bp < b.Instrument.MinNotional || quantity*price < s.Instrument.MinNotional {
		full = false
		status = "below-minimum"
	}
	capacityQty := commonQuantity(MatchedQuantity(b.Asks, []market.Level{{Price: price, Quantity: totalQuantity(b.Asks)}}, fee(b.Instrument, q), fee(s.Instrument, q)), b.Instrument.Step, s.Instrument.Step)
	cp, cok := market.VWAP(b.Asks, capacityQty)
	if !cok {
		return Opportunity{}, false
	}
	capacity := capacityQty * cp
	at := min(b.QuoteAt(), s.QuoteAt())
	ex := &Execution{RequestedUSDT: q.Amount, FilledUSDT: quantity * bp, CapacityUSDT: capacity, FillPct: math.Min(100, quantity*bp/q.Amount*100), Full: full, Status: status, BuyLevels: usedLevels(b.Asks, quantity), BuySlippagePct: (bp/b.Asks[0].Price - 1) * 100, DepthAt: at}
	return Opportunity{ID: RouteID(b.Instrument.ID, "xemm:sell:"+s.Instrument.ID), Asset: b.Instrument.Asset, Strategy: "xemm", Buy: leg(b, bp, quantity), Sell: leg(s, price, quantity), Quantity: quantity, Execution: ex, DepthUSDT: capacity, EntrySpreadPct: (price/bp - 1) * 100, QuoteSpreadPct: (price/b.Asks[0].Price - 1) * 100, CalculatedAt: now.UnixMilli(), Metrics: &Metrics{Kind: "xemm", MakerSide: "sell", ValuePct: (gross - costs) / (quantity * bp) * 100, CashUSDT: gross - costs, GrossUSDT: gross, CostsUSDT: costs, ObservedAt: at, Condition: "Maker-продажа своего спота по ask, затем taker-покупка хеджа. Нужен запас монет; исполнение и очередь maker неизвестны. Maker-комиссия оценена taker-тарифом."}}, true
}
