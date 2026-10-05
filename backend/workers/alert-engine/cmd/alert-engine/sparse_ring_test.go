package main

import (
	"bytes"
	"math"
	"testing"
)

func TestSparseHistoryStoresOnlyConfiguredSeriesAndValidity(t *testing.T) {
	history := newSparseHistory(historyPlan{"close": 3, "liquidations": 2})
	event := CandleEvent{
		Close:        101.5,
		Liquidations: 0,
		Metrics: map[string]MetricValue{
			"close":        {Value: 101.5, Valid: true},
			"liquidations": {Value: 0, Valid: true},
			"openInterest": {Value: 42, Valid: true},
		},
	}
	history.Put(100, event)

	if value, ok := history.Get(100, "close"); !ok || value != 101.5 {
		t.Fatalf("close=(%v,%v)", value, ok)
	}
	if value, ok := history.Get(100, "liquidations"); !ok || value != 0 {
		t.Fatalf("valid zero liquidation=(%v,%v)", value, ok)
	}
	if _, ok := history.Get(100, "openInterest"); ok {
		t.Fatal("unconfigured openInterest was stored")
	}

	event.Metrics["liquidations"] = MetricValue{Value: 0, Valid: false}
	history.Put(101, event)
	if _, ok := history.Get(101, "liquidations"); ok {
		t.Fatal("missing liquidation coverage became a valid zero")
	}
}

func TestSparseHistoryReconfigureShrinksAndReleasesSeries(t *testing.T) {
	history := newSparseHistory(historyPlan{"close": 5, "openInterest": 5})
	for minute := int64(1); minute <= 5; minute++ {
		history.Put(minute, CandleEvent{Metrics: map[string]MetricValue{
			"close":        {Value: float64(minute), Valid: true},
			"openInterest": {Value: float64(minute * 10), Valid: true},
		}})
	}

	history.Reconfigure(historyPlan{"close": 3})
	if history.SeriesCount() != 1 {
		t.Fatalf("series count=%d want=1", history.SeriesCount())
	}
	if history.Capacity("close") != 3 {
		t.Fatalf("close capacity=%d want=3", history.Capacity("close"))
	}
	if _, ok := history.Get(2, "close"); ok {
		t.Fatal("shrink retained data outside the new capacity")
	}
	for minute := int64(3); minute <= 5; minute++ {
		if value, ok := history.Get(minute, "close"); !ok || value != float64(minute) {
			t.Fatalf("minute %d=(%v,%v)", minute, value, ok)
		}
	}
	if _, ok := history.Get(5, "openInterest"); ok {
		t.Fatal("removed series remains readable")
	}
}

func TestSparseSeriesHandlesOutOfOrderAndNegativeMinutes(t *testing.T) {
	series := newSparseSeries(3)
	series.Put(-1, 1, true)
	series.Put(1, 3, true)
	series.Put(0, 2, true)

	for minute, want := range map[int64]float64{-1: 1, 0: 2, 1: 3} {
		if got, ok := series.Get(minute); !ok || got != want {
			t.Fatalf("minute %d=(%v,%v) want=%v", minute, got, ok, want)
		}
	}
}

func TestSparseHistoryTreatsNonFiniteValuesAsInvalid(t *testing.T) {
	history := newSparseHistory(historyPlan{"close": 2})
	history.Put(1, CandleEvent{Metrics: map[string]MetricValue{"close": {Value: math.NaN(), Valid: true}}})
	if _, ok := history.Get(1, "close"); ok {
		t.Fatal("NaN was stored as valid")
	}
}

func TestSparseSeriesAdvanceEvictsPointsOutsideRetainedWindow(t *testing.T) {
	series := newSparseSeries(3)
	series.Put(10, 10, true)
	series.Put(11, 11, true)
	series.Put(14, 14, true)

	if _, ok := series.Get(10); ok {
		t.Fatal("point outside the advanced retention window remains readable")
	}
	if _, ok := series.Get(11); ok {
		t.Fatal("second point outside the advanced retention window remains readable")
	}
	if value, ok := series.Get(14); !ok || value != 14 {
		t.Fatalf("latest point=(%v,%v), want (14,true)", value, ok)
	}
	if err := writeSnapshotSeries(&bytes.Buffer{}, "close", series); err != nil {
		t.Fatalf("advanced series cannot be saved: %v", err)
	}
}

func TestSparseSeriesLargeAdvanceClearsPreviousWindow(t *testing.T) {
	series := newSparseSeries(3)
	series.Put(10, 10, true)
	series.Put(11, 11, true)
	series.Put(20, 20, true)

	if _, ok := series.Get(10); ok {
		t.Fatal("old point survived an advance larger than capacity")
	}
	if _, ok := series.Get(11); ok {
		t.Fatal("old point survived an advance larger than capacity")
	}
	if err := writeSnapshotSeries(&bytes.Buffer{}, "low", series); err != nil {
		t.Fatalf("series after large advance cannot be saved: %v", err)
	}
}

func TestSparseHistoryMemoryEstimateReflectsActualSeries(t *testing.T) {
	history := newSparseHistory(historyPlan{"close": 1441, "liquidations": 60})
	stats := history.Stats()
	if stats.Series != 2 || stats.ValueSlots != 1501 || stats.KeySlots != 1501 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	fullRingArrays := int64(44 * 1441 * 8)
	if stats.BackingBytes >= fullRingArrays {
		t.Fatalf("sparse estimate=%d must be below full ring arrays=%d", stats.BackingBytes, fullRingArrays)
	}
}
