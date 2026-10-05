package backtest

import (
	"fmt"
	"strings"
	"time"
)

// PlanLimits restricts user-created jobs. Validate still enforces the engine's
// hard limits (32 nodes, 8 children per group, 3 edges from root to a leaf).
type PlanLimits struct {
	Name             string
	MaxSymbols       int
	MaxConditions    int
	MaxGroups        int
	MaxGroupDepth    int
	MaxGroupChildren int
}

func LimitsForPlan(plan string) PlanLimits {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "pro":
		return PlanLimits{"Pro", 10, 32, 32, 2, 8}
	case "standard", "standart":
		return PlanLimits{"Standard", 5, 12, 6, 1, 8}
	default:
		return PlanLimits{"Free", 1, 3, 2, 0, 3}
	}
}

// ValidateForPlan must receive the plan from the authenticated user, never from
// the request body. Both entry and exit share the same budget.
func (r Request) ValidateForPlan(now time.Time, plan string) error {
	if err := r.Validate(now); err != nil {
		return err
	}
	limits := LimitsForPlan(plan)
	if len(r.Symbols) > limits.MaxSymbols {
		return fmt.Errorf("Тариф %s: максимум торговых пар — %d", limits.Name, limits.MaxSymbols)
	}
	conditions, groups := 0, 0
	var visit func(Condition, int) error
	visit = func(c Condition, depth int) error {
		if c.Kind == "and" || c.Kind == "or" {
			groups++
			if depth > limits.MaxGroupDepth {
				return fmt.Errorf("Тариф %s: допустимо уровней групп — %d", limits.Name, limits.MaxGroupDepth+1)
			}
			if groups > limits.MaxGroups {
				return fmt.Errorf("Тариф %s: максимум групп входа и выхода вместе — %d", limits.Name, limits.MaxGroups)
			}
			if len(c.Children) > limits.MaxGroupChildren {
				return fmt.Errorf("Тариф %s: максимум элементов в группе — %d", limits.Name, limits.MaxGroupChildren)
			}
			for _, child := range c.Children {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		} else {
			conditions++
			if conditions > limits.MaxConditions {
				return fmt.Errorf("Тариф %s: максимум условий входа и выхода вместе — %d", limits.Name, limits.MaxConditions)
			}
		}
		return nil
	}
	if err := visit(r.Strategy.Entry, 0); err != nil {
		return err
	}
	if r.Strategy.Exit != nil {
		return visit(*r.Strategy.Exit, 0)
	}
	return nil
}
