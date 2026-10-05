package main

import (
	"testing"
	"time"
)

func TestCatalogRegistryIsImmutableAndBounded(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	registry := newCatalogRegistry(2, time.Hour)
	one, oneVersion := compactCatalog([]string{"BTCUSDT"})
	if err := registry.Register(oneVersion, one, now); err != nil {
		t.Fatalf("register first catalog: %v", err)
	}
	one[0] = "MUTATED"
	stored, ok := registry.Lookup(oneVersion, now)
	if !ok || len(stored) != 1 || stored[0] != "BTCUSDT" {
		t.Fatalf("registry retained caller-owned storage: %#v", stored)
	}
	stored[0] = "MUTATED_AGAIN"
	stored, _ = registry.Lookup(oneVersion, now)
	if stored[0] != "BTCUSDT" {
		t.Fatal("lookup exposed mutable registry storage")
	}
	if err := registry.Register(oneVersion, []string{"ETHUSDT"}, now); err == nil {
		t.Fatal("same catalog version accepted different content")
	}

	second, secondVersion := compactCatalog([]string{"ETHUSDT"})
	third, thirdVersion := compactCatalog([]string{"SOLUSDT"})
	if err := registry.Register(secondVersion, second, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(thirdVersion, third, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if registry.Len() != 2 {
		t.Fatalf("registry size=%d want=2", registry.Len())
	}
	if _, ok := registry.Lookup(oneVersion, now.Add(2*time.Minute)); ok {
		t.Fatal("oldest catalog was not evicted at max size")
	}
}

func TestCoverageTrackerWaitsForAllShardsAndPreservesMissing(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	registry := newCatalogRegistry(10, 27*time.Hour)
	tracker := newCoverageTracker(registry, 100, 27*time.Hour)
	btcCatalog, btcVersion := compactCatalog([]string{"BTCUSDT"})
	ethCatalog, ethVersion := compactCatalog([]string{"ETHUSDT"})

	first := compactDecodedBatch{
		BatchID: "first", Kind: "c", Exchange: "bybit", MarketType: "perpetual",
		Minute: 1_800_000, ShardIndex: 0, ShardTotal: 2, Coverage: true,
		CatalogVersion: btcVersion, Catalog: btcCatalog,
	}
	if err := tracker.Observe(first, now); err != nil {
		t.Fatalf("observe first shard: %v", err)
	}
	if got := tracker.Status("c", "bybit", "perpetual", first.Minute, "BTCUSDT", now); got != coveragePending {
		t.Fatalf("status before all shards=%s want=%s", got, coveragePending)
	}

	second := compactDecodedBatch{
		BatchID: "second", Kind: "c", Exchange: "bybit", MarketType: "perpetual",
		Minute: first.Minute, ShardIndex: 1, ShardTotal: 2, Coverage: false,
		CatalogVersion: ethVersion, Catalog: ethCatalog,
	}
	if err := tracker.Observe(second, now); err != nil {
		t.Fatalf("observe second shard: %v", err)
	}
	checks := map[string]coverageStatus{
		"BTCUSDT": coverageValid,
		"ETHUSDT": coverageMissing,
		"SOLUSDT": coverageUnsupported,
	}
	for symbol, want := range checks {
		if got := tracker.Status("c", "bybit", "perpetual", first.Minute, symbol, now); got != want {
			t.Fatalf("symbol=%s status=%s want=%s", symbol, got, want)
		}
	}
}

func TestCoverageTrackerRejectsConflictingShardIdentityAndCatalogOverlap(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	registry := newCatalogRegistry(10, time.Hour)
	tracker := newCoverageTracker(registry, 100, time.Hour)
	catalog, version := compactCatalog([]string{"BTCUSDT"})
	base := compactDecodedBatch{BatchID: "one", Kind: "c", Exchange: "binance", MarketType: "perpetual", Minute: 1_800_000, ShardIndex: 0, ShardTotal: 2, Coverage: true, CatalogVersion: version, Catalog: catalog}
	if err := tracker.Observe(base, now); err != nil {
		t.Fatal(err)
	}
	conflict := base
	conflict.BatchID = "two"
	conflict.ShardTotal = 3
	if err := tracker.Observe(conflict, now); err == nil {
		t.Fatal("conflicting shard total was accepted")
	}
	overlap := base
	overlap.BatchID = "three"
	overlap.ShardIndex = 1
	if err := tracker.Observe(overlap, now); err != nil {
		t.Fatalf("observe overlapping shard: %v", err)
	}
	if got := tracker.Status("c", "binance", "perpetual", base.Minute, "BTCUSDT", now); got != coverageConflict {
		t.Fatalf("overlapping catalog status=%s want=%s", got, coverageConflict)
	}
}

func TestCoverageTrackerExpiresMinuteState(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	registry := newCatalogRegistry(10, time.Minute)
	tracker := newCoverageTracker(registry, 100, time.Minute)
	catalog, version := compactCatalog([]string{"BTCUSDT"})
	batch := compactDecodedBatch{BatchID: "one", Kind: "l", Exchange: "okx", MarketType: "perpetual", Minute: 1_800_000, ShardTotal: 1, Coverage: true, CatalogVersion: version, Catalog: catalog}
	if err := tracker.Observe(batch, now); err != nil {
		t.Fatal(err)
	}
	if got := tracker.Status("l", "okx", "perpetual", batch.Minute, "BTCUSDT", now.Add(2*time.Minute)); got != coveragePending {
		t.Fatalf("expired status=%s want=%s", got, coveragePending)
	}
}
