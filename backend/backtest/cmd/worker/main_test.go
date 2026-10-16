package main

import (
	bt "shortlong/backtest"
	"testing"
	"time"
)

func TestSymbolHistoryStartPreservesOldHistoryAndBoundsNewListings(t *testing.T) {
	from := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	r := bt.Request{Timeframe: "1h", From: from}
	warmFrom := from.Add(-time.Duration(r.WarmupBars()) * time.Hour).UnixMilli()
	for _, tc := range []struct {
		name   string
		launch int64
		want   int64
	}{
		{"unknown listing", 0, warmFrom},
		{"old listing", from.AddDate(-1, 0, 0).UnixMilli(), warmFrom},
		{"listing during warmup", from.Add(-time.Hour).Add(15 * time.Minute).UnixMilli(), from.Add(-time.Hour).UnixMilli()},
		{"new listing partial first hour", time.Date(2026, 8, 13, 9, 2, 0, 0, time.UTC).UnixMilli(), time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC).UnixMilli()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := symbolHistoryStart(r, tc.launch); got != tc.want {
				t.Fatalf("history start = %s, want %s", time.UnixMilli(got), time.UnixMilli(tc.want))
			}
		})
	}
	r.Timeframe = "4h"
	launch := time.Date(2026, 8, 13, 9, 2, 0, 0, time.UTC).UnixMilli()
	if got, want := symbolHistoryStart(r, launch), time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC).UnixMilli(); got != want {
		t.Fatalf("4h history start = %d, want %d", got, want)
	}
}
