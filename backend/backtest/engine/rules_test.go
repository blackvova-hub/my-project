package engine

import (
	"math"
	bt "shortlong/backtest"
	"testing"
)

func ruleFixture(n int, step int64) []bt.Candle {
	out := make([]bt.Candle, n)
	for i := range out {
		price := 100 + float64(i)/10 + 3*math.Sin(float64(i)/7)
		out[i] = bt.Candle{Time: int64(i) * step, Open: price, High: price + 2, Low: price - 2, Close: price, Volume: float64(100 + i%13), Metrics: map[string]float64{"openInterest": 1000 + float64(i), "tradeBuyVolume": float64(200 + i%19), "tradeSellVolume": float64(150 + i%23)}}
	}
	return out
}
func testRule(indicator, measure string) bt.Condition {
	c := bt.Condition{Kind: "indicator", Indicator: indicator, Measure: measure, Operator: "gte", Threshold: 0}
	if bt.HasWindow(c) {
		c.WindowHours = 24
	}
	if bt.HasPeriod(c) {
		c.Period = 14
	}
	if measure == "change_pct" {
		c.Direction = "both"
	}
	return c
}
func ruleNear(t *testing.T, got, want float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > 1e-8 {
		t.Fatalf("got %.12f want %.12f", got, want)
	}
}

func TestIndicatorCatalogCausality(t *testing.T) {
	candles := ruleFixture(450, 3600000)
	for name, measures := range bt.IndicatorMeasures {
		for _, measure := range measures {
			t.Run(name+"/"+measure, func(t *testing.T) {
				c := testRule(name, measure)
				full := newCache(candles).ruleSeries(c)
				prefix := newCache(candles[:360]).ruleSeries(c)
				for i := 300; i < len(prefix); i++ {
					ruleNear(t, full[i], prefix[i])
				}
			})
		}
	}
}
func TestPriceOperatorsAndDirection(t *testing.T) {
	candles := ruleFixture(3, 3600000)
	candles[0].Close = 100
	candles[1].Close = 105
	candles[2].Close = 110
	c := testRule("price", "change_pct")
	c.WindowHours = 1
	c.Threshold = 5
	c.Direction = "up"
	cache := newCache(candles)
	for op, want := range map[string]bool{"gt": false, "gte": true, "lt": false, "lte": true} {
		c.Operator = op
		got, available := cache.evaluate(c, 1)
		if got != want || !available {
			t.Fatalf("%s got %v/%v", op, got, available)
		}
	}
	c.Operator = "lt"
	c.Threshold = 6
	got, ok := cache.evaluate(c, 1)
	if !got || !ok {
		t.Fatal("less than failed")
	}
	c.Direction = "down"
	got, ok = cache.evaluate(c, 1)
	if got || !ok {
		t.Fatal("growth must not match a decline rule")
	}
	candles[1].Close = 95
	cache = newCache(candles)
	c.Operator = "gte"
	c.Threshold = 5
	got, ok = cache.evaluate(c, 1)
	if !got || !ok {
		t.Fatal("decline magnitude failed")
	}
	c.Direction = "both"
	got, ok = cache.evaluate(c, 1)
	if !got || !ok {
		t.Fatal("absolute move failed")
	}
}

func TestDecimalThresholdEquality(t *testing.T) {
	candles := ruleFixture(2, 3600000)
	candles[0].Close = .100
	candles[1].Close = .105
	cache := newCache(candles)
	c := testRule("price", "change_pct")
	c.WindowHours, c.Threshold, c.Direction = 1, 5, "up"
	for operator, want := range map[string]bool{"lt": false, "lte": true, "gt": false, "gte": true} {
		c.Operator = operator
		got, available := cache.evaluate(c, 1)
		if !available || got != want {
			t.Fatalf("decimal equality %s: %v/%v", operator, got, available)
		}
	}
}
func TestWindowUnitsAndAdjacentVolumes(t *testing.T) {
	candles := ruleFixture(8, 4*3600000)
	for i := range candles {
		candles[i].Close = 100 + float64(i)
		candles[i].Volume = float64(i + 1)
	}
	cache := newCache(candles)
	c := testRule("price", "change_pct")
	c.WindowHours = 24
	ruleNear(t, cache.ruleSeries(c)[6], 6)
	c = testRule("volume", "change_pct")
	c.WindowHours = 8
	ruleNear(t, cache.ruleSeries(c)[3], (7.0-3)*100/3)
	c.Measure = "relative"
	ruleNear(t, cache.ruleSeries(c)[3], 7.0/3)
	c.Measure = "sum"
	ruleNear(t, cache.ruleSeries(c)[3], 7)
	c = testRule("openInterest", "change_pct")
	c.WindowHours = 4
	ruleNear(t, cache.ruleSeries(c)[1], .1)
}
func TestCVDUsesRealFlowAndMissingData(t *testing.T) {
	candles := ruleFixture(4, 3600000)
	for i := range candles {
		candles[i].Metrics = map[string]float64{"tradeBuyVolume": 300, "tradeSellVolume": 100}
	}
	c := testRule("cvd", "sum")
	c.WindowHours = 2
	ruleNear(t, newCache(candles).ruleSeries(c)[2], 400)
	c.Measure = "imbalance_pct"
	ruleNear(t, newCache(candles).ruleSeries(c)[2], 50)
	delete(candles[1].Metrics, "tradeSellVolume")
	got, ok := newCache(candles).evaluate(c, 2)
	if got || ok {
		t.Fatal("missing trades became a valid signal")
	}
	candles[1].Metrics["tradeSellVolume"] = 0
	candles[1].Metrics["tradeBuyVolume"] = 0
	c.WindowHours = 1
	got, ok = newCache(candles).evaluate(c, 1)
	if got || ok {
		t.Fatal("zero volume denominator is unavailable")
	}
}
func TestOscillatorAndBandsGoldenValues(t *testing.T) {
	candles := ruleFixture(220, 3600000)
	for i := range candles {
		candles[i].Close = 100 + float64(i)
		candles[i].Open = candles[i].Close
		candles[i].High = candles[i].Close + 1
		candles[i].Low = candles[i].Close - 1
		candles[i].Volume = 100
	}
	cache := newCache(candles)
	for _, indicator := range []string{"rsi", "mfi"} {
		ruleNear(t, cache.ruleSeries(testRule(indicator, "value"))[200], 100)
	}
	ruleNear(t, cache.ruleSeries(testRule("macd", "line"))[200], 7)
	ruleNear(t, cache.ruleSeries(testRule("macd", "histogram"))[200], 0)
	ruleNear(t, cache.ruleSeries(testRule("stochastic", "value"))[200], 14.0/15*100)
	ruleNear(t, cache.ruleSeries(testRule("atr", "value"))[200], 2.0/300*100)
	for _, indicator := range []string{"ema", "sma"} {
		ruleNear(t, cache.ruleSeries(testRule(indicator, "distance_pct"))[200], (300.0/293.5-1)*100)
	}
	ruleNear(t, cache.ruleSeries(testRule("obv", "imbalance_pct"))[200], 100)
}
func TestCrossingsAndUnknownGroups(t *testing.T) {
	candles := ruleFixture(4, 3600000)
	for i, x := range []float64{100, 100, 110, 100} {
		candles[i].Close = x
	}
	c := testRule("price", "change_pct")
	c.WindowHours = 1
	c.Direction = "both"
	c.Threshold = 5
	c.Operator = "crosses_above"
	cache := newCache(candles)
	got, ok := cache.evaluate(c, 2)
	if !got || !ok {
		t.Fatal("cross up missing")
	}
	c.Operator = "crosses_below"
	c.Threshold = 10
	got, ok = cache.evaluate(c, 3)
	if !got || !ok {
		t.Fatal("cross down missing")
	}
	missing := testRule("cvd", "sum")
	missing.WindowHours = 24
	group := bt.Condition{Kind: "or", Children: []bt.Condition{c, missing}}
	if _, ok := cache.evaluate(group, 3); ok {
		t.Fatal("OR consumed incomplete history")
	}
}
