package engine

import (
	"math"
	bt "shortlong/backtest"
)

func emptySeries(n int) []float64 {
	v := make([]float64, n)
	for i := range v {
		v[i] = math.NaN()
	}
	return v
}
func sma(v []float64, p int) []float64 {
	out := emptySeries(len(v))
	sum := 0.0
	for i, x := range v {
		sum += x
		if i >= p {
			sum -= v[i-p]
		}
		if i >= p-1 {
			out[i] = sum / float64(p)
		}
	}
	return out
}

// EMA is SMA-seeded, including a series with an initial unavailable prefix.
func ema(v []float64, p int) []float64 {
	out := emptySeries(len(v))
	count := 0
	sum := 0.0
	last := math.NaN()
	alpha := 2 / float64(p+1)
	for i, x := range v {
		if math.IsNaN(x) {
			count = 0
			sum = 0
			last = math.NaN()
			continue
		}
		if count < p {
			sum += x
			count++
			if count == p {
				last = sum / float64(p)
				out[i] = last
			}
		} else {
			last = alpha*x + (1-alpha)*last
			out[i] = last
		}
	}
	return out
}
func rsi(v []float64, p int) []float64 {
	out := emptySeries(len(v))
	gain, loss := 0.0, 0.0
	for i := 1; i < len(v); i++ {
		d := v[i] - v[i-1]
		g, l := math.Max(d, 0), math.Max(-d, 0)
		if i <= p {
			gain += g / float64(p)
			loss += l / float64(p)
		} else {
			gain = (gain*float64(p-1) + g) / float64(p)
			loss = (loss*float64(p-1) + l) / float64(p)
		}
		if i >= p {
			switch {
			case gain == 0 && loss == 0:
				out[i] = 50
			case loss == 0:
				out[i] = 100
			default:
				out[i] = 100 - 100/(1+gain/loss)
			}
		}
	}
	return out
}

type seriesCache struct {
	close   []float64
	values  map[bt.Operand][]float64
	candles []bt.Candle
	smc     map[string][]int
	rules   map[ruleKey][]float64
}

func newCache(c []bt.Candle) *seriesCache {
	v := make([]float64, len(c))
	for i, x := range c {
		v[i] = x.Close
	}
	return &seriesCache{close: v, values: map[bt.Operand][]float64{}, candles: c, rules: map[ruleKey][]float64{}}
}
func (s *seriesCache) series(o bt.Operand) []float64 {
	if v, ok := s.values[o]; ok {
		return v
	}
	var v []float64
	switch o.Kind {
	case "close":
		v = s.close
	case "sma":
		v = sma(s.close, o.Period)
	case "ema":
		v = ema(s.close, o.Period)
	case "rsi":
		v = rsi(s.close, o.Period)
	case "macd", "macd_signal", "macd_histogram":
		fast, slow := ema(s.close, 12), ema(s.close, 26)
		m := emptySeries(len(s.close))
		for i := range m {
			m[i] = fast[i] - slow[i]
		}
		signal := ema(m, 9)
		hist := emptySeries(len(m))
		for i := range m {
			hist[i] = m[i] - signal[i]
		}
		s.values[bt.Operand{Kind: "macd"}] = m
		s.values[bt.Operand{Kind: "macd_signal"}] = signal
		s.values[bt.Operand{Kind: "macd_histogram"}] = hist
		return s.values[o]
	}
	s.values[o] = v
	return v
}
func (s *seriesCache) value(o *bt.Operand, i int) float64 {
	if i < 0 {
		return math.NaN()
	}
	if o.Kind == "constant" {
		return o.Value
	}
	return s.series(*o)[i]
}

// Availability propagates through AND/OR: an unknown operand never becomes a
// false condition which another branch could accidentally use as a signal.
func (s *seriesCache) evaluate(c bt.Condition, i int) (bool, bool) {
	if c.Kind == "indicator" {
		return s.evaluateIndicator(c, i)
	}
	if c.Kind == "smc" {
		if i < 24 {
			return false, false
		}
		if s.smc == nil {
			s.smc = smartMoney(s.candles)
		}
		last := s.smc[c.Event+":"+c.Direction][i]
		return last >= 0 && i-last < c.WithinBars, true
	}
	if c.Kind == "and" || c.Kind == "or" {
		matched := c.Kind == "and"
		for _, n := range c.Children {
			m, ok := s.evaluate(n, i)
			if !ok {
				return false, false
			}
			if c.Kind == "and" {
				matched = matched && m
			} else {
				matched = matched || m
			}
		}
		return matched, true
	}
	l, r := s.value(c.Left, i), s.value(c.Right, i)
	if math.IsNaN(l) || math.IsNaN(r) {
		return false, false
	}
	switch c.Operator {
	case "gt":
		return l > r, true
	case "lt":
		return l < r, true
	case "gte":
		return l >= r, true
	case "lte":
		return l <= r, true
	case "crosses_above", "crosses_below":
		pl, pr := s.value(c.Left, i-1), s.value(c.Right, i-1)
		if math.IsNaN(pl) || math.IsNaN(pr) {
			return false, false
		}
		if c.Operator == "crosses_above" {
			return pl <= pr && l > r, true
		}
		return pl >= pr && l < r, true
	}
	return false, false
}
