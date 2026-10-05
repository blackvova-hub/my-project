package engine

import (
	"math"
	bt "shortlong/backtest"
)

type ruleKey struct {
	indicator, measure string
	period, hours      int
}

// Every series is causal. Missing inputs stay NaN; no forward fill of flows or
// OI across gaps and no conversion of unavailable data into a zero signal.
func rollingSum(v []float64, n int) []float64 {
	out := emptySeries(len(v))
	sum, missing := 0.0, 0
	for i, x := range v {
		if math.IsNaN(x) {
			missing++
		} else {
			sum += x
		}
		if i >= n {
			if math.IsNaN(v[i-n]) {
				missing--
			} else {
				sum -= v[i-n]
			}
		}
		if i >= n-1 && missing == 0 {
			out[i] = sum
		}
	}
	return out
}
func percentChange(v []float64, n int) []float64 {
	out := emptySeries(len(v))
	for i := n; i < len(v); i++ {
		if v[i-n] > 0 {
			out[i] = (v[i] - v[i-n]) * 100 / v[i-n]
		}
	}
	return out
}
func (s *seriesCache) metric(name string) []float64 {
	v := emptySeries(len(s.candles))
	for i, c := range s.candles {
		if x, ok := c.Metrics[name]; ok {
			v[i] = x
		}
	}
	return v
}
func (s *seriesCache) ruleSeries(c bt.Condition) []float64 {
	key := ruleKey{c.Indicator, c.Measure, c.Period, c.WindowHours}
	if v, ok := s.rules[key]; ok {
		return v
	}
	n := c.WindowHours
	if len(s.candles) > 1 {
		n = int(int64(c.WindowHours) * 3600000 / (s.candles[1].Time - s.candles[0].Time))
	}
	if n < 1 {
		n = 1
	}
	out := emptySeries(len(s.close))
	volume := make([]float64, len(s.close))
	for i, candle := range s.candles {
		volume[i] = candle.Volume
	}
	switch c.Indicator {
	case "price":
		out = percentChange(s.close, n)
	case "openInterest":
		out = percentChange(s.metric("openInterest"), n)
	case "volume", "tradeBuyVolume", "tradeSellVolume":
		v := volume
		if c.Indicator != "volume" {
			v = s.metric(c.Indicator)
		}
		sum := rollingSum(v, n)
		switch c.Measure {
		case "sum":
			out = sum
		case "change_pct":
			out = percentChange(sum, n)
		case "relative":
			for i := n; i < len(sum); i++ {
				if sum[i-n] > 0 {
					out[i] = sum[i] / sum[i-n]
				}
			}
		}
	case "cvd", "delta", "obv":
		delta, total := emptySeries(len(volume)), volume
		if c.Indicator == "obv" {
			for i := 1; i < len(delta); i++ {
				delta[i] = 0
				if s.close[i] > s.close[i-1] {
					delta[i] = volume[i]
				} else if s.close[i] < s.close[i-1] {
					delta[i] = -volume[i]
				}
			}
		} else {
			buy, sell := s.metric("tradeBuyVolume"), s.metric("tradeSellVolume")
			total = emptySeries(len(volume))
			for i := range delta {
				delta[i] = buy[i] - sell[i]
				total[i] = buy[i] + sell[i]
			}
		}
		out = rollingSum(delta, n)
		if c.Measure == "imbalance_pct" {
			sums := rollingSum(total, n)
			for i := range out {
				if sums[i] > 0 {
					out[i] = math.Max(-100, math.Min(100, out[i]/sums[i]*100))
				} else {
					out[i] = math.NaN()
				}
			}
		}
	case "rsi":
		out = s.series(bt.Operand{Kind: "rsi", Period: c.Period})
		if c.Measure == "change" {
			v := emptySeries(len(out))
			for i := n; i < len(out); i++ {
				v[i] = out[i] - out[i-n]
			}
			out = v
		}
	case "macd":
		kind := "macd_histogram"
		if c.Measure == "line" {
			kind = "macd"
		}
		out = s.series(bt.Operand{Kind: kind})
	case "ema", "sma":
		avg := s.series(bt.Operand{Kind: c.Indicator, Period: c.Period})
		for i, x := range avg {
			if x > 0 {
				out[i] = (s.close[i]/x - 1) * 100
			}
		}
	case "atr":
		tr := emptySeries(len(out))
		for i := 1; i < len(tr); i++ {
			b := s.candles[i]
			tr[i] = math.Max(b.High-b.Low, math.Max(math.Abs(b.High-s.close[i-1]), math.Abs(b.Low-s.close[i-1])))
		}
		sum, last := 0.0, math.NaN()
		for i := 1; i < len(tr); i++ {
			if i <= c.Period {
				sum += tr[i]
				if i == c.Period {
					last = sum / float64(c.Period)
				}
			} else {
				last = (last*float64(c.Period-1) + tr[i]) / float64(c.Period)
			}
			out[i] = last / s.close[i] * 100
		}
	case "stochastic", "bollinger":
		for i := c.Period - 1; i < len(out); i++ {
			lo, hi, sum := math.Inf(1), math.Inf(-1), 0.0
			for j := i - c.Period + 1; j <= i; j++ {
				b := s.candles[j]
				lo = math.Min(lo, b.Low)
				hi = math.Max(hi, b.High)
				sum += b.Close
			}
			if c.Indicator == "stochastic" {
				if hi > lo {
					out[i] = (s.close[i] - lo) / (hi - lo) * 100
				} else {
					out[i] = 50
				}
				continue
			}
			mean := sum / float64(c.Period)
			variance := 0.0
			for j := i - c.Period + 1; j <= i; j++ {
				d := s.close[j] - mean
				variance += d * d
			}
			sd := math.Sqrt(variance / float64(c.Period))
			if c.Measure == "width" {
				out[i] = 4 * sd / mean * 100
			} else if sd > 0 {
				out[i] = (s.close[i] - (mean - 2*sd)) / (4 * sd) * 100
			} else {
				out[i] = 50
			}
		}
	case "mfi":
		positive, negative := emptySeries(len(out)), emptySeries(len(out))
		previous := 0.0
		for i, b := range s.candles {
			typical := (b.High + b.Low + b.Close) / 3
			if i > 0 {
				positive[i] = 0
				negative[i] = 0
				if typical > previous {
					positive[i] = typical * b.Volume
				} else if typical < previous {
					negative[i] = typical * b.Volume
				}
			}
			previous = typical
		}
		pos, neg := rollingSum(positive, c.Period), rollingSum(negative, c.Period)
		for i := c.Period; i < len(out); i++ {
			if pos[i]+neg[i] == 0 {
				out[i] = 50
			} else {
				out[i] = pos[i] / (pos[i] + neg[i]) * 100
			}
		}
	case "volatility":
		returns := emptySeries(len(out))
		for i := 1; i < len(out); i++ {
			returns[i] = math.Log(s.close[i] / s.close[i-1])
		}
		// Population standard deviation of hourly/4h log returns; not annualized.
		for i := n; i < len(out); i++ {
			mean := 0.0
			for j := i - n + 1; j <= i; j++ {
				mean += returns[j] / float64(n)
			}
			variance := 0.0
			for j := i - n + 1; j <= i; j++ {
				d := returns[j] - mean
				variance += d * d / float64(n)
			}
			out[i] = math.Sqrt(variance) * 100
		}
	}
	s.rules[key] = out
	return out
}

func (s *seriesCache) evaluateIndicator(c bt.Condition, i int) (bool, bool) {
	if i < 0 {
		return false, false
	}
	values := s.ruleSeries(c)
	transform := func(x float64) float64 {
		if c.Measure != "change_pct" {
			return x
		}
		if c.Direction == "down" {
			return -x
		}
		if c.Direction == "both" {
			return math.Abs(x)
		}
		return x
	}
	current := transform(values[i])
	target := c.Threshold
	if math.IsNaN(current) || math.IsInf(current, 0) {
		return false, false
	}
	if c.Measure == "change_pct" && c.Direction != "both" && compareIndicator(current, 0) < 0 {
		return false, true
	}
	comparison := compareIndicator(current, target)
	switch c.Operator {
	case "gt":
		return comparison > 0, true
	case "gte":
		return comparison >= 0, true
	case "lt":
		return comparison < 0, true
	case "lte":
		return comparison <= 0, true
	case "crosses_above", "crosses_below":
		if i == 0 {
			return false, false
		}
		previous := transform(values[i-1])
		if math.IsNaN(previous) || math.IsInf(previous, 0) {
			return false, false
		}
		if c.Operator == "crosses_above" {
			return compareIndicator(previous, target) <= 0 && comparison > 0, true
		}
		return compareIndicator(previous, target) >= 0 && comparison < 0, true
	}
	return false, false
}

// Decimal prices cannot always be represented exactly by float64. Treat only
// rounding noise as equality, consistently for strict, inclusive and crossing
// operators (e.g. 0.100 -> 0.105 must be exactly the 5% threshold).
func compareIndicator(value, threshold float64) int {
	tolerance := 1e-12 * math.Max(1, math.Max(math.Abs(value), math.Abs(threshold)))
	if math.Abs(value-threshold) <= tolerance {
		return 0
	}
	if value < threshold {
		return -1
	}
	return 1
}
