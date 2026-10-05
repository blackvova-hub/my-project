package main

import "testing"

func TestCleanupScanStoresKeepsCurrentMinuteAndRemovesOldData(t *testing.T) {
	const targetMinute = int64(1_000)
	oldMinute := targetMinute - 121
	currentMinute := targetMinute
	key := InstrumentKey{Exchange: "bybit", MarketType: "perpetual", Symbol: "BTCUSDT"}

	trades := NewTradeStore()
	trades.Add(key, oldMinute*ringMinuteMs, 100, 1, "Buy", 1)
	trades.Add(key, currentMinute*ringMinuteMs, 101, 1, "Buy", 1)
	liquidations := NewLiquidationStore()
	liquidations.Add(key, oldMinute*ringMinuteMs, 10, "Buy")
	liquidations.Add(key, currentMinute*ringMinuteMs, 20, "Sell")

	cleanupScanStores(trades, liquidations, targetMinute*ringMinuteMs)

	if byMinute := trades.buckets[key.String()]; byMinute == nil || byMinute[oldMinute] != nil || byMinute[currentMinute] == nil {
		t.Fatalf("trade cleanup left unexpected buckets: %#v", byMinute)
	}
	if byMinute := liquidations.sums[key.String()]; byMinute == nil || byMinute[oldMinute] != nil || byMinute[currentMinute] == nil {
		t.Fatalf("liquidation cleanup left unexpected buckets: %#v", byMinute)
	}
}
