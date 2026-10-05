package engine

import (
	"math"
	bt "shortlong/backtest"
)

// These constants and rules match shared/market/smartMoneyEngine.ts. Events
// are stamped at confirmation, never at the older pivot / zone origin candle.
const smcSwing = 5

type smcPivot struct {
	index               int
	price, atr          float64
	high, broken, swept bool
}
type smcLevel struct {
	price               float64
	high, broken, swept bool
	source              *smcPivot
}

func smcATR(c []bt.Candle) []float64 {
	out := emptySeries(len(c))
	sum := 0.0
	for i, b := range c {
		prev := b.Close
		if i > 0 {
			prev = c[i-1].Close
		}
		tr := math.Max(b.High-b.Low, math.Max(math.Abs(b.High-prev), math.Abs(b.Low-prev)))
		if i < 14 {
			sum += tr
			if i == 13 {
				out[i] = sum / 14
			}
		} else {
			out[i] = (out[i-1]*13 + tr) / 14
		}
	}
	return out
}
func smcDisplacement(b bt.Candle, atr float64, high bool, multiple float64) bool {
	body, span := math.Abs(b.Close-b.Open), b.High-b.Low
	if math.IsNaN(atr) || atr <= 0 || span <= 0 || body < atr*multiple || body/span < 0.6 {
		return false
	}
	if high {
		return b.Close > b.Open && (b.High-b.Close)/span <= 0.25
	}
	return b.Close < b.Open && (b.Close-b.Low)/span <= 0.25
}
func smcIsPivot(c []bt.Candle, i int, high bool) bool {
	price := c[i].Low
	if high {
		price = c[i].High
	}
	for j := 1; j <= smcSwing; j++ {
		if high {
			if price <= c[i-j].High || price < c[i+j].High {
				return false
			}
		} else if price >= c[i-j].Low || price > c[i+j].Low {
			return false
		}
	}
	return true
}

func smartMoney(c []bt.Candle) map[string][]int {
	out := map[string][]int{}
	for _, event := range []string{"bos", "choch", "mss", "liquidity_sweep", "fvg", "order_block"} {
		for _, direction := range []string{"bullish", "bearish"} {
			a := make([]int, len(c))
			for i := range a {
				a[i] = -1
			}
			out[event+":"+direction] = a
		}
	}
	emit := func(event string, bullish bool, i int) {
		d := "bearish"
		if bullish {
			d = "bullish"
		}
		out[event+":"+d][i] = i
	}
	atr := smcATR(c)
	pivots := []*smcPivot{}
	levels := []*smcLevel{}
	equals := []*smcLevel{}
	var previous [2]*smcPivot
	bias := 0
	for i, b := range c {
		// A pivot becomes visible only after five candles on its right close.
		origin := i - smcSwing
		if origin >= smcSwing && !math.IsNaN(atr[origin]) {
			for kind := 0; kind < 2; kind++ {
				high := kind == 0
				if !smcIsPivot(c, origin, high) {
					continue
				}
				price := c[origin].Low
				if high {
					price = c[origin].High
				}
				p := &smcPivot{index: origin, price: price, atr: atr[origin], high: high}
				if prev := previous[kind]; prev != nil && !prev.broken && !prev.swept && origin-prev.index >= 2*smcSwing && math.Abs(price-prev.price) <= math.Max(p.atr, prev.atr)*0.1 {
					equals = append(equals, &smcLevel{price: (price + prev.price) / 2, high: high})
				}
				previous[kind] = p
				pivots = append(pivots, p)
				levels = append(levels, &smcLevel{price: price, high: high, source: p})
			}
		}
		a := atr[i]
		if math.IsNaN(a) || a <= 0 {
			continue
		}
		for _, high := range []bool{true, false} {
			for _, list := range [][]*smcLevel{equals, levels} {
				for j := len(list) - 1; j >= 0; j-- {
					l := list[j]
					if l.high != high || l.broken || l.swept {
						continue
					}
					swept := b.Low < l.price-a*0.02 && b.Close >= l.price
					if high {
						swept = b.High > l.price+a*0.02 && b.Close <= l.price
					}
					if swept {
						l.swept = true
						if l.source != nil {
							l.source.swept = true
						}
						emit("liquidity_sweep", !high, i)
					}
					break
				}
			}
		}
		var broken *smcPivot
		for _, high := range []bool{true, false} {
			for j := len(pivots) - 1; j >= 0; j-- {
				p := pivots[j]
				if p.high != high || p.broken {
					continue
				}
				threshold := p.price - a*0.02
				cross := b.Close < threshold && c[i-1].Close >= threshold
				if high {
					threshold = p.price + a*0.02
					cross = b.Close > threshold && c[i-1].Close <= threshold
				}
				if cross {
					broken = p
				}
				break
			}
			if broken != nil {
				break
			}
		}
		if p := broken; p != nil {
			sign := -1
			if p.high {
				sign = 1
			}
			choch := bias != 0 && bias != sign
			if choch {
				emit("choch", p.high, i)
				if smcDisplacement(b, a, p.high, 1) {
					emit("mss", p.high, i)
				}
			} else {
				emit("bos", p.high, i)
			}
			for _, v := range pivots {
				if (p.high && v.high && b.Close > v.price) || (!p.high && !v.high && b.Close < v.price) {
					v.broken = true
				}
			}
			for _, list := range [][]*smcLevel{levels, equals} {
				for _, v := range list {
					if (p.high && v.high && b.Close > v.price) || (!p.high && !v.high && b.Close < v.price) {
						v.broken = true
					}
				}
			}
			bias = sign
			if smcDisplacement(b, a, p.high, 0.5) || math.Abs(b.Close-p.price) >= a*0.5 {
				for j := i - 1; j >= max(p.index, i-24); j-- {
					v := c[j]
					opposing := v.Close > v.Open
					if p.high {
						opposing = v.Close < v.Open
					}
					if opposing && !math.IsNaN(atr[j]) && atr[j] > 0 && math.Abs(v.Close-v.Open) >= atr[j]*0.1 && (v.High-v.Low) > 0 && math.Abs(v.Close-v.Open)/(v.High-v.Low) >= 0.2 {
						emit("order_block", p.high, i)
						break
					}
				}
			}
			// Broken levels cannot participate again. Keep memory / scans bounded by
			// unresolved structure rather than every historical pivot.
			live := pivots[:0]
			for _, v := range pivots {
				if !v.broken {
					live = append(live, v)
				}
			}
			pivots = live
			for _, list := range []*[]*smcLevel{&levels, &equals} {
				live := (*list)[:0]
				for _, v := range *list {
					if !v.broken && !v.swept {
						live = append(live, v)
					}
				}
				*list = live
			}
		}
		if i >= 2 {
			if b.Low-c[i-2].High >= a*0.15 && smcDisplacement(c[i-1], atr[i-1], true, 0.5) {
				emit("fvg", true, i)
			} else if c[i-2].Low-b.High >= a*0.15 && smcDisplacement(c[i-1], atr[i-1], false, 0.5) {
				emit("fvg", false, i)
			}
		}
	}
	for _, v := range out {
		last := -1
		for i := range v {
			if v[i] >= 0 {
				last = i
			}
			v[i] = last
		}
	}
	return out
}
