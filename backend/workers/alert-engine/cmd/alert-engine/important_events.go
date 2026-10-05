package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	importantHistoryMinutes = 1440
	baselineMinSamples      = 12
	importantMaxSampleAge   = 4 * time.Minute
)

type importantEvent struct {
	EventType       string
	Family          string
	Confidence      int
	Title           string
	Details         string
	Exchange        string
	MarketType      string
	Symbol          string
	Direction       string
	AmountUSD       *float64
	ChangePercent   *float64
	BaselineValue   *float64
	BaselineRatio   *float64
	Percentile      *float64
	WindowMinutes   int
	EventAt         time.Time
	IdentityKey     string
	DedupKey        string
	Metadata        map[string]any
	SourceKind      string
	SourceRef       string
	CatalystEventID string
	EntityID        string
}

type importantEvaluation struct {
	Events            []importantEvent
	EvaluatedFamilies map[string]bool
}

type baselineStats struct {
	Median     float64
	P90        float64
	MAD        float64
	Percentile float64
	Count      int
}

type metricHistory interface {
	get(minute int64, metric string) (float64, bool)
}

func (e *Engine) persistImportantEvents(ctx context.Context, db *pgxpool.Pool, ce CandleEvent, evaluation importantEvaluation) error {
	if !e.cfg.ImportantEventsEnabled || db == nil {
		return nil
	}
	if !importantSampleFresh(ce.TS, time.Now().UTC()) {
		return nil
	}
	changed, err := syncQualityImportantEvents(ctx, db, evaluation)
	if err != nil {
		return err
	}
	for _, event := range changed {
		log.Printf("important event lifecycle=%s type=%s instrument=%s:%s:%s", event.Metadata["lifecycleStatus"], event.EventType, event.Exchange, event.MarketType, event.Symbol)
	}
	notifyOutbox(ctx, e.rdb)
	return nil
}
func enqueueImportantEventOutbox(ctx context.Context, tx pgx.Tx, eventID, operation string, version int, event importantEvent) error {
	payload := importantEventBroadcastPayload(eventID, operation, version, event)
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO important_event_outbox (event_id, operation, version, payload)
		VALUES ($1::uuid, $2, $3, $4::jsonb)
		ON CONFLICT (event_id, operation, version) DO NOTHING
	`, eventID, operation, version, string(raw))
	return err
}

func importantEventBroadcastPayload(eventID, operation string, version int, event importantEvent) map[string]any {
	status := "confirmed"
	if event.Metadata != nil {
		if value, ok := event.Metadata["lifecycleStatus"].(string); ok && value != "" {
			status = value
		}
	}
	return map[string]any{
		"kind": "important_event", "audience": "all", "userId": int64(0),
		"operation": operation, "eventId": eventID, "eventType": event.EventType,
		"family": event.Family, "confidence": event.Confidence,
		"status": status, "sourceKind": event.SourceKind, "sourceRef": event.SourceRef, "catalystEventId": event.CatalystEventID,
		"entityId": event.EntityID, "exchange": event.Exchange, "marketType": event.MarketType,
		"symbol": event.Symbol, "direction": event.Direction, "windowMinutes": event.WindowMinutes,
		"amountUsd": event.AmountUSD, "changePercent": event.ChangePercent,
		"baselineValue": event.BaselineValue, "baselineRatio": event.BaselineRatio,
		"percentile": event.Percentile, "occurrenceCount": version,
		"eventAt": event.EventAt, "title": event.Title, "details": event.Details,
		"confirmedVenues": metadataValue(event.Metadata, "confirmed_venues", "confirmedVenues"),
		"venueCount":      metadataValue(event.Metadata, "venue_count", "venueCount"),
		"factorNames":     metadataValue(event.Metadata, "factorNames", "factor_names"),
		"metrics":         event.Metadata,
	}
}

func flushImportantEventOutbox(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, cfg Config) error {
	if db == nil || rdb == nil {
		return nil
	}
	var failures []error
	for processed := 0; processed < 100; processed++ {
		item, found, err := claimNextImportantEventDelivery(ctx, db)
		if err != nil {
			failures = append(failures, err)
			break
		}
		if !found {
			break
		}
		if err := processClaimedImportantEventDelivery(ctx, db, rdb, cfg, item); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

type pendingImportantEventDelivery struct {
	id              int64
	payload         []byte
	claimToken      string
	publishAttempts int
}

func claimNextImportantEventDelivery(ctx context.Context, db *pgxpool.Pool) (pendingImportantEventDelivery, bool, error) {
	token, err := newOutboxClaimToken()
	if err != nil {
		return pendingImportantEventDelivery{}, false, fmt.Errorf("create important-event outbox claim token: %w", err)
	}
	var item pendingImportantEventDelivery
	err = db.QueryRow(ctx, importantOutboxClaimSQL, token, int64(outboxClaimLease/time.Second)).Scan(&item.id, &item.payload, &item.publishAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return pendingImportantEventDelivery{}, false, nil
	}
	if err != nil {
		return pendingImportantEventDelivery{}, false, err
	}
	item.claimToken = token
	return item, true, nil
}

func processClaimedImportantEventDelivery(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, cfg Config, item pendingImportantEventDelivery) error {
	var payload map[string]any
	if err := json.Unmarshal(item.payload, &payload); err != nil {
		return failImportantEventDelivery(ctx, db, item, markOutboxPermanent(fmt.Errorf("decode important-event outbox payload: %w", err)))
	}
	if err := validateImportantEventOutboxPayload(payload); err != nil {
		return failImportantEventDelivery(ctx, db, item, markOutboxPermanent(err))
	}
	payload["deliveryId"] = item.id
	raw, err := json.Marshal(payload)
	if err != nil {
		return failImportantEventDelivery(ctx, db, item, markOutboxPermanent(fmt.Errorf("encode important-event outbox payload: %w", err)))
	}
	if _, err := publishOutboxStreamOnce(ctx, rdb, cfg.AlertsStream, fmt.Sprintf("important:%d", item.id), string(raw), cfg.AlertsStreamMaxLenApprox, defaultOutboxPublishDedupMax, defaultOutboxPublishDedupAge, time.Now().UTC()); err != nil {
		workerRedisPublishErrors.Add(1)
		return failImportantEventDelivery(ctx, db, item, err)
	}
	commandTag, err := db.Exec(ctx, `
		UPDATE important_event_outbox
		SET published_at=now(), attempts=attempts+1, publish_attempts=publish_attempts+1,
		    last_error=NULL, error_class=NULL, next_attempt_at=now(),
		    claim_token=NULL, claim_expires_at=NULL
		WHERE id=$1 AND claim_token=$2
	`, item.id, item.claimToken)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("important-event outbox claim lost for delivery %d", item.id)
	}
	return nil
}

func failImportantEventDelivery(ctx context.Context, db *pgxpool.Pool, item pendingImportantEventDelivery, deliveryErr error) error {
	return scheduleImportantDeliveryFailure(ctx, db, item, deliveryErr)
}

func importantEventMetadataWithHistory(current, previous map[string]any, at time.Time, operation string) map[string]any {
	if current == nil {
		current = make(map[string]any)
	}
	history := make([]any, 0, 20)
	if previous != nil {
		switch values := previous["updateHistory"].(type) {
		case []any:
			history = append(history, values...)
		case []map[string]any:
			for _, value := range values {
				history = append(history, value)
			}
		}
	}
	history = append(history, map[string]any{"at": at.UTC().Format(time.RFC3339), "operation": operation})
	if len(history) > 20 {
		history = history[len(history)-20:]
	}
	current["updateHistory"] = history
	return current
}

func newImportantEvent(ce CandleEvent, family, eventType, direction string, window int) importantEvent {
	return importantEvent{EventType: eventType, Family: family, Confidence: 1, Exchange: ce.Exchange, MarketType: ce.MarketType, Symbol: strings.ToUpper(ce.Symbol), Direction: direction, WindowMinutes: window, EventAt: time.UnixMilli(ce.TS).UTC(), Metadata: make(map[string]any)}
}

func finalizeImportantEvent(event *importantEvent) {
	if event.Confidence < 1 || event.Confidence > 3 {
		event.Confidence = 1
	}
	if event.WindowMinutes < 1 {
		event.WindowMinutes = 1
	}
	event.Symbol = strings.ToUpper(strings.TrimSpace(event.Symbol))
	if event.SourceKind == "" {
		event.SourceKind = "market"
	}
	if event.Metadata == nil {
		event.Metadata = make(map[string]any)
	}
	clusterMinutes := metadataInt(event.Metadata, "clusterWindowMinutes")
	if clusterMinutes < 1 {
		clusterMinutes = 30
	}
	clusterKey := fmt.Sprintf("bucket:%d", event.EventAt.Unix()/(int64(clusterMinutes)*60))
	if fingerprint, ok := event.Metadata["identityFingerprint"].(string); ok && strings.TrimSpace(fingerprint) != "" {
		clusterKey = "fingerprint:" + strings.TrimSpace(fingerprint)
		clusterMinutes = 1440
	} else if event.CatalystEventID != "" {
		clusterKey = "catalyst:" + event.CatalystEventID
		clusterMinutes = 1440
	}
	if event.SourceRef != "" && event.SourceKind == "onchain" {
		clusterKey = event.SourceKind + ":" + event.SourceRef
	}
	identityType := event.EventType
	event.IdentityKey = fmt.Sprintf("%s:%s:%s:%s:%s:%s", identityType, event.Exchange, event.MarketType, event.Symbol, event.Direction, clusterKey)
	event.DedupKey = fmt.Sprintf("%s:%d", event.IdentityKey, event.EventAt.UnixMilli())
	event.Metadata["clusterWindowMinutes"] = clusterMinutes
	event.Metadata["clusterKey"] = clusterKey
}

func metadataFloat(metadata map[string]any, key string) float64 {
	if metadata == nil {
		return 0
	}
	switch value := metadata[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case json.Number:
		parsed, _ := value.Float64()
		return parsed
	default:
		return 0
	}
}

func metadataBool(metadata map[string]any, key string) bool {
	value, _ := metadata[key].(bool)
	return value
}

func metadataStrings(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	switch values := metadata[key].(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, raw := range values {
			if value, ok := raw.(string); ok {
				result = append(result, value)
			}
		}
		return result
	default:
		return nil
	}
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(left))
	for _, value := range left {
		seen[value]++
	}
	for _, value := range right {
		seen[value]--
		if seen[value] < 0 {
			return false
		}
	}
	return true
}

func relativeChange(previous, current float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 1
	}
	return math.Abs(current-previous) / math.Abs(previous)
}

func relativePointerChange(previous, current *float64) float64 {
	if previous == nil && current == nil {
		return 0
	}
	if previous == nil || current == nil {
		return 1
	}
	return relativeChange(*previous, *current)
}

func sumExactWindow(r metricHistory, endMinute int64, window int, indicator string) (float64, bool) {
	if window <= 0 {
		return 0, false
	}
	total := 0.0
	for offset := 0; offset < window; offset++ {
		value, ok := r.get(endMinute-int64(offset), indicator)
		if !ok || !finiteNonNegative(value) {
			return 0, false
		}
		total += value
	}
	return total, true
}

func maxExactWindow(r metricHistory, endMinute int64, window int, indicator string) (float64, bool) {
	if window <= 0 {
		return 0, false
	}
	maximum := 0.0
	for offset := 0; offset < window; offset++ {
		value, ok := r.get(endMinute-int64(offset), indicator)
		if !ok || !finiteNonNegative(value) {
			return 0, false
		}
		maximum = math.Max(maximum, value)
	}
	return maximum, true
}

func exactPriceChange(r metricHistory, endMinute int64, window int) (float64, bool) {
	if window <= 0 || !exactMetricWindow(r, endMinute, window+1, "close") {
		return 0, false
	}
	now, nowOK := r.get(endMinute, "close")
	then, thenOK := r.get(endMinute-int64(window), "close")
	if !nowOK || !thenOK || !finitePositive(now) || !finitePositive(then) {
		return 0, false
	}
	return computePct(now, then), true
}

func exactMetricChange(r metricHistory, endMinute int64, window int, indicator string) (float64, bool) {
	if window <= 0 || !exactMetricWindow(r, endMinute, window+1, indicator) {
		return 0, false
	}
	now, nowOK := r.get(endMinute, indicator)
	then, thenOK := r.get(endMinute-int64(window), indicator)
	if !nowOK || !thenOK || !finite(now) || !finitePositive(then) {
		return 0, false
	}
	return computePct(now, then), true
}

func exactMetricWindow(r metricHistory, endMinute int64, window int, indicator string) bool {
	for offset := 0; offset < window; offset++ {
		value, ok := r.get(endMinute-int64(offset), indicator)
		if !ok || !finite(value) {
			return false
		}
	}
	return true
}

func historicalWindowSamples(r metricHistory, currentEnd int64, window, lookback int, getter func(int64) (float64, bool)) []float64 {
	if window <= 0 || lookback < window {
		return nil
	}
	out := make([]float64, 0, lookback/window)
	for offset := window; offset <= lookback; offset += window {
		if value, ok := getter(currentEnd - int64(offset)); ok && finitePositive(value) {
			out = append(out, value)
		}
	}
	return out
}

func robustBaseline(values []float64, current float64) (baselineStats, bool) {
	clean := make([]float64, 0, len(values))
	for _, value := range values {
		if finitePositive(value) {
			clean = append(clean, value)
		}
	}
	if len(clean) < baselineMinSamples {
		return baselineStats{Count: len(clean)}, false
	}
	sort.Float64s(clean)
	medianValue := quantileSorted(clean, 0.5)
	deviations := make([]float64, len(clean))
	belowOrEqual := 0
	for index, value := range clean {
		deviations[index] = math.Abs(value - medianValue)
		if value <= current {
			belowOrEqual++
		}
	}
	sort.Float64s(deviations)
	return baselineStats{Median: medianValue, P90: quantileSorted(clean, 0.9), MAD: quantileSorted(deviations, 0.5), Percentile: float64(belowOrEqual) / float64(len(clean)) * 100, Count: len(clean)}, medianValue > 0
}

func quantileSorted(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if q <= 0 {
		return values[0]
	}
	if q >= 1 {
		return values[len(values)-1]
	}
	position := q * float64(len(values)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return values[lower]
	}
	weight := position - float64(lower)
	return values[lower]*(1-weight) + values[upper]*weight
}

func finite(value float64) bool            { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func finitePositive(value float64) bool    { return value > 0 && finite(value) }
func finiteNonNegative(value float64) bool { return value >= 0 && finite(value) }
func importantSampleFresh(timestampMS int64, now time.Time) bool {
	if timestampMS <= 0 {
		return false
	}
	age := now.UTC().Sub(time.UnixMilli(timestampMS).UTC())
	return age >= -30*time.Second && age <= importantMaxSampleAge
}
func valueOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
func floatPtr(value float64) *float64 { return &value }
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func compatibleDirections(a, b string) bool {
	return a == b || a == "mixed" || b == "mixed" || a == "flat" || b == "flat"
}
func baseAsset(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	for _, quote := range []string{"USDT", "USDC", "BUSD", "FDUSD", "USD"} {
		if strings.HasSuffix(symbol, quote) && len(symbol) > len(quote) {
			return strings.TrimSuffix(symbol, quote)
		}
	}
	return symbol
}
func exchangeLabel(exchange string) string {
	switch strings.ToLower(strings.TrimSpace(exchange)) {
	case "binance":
		return "Binance"
	case "bybit":
		return "Bybit"
	case "multiple":
		return "Binance и Bybit"
	default:
		return strings.TrimSpace(exchange)
	}
}
func formatUSD(value float64) string {
	abs := math.Abs(value)
	switch {
	case abs >= 1e9:
		return fmt.Sprintf("$%.2f млрд", value/1e9)
	case abs >= 1e6:
		return fmt.Sprintf("$%.2f млн", value/1e6)
	case abs >= 1e3:
		return fmt.Sprintf("$%.0f тыс.", value/1e3)
	default:
		return fmt.Sprintf("$%.0f", value)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
