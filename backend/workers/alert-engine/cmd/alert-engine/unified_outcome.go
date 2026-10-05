package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type unifiedConditionOutcome struct {
	Indicator        string
	Negate           bool
	Available        bool
	Matched          bool
	EffectiveMatched bool
	Reason           string
	Evaluation       ConditionEval
}

type unifiedRuleOutcome struct {
	RuleID         int64
	UserID         int64
	MarketType     string
	Symbol         string
	Minute         int64
	Available      bool
	Matched        bool
	Reason         string
	Conditions     []unifiedConditionOutcome
	Alert          AlertEvent
	CooldownKey    string
	SignalIdentity string
}

func (i *historyDependencyIndex) Rules(marketType, symbol string) []Rule {
	if i == nil {
		return nil
	}
	marketType = normalizeMarketType(marketType)
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	exact := i.exact[marketType+":"+symbol]
	wildcard := i.wildcard[marketType]
	out := make([]Rule, 0, len(exact)+len(wildcard))
	for _, rule := range exact {
		out = append(out, cloneRule(rule))
	}
	for _, rule := range wildcard {
		copyRule := cloneRule(rule)
		copyRule.Symbol = symbol
		out = append(out, copyRule)
	}
	return out
}

// EvaluateReady returns ready=false until every coverage state needed by the
// applicable rules has arrived. It performs no external reads or writes.
func (h *sparseEngineState) EvaluateReady(marketType, symbol string, minuteTimestamp int64, cooldownPrefix string, now time.Time) ([]unifiedRuleOutcome, bool) {
	if h == nil {
		return nil, false
	}
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	h.syncRules()
	marketType = normalizeMarketType(marketType)
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if marketType == "" || symbol == "" || minuteTimestamp <= 0 {
		return nil, false
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	rules := dedupeCombinedLiquidationRules(h.dependencies.Rules(marketType, symbol))
	sort.SliceStable(rules, func(left, right int) bool {
		if rules[left].ID == rules[right].ID {
			return normalizeExchange(rules[left].Exchange) < normalizeExchange(rules[right].Exchange)
		}
		return rules[left].ID < rules[right].ID
	})
	outcomes := make([]unifiedRuleOutcome, 0, len(rules))
	for _, rule := range rules {
		ruleExchange := normalizeExchange(rule.Exchange)
		if ruleExchange == "" {
			continue
		}
		usesCombined := ruleUsesCombinedLiquidations(rule)
		if !usesCombined && ruleExchange != "bybit" && ruleExchange != "binance" {
			// Rule evaluation filters non-combined rules from liquidation-only feeds.
			continue
		}
		coverageFailure, pending := h.ruleCoverageFailure(rule, marketType, symbol, minuteTimestamp, now)
		if pending {
			return nil, false
		}
		if coverageFailure != "" {
			outcomes = append(outcomes, h.coverageFailureOutcome(rule, marketType, symbol, minuteTimestamp, cooldownPrefix, coverageFailure))
			continue
		}
		outcomes = append(outcomes, h.evaluateRuleLocked(rule, marketType, symbol, minuteTimestamp, cooldownPrefix))
	}
	return outcomes, true
}

func (h *sparseEngineState) ruleCoverageFailure(rule Rule, marketType, symbol string, minuteTimestamp int64, now time.Time) (string, bool) {
	if ruleUsesCombinedLiquidations(rule) {
		for _, venue := range combinedLiquidationVenues {
			kind := "l"
			if venue == "bybit" || venue == "binance" {
				kind = "c"
			}
			status := h.coverage.Status(kind, venue, marketType, minuteTimestamp, symbol, now)
			if status == coveragePending {
				return "", true
			}
			if status != coverageValid {
				return fmt.Sprintf("coverage_%s:%s", status, venue), false
			}
		}
		return "", false
	}
	venue := normalizeExchange(rule.Exchange)
	status := h.coverage.Status("c", venue, marketType, minuteTimestamp, symbol, now)
	if status == coveragePending {
		return "", true
	}
	if status != coverageValid {
		return fmt.Sprintf("coverage_%s:%s", status, venue), false
	}
	return "", false
}

func (h *sparseEngineState) coverageFailureOutcome(rule Rule, marketType, symbol string, minuteTimestamp int64, cooldownPrefix, reason string) unifiedRuleOutcome {
	ruleExchange := normalizeExchange(rule.Exchange)
	cooldownExchange := ruleExchange
	if ruleUsesCombinedLiquidations(rule) {
		cooldownExchange = strings.Join(combinedLiquidationVenues, "+")
	}
	return unifiedRuleOutcome{
		RuleID: rule.ID, UserID: rule.UserID, MarketType: marketType, Symbol: symbol, Minute: minuteTimestamp,
		Available: false, Matched: false, Reason: reason,
		CooldownKey:    coinCooldownKey(cooldownPrefix, rule.UserID, cooldownExchange, marketType, symbol),
		SignalIdentity: unifiedSignalIdentity(rule.ID, ruleExchange, marketType, symbol, minuteTimestamp),
	}
}

func (h *sparseEngineState) evaluateRuleLocked(rule Rule, marketType, symbol string, minuteTimestamp int64, cooldownPrefix string) unifiedRuleOutcome {
	ruleExchange := normalizeExchange(rule.Exchange)
	minute := minuteKey(minuteTimestamp)
	ruleHistory := h.instrumentHistoryLocked(ruleExchange, marketType, symbol)
	conditions := ruleConditions(rule)
	usesCombined := ruleUsesCombinedLiquidations(rule)
	conditionOutcomes := make([]unifiedConditionOutcome, 0, len(conditions))
	available := true
	matched := true
	reason := "matched"
	var percent, priceNow, priceThen, liquidationSum float64
	var bybitSum, binanceSum, okxSum, bitgetSum, gateioSum float64

	venueHistories := make(map[string]*sparseHistory, len(combinedLiquidationVenues))
	if usesCombined {
		for _, venue := range combinedLiquidationVenues {
			venueHistories[venue] = h.instrumentHistoryLocked(venue, marketType, symbol)
		}
	}
	for index, condition := range conditions {
		var result EvalResult
		if condition.Indicator == "liquidationsCombined" {
			result = evaluateSparseCombinedLiquidationCondition(venueHistories, ruleHistory, minute, rule.WindowMinutes, condition)
		} else {
			result = evaluateSparseCondition(ruleHistory, minute, rule.WindowMinutes, condition)
		}
		effective := result.Matched
		if condition.Negate && result.Available {
			effective = !effective
		}
		conditionOutcomes = append(conditionOutcomes, unifiedConditionOutcome{Indicator: condition.Indicator, Negate: condition.Negate, Available: result.Available, Matched: result.Matched, EffectiveMatched: effective, Reason: result.Reason, Evaluation: result.Eval})
		if !result.Available {
			available = false
		}
		if !effective && matched {
			matched = false
			reason = result.Reason
		}
		if index == 0 {
			percent, priceNow, priceThen = result.Eval.Pct, result.Eval.Now, result.Eval.Then
		}
		if condition.Indicator == "liquidationsCombined" {
			liquidationSum = result.Eval.LiqSum
			bybitSum, binanceSum, okxSum = result.Eval.BybitLiqSum, result.Eval.BinanceLiqSum, result.Eval.OKXLiqSum
			bitgetSum, gateioSum = result.Eval.BitgetLiqSum, result.Eval.GateIOLiqSum
		} else if strings.HasPrefix(condition.Indicator, "liquidations") && !usesCombined {
			liquidationSum = result.Eval.LiqSum
		}
		if !effective {
			break
		}
	}
	if !available && matched {
		matched = false
		reason = "unavailable"
	}
	cooldownExchange := ruleExchange
	if usesCombined {
		cooldownExchange = strings.Join(combinedLiquidationVenues, "+")
	}
	alert := AlertEvent{
		UserID: rule.UserID, RuleID: rule.ID, Exchange: ruleExchange, MarketType: marketType,
		Indicator: conditions[0].Indicator, Symbol: symbol, TS: minuteTimestamp, WindowMinutes: rule.WindowMinutes,
		ThresholdPct: conditions[0].ThresholdPct, Direction: string(conditions[0].Direction),
		ChangePercent: math.Round(percent*10000) / 10000, PriceNow: priceNow, PriceThen: priceThen,
		Liquidations: liquidationSum, BybitLiquidations: bybitSum, BinanceLiquidations: binanceSum,
		OKXLiquidations: okxSum, BitgetLiquidations: bitgetSum, GateIOLiquidations: gateioSum,
		CombinedExchanges: usesCombined, ScannerSlot: rule.ScannerSlot,
	}
	return unifiedRuleOutcome{
		RuleID: rule.ID, UserID: rule.UserID, MarketType: marketType, Symbol: symbol, Minute: minuteTimestamp,
		Available: available, Matched: matched, Reason: reason, Conditions: conditionOutcomes, Alert: alert,
		CooldownKey:    coinCooldownKey(cooldownPrefix, rule.UserID, cooldownExchange, marketType, symbol),
		SignalIdentity: unifiedSignalIdentity(rule.ID, ruleExchange, marketType, symbol, minuteTimestamp),
	}
}

func (h *sparseEngineState) instrumentHistoryLocked(exchange, marketType, symbol string) *sparseHistory {
	record := h.instruments[instrumentCacheKey(exchange, marketType, symbol)]
	if record == nil {
		return nil
	}
	return record.history
}

func unifiedSignalIdentity(ruleID int64, exchange, marketType, symbol string, minuteTimestamp int64) string {
	return fmt.Sprintf("%d:%s:%s:%s:1m:%d", ruleID, normalizeExchange(exchange), normalizeMarketType(marketType), strings.ToUpper(strings.TrimSpace(symbol)), minuteTimestamp)
}
