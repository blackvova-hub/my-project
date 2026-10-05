package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkerCompactV3DecodesSharedScannerGoldenFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "compact_v3_liquidation.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeCompactBatch(bytes.TrimSpace(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.BatchID != "l:okx:perpetual:1800000:0:1:e9c34a7d21d64b45a82652eb2df9dd91:0:" || len(decoded.Events) != 2 {
		t.Fatalf("decoded header/events=%+v", decoded)
	}
	if decoded.Events[0].Symbol != "BTCUSDT" || decoded.Events[0].Liquidations != 100 || !decoded.Events[0].Metrics["liquidations"].Valid {
		t.Fatalf("non-zero row=%+v", decoded.Events[0])
	}
	if decoded.Events[1].Symbol != "ETHUSDT" || decoded.Events[1].Liquidations != 0 || !decoded.Events[1].Metrics["liquidations"].Valid {
		t.Fatalf("covered zero row=%+v", decoded.Events[1])
	}
	var wire compactWireBatch
	if err := json.Unmarshal(fixture, &wire); err != nil {
		t.Fatal(err)
	}
	reencoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded, bytes.TrimSpace(fixture)) {
		t.Fatalf("worker wire struct drifted\n got: %s\nwant: %s", reencoded, bytes.TrimSpace(fixture))
	}
}
