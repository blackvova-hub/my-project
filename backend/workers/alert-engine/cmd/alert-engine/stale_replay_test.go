package main

import (
	"testing"
	"time"
)

func TestSparseSeriesRejectsReplayOlderThanRetainedWindow(t *testing.T) {
	series := newSparseSeries(3)
	series.Put(10, 10, true)
	series.Put(11, 11, true)
	series.Put(12, 12, true)
	if accepted := series.Put(7, 700, true); accepted {
		t.Fatal("stale replay outside retained window was accepted")
	}
	for minute := int64(10); minute <= 12; minute++ {
		if value, ok := series.Get(minute); !ok || value != float64(minute) {
			t.Fatalf("fresh minute %d corrupted: value=%v ok=%v", minute, value, ok)
		}
	}
}

func TestSparseSeriesAcceptsOutOfOrderMinuteStillInsideWindow(t *testing.T) {
	series := newSparseSeries(3)
	series.Put(12, 12, true)
	if accepted := series.Put(10, 10, true); !accepted {
		t.Fatal("out-of-order minute inside retained window was rejected")
	}
	if value, ok := series.Get(10); !ok || value != 10 {
		t.Fatalf("retained value=%v ok=%v", value, ok)
	}
}

func TestCoverageFalseCandleDistinguishesPresentRowFromAbsentRow(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	registry := newCatalogRegistry(10, 27*time.Hour)
	tracker := newCoverageTracker(registry, 100, 27*time.Hour)
	catalog, version := compactCatalog([]string{"BTCUSDT", "ETHUSDT"})
	batch := compactDecodedBatch{
		BatchID: "partial", Kind: "c", Exchange: "bybit", MarketType: "perpetual", Minute: 1_800_000,
		ShardTotal: 1, Coverage: false, CatalogVersion: version, Catalog: catalog,
		Events: []CandleEvent{{Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT", TS: 1_800_000}},
	}
	if err := tracker.Observe(batch, now); err != nil {
		t.Fatal(err)
	}
	if got := tracker.Status("c", "bybit", "perpetual", batch.Minute, "BTCUSDT", now); got != coverageValid {
		t.Fatalf("present candle row status=%s want=%s", got, coverageValid)
	}
	if got := tracker.Status("c", "bybit", "perpetual", batch.Minute, "ETHUSDT", now); got != coverageMissing {
		t.Fatalf("absent candle row status=%s want=%s", got, coverageMissing)
	}
}
