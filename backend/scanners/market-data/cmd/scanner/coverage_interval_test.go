package main

import "testing"

func TestCompletedConnectionIntervalPreservesCoveredMinuteAfterDisconnect(t *testing.T) {
	const minute = int64(1_800_000)
	interval := streamCoverageInterval{startMs: minute - 1, endMs: minute + ringMinuteMs}
	liquidations := NewLiquidationStore()
	liquidations.coverage = []streamCoverageInterval{interval}
	trades := NewTradeStore()
	trades.coverage = []streamCoverageInterval{interval}
	if !liquidations.CoversMinute(minute) || !trades.CoversMinute(minute) {
		t.Fatal("a fully covered minute became missing after disconnect")
	}
}

func TestConnectionGapNeverClaimsMinuteCoverage(t *testing.T) {
	const minute = int64(1_800_000)
	interval := streamCoverageInterval{startMs: minute - 1, endMs: minute + ringMinuteMs - 1}
	liquidations := NewLiquidationStore()
	liquidations.coverage = []streamCoverageInterval{interval}
	trades := NewTradeStore()
	trades.coverage = []streamCoverageInterval{interval}
	if liquidations.CoversMinute(minute) || trades.CoversMinute(minute) {
		t.Fatal("an interval ending before the minute boundary claimed coverage")
	}
}
