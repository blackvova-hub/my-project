package backtest

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestIndicatorValidation(t *testing.T) {
	base := Condition{Kind: "indicator", Indicator: "price", Measure: "change_pct", WindowHours: 24, Direction: "up", Threshold: 5, Operator: "gte"}
	for name, patch := range map[string]func(*Condition){"window": func(c *Condition) { c.WindowHours = 0 }, "direction": func(c *Condition) { c.Direction = "bullish" }, "measure": func(c *Condition) { c.Measure = "price" }, "negative": func(c *Condition) { c.Threshold = -1 }, "nan": func(c *Condition) { c.Threshold = math.NaN() }, "extraneous": func(c *Condition) { c.Left = &Operand{Kind: "close"} }, "period": func(c *Condition) { c.Period = 14 }} {
		t.Run(name, func(t *testing.T) {
			c := base
			patch(&c)
			if validateIndicator(c) == nil {
				t.Fatal("invalid rule accepted")
			}
		})
	}
	for name, measures := range IndicatorMeasures {
		for _, measure := range measures {
			c := Condition{Kind: "indicator", Indicator: name, Measure: measure, Operator: "gte", Threshold: 0}
			if HasWindow(c) {
				c.WindowHours = 24
			}
			if HasPeriod(c) {
				c.Period = 14
			}
			if measure == "change_pct" {
				c.Direction = "up"
			}
			if measure == "signal_cross" {
				c.Operator = "crosses_above"
			}
			if err := validateIndicator(c); err != nil {
				t.Fatalf("%s/%s: %v", name, measure, err)
			}
			raw, err := json.Marshal(c)
			if err != nil || !strings.Contains(string(raw), `"threshold":0`) {
				t.Fatalf("zero threshold lost: %s / %v", raw, err)
			}
			var decoded Condition
			if err = json.Unmarshal(raw, &decoded); err != nil || validateIndicator(decoded) != nil {
				t.Fatal("roundtrip failed")
			}
		}
	}
	r := Request{Market: "spot", Timeframe: "1h", Strategy: Strategy{Entry: Condition{Kind: "indicator", Indicator: "openInterest", Measure: "change_pct", WindowHours: 24, Direction: "up", Operator: "gte"}}}
	if r.validateIndicators() == nil {
		t.Fatal("Spot OI accepted")
	}
	r.Market = "linear"
	r.Timeframe = "4h"
	r.Strategy.Entry.WindowHours = 3
	if r.validateIndicators() == nil {
		t.Fatal("unaligned window accepted")
	}
}
func TestIndependentMetricWarmups(t *testing.T) {
	r := Request{Timeframe: "4h", Strategy: Strategy{Entry: Condition{Kind: "and", Children: []Condition{
		{Kind: "indicator", Indicator: "rsi", Measure: "value", Period: 400},
		{Kind: "indicator", Indicator: "cvd", Measure: "sum", WindowHours: 24},
	}}}}
	if r.WarmupBars() < 2000 {
		t.Fatal("RSI warmup lost")
	}
	if got := r.MetricWarmup()["trades"]; got != 8 {
		t.Fatalf("trades inherited RSI warmup: %d", got)
	}
	if (Condition{Indicator: "volume", Measure: "change_pct", WindowHours: 24}).RequiredBars(time.Hour) != 49 {
		t.Fatal("volume needs two windows")
	}
}

func TestThresholdPresenceAndUnknownParameters(t *testing.T) {
	for _, raw := range []string{
		`{"kind":"indicator","indicator":"rsi","measure":"value","operator":"gte","period":14}`,
		`{"kind":"indicator","indicator":"rsi","measure":"value","operator":"gte","period":14,"threshold":null}`,
		`{"kind":"indicator","indicator":"rsi","measure":"value","operator":"gte","period":14,"threshold":0,"typo":5}`,
	} {
		var c Condition
		if json.Unmarshal([]byte(raw), &c) == nil {
			t.Fatalf("invalid JSON accepted: %s", raw)
		}
	}
}

func TestMinuteRequestsSupportAllIndicatorsAndHourlyWindows(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for indicator, measures := range IndicatorMeasures {
		for _, measure := range measures {
			t.Run(indicator+"/"+measure, func(t *testing.T) {
				r := validRequest()
				r.Market, r.Timeframe, r.Strategy.Version = "linear", "1m", 2
				c := Condition{Kind: "indicator", Indicator: indicator, Measure: measure, Operator: "gte", Threshold: 0}
				if HasWindow(c) {
					c.WindowHours = 3
				}
				if HasPeriod(c) {
					c.Period = 14
				}
				if measure == "change_pct" {
					c.Direction = "up"
				}
				if measure == "signal_cross" {
					c.Operator = "crosses_above"
				}
				r.Strategy.Entry = c
				if err := r.ValidateForPlan(now, "free"); err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				var decoded Request
				if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Timeframe != "1m" {
					t.Fatalf("minute contract lost: %v", err)
				}
			})
		}
	}
	if got := (Condition{Indicator: "price", Measure: "change_pct", WindowHours: 720}).RequiredBars(time.Minute); got != 43201 {
		t.Fatalf("30-day minute window = %d bars", got)
	}
	for _, c := range []Condition{
		{Indicator: "openInterest", Measure: "change_pct", WindowHours: 24},
		{Indicator: "cvd", Measure: "sum", WindowHours: 24},
		{Indicator: "tradeBuyVolume", Measure: "change_pct", WindowHours: 24},
	} {
		if got, want := c.RequiredBars(time.Minute), c.RequiredBars(time.Hour)*60; got != want {
			t.Fatalf("%s native-hour warmup: got %d minutes, want %d", c.Indicator, got, want)
		}
	}
}
