package main

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

func rulesDependencyFingerprint(rules *RulesCache, includeImportant bool) string {
	var snapshot []Rule
	if rules != nil {
		snapshot, _ = rules.Snapshot()
	}
	return rulesDependencyFingerprintForSnapshot(snapshot, includeImportant)
}

func rulesDependencyFingerprintForSnapshot(snapshot []Rule, includeImportant bool) string {
	entries := make([]string, 0, len(snapshot)+1)
	entries = append(entries, "important="+strconv.FormatBool(includeImportant))
	for _, rule := range snapshot {
		indicators := make([]string, 0, len(ruleConditions(rule)))
		for _, condition := range ruleConditions(rule) {
			indicators = append(indicators, strings.TrimSpace(condition.Indicator))
		}
		sort.Strings(indicators)
		entries = append(entries, strings.Join([]string{
			normalizeExchange(rule.Exchange), normalizeMarketType(rule.MarketType), strings.ToUpper(strings.TrimSpace(rule.Symbol)),
			strconv.Itoa(rule.WindowMinutes), strings.Join(indicators, ","),
		}, "\x00"))
	}
	sort.Strings(entries[1:])
	digest := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return hex.EncodeToString(digest[:])
}
