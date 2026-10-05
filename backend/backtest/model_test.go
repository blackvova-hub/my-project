package backtest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestZeroThresholdSurvivesStrategyPersistence(t *testing.T) {
	r := validRequest()
	r.Strategy.Entry.Children[0].Right.Value = 0
	raw, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(raw), `"kind":"constant","value":0`) {
		t.Fatalf("zero threshold omitted: %s (%v)", raw, err)
	}
}

func validRequest() Request {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	return Request{Exchange: "bybit", Market: "spot", Symbols: []string{"BTCUSDT"}, Timeframe: "5m", From: now.Add(-24 * time.Hour), To: now, Strategy: Strategy{Version: 1, Direction: "long", InitialCapital: 10000, PositionSizePct: 20, StopLossPct: 2, Entry: Condition{Kind: "and", Children: []Condition{{Kind: "compare", Operator: "lt", Left: &Operand{Kind: "rsi", Period: 14}, Right: &Operand{Kind: "constant", Value: 30}}}}}}
}
func TestValidationBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	cases := map[string]func(*Request){"future": func(r *Request) { r.To = now.Add(time.Hour) }, "old": func(r *Request) { r.From = now.AddDate(-2, 0, 0) }, "minute": func(r *Request) { r.Timeframe = "1m" }, "duplicates": func(r *Request) { r.Symbols = []string{"BTCUSDT", "BTCUSDT"} }, "spot short": func(r *Request) { r.Strategy.Direction = "short" }, "unknown node": func(r *Request) { r.Strategy.Entry.Kind = "script" }, "huge period": func(r *Request) { r.Strategy.Entry.Children[0].Left.Period = 401 }, "empty group": func(r *Request) { r.Strategy.Entry.Children = nil }, "no stop": func(r *Request) { r.Strategy.StopLossPct = 0 }, "bad operator": func(r *Request) { r.Strategy.Entry.Children[0].Operator = "eval" }}
	if err := validRequest().Validate(now); err != nil {
		t.Fatal(err)
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := validRequest()
			change(&r)
			if err := r.Validate(now); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestSmartMoneyContractRoundTripAndValidation(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, event := range []string{"bos", "choch", "mss", "liquidity_sweep", "fvg", "order_block"} {
		r := validRequest()
		r.Strategy.Entry = Condition{Kind: "smc", Event: event, Direction: "bullish", WithinBars: 3}
		if err := r.Validate(now); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Request
		if err = json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Strategy.Entry.Event != event || decoded.Strategy.Entry.WithinBars != 3 {
			t.Fatal("lost SMC parameters")
		}
	}
	for _, change := range []func(*Condition){func(c *Condition) { c.WithinBars = 0 }, func(c *Condition) { c.WithinBars = 101 }, func(c *Condition) { c.Event = "unknown" }, func(c *Condition) { c.Direction = "long" }, func(c *Condition) { c.Left = &Operand{Kind: "close"} }} {
		r := validRequest()
		r.Strategy.Entry = Condition{Kind: "smc", Event: "bos", Direction: "bullish", WithinBars: 1}
		change(&r.Strategy.Entry)
		if r.Validate(now) == nil {
			t.Fatal("invalid SMC accepted")
		}
	}
}
