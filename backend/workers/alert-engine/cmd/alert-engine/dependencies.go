package main

import "strings"

// compactHistoryRetentionMinutes is the hard upper bound for in-memory
// dependency plans. It matches the compact-stream retention budget (26h).
const compactHistoryRetentionMinutes = 26 * 60

// historyPlan maps a canonical metric to the exact number of minute slots it
// needs. Keeping capacity per series is what prevents one long-window rule
// from expanding every metric for every instrument.
type historyPlan map[string]int

func (p historyPlan) require(metric string, capacity int) {
	metric = strings.TrimSpace(metric)
	if !isHistoryMetric(metric) || capacity <= 0 {
		return
	}
	if capacity > compactHistoryRetentionMinutes {
		capacity = compactHistoryRetentionMinutes
	}
	if capacity > p[metric] {
		p[metric] = capacity
	}
}

func isHistoryMetric(metric string) bool {
	if metric == "openInterestTimestamp" {
		return true
	}
	for _, candidate := range compactMetricLayout {
		if metric == candidate {
			return true
		}
	}
	return false
}

func resolveHistoryPlan(rules []Rule, exchange, marketType, symbol string, includeImportant bool) historyPlan {
	exchange = normalizeExchange(exchange)
	marketType = normalizeMarketType(marketType)
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	plan := make(historyPlan)
	if exchange == "" || marketType == "" || symbol == "" {
		return plan
	}

	for _, rule := range rules {
		if normalizeMarketType(rule.MarketType) != marketType || !ruleMatchesSymbol(rule, symbol) {
			continue
		}
		ruleExchange := normalizeExchange(rule.Exchange)
		if ruleExchange == "" {
			continue
		}
		conditions := ruleConditions(rule)
		usesCombined := ruleUsesCombinedLiquidations(rule)
		if usesCombined && isCombinedLiquidationVenue(exchange) {
			plan.require("liquidations", rule.WindowMinutes)
		}
		if ruleExchange != exchange {
			continue
		}
		for _, condition := range conditions {
			addConditionHistory(plan, condition.Indicator, rule.WindowMinutes)
		}
		if usesCombined {
			// Rule evaluation takes the current close from the rule venue. It
			// may be unavailable on liquidation-only venues, which must remain
			// an invalid value rather than an implicit zero.
			plan.require("close", 1)
		}
	}

	if includeImportant && (exchange == "bybit" || exchange == "binance") {
		addImportantHistory(plan)
	}
	return plan
}

func ruleMatchesSymbol(rule Rule, symbol string) bool {
	raw := strings.TrimSpace(rule.Symbol)
	if raw == "" || raw == wildcardSymbol || strings.EqualFold(raw, "ALL") {
		return true
	}
	for _, candidate := range splitRuleSymbols(raw) {
		if candidate == wildcardSymbol || candidate == symbol {
			return true
		}
	}
	return false
}

func isCombinedLiquidationVenue(exchange string) bool {
	for _, venue := range combinedLiquidationVenues {
		if exchange == venue {
			return true
		}
	}
	return false
}

func addConditionHistory(plan historyPlan, indicator string, windowMinutes int) {
	indicator = strings.TrimSpace(indicator)
	if windowMinutes <= 0 {
		return
	}
	switch {
	case indicator == "liquidationsCombined":
		plan.require("liquidations", windowMinutes)
	case strings.HasPrefix(indicator, "liquidations"):
		plan.require(indicator, windowMinutes)
		plan.require("close", 1)
	case indicator == "longShortRatio":
		plan.require("longRatio", windowMinutes+1)
		plan.require("shortRatio", windowMinutes+1)
	case strings.HasPrefix(indicator, "price"):
		startPoint, endPoint := parsePricePoints(indicator)
		plan.require(startPoint, windowMinutes+1)
		plan.require(endPoint, 1)
	default:
		plan.require(indicator, windowMinutes+1)
	}
}

func addImportantHistory(plan historyPlan) {
	const longestPriceWindow = 15
	// historicalWindowSamples reaches back importantHistoryMinutes and each
	// sample reads its own exact window. These capacities describe the actual
	// oldest accessed minute, including the current slot.
	plan.require("close", importantHistoryMinutes+longestPriceWindow+1)
	plan.require("quoteVolumeUsd", importantHistoryMinutes+longestPriceWindow)
	plan.require("openInterest", 6)
	plan.require("openInterestTimestamp", 6)
	plan.require("markPrice", 1)
	for _, metric := range []string{
		"largestTradeUsd", "medianTradeUsd", "tradeClusterUsd", "tradeClusterSide",
		"tradeClusterCount", "tradePriceImpactPct",
	} {
		plan.require(metric, 1)
	}
}
