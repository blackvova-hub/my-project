package analytics

import (
	"math"
	"sort"
	"strconv"
)

// Reconstruct uses flat-to-flat inventory accounting. Exchange position-side is
// part of the key so hedge-mode longs and shorts can never cancel each other.
// Reversals split a fill and its fee proportionally at the zero boundary.
func Reconstruct(fills []Fill, ledger []Ledger) []Trade {
	sort.SliceStable(fills, func(i, j int) bool {
		if fills[i].At == fills[j].At {
			return fills[i].ID < fills[j].ID
		}
		return fills[i].At < fills[j].At
	})
	active := map[string]*Trade{}
	out := []Trade{}
	avg := map[string]float64{}
	exitQty := map[string]float64{}
	for _, original := range fills {
		if original.Quantity <= 0 || original.Price <= 0 {
			continue
		}
		f := original
		key := f.ConnectionID + ":" + f.Market + ":" + f.Symbol + ":" + f.PositionSide
		for part := 0; f.Quantity > 1e-10 && part < 2; part++ {
			t := active[key]
			if t == nil {
				side := "LONG"
				if f.Side == "SELL" {
					side = "SHORT"
				}
				complete := true
				// A closing fill at the history boundary has an unknown opening basis.
				if f.ClosedQuantity != nil && *f.ClosedQuantity > 0 && part == 0 {
					complete = false
				}
				if f.Market == "spot" && side == "SHORT" {
					complete = false
				}
				if (f.PositionSide == "LONG" && f.Side == "SELL") || (f.PositionSide == "SHORT" && f.Side == "BUY") {
					complete = false
				}
				t = &Trade{ID: f.ConnectionID + ":" + f.ID + ":" + strconv.Itoa(part), ConnectionID: f.ConnectionID, Exchange: f.Exchange, Symbol: f.Symbol, Market: f.Market, Side: side, OpenedAt: f.At, Leverage: f.Leverage, Complete: complete, Fills: []Fill{}}
				active[key] = t
				avg[key] = 0
				exitQty[key] = 0
				if !complete {
					// Preserve a boundary closing fill as an explicitly incomplete
					// trade rather than inventing an opposite opening position.
					if t.Side == "LONG" {
						t.Side = "SHORT"
					} else {
						t.Side = "LONG"
					}
					t.Remaining = f.Quantity
					t.Quantity = f.Quantity
				}
			}
			isEntry := (t.Side == "LONG" && f.Side == "BUY") || (t.Side == "SHORT" && f.Side == "SELL")
			if isEntry {
				f.Action = "Add"
				if t.Quantity == 0 {
					f.Action = "Entry"
				}
				avg[key] = (avg[key]*t.Remaining + f.Price*f.Quantity) / (t.Remaining + f.Quantity)
				t.Entry = (t.Entry*t.Quantity + f.Price*f.Quantity) / (t.Quantity + f.Quantity)
				t.Quantity += f.Quantity
				t.Remaining += f.Quantity
				t.Size += f.Quantity * f.Price
				t.Fees += f.Fee
				t.Complete = t.Complete && f.FeeKnown
				t.Fills = append(t.Fills, f)
				f.Quantity = 0
			} else {
				q := math.Min(t.Remaining, f.Quantity)
				used := f
				used.Quantity = q
				used.Fee = f.Fee * q / f.Quantity
				used.Action = "Partial close"
				if q >= t.Remaining-1e-10 {
					used.Action = "Exit"
				}
				sign := 1.0
				if t.Side == "SHORT" {
					sign = -1
				}
				t.Gross += (f.Price - avg[key]) * q * sign
				t.Exit = (t.Exit*exitQty[key] + q*f.Price) / (exitQty[key] + q)
				exitQty[key] += q
				t.Remaining -= q
				t.Fees += used.Fee
				t.Fills = append(t.Fills, used)
				t.Complete = t.Complete && f.FeeKnown
				f.Quantity -= q
				f.Fee -= used.Fee
				if t.Remaining < 1e-10 {
					at := f.At
					t.ClosedAt = &at
					t.Duration = float64(at-t.OpenedAt) / 60000
					out = append(out, *t)
					delete(active, key)
				}
			}
		}
	}
	for _, t := range active {
		out = append(out, *t)
	}
	// Funding belongs only to the position open at settlement. Ambiguous hedge
	// settlements are kept in the ledger and not arbitrarily assigned to a trade.
	for _, l := range ledger {
		if l.Category != "funding" || (l.Currency != "USDT" && l.Currency != "USD" && l.Currency != "USDC") {
			continue
		}
		idx := -1
		matches := 0
		for i := range out {
			t := &out[i]
			if t.ConnectionID == l.ConnectionID && t.Symbol == l.Symbol && t.OpenedAt <= l.At && (t.ClosedAt == nil || *t.ClosedAt >= l.At) {
				idx = i
				matches++
			}
		}
		if matches == 1 {
			out[idx].Funding += l.Amount
		} else if matches > 1 {
			for i := range out {
				t := &out[i]
				if t.ConnectionID == l.ConnectionID && t.Symbol == l.Symbol && t.OpenedAt <= l.At && (t.ClosedAt == nil || *t.ClosedAt >= l.At) {
					t.Complete = false
				}
			}
		}
	}
	for i := range out {
		out[i].Net = out[i].Gross - out[i].Fees + out[i].Funding
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenedAt > out[j].OpenedAt })
	return out
}

// Excursions uses mark candles and the inventory actually held at each candle.
// Candles containing executions are excluded: OHLC cannot establish intrabar
// event order. Values are conservative sampled excursions, not tick precision.
func Excursions(t Trade, candles []Candle) (*float64, *float64, *float64) {
	if !t.Complete || t.ClosedAt == nil || len(candles) == 0 {
		return nil, nil, nil
	}
	fills := append([]Fill(nil), t.Fills...)
	sort.SliceStable(fills, func(i, j int) bool { return fills[i].At < fills[j].At })
	var qty, basis, realized, mfe, mae float64
	idx := 0
	observed := false
	sign := 1.0
	if t.Side == "SHORT" {
		sign = -1
	}
	for _, c := range candles {
		start := c.Time * 1000
		end := start + 60000
		for idx < len(fills) && fills[idx].At <= start {
			f := fills[idx]
			if f.Action == "Entry" || f.Action == "Add" {
				basis = (basis*qty + f.Price*f.Quantity) / (qty + f.Quantity)
				qty += f.Quantity
			} else {
				realized += (f.Price - basis) * f.Quantity * sign
				qty -= f.Quantity
			}
			idx++
		}
		if qty <= 1e-10 || start < t.OpenedAt || end > *t.ClosedAt || (idx < len(fills) && fills[idx].At < end) {
			continue
		}
		hi := realized + (c.High-basis)*qty*sign
		lo := realized + (c.Low-basis)*qty*sign
		if hi < lo {
			hi, lo = lo, hi
		}
		mfe = math.Max(mfe, hi)
		mae = math.Min(mae, lo)
		observed = true
	}
	if !observed {
		return nil, nil, nil
	}
	// The final realized outcome is another observed point. Including it
	// prevents a sampled maximum below the known closing profit.
	mfe = math.Max(mfe, t.Gross)
	mae = math.Min(mae, t.Gross)
	var captured *float64
	if mfe > 0 {
		v := t.Gross / mfe * 100
		captured = &v
	}
	return &mfe, &mae, captured
}
