package main

import (
	"math"
	"sort"
)

const aggressiveTradeClusterWindowMs = int64(3_000)

// p2Median is the P-squared streaming quantile estimator for p=0.5. It keeps
// five marker heights regardless of the number of trades in the minute.
type p2Median struct {
	count   int64
	initial [5]float64
	q       [5]float64
	n       [5]int64
	np      [5]float64
}

func (p *p2Median) Add(value float64) {
	if !tradeFinitePositive(value) {
		return
	}
	if p.count < 5 {
		p.initial[p.count] = value
		p.count++
		if p.count == 5 {
			sort.Float64s(p.initial[:])
			copy(p.q[:], p.initial[:])
			p.n = [5]int64{1, 2, 3, 4, 5}
			p.np = [5]float64{1, 2, 3, 4, 5}
		}
		return
	}

	p.count++
	k := 0
	switch {
	case value < p.q[0]:
		p.q[0], k = value, 0
	case value < p.q[1]:
		k = 0
	case value < p.q[2]:
		k = 1
	case value < p.q[3]:
		k = 2
	case value <= p.q[4]:
		k = 3
	default:
		p.q[4], k = value, 3
	}
	for i := k + 1; i < 5; i++ {
		p.n[i]++
	}
	increments := [5]float64{0, 0.25, 0.5, 0.75, 1}
	for i := range p.np {
		p.np[i] += increments[i]
	}
	for i := 1; i <= 3; i++ {
		d := p.np[i] - float64(p.n[i])
		direction := int64(0)
		if d >= 1 && p.n[i+1]-p.n[i] > 1 {
			direction = 1
		} else if d <= -1 && p.n[i-1]-p.n[i] < -1 {
			direction = -1
		}
		if direction == 0 {
			continue
		}
		candidate := p.parabolic(i, direction)
		if candidate > p.q[i-1] && candidate < p.q[i+1] {
			p.q[i] = candidate
		} else {
			p.q[i] = p.linear(i, direction)
		}
		p.n[i] += direction
	}
}

func (p *p2Median) parabolic(i int, direction int64) float64 {
	n0, n1, n2 := float64(p.n[i-1]), float64(p.n[i]), float64(p.n[i+1])
	d := float64(direction)
	return p.q[i] + d/(n2-n0)*((n1-n0+d)*(p.q[i+1]-p.q[i])/(n2-n1)+(n2-n1-d)*(p.q[i]-p.q[i-1])/(n1-n0))
}

func (p *p2Median) linear(i int, direction int64) float64 {
	j := i + int(direction)
	return p.q[i] + float64(direction)*(p.q[j]-p.q[i])/float64(p.n[j]-p.n[i])
}

func (p *p2Median) Value() float64 {
	if p.count == 0 {
		return 0
	}
	if p.count >= 5 {
		return p.q[2]
	}
	values := append([]float64(nil), p.initial[:p.count]...)
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

func canonicalTradeSide(side string) (float64, bool) {
	switch {
	case stringsEqualFold(side, "Buy"):
		return 1, true
	case stringsEqualFold(side, "Sell"):
		return -1, true
	default:
		return 0, false
	}
}

// addTrade folds a public trade observation into a bounded minute aggregate.
// For perpetual feeds an observation can itself be exchange-aggregated; these
// fields therefore describe public aggressive-flow observations, not people or
// uniquely identified orders.
func (s *TradeStats) addTrade(tsMs int64, price, size float64, side string, executionCount float64) {
	if s == nil || tsMs <= 0 || !tradeFinitePositive(price) || !tradeFinitePositive(size) ||
		math.IsNaN(executionCount) || math.IsInf(executionCount, 0) {
		return
	}
	if executionCount <= 0 {
		executionCount = 1
	}
	sideValue, ok := canonicalTradeSide(side)
	if !ok {
		return
	}
	usd := price * size
	if !tradeFinitePositive(usd) {
		return
	}
	if sideValue > 0 {
		s.BuyVolume += size
		s.BuyUSD += usd
	} else {
		s.SellVolume += size
		s.SellUSD += usd
	}
	s.Delta = s.BuyVolume - s.SellVolume
	s.Count += executionCount
	s.median.Add(usd)
	s.MedianUSD = s.median.Value()
	if usd > s.LargestUSD {
		s.LargestUSD = usd
		s.LargestSide = sideValue
	}
	if tsMs >= s.Timestamp {
		s.Timestamp = tsMs
		s.LastPrice = price
		s.LastSize = size
		s.LastSide = sideValue
	}

	// Public feeds are timestamp ordered in normal operation. Ignore an
	// out-of-order observation only for cluster sequencing; all minute totals
	// and the median above still include it.
	if s.clusterLastTS > 0 && tsMs < s.clusterLastTS {
		return
	}
	if s.clusterStartTS == 0 || s.clusterSide != sideValue ||
		tsMs-s.clusterStartTS > aggressiveTradeClusterWindowMs {
		s.clusterStartTS = tsMs
		s.clusterStartPrice = price
		s.clusterUSD = 0
		s.clusterCount = 0
		s.clusterSide = sideValue
	}
	s.clusterLastTS = tsMs
	s.clusterLastPrice = price
	s.clusterUSD += usd
	s.clusterCount++
	if s.clusterUSD > s.ClusterUSD {
		s.ClusterUSD = s.clusterUSD
		s.ClusterCount = s.clusterCount
		s.ClusterSide = s.clusterSide
		s.PriceImpactPct = 0
		if s.clusterStartPrice > 0 {
			s.PriceImpactPct = (s.clusterLastPrice/s.clusterStartPrice - 1) * 100
		}
	}
}

func tradeFinitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func stringsEqualFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		a, b := left[i], right[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
