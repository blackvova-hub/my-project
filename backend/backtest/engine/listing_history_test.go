package engine

import (
	"context"
	"math"
	"reflect"
	bt "shortlong/backtest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLateHistoryWarmsEachPairAndUsesNextOpen(t *testing.T) {
	for _, cut := range []int{1, 2, 4} {
		t.Run("prefix_"+strconv.Itoa(cut), func(t *testing.T) {
			r, _ := fixture()
			r.To = r.From.Add(10 * time.Hour)
			r.Strategy.Entry.Right.Value = 105
			r.Strategy.TakeProfitPct = 0
			r.Strategy.StopLossPct = 50
			data := candlesFor(r)
			c := data["BTCUSDT"][cut:]
			data["BTCUSDT"] = c
			warm := r.WarmupBars()
			for i := 0; i < warm; i++ {
				c[i].Open, c[i].High, c[i].Low, c[i].Close = 120, 120, 120, 120
			}
			c[warm].High, c[warm].Close = 106, 106
			for i := warm + 1; i < len(c); i++ {
				c[i].Open, c[i].High, c[i].Low, c[i].Close = 110, 110, 110, 110
			}
			res := runTest(t, r, data)
			wantEntry := c[warm+1].Time
			if len(res.Trades) != 1 || res.Trades[0].EntryTime != wantEntry {
				t.Fatalf("entry must follow the first post-warmup close: %+v", res.Trades)
			}
			near(t, res.Trades[0].EntryPrice, 110)
			stats := res.Signals[0]
			if stats.AvailableFrom != c[0].Time || stats.TradingFrom != wantEntry || stats.SkippedReason != "" || stats.EvaluatedBars != len(c)-warm-1 {
				t.Fatalf("incorrect late-history coverage: %+v", stats)
			}
			if res.Equity[0].Time != r.From.UnixMilli() || res.Equity[len(res.Equity)-1].Time != r.To.UnixMilli() {
				t.Fatal("late history truncated the requested portfolio timeline")
			}
		})
	}
}

func TestMixedHistoryKeepsOlderPairsAndSharedCapital(t *testing.T) {
	r, _ := fixture()
	r.To = r.From.Add(12 * time.Hour)
	r.Symbols = []string{"ETHUSDT", "TERUSDT", "BTCUSDT", "AKEUSDT"}
	r.Strategy.PositionSizePct = 80
	r.Strategy.TakeProfitPct = 0
	data := candlesFor(r)
	warm := r.WarmupBars()
	data["BTCUSDT"] = data["BTCUSDT"][warm+2:]
	for i := range data["BTCUSDT"] {
		c := &data["BTCUSDT"][i]
		c.Open, c.High, c.Low, c.Close = 200, 200, 200, 200
	}
	delete(data, "AKEUSDT") // The first sorted symbol has no candles at all.
	c := data["TERUSDT"]
	data["TERUSDT"] = c[len(c)-warm-1:]
	res := runTest(t, r, data)
	if len(res.Trades) != 2 {
		t.Fatalf("empty and short series must not trade: %+v", res.Trades)
	}
	trades := map[string]bt.Trade{}
	for _, trade := range res.Trades {
		trades[trade.Symbol] = trade
	}
	old, late := trades["ETHUSDT"], trades["BTCUSDT"]
	if old.EntryTime != r.From.Add(time.Hour).UnixMilli() || late.EntryTime != r.From.Add(5*time.Hour).UnixMilli() {
		t.Fatalf("listing delayed an older pair or bypassed its own warmup: %+v", trades)
	}
	near(t, old.Quantity*old.EntryPrice, 8000)
	near(t, late.Quantity*late.EntryPrice, 2000)
	near(t, res.Metrics.FinalCapital, 10000)
	if len(res.Equity) != 13 || res.Equity[1].Time != r.From.Add(time.Hour).UnixMilli() {
		t.Fatalf("portfolio did not preserve the common timeline: %+v", res.Equity)
	}
	stats := map[string]bt.SignalStats{}
	for _, s := range res.Signals {
		stats[s.Symbol] = s
	}
	if stats["AKEUSDT"].AvailableFrom != 0 || stats["AKEUSDT"].TradingFrom != 0 || stats["AKEUSDT"].SkippedReason == "" || stats["AKEUSDT"].EvaluatedBars != 0 {
		t.Fatal("empty series was not reported as skipped", stats)
	}
	if stats["TERUSDT"].AvailableFrom == 0 || stats["TERUSDT"].TradingFrom != 0 || stats["TERUSDT"].SkippedReason == "" || stats["TERUSDT"].EvaluatedBars != 0 {
		t.Fatal("short series was not reported as skipped", stats)
	}
	if stats["ETHUSDT"].EvaluatedBars != 11 || stats["BTCUSDT"].EvaluatedBars != 7 {
		t.Fatal("signal counts must use each pair's own availability", stats)
	}
	if stats["ETHUSDT"].AvailableFrom != r.From.Add(-time.Duration(warm)*time.Hour).UnixMilli() || stats["ETHUSDT"].TradingFrom != old.EntryTime || stats["BTCUSDT"].TradingFrom != late.EntryTime {
		t.Fatal("coverage must report the real prefix and earliest possible entry", stats)
	}
	r.Symbols = []string{"AKEUSDT", "BTCUSDT", "TERUSDT", "ETHUSDT"}
	if other := runTest(t, r, data); !reflect.DeepEqual(res, other) {
		t.Fatal("request order changed mixed-history portfolio")
	}
}

func TestAllHistoryUnavailableOrTooShort(t *testing.T) {
	for _, bars := range []int{0, 1, 3} {
		t.Run("bars_"+strconv.Itoa(bars), func(t *testing.T) {
			r, data := fixture()
			c := data["BTCUSDT"]
			data["BTCUSDT"] = c[len(c)-bars:]
			_, err := Run(context.Background(), r, data, nil)
			if err == nil || !strings.Contains(err.Error(), "Недостаточно истории для прогрева и проверки входа") {
				t.Fatalf("expected a useful error for %d remaining bars, got %v", bars, err)
			}
		})
	}
}

func TestLateHistoryStillRejectsGapsAndInvalidCandles(t *testing.T) {
	for _, defect := range []string{"interior gap", "trailing gap", "timestamp gap", "unaligned start", "too early", "NaN"} {
		t.Run(defect, func(t *testing.T) {
			r, data := fixture()
			c := data["BTCUSDT"][1:]
			switch defect {
			case "interior gap":
				c = append(c[:2:2], c[3:]...)
			case "trailing gap":
				c = c[:len(c)-1]
			case "timestamp gap":
				c[2].Time += time.Hour.Milliseconds()
			case "unaligned start":
				c[0].Time++
			case "too early":
				c[0].Time = r.From.UnixMilli() - int64(r.WarmupBars()+1)*time.Hour.Milliseconds()
			case "NaN":
				c[0].Close = math.NaN()
			}
			data["BTCUSDT"] = c
			_, err := Run(context.Background(), r, data, nil)
			if err == nil || !strings.HasPrefix(err.Error(), "BTCUSDT:") {
				t.Fatalf("invalid late tail accepted or missing symbol: %v", err)
			}
		})
	}
}

func TestAvailableBarsWithoutValidIndicatorValuesDoNotFail(t *testing.T) {
	r, _ := fixture()
	r.Strategy.Version = 2
	r.Strategy.Entry = bt.Condition{Kind: "indicator", Indicator: "openInterest", Measure: "change_pct", Direction: "up", WindowHours: 1, Operator: "gte", Threshold: 1}
	res := runTest(t, r, candlesFor(r))
	if len(res.Trades) != 0 || res.Signals[0].EvaluatedBars != 0 || res.Signals[0].TradingFrom == 0 || res.Signals[0].SkippedReason != "" {
		t.Fatalf("missing indicator values must remain unavailable, not become a history error: %+v", res)
	}
}
