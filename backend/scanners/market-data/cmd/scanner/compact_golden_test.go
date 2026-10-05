package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestScannerCompactV3MatchesSharedWorkerGoldenFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "compact_v3_liquidation.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	batch, err := buildCompactLiquidationBatch(
		[]compactLiquidationRow{{Symbol: "BTCUSDT", Values: [5]float64{100, 70, 30, 4, 50}}},
		[]string{"ETHUSDT", "BTCUSDT"}, 1_800_000, true, "okx:test", "okx", "perpetual",
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, bytes.TrimSpace(fixture)) {
		t.Fatalf("scanner compact wire drifted\n got: %s\nwant: %s", raw, bytes.TrimSpace(fixture))
	}
}
