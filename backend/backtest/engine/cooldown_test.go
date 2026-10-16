package engine

import (
	bt "shortlong/backtest"
	"testing"
	"time"
)

func TestReentryCooldownStartsAtEveryExitAndIncludesExactBoundary(t *testing.T) {
	for _, direction := range []string{"long", "short"} {
		for _, reason := range []string{"sl", "tp", "condition", "gap"} {
			t.Run(direction+"/"+reason, func(t *testing.T) {
				r, _ := fixture()
				r.Timeframe, r.Strategy.Direction = "1m", direction
				r.To = r.From.Add(100 * time.Minute)
				r.Strategy.TakeProfitPct = 0
				data := candlesFor(r)
				if reason == "condition" {
					exit := r.Strategy.Entry
					r.Strategy.Exit = &exit
				}
				for i := range data["BTCUSDT"] {
					bar := &data["BTCUSDT"][i]
					switch reason {
					case "sl":
						bar.High, bar.Low = 110, 90
					case "tp":
						r.Strategy.TakeProfitPct, r.Strategy.StopLossPct = 5, 50
						bar.High, bar.Low = 110, 90
					case "gap":
						if bar.Time == r.From.Add(2*time.Minute).UnixMilli() {
							if direction == "long" {
								bar.Open, bar.Low = 90, 90
							} else {
								bar.Open, bar.High = 110, 110
							}
						}
					}
				}
				result := runTest(t, r, data)
				if len(result.Trades) < 2 {
					t.Fatalf("expected a later reentry: %+v", result.Trades)
				}
				first, second := result.Trades[0], result.Trades[1]
				if got := second.EntryTime - first.ExitTime; got != (30 * time.Minute).Milliseconds() {
					t.Fatalf("cooldown = %s, want exactly 30m: %+v", time.Duration(got)*time.Millisecond, result.Trades)
				}
				for i := 1; i < len(result.Trades); i++ {
					if result.Trades[i].EntryTime-result.Trades[i-1].EntryTime < bt.ReentryCooldown.Milliseconds() {
						t.Fatal("repeat purchase before entry cooldown expired", result.Trades)
					}
					if result.Trades[i].EntryTime-result.Trades[i-1].ExitTime < bt.ReentryCooldown.Milliseconds() {
						t.Fatal("reentry before cooldown expired", result.Trades)
					}
				}
			})
		}
	}
}

func TestReentryCooldownDoesNotBlockOtherPairs(t *testing.T) {
	r, _ := fixture()
	r.Timeframe = "1m"
	r.To = r.From.Add(75 * time.Minute)
	r.Symbols = []string{"BTCUSDT", "ETHUSDT"}
	r.Strategy.Entry.Right.Value = 105
	data := candlesFor(r)
	for _, symbol := range r.Symbols {
		for i := range data[symbol] {
			bar := &data[symbol][i]
			if symbol == "BTCUSDT" || bar.Time >= r.From.Add(2*time.Minute).UnixMilli() {
				bar.Open, bar.High, bar.Low, bar.Close = 110, 110, 90, 110
			}
		}
	}
	result := runTest(t, r, data)
	var btc, eth []bt.Trade
	for _, trade := range result.Trades {
		if trade.Symbol == "BTCUSDT" {
			btc = append(btc, trade)
		} else {
			eth = append(eth, trade)
		}
	}
	if len(btc) < 2 || len(eth) == 0 {
		t.Fatal("missing independent trades", result.Trades)
	}
	if eth[0].EntryTime != r.From.Add(3*time.Minute).UnixMilli() || eth[0].EntryTime >= btc[1].EntryTime {
		t.Fatal("one instrument's cooldown blocked another", result.Trades)
	}
}

func TestReentryCooldownRestartsAfterClosingLongHeldPosition(t *testing.T) {
	for _, timeframe := range []string{"1m", "5m"} {
		t.Run(timeframe, func(t *testing.T) {
			r, _ := fixture()
			r.Timeframe = timeframe
			r.To = r.From.Add(2 * time.Hour)
			r.Strategy.TakeProfitPct = 0
			data := candlesFor(r)
			for i := range data["BTCUSDT"] {
				bar := &data["BTCUSDT"][i]
				if bar.Time == r.From.Add(40*time.Minute).UnixMilli() {
					bar.Open, bar.Low = 90, 90
				}
			}
			result := runTest(t, r, data)
			if len(result.Trades) != 2 {
				t.Fatal("expected one reentry after the long-held position", result.Trades)
			}
			first, second := result.Trades[0], result.Trades[1]
			if first.EntryTime != r.From.Add(bt.Interval(timeframe)).UnixMilli() {
				t.Fatal("cooldown delayed the first purchase", first)
			}
			if first.ExitTime != r.From.Add(40*time.Minute).UnixMilli() || second.EntryTime != r.From.Add(70*time.Minute).UnixMilli() {
				t.Fatal("exit must start a fresh 30-minute cooldown, measured in time rather than bars", result.Trades)
			}
		})
	}
}
