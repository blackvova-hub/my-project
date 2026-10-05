package main

import (
	"encoding/hex"
	"errors"
	"math"
	"strings"
)

func validateSignalOutboxPayload(event AlertEvent) error {
	if event.UserID <= 0 || event.RuleID <= 0 || strings.TrimSpace(event.Exchange) == "" ||
		strings.TrimSpace(event.MarketType) == "" || strings.TrimSpace(event.Symbol) == "" || event.TS <= 0 {
		return errors.New("invalid signal outbox payload")
	}
	if normalizeExchange(event.Exchange) == "" || normalizeMarketType(event.MarketType) == "" {
		return errors.New("invalid signal outbox payload")
	}
	return nil
}

func validateImportantEventOutboxPayload(payload map[string]any) error {
	if payload == nil || jsonPayloadString(payload, "kind") != "important_event" || !validUUIDText(jsonPayloadString(payload, "eventId")) {
		return errors.New("invalid important-event outbox payload")
	}
	switch jsonPayloadString(payload, "operation") {
	case "created", "updated", "resolved":
	default:
		return errors.New("invalid important-event outbox payload")
	}
	if !jsonPayloadPositiveInteger(payload, "occurrenceCount") {
		return errors.New("invalid important-event outbox payload")
	}
	return nil
}

func jsonPayloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func jsonPayloadPositiveInteger(payload map[string]any, key string) bool {
	value, ok := payload[key].(float64)
	return ok && value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) && math.Trunc(value) == value
}

func validUUIDText(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := strings.ReplaceAll(value, "-", "")
	decoded, err := hex.DecodeString(compact)
	return err == nil && len(decoded) == 16
}
