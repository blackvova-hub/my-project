package marketdata

import (
	"context"
	bt "shortlong/backtest"
	"strings"
	"testing"
	"time"
)

func TestMetricHistoryStartsAtAvailableTimeframeCandle(t *testing.T) {
	const hour = int64(time.Hour / time.Millisecond)
	r := bt.Request{From: time.UnixMilli(48 * hour), Timeframe: "1h"}
	for _, tc := range []struct {
		name        string
		first, want int64
	}{
		{"full warmup", 0, 24 * hour},
		{"partial warmup", 36 * hour, 36 * hour},
		{"listing inside test period", 72 * hour, 72 * hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := metricStart(r, 24, tc.first); got != tc.want {
				t.Fatalf("metric history starts at %d; want %d", got, tc.want)
			}
		})
	}
}

func TestEnrichEmptyAvailableHistoryDoesNotFetchMetrics(t *testing.T) {
	history := &History{}
	r := bt.Request{Timeframe: "1h", Strategy: bt.Strategy{Entry: bt.Condition{Kind: "indicator", Indicator: "openInterest", Measure: "change_pct", WindowHours: 24}}}
	if err := history.Enrich(context.Background(), r, "LYTEUSDT", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMinuteExternalMetricsOnlyAppearAtNativeHourCloses(t *testing.T) {
	const hour = int64(time.Hour / time.Millisecond)
	rows := availableTestCandles(15*MinuteStep, 3*hour, MinuteStep)
	points := map[int64]map[string]float64{
		0:        {"tradeBuyVolume": 9000, "tradeSellVolume": 9000},
		hour:     {"openInterest": 1000, "tradeBuyVolume": 1200, "tradeSellVolume": 400},
		2 * hour: {"openInterest": 1100, "tradeBuyVolume": 1800, "tradeSellVolume": 600},
		3 * hour: {"openInterest": 1200},
	}
	if err := applyHistoricalMetrics("LYTEUSDT", "oi", "1m", rows[0].Time, rows, []string{"openInterest"}, points); err != nil {
		t.Fatal(err)
	}
	if err := applyHistoricalMetrics("LYTEUSDT", "trades", "1m", rows[0].Time, rows, []string{"tradeBuyVolume", "tradeSellVolume"}, points); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		closing := row.Time + MinuteStep
		if closing%hour != 0 {
			for _, name := range []string{"openInterest", "tradeBuyVolume", "tradeSellVolume"} {
				if _, ok := row.Metrics[name]; ok {
					t.Fatalf("%s was forward-filled into minute %d", name, row.Time)
				}
			}
			continue
		}
		if row.Metrics["openInterest"] != points[closing]["openInterest"] {
			t.Fatalf("OI observation is not aligned to its actual availability: %+v", row)
		}
		if closing == hour {
			if _, ok := row.Metrics["tradeBuyVolume"]; ok {
				t.Fatal("first partial listing hour was accepted as a complete flow window")
			}
		} else if row.Metrics["tradeBuyVolume"] != points[closing-hour]["tradeBuyVolume"] || row.Metrics["tradeSellVolume"] != points[closing-hour]["tradeSellVolume"] {
			t.Fatalf("hourly flows were spread over minutes or shifted: %+v", row)
		}
	}
}

func TestMinuteExternalMetricGapsAfterStartStillFail(t *testing.T) {
	const hour = int64(time.Hour / time.Millisecond)
	for _, kind := range []string{"oi", "trades"} {
		rows := availableTestCandles(15*MinuteStep, 2*hour, MinuteStep)
		name, missing := "openInterest", 2*hour
		points := map[int64]map[string]float64{hour: {"openInterest": 1000}}
		if kind == "trades" {
			name, missing = "tradeBuyVolume", hour
			points = map[int64]map[string]float64{}
		}
		err := applyHistoricalMetrics("LYTEUSDT", kind, "1m", rows[0].Time, rows, []string{name}, points)
		if err == nil || !strings.Contains(err.Error(), "LYTEUSDT") || !strings.Contains(err.Error(), time.UnixMilli(missing).UTC().Format("02.01.2006 15:04")) {
			t.Fatalf("%s source gap was hidden: %v", kind, err)
		}
	}
}
