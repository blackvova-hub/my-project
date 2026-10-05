package engine

import (
	bt "shortlong/backtest"
	"testing"
)

func TestAllComparisonsAtEitherSideAndEquality(t *testing.T) {
	for _, tc := range []struct {
		op        string
		threshold float64
		want      bool
	}{{"lt", 101, true}, {"lt", 100, false}, {"lt", 99, false}, {"lte", 101, true}, {"lte", 100, true}, {"lte", 99, false}, {"gt", 101, false}, {"gt", 100, false}, {"gt", 99, true}, {"gte", 101, false}, {"gte", 100, true}, {"gte", 99, true}} {
		t.Run(tc.op, func(t *testing.T) {
			r, _ := fixture()
			r.Strategy.Entry.Operator = tc.op
			r.Strategy.Entry.Right.Value = tc.threshold
			res := runTest(t, r, candlesFor(r))
			if (len(res.Trades) > 0) != tc.want {
				t.Fatalf("100 %s %v: trades %v", tc.op, tc.threshold, res.Trades)
			}
			if (res.Signals[0].MatchedBars > 0) != tc.want {
				t.Fatal(res.Signals)
			}
		})
	}
}
func TestRSILessAndLessOrEqualAtZero(t *testing.T) {
	c := make([]bt.Candle, 40)
	for i := range c {
		c[i].Close = 100 - float64(i)
	}
	s := newCache(c)
	node := bt.Condition{Kind: "compare", Operator: "lte", Left: &bt.Operand{Kind: "rsi", Period: 14}, Right: &bt.Operand{Kind: "constant", Value: 0}}
	if got, ok := s.evaluate(node, 39); !ok || !got {
		t.Fatal("RSI=0 must satisfy <=0")
	}
	node.Operator = "lt"
	if got, _ := s.evaluate(node, 39); got {
		t.Fatal("RSI=0 must not satisfy <0")
	}
	node.Right.Value = 30
	if got, _ := s.evaluate(node, 39); !got {
		t.Fatal("RSI<30 must match")
	}
}
func TestCrossingsAndGroupLogic(t *testing.T) {
	s := newCache([]bt.Candle{{Close: 100}, {Close: 100}, {Close: 101}, {Close: 99}})
	node := bt.Condition{Kind: "compare", Operator: "crosses_above", Left: &bt.Operand{Kind: "close"}, Right: &bt.Operand{Kind: "constant", Value: 100}}
	if got, _ := s.evaluate(node, 1); got {
		t.Fatal("touch is not crossing")
	}
	if got, _ := s.evaluate(node, 2); !got {
		t.Fatal("up crossing")
	}
	node.Operator = "crosses_below"
	if got, _ := s.evaluate(node, 3); !got {
		t.Fatal("down crossing")
	}
	low := node
	low.Operator = "lt"
	high := node
	high.Operator = "gt"
	group := bt.Condition{Kind: "and", Children: []bt.Condition{low, high}}
	if got, _ := s.evaluate(group, 3); got {
		t.Fatal("AND ignored")
	}
	group.Kind = "or"
	if got, _ := s.evaluate(group, 3); !got {
		t.Fatal("OR ignored")
	}
}
