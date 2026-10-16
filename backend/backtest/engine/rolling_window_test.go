package engine

import (
	"math"
	"math/rand"
	bt "shortlong/backtest"
	"testing"
	"time"
)

func flatMinutePrices(prices ...float64) []bt.Candle {
	candles := make([]bt.Candle, len(prices))
	for i, price := range prices {
		candles[i] = bt.Candle{Time: int64(i) * minuteMillis, Open: price, High: price, Low: price, Close: price, Volume: 1}
	}
	return candles
}

// Deliberately quadratic oracle: enumerate every ordered pair of distinct bars,
// plus only intrabar movements whose order is known from OHLC.
func brutePriceMoves(candles []bt.Candle) (up, down float64) {
	for i, earlier := range candles {
		up = max(up, earlier.High/earlier.Open-1, earlier.Close/earlier.Low-1)
		down = max(down, 1-earlier.Low/earlier.Open, 1-earlier.Close/earlier.High)
		for _, later := range candles[i+1:] {
			up = max(up, later.High/earlier.Low-1)
			down = max(down, 1-later.Low/earlier.High)
		}
	}
	return up * 100, down * 100
}

func TestRollingPriceMovesMatchOrderedBruteForce(t *testing.T) {
	random := rand.New(rand.NewSource(17))
	candles := make([]bt.Candle, 217)
	for i := range candles {
		open, close := 80+random.Float64()*80, 80+random.Float64()*80
		candles[i] = bt.Candle{Time: int64(i) * minuteMillis, Open: open, Close: close, Low: min(open, close) - random.Float64()*20, High: max(open, close) + random.Float64()*20}
	}
	for _, window := range []int{1, 2, 9, 60, 120, 300} {
		up := rollingPriceChange(candles, window, "up")
		down := rollingPriceChange(candles, window, "down")
		both := rollingPriceChange(candles, window, "both")
		for i := range candles {
			if i < window-1 {
				if !math.IsNaN(up[i]) || !math.IsNaN(down[i]) || !math.IsNaN(both[i]) {
					t.Fatal("incomplete window produced a value", window, i)
				}
				continue
			}
			wantUp, wantDown := brutePriceMoves(candles[i-window+1 : i+1])
			ruleNear(t, up[i], wantUp)
			ruleNear(t, down[i], -wantDown)
			ruleNear(t, both[i], max(wantUp, wantDown))
		}
	}
}

func TestMinutePriceKeepsPeakAndDirectionInCache(t *testing.T) {
	prices := make([]float64, 60)
	for i := range prices {
		prices[i] = 100
	}
	prices[58], prices[59] = 150, 120
	cache := newCache(flatMinutePrices(prices...))
	c := testRule("price", "change_pct")
	c.WindowHours = 1
	for _, tc := range []struct {
		direction string
		want      float64
	}{{"up", 50}, {"down", -20}, {"both", 50}, {"up", 50}} {
		c.Direction = tc.direction
		ruleNear(t, cache.ruleSeries(c)[59], tc.want)
	}
	for i := range prices {
		prices[i] = 150
	}
	prices[59] = 100
	cache = newCache(flatMinutePrices(prices...))
	c.Direction = "up"
	ruleNear(t, cache.ruleSeries(c)[59], 0)
	c.Direction = "down"
	ruleNear(t, cache.ruleSeries(c)[59], -100.0/3)
	c.Direction = "both"
	ruleNear(t, cache.ruleSeries(c)[59], 100.0/3)
}

func TestPriceWindowExpiryCrossingsAndCausality(t *testing.T) {
	prices := make([]float64, 125)
	for i := range prices {
		prices[i] = 100
	}
	prices[60] = 150
	candles := flatMinutePrices(prices...)
	cache := newCache(candles)
	c := testRule("price", "change_pct")
	c.WindowHours, c.Direction, c.Threshold = 1, "up", 20
	c.Operator = "crosses_above"
	if matched, valid := cache.evaluate(c, 60); !matched || !valid {
		t.Fatal("ordered runup must cross the threshold after the spike closes")
	}
	c.Operator = "crosses_below"
	if matched, valid := cache.evaluate(c, 119); !matched || !valid {
		t.Fatal("expired low must remove the previous runup from the window")
	}
	c.Direction, c.Operator = "down", "crosses_above"
	if matched, valid := cache.evaluate(c, 61); !matched || !valid {
		t.Fatal("drawdown crossing must compare positive decline magnitudes")
	}
	c.Direction = "both"
	if matched, valid := cache.evaluate(c, 60); !matched || !valid {
		t.Fatal("both-direction crossing lost the runup")
	}
	for _, direction := range []string{"up", "down", "both"} {
		c.Direction = direction
		full := cache.ruleSeries(c)
		prefix := newCache(candles[:121]).ruleSeries(c)
		for i := 59; i < len(prefix); i++ {
			ruleNear(t, full[i], prefix[i])
		}
	}
	up, down := rollingPriceChange(candles, 60, "up"), rollingPriceChange(candles, 60, "down")
	ruleNear(t, up[118], 50)
	ruleNear(t, up[119], 0)
	ruleNear(t, down[119], -100.0/3)
	ruleNear(t, down[120], 0)
}

func TestSingleMinuteExtremaUseOnlyKnownOrdering(t *testing.T) {
	candles := []bt.Candle{{Open: 120, High: 150, Low: 100, Close: 120}, {Open: 150, High: 150, Low: 100, Close: 100}}
	up, down := rollingPriceChange(candles, 1, "up"), rollingPriceChange(candles, 1, "down")
	ruleNear(t, up[0], 25) // high/low=50% would assume their unknown ordering.
	ruleNear(t, down[0], -20)
	ruleNear(t, up[1], 0)
	ruleNear(t, down[1], -100.0/3)
	invalid := append([]bt.Candle{}, candles...)
	invalid[0].Close = math.NaN()
	values := rollingPriceChange(invalid, 1, "up")
	if !math.IsNaN(values[0]) || values[1] != 0 {
		t.Fatal("invalid data must expire without poisoning the next window", values)
	}
}

func TestMinutePriceSignalExecutesAtNextMinuteOpen(t *testing.T) {
	r, _ := fixture()
	r.Timeframe, r.Strategy.Version = "1m", 2
	r.To = r.From.Add(5 * time.Minute)
	r.Strategy.TakeProfitPct, r.Strategy.StopLossPct = 0, 50
	r.Strategy.Entry = bt.Condition{Kind: "indicator", Indicator: "price", Measure: "change_pct", Direction: "up", WindowHours: 1, Operator: "gte", Threshold: 50}
	data := candlesFor(r)
	c := data["BTCUSDT"]
	warm := r.WarmupBars()
	c[warm+1].High, c[warm+1].Close = 150, 120
	for i := warm + 2; i < len(c); i++ {
		c[i].Open, c[i].High, c[i].Low, c[i].Close = 120, 120, 120, 120
	}
	result := runTest(t, r, data)
	if len(result.Trades) != 1 || result.Trades[0].EntryTime != r.From.Add(2*time.Minute).UnixMilli() {
		t.Fatalf("a minute's high must not trigger a same-minute entry: %+v", result.Trades)
	}
	near(t, result.Trades[0].EntryPrice, 120)
}

func TestRollingVarianceMatchesStableOracleAndRecoversMissingValues(t *testing.T) {
	values := make([]float64, 300)
	for i := range values {
		values[i] = .01 + math.Sin(float64(i)*.73)*1e-9
	}
	values[80] = math.NaN()
	for _, window := range []int{1, 2, 60, 120} {
		got := rollingStdDev(values, window)
		for i := window - 1; i < len(values); i++ {
			segment := values[i-window+1 : i+1]
			mean, missing := 0.0, false
			for _, value := range segment {
				if math.IsNaN(value) {
					missing = true
				}
				mean += value / float64(window)
			}
			if missing {
				if !math.IsNaN(got[i]) {
					t.Fatal("missing return became valid volatility")
				}
				continue
			}
			variance := 0.0
			for _, value := range segment {
				variance += (value - mean) * (value - mean) / float64(window)
			}
			if math.IsNaN(got[i]) || math.Abs(got[i]-math.Sqrt(variance)) > 1e-17 {
				t.Fatalf("unstable variance window=%d i=%d got %.15g want %.15g", window, i, got[i], math.Sqrt(variance))
			}
		}
	}
}

var rollingBenchmarkResult []float64

func BenchmarkMinutePriceOneYear(b *testing.B) {
	candles := make([]bt.Candle, 365*24*60)
	for i := range candles {
		price := 100 + 10*math.Sin(float64(i)/71)
		candles[i] = bt.Candle{Time: int64(i) * minuteMillis, Open: price, High: price + 1, Low: price - 1, Close: price}
	}
	for _, window := range []int{60, 720 * 60} {
		b.Run((time.Duration(window) * time.Minute).String(), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rollingBenchmarkResult = rollingPriceChange(candles, window, "both")
			}
		})
	}
}
