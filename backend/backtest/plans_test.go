package backtest

import (
	"fmt"
	"testing"
	"time"
)

func TestPlanSymbolBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		plan string
		max  int
	}{
		{"free", 1}, {"", 1}, {"unknown", 1}, {"standard", 5}, {"standart", 5}, {" PRO ", 10},
	} {
		t.Run(tc.plan, func(t *testing.T) {
			r := validRequest()
			r.Symbols = nil
			for i := 0; i < tc.max; i++ {
				r.Symbols = append(r.Symbols, fmt.Sprintf("COIN%dUSDT", i))
			}
			if err := r.ValidateForPlan(now, tc.plan); err != nil {
				t.Fatal(err)
			}
			r.Symbols = append(r.Symbols, "EXTRAUSDT")
			if err := r.ValidateForPlan(now, tc.plan); err == nil {
				t.Fatal("excess symbols accepted")
			}
		})
	}
}

func TestPlanConditionBudgetsIncludeExitAndLegacyRules(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	r := validRequest()
	leaf := r.Strategy.Entry.Children[0]
	r.Strategy.Entry.Children = []Condition{leaf, leaf}
	r.Strategy.Exit = &Condition{Kind: "or", Children: []Condition{leaf}}
	if err := r.ValidateForPlan(now, "free"); err != nil {
		t.Fatal(err)
	}
	r.Strategy.Exit.Children = append(r.Strategy.Exit.Children, leaf)
	if err := r.ValidateForPlan(now, "free"); err == nil {
		t.Fatal("exit bypassed shared rule budget")
	}
	r.Strategy.Entry.Children = []Condition{leaf, leaf, leaf, leaf, leaf, leaf}
	r.Strategy.Exit.Children = []Condition{leaf, leaf, leaf, leaf, leaf, leaf}
	if err := r.ValidateForPlan(now, "standard"); err != nil {
		t.Fatal(err)
	}
	r.Strategy.Exit.Children = append(r.Strategy.Exit.Children, leaf)
	if err := r.ValidateForPlan(now, "standard"); err == nil {
		t.Fatal("standard rule limit bypassed")
	}
	if err := r.ValidateForPlan(now, "pro"); err != nil {
		t.Fatal(err)
	}
}

func TestPlanGroupDepthAndCount(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	r := validRequest()
	group := r.Strategy.Entry
	r.Strategy.Entry = Condition{Kind: "and", Children: []Condition{group}}
	if err := r.ValidateForPlan(now, "free"); err == nil {
		t.Fatal("nested free group accepted")
	}
	if err := r.ValidateForPlan(now, "standard"); err != nil {
		t.Fatal(err)
	}
	r.Strategy.Entry = Condition{Kind: "or", Children: []Condition{r.Strategy.Entry}}
	if err := r.ValidateForPlan(now, "standard"); err == nil {
		t.Fatal("deep standard group accepted")
	}
	if err := r.ValidateForPlan(now, "pro"); err != nil {
		t.Fatal(err)
	}
	r.Strategy.Entry = Condition{Kind: "and", Children: []Condition{group, group, group, group, group}}
	if err := r.ValidateForPlan(now, "standard"); err != nil {
		t.Fatal(err)
	}
	r.Strategy.Exit = &group
	if err := r.ValidateForPlan(now, "standard"); err == nil {
		t.Fatal("exit bypassed group budget")
	}
	if err := r.ValidateForPlan(now, "pro"); err != nil {
		t.Fatal(err)
	}
}

func TestProRetainsStructuralProtection(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	r := validRequest()
	leaf := r.Strategy.Entry.Children[0]
	group := Condition{Kind: "and", Children: []Condition{leaf, leaf, leaf, leaf, leaf, leaf, leaf}}
	r.Strategy.Entry = Condition{Kind: "or", Children: []Condition{group, group, group, {Kind: "and", Children: group.Children[:6]}}}
	if err := r.ValidateForPlan(now, "pro"); err != nil {
		t.Fatal(err)
	} // 32 total nodes
	r.Strategy.Entry.Children[3].Children = group.Children
	if err := r.ValidateForPlan(now, "pro"); err == nil {
		t.Fatal("33-node strategy accepted")
	}
}
