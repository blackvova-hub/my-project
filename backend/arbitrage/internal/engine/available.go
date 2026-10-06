package engine

import (
	"math"
	"shortlong/arbitrage/internal/market"
	"time"
)

// MatchedQuantity walks both books in base units and stops before a marginal
// trade loses its edge. It cannot include unrelated or unprofitable depth.
func MatchedQuantity(asks, bids []market.Level, buyFee, sellFee float64) float64 {
	i, j, quantity, askLeft, bidLeft := 0, 0, 0.0, 0.0, 0.0
	for i < len(asks) && j < len(bids) {
		if asks[i].Price*(1+buyFee) >= bids[j].Price*(1-sellFee) {
			break
		}
		if askLeft <= 0 {
			askLeft = asks[i].Quantity
		}
		if bidLeft <= 0 {
			bidLeft = bids[j].Quantity
		}
		take := math.Min(askLeft, bidLeft)
		quantity += take
		askLeft -= take
		bidLeft -= take
		if askLeft <= 1e-12 {
			i++
			askLeft = 0
		}
		if bidLeft <= 1e-12 {
			j++
			bidLeft = 0
		}
	}
	return quantity
}

// The ordinary scanner has an availability filter, not an implicit order budget.
// Both displayed prices are VWAPs over the same available, rounded quantity.
func CalculateAvailable(b, s market.Book, q Query, now time.Time) (Opportunity, bool) {
	if b.Instrument.Identity() != s.Instrument.Identity() || b.Instrument.ID == s.Instrument.ID || b.Instrument.Exchange == s.Instrument.Exchange || !supportedLegs(b, s, q.Strategy) || !b.Fresh(now, time.Duration(q.MaxAgeMS)*time.Millisecond) || !s.Fresh(now, time.Duration(q.MaxAgeMS)*time.Millisecond) {
		return Opportunity{}, false
	}
	bf, sf := 0.0, 0.0
	if q.Strategy == "cex-cex" {
		bf, sf = fee(b.Instrument, q), fee(s.Instrument, q)
	}
	quantity := commonQuantity(MatchedQuantity(b.Asks, s.Bids, bf, sf), b.Instrument.Step, s.Instrument.Step)
	bp, bok := market.VWAP(b.Asks, quantity)
	sp, sok := market.VWAP(s.Bids, quantity)
	if quantity <= 0 || !bok || !sok || quantity < b.Instrument.MinQuantity || quantity < s.Instrument.MinQuantity || bp*quantity < b.Instrument.MinNotional || sp*quantity < s.Instrument.MinNotional {
		return Opportunity{}, false
	}
	at := min(b.QuoteAt(), s.QuoteAt())
	capacity := quantity * bp
	ex := &Execution{RequestedUSDT: capacity, FilledUSDT: capacity, CapacityUSDT: capacity, FillPct: 100, Full: true, Status: "ready", BuyLevels: usedLevels(b.Asks, quantity), SellLevels: usedLevels(s.Bids, quantity), BuySlippagePct: (bp/b.Asks[0].Price - 1) * 100, SellSlippagePct: (1 - sp/s.Bids[0].Price) * 100, DepthAt: at}
	o := Opportunity{ID: RouteID(b.Instrument.ID, s.Instrument.ID), Asset: b.Instrument.Asset, Strategy: q.Strategy, Buy: leg(b, bp, quantity), Sell: leg(s, sp, quantity), Quantity: quantity, Execution: ex, DepthUSDT: capacity, EntrySpreadPct: (sp/bp - 1) * 100, QuoteSpreadPct: (s.Bids[0].Price/b.Asks[0].Price - 1) * 100, AgeMS: now.UnixMilli() - at, CalculatedAt: now.UnixMilli()}
	if q.Strategy == "cex-cex" {
		o.ID = RouteID("cex-cex:"+b.Instrument.ID, s.Instrument.ID)
		gross := quantity * (sp - bp)
		costs := quantity * (bp*bf + sp*sf)
		o.EntryFees = costs
		o.NetEntryPct = (gross - costs) / capacity * 100
		o.Metrics = &Metrics{Kind: "cex-cex", ValuePct: o.NetEntryPct, CashUSDT: gross - costs, GrossUSDT: gross, CostsUSDT: costs, ObservedAt: at, Condition: "Одновременные taker-сделки; средства на двух биржах. Комиссии — публичная оценка; исполнение не гарантировано."}
	}
	return o, true
}
