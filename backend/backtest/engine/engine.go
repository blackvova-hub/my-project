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
	symbol      string
	candles     []bt.Candle
	cache       *seriesCache
	position    *position
	nextEntryAt int64
	offset      int // Index of the first real candle on the shared request timeline.
}

func (v *instrument) barAt(i int) (bt.Candle, int, bool) {
	index := i - v.offset
	if index < 0 || index >= len(v.candles) {
		return bt.Candle{}, index, false
	}
	return v.candles[index], index, true
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
	from, to := r.From.UnixMilli(), r.To.UnixMilli()
	historyFrom := from - int64(warm)*step
	symbols := append([]string(nil), r.Symbols...)
	sort.Strings(symbols)
	instruments := make([]instrument, 0, len(symbols))
	result := &bt.Result{EngineVersion: bt.EngineVersion, Trades: []bt.Trade{}}
	usable := false
	for _, symbol := range symbols {
		c := data[symbol]
		stats := bt.SignalStats{Symbol: symbol}
		v := instrument{symbol: symbol, candles: c}
		if len(c) == 0 {
			stats.SkippedReason = "Нет истории в выбранном периоде"
			instruments = append(instruments, v)
			result.Signals = append(result.Signals, stats)
			continue
		}
		first := c[0].Time
		if first < historyFrom || first >= to || first%step != 0 {
			return nil, fmt.Errorf("%s: Некорректное начало истории", symbol)
		}
		// Only a missing prefix is allowed. Interior and trailing gaps remain fatal.
		if err := ValidateCandles(c, first, to, step); err != nil {
			return nil, fmt.Errorf("%s: %w", symbol, err)
		}
		stats.AvailableFrom = first
		// The first warm bars seed indicators; the following closed bar supplies
		// the first signal. Signals before the requested period cannot open trades.
		tradingFrom := max(from+step, first+int64(warm+1)*step)
		if tradingFrom < to {
			stats.TradingFrom = tradingFrom
			usable = true
		} else {
			stats.SkippedReason = "Недостаточно истории для прогрева и проверки входа"
		}
		v.offset = int((first - from) / step)
		v.cache = newCache(c)
		instruments = append(instruments, v)
		result.Signals = append(result.Signals, stats)
	}
	if !usable {
		return nil, errors.New("Недостаточно истории для прогрева и проверки входа по всем выбранным парам. Выберите другой период или другие пары")
	}
	s := r.Strategy
	cash := s.InitialCapital
	feeRate, slip := s.FeePct/100, s.SlippagePct/100
	sign := 1.0
	if s.Direction == "short" {
		sign = -1
	}
	equity := []bt.EquityPoint{{Time: from, Value: cash}}
	count := int((to - from) / step)
	mark := func(i int, opening bool) float64 {
		value := cash
		for k := range instruments {
			v := &instruments[k]
			if p := v.position; p != nil {
				bar, _, available := v.barAt(i)
				if !available {
					continue // Positions can only exist within a validated real tail.
				}
				price := bar.Close
				if opening {
					price = bar.Open
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
		v.nextEntryAt = max(v.nextEntryAt, ts+bt.ReentryCooldown.Milliseconds())
	}
	for i := 0; i < count; i++ {
		if i%128 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if progress != nil {
				progress(i * 100 / count)
			}
		}
		exited := make([]bool, len(instruments))
		entrySignals := make([]bool, len(instruments))
		if i > 0 {
			for k := range instruments {
				v := &instruments[k]
				_, index, available := v.barAt(i)
				if !available || index <= warm {
					continue
				}
				matched, valid := v.cache.evaluate(s.Entry, index-1)
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
			bar, index, available := v.barAt(i)
			if !available {
				continue
			}
			stop := p.entry * (1 - sign*s.StopLossPct/100)
			take := p.entry * (1 + sign*s.TakeProfitPct/100)
			reason := ""
			if sign*(bar.Open-stop) <= 0 {
				reason = "sl"
			} else if s.TakeProfitPct > 0 && sign*(bar.Open-take) >= 0 {
				reason = "tp"
			} else if s.Exit != nil {
				if matched, valid := v.cache.evaluate(*s.Exit, index-1); matched && valid {
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
		if i > 0 {
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
				bar, _, available := v.barAt(i)
				if !available {
					continue
				}
				if bar.Time < v.nextEntryAt {
					continue
				}
				fill := bar.Open * (1 + sign*slip)
				fee := notional * feeRate
				cash -= notional + fee
				v.position = &position{entry: fill, qty: notional / fill, fee: fee, time: bar.Time}
				v.nextEntryAt = bar.Time + bt.ReentryCooldown.Milliseconds()
			}
		}
		for k := range instruments {
			v := &instruments[k]
			p := v.position
			if p == nil {
				continue
			}
			bar, _, available := v.barAt(i)
			if !available {
				continue
			}
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
			} else if i == count-1 {
				closePosition(v, bar.Close, bar.Time+step, "end_of_period")
			}
		}
		if len(result.Trades) > 50000 {
			return nil, errors.New("Более 50 000 сделок: сократите период или уточните условия")
		}
		equity = append(equity, bt.EquityPoint{Time: from + int64(i+1)*step, Value: mark(i, false)})
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
