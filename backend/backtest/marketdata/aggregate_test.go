package marketdata

import (
	bt "shortlong/backtest"
	"testing"
)

func TestAggregateOHLCVAndRejectIncompleteWindows(t *testing.T) {
	base := []bt.Candle{
		{Time: 0, Open: 100, High: 110, Low: 99, Close: 105, Volume: 2},
		{Time: 300000, Open: 105, High: 115, Low: 102, Close: 110, Volume: 3},
		{Time: 600000, Open: 110, High: 112, Low: 95, Close: 98, Volume: 4},
	}
	got, err := Aggregate(base, "15m", 0, 900000)
	if err != nil || len(got) != 1 || got[0].Open != 100 || got[0].High != 115 || got[0].Low != 95 || got[0].Close != 98 || got[0].Volume != 9 {
		t.Fatal(got, err)
	}
	if _, err = Aggregate(base[:2], "15m", 0, 900000); err == nil {
		t.Fatal("partial window accepted")
	}
	if _, err = Aggregate(base, "15m", 300000, 1200000); err == nil {
		t.Fatal("unaligned window accepted")
	}
}
