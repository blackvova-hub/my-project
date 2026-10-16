package engine

import (
	"math"
	bt "shortlong/backtest"
	"testing"
)

func minuteExternalFixture(hours int) []bt.Candle {
	candles := ruleFixture(hours*60, minuteMillis)
	for i := range candles {
		candles[i].Metrics = nil
		if (i+1)%60 == 0 {
			hour := i / 60
			candles[i].Metrics = map[string]float64{"openInterest": 1000 + float64(hour)*100, "tradeBuyVolume": float64(hour+1) * 10, "tradeSellVolume": float64(hour + 1)}
		}
	}
	return candles
}

func TestMinuteExternalMetricsPreserveNativeHourlyWindows(t *testing.T) {
	cache := newCache(minuteExternalFixture(6))
	for _, tc := range []struct {
		indicator, measure string
		hours, at          int
		want               float64
	}{
		{"openInterest", "change_pct", 2, 179, 20},
		{"tradeBuyVolume", "sum", 2, 119, 30},
		{"tradeSellVolume", "sum", 2, 119, 3},
		{"cvd", "sum", 2, 119, 27},
		{"delta", "sum", 2, 119, 27},
		{"cvd", "imbalance_pct", 2, 119, 27.0 / 33 * 100},
		{"delta", "imbalance_pct", 2, 119, 27.0 / 33 * 100},
		{"tradeBuyVolume", "relative", 2, 239, 70.0 / 30},
		{"tradeSellVolume", "change_pct", 2, 239, (7.0 - 3) / 3 * 100},
	} {
		t.Run(tc.indicator+"/"+tc.measure, func(t *testing.T) {
			c := testRule(tc.indicator, tc.measure)
			c.WindowHours = tc.hours
			values := cache.ruleSeries(c)
			ruleNear(t, values[tc.at], tc.want)
			for i, value := range values {
				if (i+1)%60 != 0 && !math.IsNaN(value) {
					t.Fatalf("native metric was forward-filled to minute %d", i)
				}
			}
		})
	}
}

func TestMinutePartialListingHourKeepsOIAndMissingFlowsSeparate(t *testing.T) {
	candles := minuteExternalFixture(5)[15:]
	delete(candles[44].Metrics, "tradeBuyVolume")
	delete(candles[44].Metrics, "tradeSellVolume")
	cache := newCache(candles)
	c := testRule("tradeBuyVolume", "sum")
	c.WindowHours = 2
	values := cache.ruleSeries(c)
	if !math.IsNaN(values[104]) {
		t.Fatal("first partial listing hour became a zero flow")
	}
	ruleNear(t, values[164], 50)
	c = testRule("openInterest", "change_pct")
	c.WindowHours = 1
	ruleNear(t, cache.ruleSeries(c)[104], 10)
	c = testRule("cvd", "sum")
	c.WindowHours = 1
	if !math.IsNaN(cache.ruleSeries(c)[44]) {
		t.Fatal("missing first partial-hour trade sides became a CVD signal")
	}
	ruleNear(t, cache.ruleSeries(c)[104], 18)
}

func TestMinuteExternalCrossingsComparePreviousNativeSample(t *testing.T) {
	candles := minuteExternalFixture(5)
	for hour, value := range []float64{100, 100, 120, 120, 100} {
		candles[(hour+1)*60-1].Metrics["openInterest"] = value
	}
	cache := newCache(candles)
	c := testRule("openInterest", "change_pct")
	c.WindowHours, c.Direction, c.Threshold, c.Operator = 1, "up", 10, "crosses_above"
	if matched, valid := cache.evaluate(c, 179); !matched || !valid {
		t.Fatal("hourly crossing incorrectly compared the preceding empty minute")
	}
	if matched, valid := cache.evaluate(c, 180); matched || valid {
		t.Fatal("hourly metric must remain unavailable between native boundaries")
	}
	c.Direction = "down"
	if matched, valid := cache.evaluate(c, 299); !matched || !valid {
		t.Fatal("native decline crossing missing")
	}
	c = testRule("cvd", "sum")
	c.WindowHours, c.Threshold, c.Operator = 1, 18, "crosses_above"
	if matched, valid := cache.evaluate(c, 179); !matched || !valid {
		t.Fatal("native flow crossing missing")
	}
}

func TestMinuteExternalMetricCausality(t *testing.T) {
	candles := minuteExternalFixture(8)
	for _, name := range []string{"openInterest", "cvd", "delta", "tradeBuyVolume", "tradeSellVolume"} {
		measure := "sum"
		if name == "openInterest" {
			measure = "change_pct"
		}
		c := testRule(name, measure)
		c.WindowHours = 2
		full := newCache(candles).ruleSeries(c)
		prefix := newCache(candles[:301]).ruleSeries(c)
		for i := 179; i < len(prefix); i++ {
			if math.IsNaN(prefix[i]) {
				if !math.IsNaN(full[i]) {
					t.Fatal("future metric changed availability", name, i)
				}
			} else {
				ruleNear(t, full[i], prefix[i])
			}
		}
	}
}

func TestMinuteIndicatorCatalogRemainsCausal(t *testing.T) {
	candles := minuteExternalFixture(34)
	for name, measures := range bt.IndicatorMeasures {
		for _, measure := range measures {
			t.Run(name+"/"+measure, func(t *testing.T) {
				c := testRule(name, measure)
				if bt.HasWindow(c) {
					c.WindowHours = 1
				}
				full := newCache(candles).ruleSeries(c)
				prefix := newCache(candles[:1801]).ruleSeries(c)
				available := false
				for i := 1700; i < len(prefix); i++ {
					if math.IsNaN(prefix[i]) {
						if !math.IsNaN(full[i]) {
							t.Fatal("future candles changed metric availability", i)
						}
						continue
					}
					available = true
					ruleNear(t, full[i], prefix[i])
				}
				if !available {
					t.Fatal("minute execution disabled a supported indicator")
				}
			})
		}
	}
}
