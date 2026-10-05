package engine

import (
	"context"
	"math"
	"reflect"
	bt "shortlong/backtest"
	"strings"
	"testing"
	"time"
)

func fixture() (bt.Request, map[string][]bt.Candle) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	r := bt.Request{Exchange: "bybit", Market: "linear", Symbols: []string{"BTCUSDT"}, Timeframe: "1h", From: from, To: from.Add(4 * time.Hour), Strategy: bt.Strategy{Version: 1, Direction: "long", InitialCapital: 10000, PositionSizePct: 20, TakeProfitPct: 5, StopLossPct: 2, Entry: bt.Condition{Kind: "compare", Operator: "gt", Left: &bt.Operand{Kind: "close"}, Right: &bt.Operand{Kind: "constant", Value: 0}}}}
	return r, candlesFor(r)
}
func candlesFor(r bt.Request) map[string][]bt.Candle {
	data := map[string][]bt.Candle{}
	step := bt.Interval(r.Timeframe).Milliseconds()
	for _, symbol := range r.Symbols {
		c := []bt.Candle{}
		for ts := r.From.UnixMilli() - int64(r.WarmupBars())*step; ts < r.To.UnixMilli(); ts += step {
			c = append(c, bt.Candle{Time: ts, Open: 100, High: 100, Low: 100, Close: 100, Volume: 1})
		}
		data[symbol] = c
	}
	return data
}
func runTest(t *testing.T, r bt.Request, data map[string][]bt.Candle) *bt.Result {
	t.Helper()
	res, err := Run(context.Background(), r, data, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
func near(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-7 {
		t.Fatalf("got %.10f want %.10f", got, want)
	}
}

func TestExecutionUsesNextOpenAndDoesNotTradeWarmup(t *testing.T) {
	r, data := fixture()
	r.Strategy.Entry.Right.Value = 105
	c := data["BTCUSDT"]
	w := r.WarmupBars()
	c[w-1].Open = 120
	c[w-1].High = 120
	c[w-1].Low = 120
	c[w-1].Close = 120
	c[w+1].Close = 106
	c[w+1].High = 106
	c[w+2].Open = 110
	c[w+2].High = 111
	c[w+2].Low = 110
	c[w+2].Close = 111
	c[w+3].Open = 111
	c[w+3].Low = 111
	c[w+3].High = 111
	c[w+3].Close = 111
	res := runTest(t, r, data)
	if len(res.Trades) != 1 {
		t.Fatalf("trades=%+v", res.Trades)
	}
	trade := res.Trades[0]
	near(t, trade.EntryPrice, 110)
	if trade.EntryTime != r.From.Add(2*time.Hour).UnixMilli() {
		t.Fatal("entry used warmup or same candle")
	}
	near(t, trade.ExitPrice, 111)
}
func TestBothBarriersChooseStopAndGapUsesOpen(t *testing.T) {
	t.Run("both touched", func(t *testing.T) {
		r, data := fixture()
		r.To = r.From.Add(2 * time.Hour)
		data = candlesFor(r)
		c := data["BTCUSDT"]
		i := len(c) - 1
		c[i].High = 110
		c[i].Low = 90
		res := runTest(t, r, data)
		if len(res.Trades) != 1 || res.Trades[0].Reason != "sl" {
			t.Fatal(res.Trades)
		}
		near(t, res.Trades[0].ExitPrice, 98)
		near(t, res.Metrics.FinalCapital, 9960)
	})
	t.Run("gap through stop", func(t *testing.T) {
		r, data := fixture()
		r.To = r.From.Add(3 * time.Hour)
		data = candlesFor(r)
		c := data["BTCUSDT"]
		i := len(c) - 1
		c[i].Open = 90
		c[i].Low = 90
		c[i].Close = 95
		res := runTest(t, r, data)
		near(t, res.Trades[0].ExitPrice, 90)
		if res.Trades[0].Reason != "sl" {
			t.Fatal(res.Trades)
		}
	})
}
func TestFeesSlippageAndSharedCapitalReconcile(t *testing.T) {
	r, data := fixture()
	r.Strategy.FeePct = .1
	r.Strategy.SlippagePct = .05
	res := runTest(t, r, data)
	trade := res.Trades[0]
	near(t, trade.EntryPrice, 100.05)
	near(t, trade.ExitPrice, 99.95)
	qty := 2000 / 100.05
	fees := 2 + qty*99.95*.001
	pnl := qty*(99.95-100.05) - fees
	near(t, trade.Fees, fees)
	near(t, trade.PnL, pnl)
	near(t, res.Metrics.FinalCapital, 10000+pnl)
	near(t, res.Metrics.TotalFees, fees)
	r.Strategy.FeePct = 0
	r.Strategy.SlippagePct = 0
	r.Strategy.PositionSizePct = 80
	r.Symbols = []string{"ETHUSDT", "BTCUSDT"}
	data = candlesFor(r)
	res = runTest(t, r, data)
	near(t, res.Trades[0].Quantity*res.Trades[0].EntryPrice, 8000)
	near(t, res.Trades[1].Quantity*res.Trades[1].EntryPrice, 2000)
	near(t, res.Metrics.FinalCapital, 10000)
	r.Symbols = []string{"BTCUSDT", "ETHUSDT"}
	other := runTest(t, r, data)
	if !reflect.DeepEqual(res, other) {
		t.Fatal("symbol order changed portfolio")
	}
}

func TestEntrySlippageBeyondTightStopCannotInventFavourableFill(t *testing.T) {
	r, _ := fixture()
	r.To = r.From.Add(2 * time.Hour)
	r.Strategy.SlippagePct = 5
	r.Strategy.StopLossPct = 1
	result := runTest(t, r, candlesFor(r))
	if len(result.Trades) != 1 {
		t.Fatal(result.Trades)
	}
	near(t, result.Trades[0].EntryPrice, 105)
	near(t, result.Trades[0].ExitPrice, 95)
	if result.Trades[0].ExitTime != result.Trades[0].EntryTime {
		t.Fatal("stop was already breached at entry")
	}
}

func TestOneYearFiveSymbolFiveMinutePortfolio(t *testing.T) {
	r, _ := fixture()
	r.Timeframe = "5m"
	r.From = r.To.AddDate(-1, 0, 0)
	r.Symbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}
	r.Strategy.Entry.Right.Value = 1e9
	progress := 0
	result, err := Run(context.Background(), r, candlesFor(r), func(p int) { progress = p })
	if err != nil {
		t.Fatal(err)
	}
	if progress != 100 || len(result.Equity) > 1800 || len(result.Trades) != 0 {
		t.Fatalf("progress=%d points=%d trades=%d", progress, len(result.Equity), len(result.Trades))
	}
	near(t, result.Metrics.FinalCapital, r.Strategy.InitialCapital)
}
func TestShortAndIndicatorExit(t *testing.T) {
	r, _ := fixture()
	r.Strategy.Direction = "short"
	r.Strategy.TakeProfitPct = 0
	r.Strategy.Exit = &bt.Condition{Kind: "compare", Operator: "lt", Left: &bt.Operand{Kind: "close"}, Right: &bt.Operand{Kind: "constant", Value: 95}}
	data := candlesFor(r)
	c := data["BTCUSDT"]
	i := r.WarmupBars() + 2
	c[i].Open = 94
	c[i].Low = 90
	c[i].High = 94
	c[i].Close = 90
	c[i+1].Open = 91
	c[i+1].Low = 91
	c[i+1].High = 92
	c[i+1].Close = 92
	res := runTest(t, r, data)
	if res.Trades[0].Reason != "condition" {
		t.Fatal(res.Trades)
	}
	near(t, res.Trades[0].ExitPrice, 91)
	near(t, res.Metrics.FinalCapital, 10180)
}
func TestMissingHistoryInvalidOHLCAndCancellation(t *testing.T) {
	r, data := fixture()
	data["BTCUSDT"] = data["BTCUSDT"][1:]
	if _, err := Run(context.Background(), r, data, nil); err == nil || !strings.Contains(err.Error(), "Неполная история") {
		t.Fatal(err)
	}
	data = candlesFor(r)
	data["BTCUSDT"][0].Close = math.NaN()
	if _, err := Run(context.Background(), r, data, nil); err == nil {
		t.Fatal("NaN accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, r, candlesFor(r), nil); err != context.Canceled {
		t.Fatal(err)
	}
}
func TestIndicatorSeedsCrossesAndUnknownOR(t *testing.T) {
	near(t, ema([]float64{1, 2, 3, 4}, 3)[3], 3)
	near(t, sma([]float64{1, 2, 3, 4}, 3)[3], 3)
	near(t, rsi([]float64{1, 2, 3, 4}, 2)[3], 100)
	near(t, rsi([]float64{4, 3, 2, 1}, 2)[3], 0)
	near(t, rsi([]float64{4, 4, 4, 4}, 2)[3], 50)
	s := &seriesCache{close: []float64{9, 10, 11, 12}, values: map[bt.Operand][]float64{}}
	c := bt.Condition{Kind: "compare", Operator: "crosses_above", Left: &bt.Operand{Kind: "close"}, Right: &bt.Operand{Kind: "constant", Value: 10}}
	if matched, ok := s.evaluate(c, 2); !matched || !ok {
		t.Fatal("cross missing")
	}
	if matched, _ := s.evaluate(c, 3); matched {
		t.Fatal("persistent state treated as cross")
	}
	group := bt.Condition{Kind: "or", Children: []bt.Condition{c, {Kind: "compare", Operator: "gt", Left: &bt.Operand{Kind: "ema", Period: 400}, Right: &bt.Operand{Kind: "constant"}}}}
	if matched, ok := s.evaluate(group, 2); matched || ok {
		t.Fatal("unavailable indicator accepted in OR")
	}
}
func TestMetricsZeroTradesLossesAndDailySharpe(t *testing.T) {
	m := metrics(100, []bt.EquityPoint{{Time: 0, Value: 100}, {Time: 86400000, Value: 110}, {Time: 172800000, Value: 88}}, []bt.Trade{{PnL: 10}, {PnL: -20}})
	near(t, m.MaxDrawdownPct, 20)
	near(t, m.ProfitPct, -12)
	near(t, m.WinRate, 50)
	near(t, *m.ProfitFactor, .5)
	want := (-.05) / math.Sqrt(.045) * math.Sqrt(365)
	near(t, *m.Sharpe, want)
	r, data := fixture()
	r.Strategy.Entry.Right.Value = 1000
	res := runTest(t, r, data)
	if res.Metrics.TradeCount != 0 || res.Metrics.ProfitFactor != nil || res.Metrics.Sharpe != nil {
		t.Fatal(res.Metrics)
	}
	near(t, res.Metrics.FinalCapital, 10000)
}
