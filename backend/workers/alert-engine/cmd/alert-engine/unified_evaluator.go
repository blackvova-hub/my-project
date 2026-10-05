package main

import (
	"math"
	"strings"
)

// evaluateSparseCombinedLiquidationCondition preserves the production evaluator semantics but
// reads the dependency-aware history. Parity tests intentionally compare every
// field of EvalResult, including availability and reason.
func evaluateSparseCombinedLiquidationCondition(venueHistories map[string]*sparseHistory, priceHistory *sparseHistory, minute int64, windowMinutes int, condition Condition) EvalResult {
	if windowMinutes <= 0 {
		return EvalResult{Available: false, Matched: false, Reason: "invalid_window"}
	}
	venueSums := make(map[string]float64, len(combinedLiquidationVenues))
	hasData := false
	for _, venue := range combinedLiquidationVenues {
		history := venueHistories[venue]
		if history == nil {
			return EvalResult{Available: false, Matched: false, Reason: "missing_venue"}
		}
		if _, ok := history.Get(minute, "liquidations"); !ok {
			return EvalResult{Available: false, Matched: false, Reason: "no_current_value"}
		}
		for offset := 0; offset < windowMinutes; offset++ {
			value, ok := history.Get(minute-int64(offset), "liquidations")
			if !ok {
				continue
			}
			venueSums[venue] += value
			hasData = true
		}
	}
	if !hasData {
		return EvalResult{Available: false, Matched: false, Reason: "no_data"}
	}
	var sum float64
	for _, venue := range combinedLiquidationVenues {
		sum += venueSums[venue]
	}
	priceNow, _ := priceHistory.Get(minute, "close")
	evaluation := ConditionEval{
		Now: priceNow, Then: priceNow, Cmp: ">=", Want: condition.ThresholdAmount, Got: sum, LiqSum: sum,
		BybitLiqSum: venueSums["bybit"], BinanceLiqSum: venueSums["binance"], OKXLiqSum: venueSums["okx"],
		BitgetLiqSum: venueSums["bitget"], GateIOLiqSum: venueSums["gateio"],
	}
	matched := sum >= condition.ThresholdAmount
	reason := "threshold_not_met"
	if matched {
		reason = "matched"
	}
	return EvalResult{Available: true, Matched: matched, Eval: evaluation, Reason: reason}
}

func evaluateSparseCondition(history *sparseHistory, minute int64, windowMinutes int, condition Condition) EvalResult {
	if history == nil {
		return EvalResult{Available: false, Matched: false, Reason: "no_history"}
	}
	if windowMinutes <= 0 {
		return EvalResult{Available: false, Matched: false, Reason: "invalid_window"}
	}
	if strings.HasPrefix(condition.Indicator, "liquidations") {
		if _, ok := history.Get(minute, condition.Indicator); !ok {
			return EvalResult{Available: false, Matched: false, Reason: "no_current_value"}
		}
		var sum float64
		count := 0
		for offset := 0; offset < windowMinutes; offset++ {
			value, ok := history.Get(minute-int64(offset), condition.Indicator)
			if !ok {
				continue
			}
			sum += value
			count++
		}
		if count == 0 {
			return EvalResult{Available: false, Matched: false, Reason: "no_data"}
		}
		priceNow, _ := history.Get(minute, "close")
		evaluation := ConditionEval{Now: priceNow, Then: priceNow, Cmp: ">=", Want: condition.ThresholdAmount, Got: sum, LiqSum: sum}
		matched := sum >= condition.ThresholdAmount
		reason := "threshold_not_met"
		if matched {
			reason = "matched"
		}
		return EvalResult{Available: true, Matched: matched, Eval: evaluation, Reason: reason}
	}

	var now float64
	var okNow bool
	var bestThen float64
	var bestPct float64
	var bestAnyThen float64
	var bestAnyPct float64
	found := false
	foundAny := false
	hadThenData := false
	updateBestAny := func(percent, then float64) {
		if !foundAny {
			foundAny, bestAnyPct, bestAnyThen = true, percent, then
			return
		}
		switch condition.Direction {
		case DirUp:
			if percent > bestAnyPct {
				bestAnyPct, bestAnyThen = percent, then
			}
		case DirDown:
			if percent < bestAnyPct {
				bestAnyPct, bestAnyThen = percent, then
			}
		default:
			if math.Abs(percent) > math.Abs(bestAnyPct) {
				bestAnyPct, bestAnyThen = percent, then
			}
		}
	}
	updateMatched := func(percent, then float64) {
		if !shouldTrigger(condition.Direction, percent, condition.ThresholdPct) {
			return
		}
		if !found {
			found, bestPct, bestThen = true, percent, then
			return
		}
		switch condition.Direction {
		case DirUp:
			if percent > bestPct {
				bestPct, bestThen = percent, then
			}
		case DirDown:
			if percent < bestPct {
				bestPct, bestThen = percent, then
			}
		default:
			if math.Abs(percent) > math.Abs(bestPct) {
				bestPct, bestThen = percent, then
			}
		}
	}

	switch {
	case condition.Indicator == "longShortRatio":
		longNow, longOK := history.Get(minute, "longRatio")
		shortNow, shortOK := history.Get(minute, "shortRatio")
		if longOK && shortOK && shortNow != 0 {
			now, okNow = longNow/shortNow, true
			for offset := 1; offset <= windowMinutes; offset++ {
				longThen, longThenOK := history.Get(minute-int64(offset), "longRatio")
				shortThen, shortThenOK := history.Get(minute-int64(offset), "shortRatio")
				if !longThenOK || !shortThenOK || shortThen == 0 {
					continue
				}
				then := longThen / shortThen
				if then == 0 {
					continue
				}
				hadThenData = true
				percent := computePct(now, then)
				updateBestAny(percent, then)
				updateMatched(percent, then)
			}
		}
	case strings.HasPrefix(condition.Indicator, "price"):
		startPoint, endPoint := parsePricePoints(condition.Indicator)
		now, okNow = history.Get(minute, endPoint)
		if okNow {
			for offset := 1; offset <= windowMinutes; offset++ {
				then, ok := history.Get(minute-int64(offset), startPoint)
				if !ok || then == 0 {
					continue
				}
				hadThenData = true
				percent := computePct(now, then)
				updateBestAny(percent, then)
				updateMatched(percent, then)
			}
		}
	default:
		now, okNow = history.Get(minute, condition.Indicator)
		if okNow {
			for offset := 1; offset <= windowMinutes; offset++ {
				then, ok := history.Get(minute-int64(offset), condition.Indicator)
				if !ok || then == 0 {
					continue
				}
				hadThenData = true
				percent := computePct(now, then)
				updateBestAny(percent, then)
				updateMatched(percent, then)
			}
		}
	}

	if !okNow {
		return EvalResult{Available: false, Matched: false, Reason: "no_current_value"}
	}
	if !hadThenData {
		return EvalResult{Available: false, Matched: false, Reason: "no_history"}
	}
	comparison := ">="
	if condition.Direction == DirDown {
		comparison = "<="
	}
	if !found {
		got, then := 0.0, 0.0
		if foundAny {
			got, then = bestAnyPct, bestAnyThen
		}
		return EvalResult{Available: true, Matched: false, Eval: ConditionEval{Now: now, Then: then, Pct: got, Cmp: comparison, Want: condition.ThresholdPct, Got: got}, Reason: "threshold_not_met"}
	}
	return EvalResult{Available: true, Matched: true, Eval: ConditionEval{Now: now, Then: bestThen, Pct: bestPct, Cmp: comparison, Want: condition.ThresholdPct, Got: bestPct}, Reason: "matched"}
}
