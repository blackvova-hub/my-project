package engine

import (
	"math"
	bt "shortlong/backtest"
)

// A two-stack queue preserves the order of a noncommutative associative
// aggregate. Each sample moves between stacks at most once, so a rolling
// window takes O(N) time and O(window) additional memory.
type aggregateEntry[T any] struct {
	value, aggregate T
}

type aggregateQueue[T any] struct {
	front, back []aggregateEntry[T]
	merge       func(T, T) T
}

func (q *aggregateQueue[T]) len() int { return len(q.front) + len(q.back) }

func (q *aggregateQueue[T]) push(value T) {
	aggregate := value
	if n := len(q.back); n > 0 {
		aggregate = q.merge(q.back[n-1].aggregate, value)
	}
	q.back = append(q.back, aggregateEntry[T]{value, aggregate})
}

func (q *aggregateQueue[T]) pop() {
	if len(q.front) == 0 {
		for len(q.back) > 0 {
			last := len(q.back) - 1
			value := q.back[last].value
			q.back = q.back[:last]
			aggregate := value
			if n := len(q.front); n > 0 {
				aggregate = q.merge(value, q.front[n-1].aggregate)
			}
			q.front = append(q.front, aggregateEntry[T]{value, aggregate})
		}
	}
	q.front = q.front[:len(q.front)-1]
}

func (q *aggregateQueue[T]) aggregate() T {
	if len(q.front) == 0 {
		return q.back[len(q.back)-1].aggregate
	}
	if len(q.back) == 0 {
		return q.front[len(q.front)-1].aggregate
	}
	return q.merge(q.front[len(q.front)-1].aggregate, q.back[len(q.back)-1].aggregate)
}

type priceMoveSummary struct {
	minLow, maxHigh, up, down float64
	invalid                   bool
}

func summarizePriceBar(b bt.Candle) priceMoveSummary {
	for _, value := range []float64{b.Open, b.High, b.Low, b.Close} {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return priceMoveSummary{minLow: 1, maxHigh: 1, invalid: true}
		}
	}
	// Open precedes both extrema and close follows them. The relative order of
	// high and low within this minute is unknown and is never assumed.
	return priceMoveSummary{
		minLow: b.Low, maxHigh: b.High,
		up:   max(0, b.High/b.Open-1, b.Close/b.Low-1),
		down: max(0, 1-b.Low/b.Open, 1-b.Close/b.High),
	}
}

func mergePriceMoves(earlier, later priceMoveSummary) priceMoveSummary {
	return priceMoveSummary{
		minLow: min(earlier.minLow, later.minLow), maxHigh: max(earlier.maxHigh, later.maxHigh),
		up:      max(earlier.up, later.up, later.maxHigh/earlier.minLow-1),
		down:    max(earlier.down, later.down, 1-later.minLow/earlier.maxHigh),
		invalid: earlier.invalid || later.invalid,
	}
}

// rollingPriceChange includes exactly the last n closed bars. A down series is
// negative so the existing directional comparison transform stays unchanged.
func rollingPriceChange(candles []bt.Candle, n int, direction string) []float64 {
	out := emptySeries(len(candles))
	if n < 1 {
		return out
	}
	q := aggregateQueue[priceMoveSummary]{merge: mergePriceMoves}
	for i, candle := range candles {
		if q.len() == n {
			q.pop()
		}
		q.push(summarizePriceBar(candle))
		if q.len() < n {
			continue
		}
		value := q.aggregate()
		if value.invalid {
			continue
		}
		switch direction {
		case "down":
			out[i] = -value.down * 100
		case "both":
			out[i] = max(value.up, value.down) * 100
		default:
			out[i] = value.up * 100
		}
	}
	return out
}

type varianceSummary struct {
	count    int
	mean, m2 float64
	invalid  bool
}

// Parallel Welford/Chan moments avoid subtracting nearly equal sums of squares.
func mergeVariance(a, b varianceSummary) varianceSummary {
	n := a.count + b.count
	delta := b.mean - a.mean
	return varianceSummary{
		count: n, mean: a.mean + delta*float64(b.count)/float64(n),
		m2:      a.m2 + b.m2 + delta*delta*float64(a.count)*float64(b.count)/float64(n),
		invalid: a.invalid || b.invalid,
	}
}

func rollingStdDev(values []float64, n int) []float64 {
	out := emptySeries(len(values))
	if n < 1 {
		return out
	}
	q := aggregateQueue[varianceSummary]{merge: mergeVariance}
	for i, value := range values {
		if q.len() == n {
			q.pop()
		}
		entry := varianceSummary{count: 1, mean: value}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			entry.mean, entry.invalid = 0, true
		}
		q.push(entry)
		if q.len() == n {
			summary := q.aggregate()
			if !summary.invalid {
				out[i] = math.Sqrt(max(0, summary.m2/float64(n)))
			}
		}
	}
	return out
}
