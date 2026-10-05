package auth

import "strings"

type PlanLevel int

const (
	PlanFree PlanLevel = iota
	PlanStandard
	PlanPro
)

func NormalizePlan(plan string) string {
	return strings.ToLower(strings.TrimSpace(plan))
}

func PlanRank(plan string) PlanLevel {
	switch NormalizePlan(plan) {
	case "pro":
		return PlanPro
	case "standard", "standart":
		return PlanStandard
	default:
		return PlanFree
	}
}

func HasAtLeast(plan string, min PlanLevel) bool {
	return PlanRank(plan) >= min
}

func IsPro(plan string) bool {
	return PlanRank(plan) >= PlanPro
}

func IsStandard(plan string) bool {
	return PlanRank(plan) >= PlanStandard
}
