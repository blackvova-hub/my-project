package similarity

import (
	"crypto/sha256"
	"fmt"
	"math"
	bt "shortlong/backtest"
	"shortlong/backtest/engine"
	"sort"
)

const Step int64 = 300000
const FeatureVersion = "ohlcv_v2"

var GroupSizes = [5]int{24, 12, 6, 6, 8}
var DefaultWeights = [5]float64{.4, .2, .15, .15, .1}

const Dimensions = 56

type Signature struct {
	Groups     [5][]float64 `json:"groups"`
	Trend      string       `json:"trend"`
	Volatility string       `json:"volatility"`
	Volume     string       `json:"volume"`
}

func mean(a []float64) float64 {
	s := 0.
	for _, v := range a {
		s += v
	}
	return s / float64(len(a))
}
func median(a []float64) float64 {
	b := append([]float64(nil), a...)
	sort.Float64s(b)
	n := len(b)
	if n%2 == 0 {
		return (b[n/2-1] + b[n/2]) / 2
	}
	return b[n/2]
}
func std(a []float64) float64 {
	m := mean(a)
	v := 0.
	for _, x := range a {
		v += (x - m) * (x - m)
	}
	return math.Sqrt(v / float64(len(a)))
}
func squash(v float64) float64 { return math.Tanh(v) }
func relativeChange(current, previous float64) float64 {
	if previous <= 0 {
		if current > 0 {
			return 1
		}
		return 0
	}
	return squash(math.Log(math.Max(current/previous, 1e-12)))
}
func regime(v float64) string {
	if v < .7 {
		return "low"
	}
	if v > 1.5 {
		return "high"
	}
	return "normal"
}
func bins(values []float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		lo := i * len(values) / n
		hi := (i + 1) * len(values) / n
		if hi <= lo {
			hi = lo + 1
		}
		out[i] = mean(values[lo:hi])
	}
	return out
}

// Both regimes and scale estimates use only candles preceding the window end.
// The preceding N bars form a causal reference regime, never future outcomes.
func Features(c []bt.Candle, n int) (Signature, error) {
	var s Signature
	if n < 12 || len(c) != 2*n {
		return s, fmt.Errorf("need %d contiguous reference and window candles", 2*n)
	}
	if err := engine.ValidateCandles(c, c[0].Time, c[len(c)-1].Time+Step, Step); err != nil {
		return s, err
	}
	prior, w := c[:n], c[n:]
	returns := make([]float64, n)
	oldReturns := make([]float64, n)
	volumes := make([]float64, n)
	oldVolumes := make([]float64, n)
	tr := make([]float64, n)
	path := make([]float64, n)
	base := prior[n-1].Close
	for i, v := range w {
		prev := base
		if i > 0 {
			prev = w[i-1].Close
		}
		returns[i] = math.Log(v.Close / prev)
		tr[i] = math.Max(v.High-v.Low, math.Max(math.Abs(v.High-prev), math.Abs(v.Low-prev))) / prev
		volumes[i] = v.Volume
		path[i] = math.Log(v.Close / base)
		oldVolumes[i] = prior[i].Volume
		prev = prior[i].Open
		if i > 0 {
			prev = prior[i-1].Close
		}
		oldReturns[i] = math.Log(prior[i].Close / prev)
	}
	atr := math.Max(mean(tr), 1e-8)
	rv := std(returns)
	oldRV := math.Max(std(oldReturns), 1e-8)
	volMedian := median(oldVolumes)
	volScale := math.Max(volMedian, mean(oldVolumes)*.01)
	if volScale <= 0 {
		volScale = mean(volumes)
	}
	oldMean, oldStd := mean(oldVolumes), std(oldVolumes)
	oldStd = math.Max(oldStd, oldMean*.01)
	volPath := make([]float64, n)
	for i, v := range volumes {
		if volScale > 0 {
			volPath[i] = squash(math.Log1p(v/volScale) - math.Log(2))
		}
	}
	priceScale := math.Max(atr*math.Sqrt(float64(n)), 1e-8)
	p := bins(path, 16)
	for i := range p {
		p[i] = squash(p[i] / priceScale)
	}
	r := bins(returns, 8)
	for i := range r {
		r[i] = squash(r[i] / atr)
	}
	s.Groups[0] = append(p, r...)
	spikes := 0.
	for _, v := range volumes {
		if v > 3*volScale {
			spikes++
		}
	}
	volumeRatio, z := 1., 0.
	if oldMean > 0 {
		volumeRatio = mean(volumes) / oldMean
		z = (mean(volumes) - oldMean) / oldStd
	} else if mean(volumes) > 0 {
		volumeRatio = 2
		z = 3
	}
	s.Groups[1] = append(bins(volPath, 8), relativeChange(mean(volumes), oldMean), squash(z/3), spikes/float64(n), relativeChange(mean(volumes[n/2:]), mean(volumes[:n/2])))
	s.Groups[2] = []float64{squash(atr / .01), squash(rv / .01), squash(math.Log(math.Max(rv/oldRV, 1e-8))), squash(mean(tr[n/2:])/math.Max(mean(tr[:n/2]), 1e-8) - 1), squash(rv / atr), squash(std(tr) / atr)}
	body, upper, lower, bull, big := 0., 0., 0., 0., 0.
	lo, hi, peak, dd, up := base, base, base, 0., 0.
	for _, v := range w {
		body += math.Abs(v.Close-v.Open) / v.Close / atr
		upper += (v.High - math.Max(v.Open, v.Close)) / v.Close / atr
		lower += (math.Min(v.Open, v.Close) - v.Low) / v.Close / atr
		if v.Close > v.Open {
			bull++
		}
		if (v.High-v.Low)/v.Close/atr > 3 {
			big++
		}
		lo = math.Min(lo, v.Low)
		hi = math.Max(hi, v.High)
		peak = math.Max(peak, v.High)
		dd = math.Max(dd, 1-v.Low/peak)
		up = math.Max(up, v.High/base-1)
	}
	nf := float64(n)
	s.Groups[3] = []float64{squash(body / nf), squash(upper / nf), squash(lower / nf), bull / nf, 1 - bull/nf, big / nf}
	cum := w[n-1].Close/base - 1
	pos := .5
	if hi > lo {
		pos = (w[n-1].Close - lo) / (hi - lo)
	}
	s.Groups[4] = []float64{squash(cum / .1), squash((hi/lo - 1) / .1), squash(dd / .1), squash(up / .1), pos, squash(cum / priceScale), squash(path[n/2] / priceScale), squash((path[n-1] - path[n/2]) / priceScale)}
	s.Trend = "sideways"
	if cum > priceScale*.5 {
		s.Trend = "bullish"
	}
	if cum < -priceScale*.5 {
		s.Trend = "bearish"
	}
	s.Volatility = regime(rv / oldRV)
	s.Volume = regime(volumeRatio)
	return s, nil
}
func (s Signature) Vector(weights [5]float64) []float64 {
	v := make([]float64, 0, Dimensions)
	for g, values := range s.Groups {
		scale := math.Sqrt(weights[g] / float64(len(values)))
		for _, x := range values {
			v = append(v, x*scale)
		}
	}
	return v
}
func FromVector(v []float64, weights [5]float64) (Signature, error) {
	var s Signature
	if len(v) != Dimensions {
		return s, fmt.Errorf("invalid vector size")
	}
	at := 0
	for g, n := range GroupSizes {
		scale := math.Sqrt(weights[g] / float64(n))
		for i := 0; i < n; i++ {
			s.Groups[g] = append(s.Groups[g], v[at]/scale)
			at++
		}
	}
	return s, nil
}

type Breakdown struct {
	Price      float64 `json:"price"`
	Volume     float64 `json:"volume"`
	Volatility float64 `json:"volatility"`
	Candles    float64 `json:"candles"`
	Statistics float64 `json:"statistics"`
}

func Compare(a, b Signature, weights [5]float64) (float64, Breakdown) {
	var scores [5]float64
	score := 0.
	for g := range a.Groups {
		d := 0.
		for i, x := range a.Groups[g] {
			delta := x - b.Groups[g][i]
			d += delta * delta
		}
		scores[g] = 100 * math.Exp(-2*math.Sqrt(d/float64(len(a.Groups[g]))))
		score += weights[g] * scores[g]
	}
	return score, Breakdown{scores[0], scores[1], scores[2], scores[3], scores[4]}
}
func WindowID(market, symbol string, n int, end int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%d:%d", FeatureVersion, market, symbol, n, end)))
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

type Outcome struct {
	Horizon     int     `json:"horizonBars"`
	Return      float64 `json:"returnPct"`
	Upside      float64 `json:"maxUpsidePct"`
	Downside    float64 `json:"maxDownsidePct"`
	AvailableAt int64   `json:"availableAt"`
}

func Future(c []bt.Candle, end int64, horizon int) (Outcome, error) {
	o := Outcome{Horizon: horizon, AvailableAt: end + int64(horizon)*Step}
	if len(c) != horizon+1 {
		return o, fmt.Errorf("incomplete future horizon")
	}
	if err := engine.ValidateCandles(c, end-Step, o.AvailableAt, Step); err != nil {
		return o, err
	}
	base := c[0].Close
	hi, lo := base, base
	for _, v := range c[1:] {
		hi = math.Max(hi, v.High)
		lo = math.Min(lo, v.Low)
	}
	o.Return = (c[len(c)-1].Close/base - 1) * 100
	o.Upside = (hi/base - 1) * 100
	o.Downside = (lo/base - 1) * 100
	return o, nil
}
