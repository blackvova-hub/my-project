// Package engine is a deterministic, network-free, shared-capital simulator.
package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	bt "shortlong/backtest"
	"sort"
	"time"
)

type position struct {
	entry float64
	qty   float64
	fee   float64
	time  int64
}
type instrument struct {
	symbol   string
	candles  []bt.Candle
	cache    *seriesCache
	position *position
}

func ValidateCandles(c []bt.Candle, from, to int64, step int64) error {
	expected := int((to - from) / step)
	if len(c) != expected {
		return fmt.Errorf("Неполная история: получено %d из %d свечей. Выберите более позднее начало периода", len(c), expected)
	}
	for i, b := range c {
		if b.Time != from+int64(i)*step {
			return fmt.Errorf("Пропуск свечи %s", time.UnixMilli(from+int64(i)*step).UTC().Format(time.RFC3339))
		}
		for _, v := range []float64{b.Open, b.High, b.Low, b.Close, b.Volume} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return errors.New("История содержит некорректные числа")
			}
		}
		if b.Low <= 0 || b.High < b.Low || b.Open < b.Low || b.Open > b.High || b.Close < b.Low || b.Close > b.High || b.Volume < 0 {
			return errors.New("История содержит некорректную OHLCV свечу")
		}
	}
	return nil
}

// Run consumes only immutable candles. Progress and cancellation remain bounded
// even on a one-year, five-symbol, 5m request.
func Run(ctx context.Context, r bt.Request, data map[string][]bt.Candle, progress func(int)) (*bt.Result, error) {
	if err := r.Validate(r.To.Add(time.Second)); err != nil {
		return nil, err
	}
	step := bt.Interval(r.Timeframe).Milliseconds()
	warm := r.WarmupBars()
	from := r.From.UnixMilli() - int64(warm)*step
	symbols := append([]string(nil), r.Symbols...)
	sort.Strings(symbols)
	instruments := make([]instrument, 0, len(symbols))
	for _, symbol := range symbols {
		c := data[symbol]
		if err := ValidateCandles(c, from, r.To.UnixMilli(), step); err != nil {
			return nil, fmt.Errorf("%s: %w", symbol, err)
		}
		instruments = append(instruments, instrument{symbol: symbol, candles: c, cache: newCache(c)})
	}
	s := r.Strategy
	cash := s.InitialCapital
	feeRate, slip := s.FeePct/100, s.SlippagePct/100
	sign := 1.0
	if s.Direction == "short" {
		sign = -1
	}
	result := &bt.Result{EngineVersion: bt.EngineVersion, Trades: []bt.Trade{}}
	for _, symbol := range symbols {
		result.Signals = append(result.Signals, bt.SignalStats{Symbol: symbol})
	}
	equity := []bt.EquityPoint{{Time: r.From.UnixMilli(), Value: cash}}
	total := len(instruments[0].candles)
	count := total - warm
	mark := func(i int, opening bool) float64 {
		value := cash
		for k := range instruments {
			v := &instruments[k]
			if p := v.position; p != nil {
				price := v.candles[i].Close
				if opening {
					price = v.candles[i].Open
				}
				value += p.qty*p.entry + sign*p.qty*(price-p.entry)
			}
		}
		return value
	}
	closePosition := func(v *instrument, price float64, ts int64, reason string) {
		p := v.position
		fill := price * (1 - sign*slip)
		exitFee := p.qty * fill * feeRate
		pnl := sign*p.qty*(fill-p.entry) - p.fee - exitFee
		cash += p.qty*p.entry + sign*p.qty*(fill-p.entry) - exitFee
		result.Trades = append(result.Trades, bt.Trade{Symbol: v.symbol, Direction: s.Direction, EntryTime: p.time, ExitTime: ts, EntryPrice: p.entry, ExitPrice: fill, Quantity: p.qty, Fees: p.fee + exitFee, PnL: pnl, PnLPct: 100 * pnl / (p.qty * p.entry), Reason: reason})
		v.position = nil
	}
	for i := warm; i < total; i++ {
		if (i-warm)%128 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if progress != nil {
				progress((i - warm) * 100 / count)
			}
		}
		exited := make([]bool, len(instruments))
		entrySignals := make([]bool, len(instruments))
		if i > warm {
			for k := range instruments {
				matched, valid := instruments[k].cache.evaluate(s.Entry, i-1)
				if valid {
					result.Signals[k].EvaluatedBars++
				}
				entrySignals[k] = matched && valid
				if entrySignals[k] {
					result.Signals[k].MatchedBars++
				}
			}
		}
		// Existing positions: gaps get the opening price, then previous-close exits.
		for k := range instruments {
			v := &instruments[k]
			p := v.position
			if p == nil {
				continue
			}
			bar := v.candles[i]
			stop := p.entry * (1 - sign*s.StopLossPct/100)
			take := p.entry * (1 + sign*s.TakeProfitPct/100)
			reason := ""
			if sign*(bar.Open-stop) <= 0 {
				reason = "sl"
			} else if s.TakeProfitPct > 0 && sign*(bar.Open-take) >= 0 {
				reason = "tp"
			} else if s.Exit != nil {
				if matched, valid := v.cache.evaluate(*s.Exit, i-1); matched && valid {
					reason = "condition"
				}
			}
			if reason != "" {
				closePosition(v, bar.Open, bar.Time, reason)
				exited[k] = true
			}
		}
		// A common opening equity snapshot determines all allocations. Symbol order
		// is canonical, so permutations of the user's symbol list cannot change PnL.
		allocation := math.Max(0, mark(i, true)) * s.PositionSizePct / 100
		if i > warm {
			for k := range instruments {
				v := &instruments[k]
				if v.position != nil || exited[k] {
					continue
				}
				if !entrySignals[k] {
					continue
				}
				notional := math.Min(allocation, math.Max(cash, 0)/(1+feeRate))
				if notional <= 1e-8 {
					continue
				}
				fill := v.candles[i].Open * (1 + sign*slip)
				fee := notional * feeRate
				cash -= notional + fee
				v.position = &position{entry: fill, qty: notional / fill, fee: fee, time: v.candles[i].Time}
			}
		}
		for k := range instruments {
			v := &instruments[k]
			p := v.position
			if p == nil {
				continue
			}
			bar := v.candles[i]
			stop := p.entry * (1 - sign*s.StopLossPct/100)
			take := p.entry * (1 + sign*s.TakeProfitPct/100)
			stopHit, takeHit := bar.Low <= stop, bar.High >= take
			if sign < 0 {
				stopHit, takeHit = bar.High >= stop, bar.Low <= take
			}
			// OHLC cannot establish intrabar ordering. Always take the adverse outcome.
			if stopHit {
				fill, timestamp := stop, bar.Time+step
				// Entry slippage can put a very tight stop beyond the opening
				// market price. Execute that already-triggered stop at the open,
				// not at a more favourable price which the bar never traded.
				if p.time == bar.Time && sign*(bar.Open-stop) <= 0 {
					fill, timestamp = bar.Open, bar.Time
				}
				closePosition(v, fill, timestamp, "sl")
			} else if s.TakeProfitPct > 0 && takeHit {
				closePosition(v, take, bar.Time+step, "tp")
			} else if i == total-1 {
				closePosition(v, bar.Close, bar.Time+step, "end_of_period")
			}
		}
		if len(result.Trades) > 50000 {
			return nil, errors.New("Более 50 000 сделок: сократите период или уточните условия")
		}
		equity = append(equity, bt.EquityPoint{Time: instruments[0].candles[i].Time + step, Value: mark(i, false)})
	}
	result.Metrics = metrics(s.InitialCapital, equity, result.Trades)
	result.Equity = sampleEquity(equity, 1800)
	if progress != nil {
		progress(100)
	}
	return result, nil
}

func metrics(initial float64, equity []bt.EquityPoint, trades []bt.Trade) bt.Metrics {
	final := equity[len(equity)-1].Value
	m := bt.Metrics{FinalCapital: final, ProfitPct: 100 * (final/initial - 1), TradeCount: len(trades)}
	peak := initial
	for _, p := range equity {
		peak = math.Max(peak, p.Value)
		if peak > 0 {
			m.MaxDrawdownPct = math.Max(m.MaxDrawdownPct, 100*(peak-p.Value)/peak)
		}
	}
	wins := 0
	profit, loss := 0.0, 0.0
	for _, t := range trades {
		m.TotalFees += t.Fees
		if t.PnL > 0 {
			wins++
			profit += t.PnL
		} else {
			loss -= t.PnL
		}
	}
	if len(trades) > 0 {
		m.WinRate = 100 * float64(wins) / float64(len(trades))
	}
	if loss > 0 {
		pf := profit / loss
		m.ProfitFactor = &pf
	}
	// Daily marked-to-market returns, 365-day crypto year, risk-free rate zero.
	daily := []float64{initial}
	lastDay := (equity[0].Time) / 86400000
	for i := 1; i < len(equity); i++ {
		day := (equity[i].Time - 1) / 86400000
		if day != lastDay {
			daily = append(daily, equity[i-1].Value)
			lastDay = day
		}
	}
	daily = append(daily, final)
	returns := []float64{}
	for i := 1; i < len(daily); i++ {
		if daily[i-1] <= 0 {
			return m
		}
		returns = append(returns, daily[i]/daily[i-1]-1)
	}
	if len(returns) > 1 {
		avg := 0.0
		for _, v := range returns {
			avg += v
		}
		avg /= float64(len(returns))
		variance := 0.0
		for _, v := range returns {
			variance += (v - avg) * (v - avg)
		}
		variance /= float64(len(returns) - 1)
		if variance > 1e-18 {
			value := avg / math.Sqrt(variance) * math.Sqrt(365)
			m.Sharpe = &value
		}
	}
	return m
}

// Keep both extrema in each bucket, ordered by time. Metrics use full equity.
func sampleEquity(v []bt.EquityPoint, limit int) []bt.EquityPoint {
	if len(v) <= limit {
		return v
	}
	size := int(math.Ceil(float64(len(v)-2) / float64((limit-2)/2)))
	out := []bt.EquityPoint{v[0]}
	for start := 1; start < len(v)-1; start += size {
		end := min(start+size, len(v)-1)
		lo, hi := start, start
		for i := start + 1; i < end; i++ {
			if v[i].Value < v[lo].Value {
				lo = i
			}
			if v[i].Value > v[hi].Value {
				hi = i
			}
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		out = append(out, v[lo])
		if hi != lo {
			out = append(out, v[hi])
		}
	}
	return append(out, v[len(v)-1])
}
