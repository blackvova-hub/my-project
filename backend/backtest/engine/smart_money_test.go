package engine

import (
	bt "shortlong/backtest"
	"testing"
	"time"
)

// Same sequence as the chart's Smart Money regression fixture.
func structureCandles() []bt.Candle {
	c := []bt.Candle{}
	for i := 0; i <= 26; i++ {
		close, high, low, open := 100.0, 101.0, 99.0, 99.8
		if i >= 15 && i <= 20 {
			high = 101 + float64(i-15)*1.8
			close = high - 0.8
			low = close - 1
			open = close + 0.8
		}
		if i > 20 && i <= 25 {
			high = 110 - float64(i-20)*1.4
			close = high - 0.8
			low = close - 1
			open = close + 0.8
		}
		if i == 26 {
			open = 108.8
			high = 112
			low = 108.5
			close = 111.5
		}
		c = append(c, bt.Candle{Time: int64(i + 1), Open: open, High: high, Low: low, Close: close, Volume: 100})
	}
	for _, close := range []float64{111, 110, 109, 108, 107, 108, 109, 110, 111, 112} {
		c = append(c, bt.Candle{Time: int64(len(c) + 1), Open: close - 0.3, High: close + 0.5, Low: close - 0.5, Close: close, Volume: 100})
	}
	return append(c, bt.Candle{Time: 38, Open: 112, High: 112.2, Low: 103.5, Close: 104, Volume: 500})
}
func TestSmartMoneyConfirmedEventsAndNoLookahead(t *testing.T) {
	c := structureCandles()
	full := smartMoney(c)
	for key, index := range map[string]int{"bos:bullish": 26, "choch:bearish": 37, "mss:bearish": 37, "order_block:bullish": 26, "order_block:bearish": 37} {
		if full[key][index] != index {
			t.Errorf("missing %s at %d: %v", key, index, full[key])
		}
	}
	for end := 24; end < len(c); end++ {
		prefix := smartMoney(c[:end+1])
		for key, v := range prefix {
			for i := 0; i <= end; i++ {
				if v[i] != full[key][i] {
					t.Fatalf("future changed %s at %d", key, i)
				}
			}
		}
	}
	mirrored := append([]bt.Candle(nil), c...)
	for i, b := range c {
		mirrored[i].Open = 300 - b.Open
		mirrored[i].Close = 300 - b.Close
		mirrored[i].High = 300 - b.Low
		mirrored[i].Low = 300 - b.High
	}
	opposite := smartMoney(mirrored)
	for _, event := range []string{"bos", "choch", "mss", "order_block", "fvg", "liquidity_sweep"} {
		for i := range c {
			if full[event+":bullish"][i] != opposite[event+":bearish"][i] {
				t.Fatalf("direction asymmetry %s at %d", event, i)
			}
		}
	}
}
func TestSmartMoneySweepVersusCloseAndWindow(t *testing.T) {
	c := structureCandles()[:27]
	c[26].High = 111.2
	c[26].Close = 109.4
	c[26].Low = 108.5
	v := smartMoney(c)
	if v["liquidity_sweep:bearish"][26] != 26 || v["bos:bullish"][26] >= 0 {
		t.Fatal("wick-only break treated as BOS")
	}
	cache := newCache(structureCandles())
	node := bt.Condition{Kind: "smc", Event: "bos", Direction: "bullish", WithinBars: 3}
	for i, want := range map[int]bool{25: false, 26: true, 27: true, 28: true, 29: false} {
		got, valid := cache.evaluate(node, i)
		if !valid || got != want {
			t.Errorf("window %d: %v/%v", i, got, valid)
		}
	}
}
func TestSmartMoneyFVGOnThirdClose(t *testing.T) {
	c := make([]bt.Candle, 26)
	for i := range c {
		c[i] = bt.Candle{Time: int64(i + 1), Open: 100, High: 101, Low: 99, Close: 100, Volume: 100}
	}
	c = append(c, bt.Candle{Time: 27, Open: 100, High: 104, Low: 99.8, Close: 103.8, Volume: 300}, bt.Candle{Time: 28, Open: 103, High: 104, Low: 102, Close: 103.5, Volume: 100})
	v := smartMoney(c)["fvg:bullish"]
	if v[26] != -1 || v[27] != 27 {
		t.Fatal("early FVG", v)
	}
}
func TestSmartMoneyExecutesAfterConfirmation(t *testing.T) {
	r, _ := fixture()
	r.Strategy.Entry = bt.Condition{Kind: "smc", Event: "bos", Direction: "bullish", WithinBars: 1}
	r.To = r.From.Add(40 * time.Hour)
	r.Strategy.StopLossPct = 50
	r.Strategy.TakeProfitPct = 0
	data := candlesFor(r)
	c := data["BTCUSDT"]
	warm := r.WarmupBars()
	for i := range c {
		c[i].Open = 99.8
		c[i].High = 101
		c[i].Low = 99
	}
	for i, b := range structureCandles() {
		b.Time = c[warm+i].Time
		c[warm+i] = b
	}
	res := runTest(t, r, data)
	if len(res.Trades) != 1 || res.Trades[0].EntryTime != c[warm+27].Time {
		t.Fatalf("entry before confirmation: %+v", res.Trades)
	}
	if res.Signals[0].MatchedBars != 1 {
		t.Fatal(res.Signals)
	}
}
