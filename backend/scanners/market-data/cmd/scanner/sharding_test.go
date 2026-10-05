package main

import "testing"

func TestStableSymbolShardingSurvivesCatalogInsertions(t *testing.T) {
	base := []string{"ADAUSDT", "BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT"}
	changed := []string{"ADAUSDT", "BTCUSDT", "DOGEUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT"}
	for _, symbol := range base {
		if stableSymbolShard(symbol, 2) != stableSymbolShard(symbol, 2) {
			t.Fatalf("non-deterministic shard for %s", symbol)
		}
		if !containsString(changed, symbol) {
			t.Fatalf("fixture lost common symbol %s", symbol)
		}
	}
	for shard := 0; shard < 2; shard++ {
		baseShard, err := shardSymbols(base, shard, 2)
		if err != nil {
			t.Fatal(err)
		}
		changedShard, err := shardSymbols(changed, shard, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, symbol := range baseShard {
			if !containsString(changedShard, symbol) {
				t.Fatalf("common symbol %s moved from shard %d after insertion", symbol, shard)
			}
		}
	}
}

func TestStableSymbolShardingPartitionsCatalogWithoutOverlap(t *testing.T) {
	catalog := []string{"ADAUSDT", "BTCUSDT", "DOGEUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT"}
	seen := make(map[string]int, len(catalog))
	for shard := 0; shard < 3; shard++ {
		symbols, err := shardSymbols(catalog, shard, 3)
		if err != nil {
			t.Fatal(err)
		}
		for _, symbol := range symbols {
			seen[symbol]++
		}
	}
	for _, symbol := range catalog {
		if seen[symbol] != 1 {
			t.Fatalf("symbol %s appeared in %d shards", symbol, seen[symbol])
		}
	}
}
