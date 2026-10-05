package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type historyDependencyIndex struct {
	exact            map[string][]Rule
	wildcard         map[string][]Rule
	includeImportant bool
}

func newHistoryDependencyIndex(rules []Rule, includeImportant bool) *historyDependencyIndex {
	index := &historyDependencyIndex{
		exact:            make(map[string][]Rule),
		wildcard:         make(map[string][]Rule),
		includeImportant: includeImportant,
	}
	for _, rule := range rules {
		marketType := normalizeMarketType(rule.MarketType)
		if marketType == "" {
			continue
		}
		symbols := splitRuleSymbols(rule.Symbol)
		if len(symbols) == 0 {
			symbols = []string{wildcardSymbol}
		}
		for _, symbol := range symbols {
			copyRule := cloneRule(rule)
			copyRule.Symbol = symbol
			if symbol == wildcardSymbol {
				index.wildcard[marketType] = append(index.wildcard[marketType], copyRule)
				continue
			}
			key := marketType + ":" + symbol
			index.exact[key] = append(index.exact[key], copyRule)
		}
	}
	return index
}

func (i *historyDependencyIndex) Plan(exchange, marketType, symbol string) historyPlan {
	if i == nil {
		return historyPlan{}
	}
	marketType = normalizeMarketType(marketType)
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	candidates := i.exact[marketType+":"+symbol]
	wildcard := i.wildcard[marketType]
	if len(wildcard) > 0 {
		combined := make([]Rule, 0, len(candidates)+len(wildcard))
		combined = append(combined, candidates...)
		combined = append(combined, wildcard...)
		candidates = combined
	}
	return resolveHistoryPlan(candidates, exchange, marketType, symbol, i.includeImportant)
}

type unifiedInstrumentHistory struct {
	history  *sparseHistory
	lastSeen time.Time
}

type sparseEngineStats struct {
	Instruments     int
	Series          int
	ValueSlots      int
	KeySlots        int
	BackingBytes    int64
	MaxHistoryDepth int
	Catalogs        int
	CoverageMinutes int
}

var errUnifiedInstrumentCapacity = errors.New("unified history instrument capacity exceeded")

type plannedUnifiedEvent struct {
	key   string
	event CandleEvent
	plan  historyPlan
}

// sparseEngineState is deliberately side-effect free: it owns only compact
// catalog/coverage state and sparse in-memory series. It cannot evaluate or
// publish signals, write cooldowns, call Telegram, or write PostgreSQL.
type sparseEngineState struct {
	snapshotMu           sync.RWMutex
	mu                   sync.Mutex
	rules                *RulesCache
	ruleGeneration       uint64
	dependencies         *historyDependencyIndex
	includeImportant     bool
	maxInstruments       int
	ttl                  time.Duration
	lastCleanup          time.Time
	instruments          map[string]*unifiedInstrumentHistory
	streamHighWater      map[string]compactStreamHighWater
	streamProgress       map[string]*compactStreamProgress
	bootstrapFingerprint string
	catalogs             *catalogRegistry
	coverage             *coverageTracker
}

func newSparseEngineState(rules *RulesCache, includeImportant bool, maxInstruments int) *sparseEngineState {
	if rules == nil {
		rules = NewRulesCache()
	}
	if maxInstruments <= 0 {
		maxInstruments = 20_000
	}
	ttl := 27 * time.Hour
	catalogs := newCatalogRegistry(2048, ttl)
	return &sparseEngineState{
		rules:            rules,
		dependencies:     newHistoryDependencyIndex(nil, includeImportant),
		includeImportant: includeImportant,
		maxInstruments:   maxInstruments,
		ttl:              ttl,
		instruments:      make(map[string]*unifiedInstrumentHistory),
		streamHighWater:  make(map[string]compactStreamHighWater),
		streamProgress:   make(map[string]*compactStreamProgress),
		catalogs:         catalogs,
		coverage:         newCoverageTracker(catalogs, 20_000, ttl),
	}
}

// SetRulesCache is used during startup before the compact consumer begins.
// Keeping this explicit prevents the slot-filtered cache from being
// mistaken for the union SLOT_1/SLOT_2/SLOT_3 dependency set.
func (h *sparseEngineState) SetRulesCache(rules *RulesCache) {
	if h == nil || rules == nil {
		return
	}
	h.snapshotMu.Lock()
	defer h.snapshotMu.Unlock()
	h.mu.Lock()
	h.rules = rules
	h.ruleGeneration = 0
	h.dependencies = newHistoryDependencyIndex(nil, h.includeImportant)
	h.bootstrapFingerprint = ""
	h.mu.Unlock()
}

func (h *sparseEngineState) currentDependencyFingerprint() string {
	if h == nil {
		return ""
	}
	return rulesDependencyFingerprint(h.rules, h.includeImportant)
}

func (h *sparseEngineState) setBootstrapDependencyFingerprint(expected string) error {
	if h == nil {
		return ErrUnifiedSnapshotInvalidState
	}
	fingerprint := h.currentDependencyFingerprint()
	if !validSHA256Hex(fingerprint) || fingerprint != strings.ToLower(fingerprint) {
		return fmt.Errorf("%w: invalid rules dependency fingerprint", ErrUnifiedSnapshotInvalidState)
	}
	if expected != fingerprint {
		return fmt.Errorf("%w: expected=%s current=%s", errCompactBootstrapRulesChanged, expected, fingerprint)
	}
	h.snapshotMu.Lock()
	defer h.snapshotMu.Unlock()
	h.mu.Lock()
	h.bootstrapFingerprint = fingerprint
	h.mu.Unlock()
	return nil
}

func (h *sparseEngineState) requireBootstrapDependenciesStable() error {
	if h == nil {
		return ErrUnifiedSnapshotInvalidState
	}
	h.mu.Lock()
	bootstrapped := h.bootstrapFingerprint
	h.mu.Unlock()
	if bootstrapped == "" {
		return nil
	}
	current := h.currentDependencyFingerprint()
	if current != bootstrapped {
		return fmt.Errorf("%w: bootstrapped=%s current=%s", errCompactBootstrapRulesChanged, bootstrapped, current)
	}
	return nil
}

func (h *sparseEngineState) replaceRuntimeState(next *sparseEngineState) error {
	if h == nil || next == nil || h.rules != next.rules || h.includeImportant != next.includeImportant {
		return ErrUnifiedSnapshotInvalidState
	}
	h.snapshotMu.Lock()
	defer h.snapshotMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ruleGeneration = next.ruleGeneration
	h.dependencies = next.dependencies
	h.maxInstruments = next.maxInstruments
	h.ttl = next.ttl
	h.lastCleanup = next.lastCleanup
	h.instruments = next.instruments
	h.streamHighWater = next.streamHighWater
	h.streamProgress = next.streamProgress
	h.bootstrapFingerprint = next.bootstrapFingerprint
	h.catalogs = next.catalogs
	h.coverage = next.coverage
	return nil
}

func (h *sparseEngineState) ObserveBatch(batch compactDecodedBatch, now time.Time) error {
	if h == nil {
		return nil
	}
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	return h.observeBatch(batch, now)
}

func (h *sparseEngineState) ObserveStreamBatch(stream, messageID string, batch compactDecodedBatch, now time.Time) error {
	if h == nil {
		return nil
	}
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	if err := h.requireRegisteredStreamMessage(stream, messageID, batch); err != nil {
		return err
	}
	if err := h.observeBatch(batch, now); err != nil {
		return err
	}
	return h.markStreamMessageApplied(stream, messageID, batch, now)
}

func (h *sparseEngineState) observeBatch(batch compactDecodedBatch, now time.Time) error {
	for _, event := range batch.Events {
		if normalizeExchange(event.Exchange) != normalizeExchange(batch.Exchange) ||
			normalizeMarketType(event.MarketType) != normalizeMarketType(batch.MarketType) ||
			event.TS != batch.Minute || strings.TrimSpace(event.Symbol) == "" {
			return errors.New("compact event identity does not match batch")
		}
	}
	h.syncRules()

	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupLocked(now)
	planned := make([]plannedUnifiedEvent, 0, len(batch.Events))
	newKeys := make(map[string]struct{})
	for _, event := range batch.Events {
		key := instrumentCacheKey(event.Exchange, event.MarketType, event.Symbol)
		plan := h.dependencies.Plan(event.Exchange, event.MarketType, event.Symbol)
		planned = append(planned, plannedUnifiedEvent{key: key, event: event, plan: plan})
		if len(plan) > 0 && h.instruments[key] == nil {
			newKeys[key] = struct{}{}
		}
	}
	if len(h.instruments)+len(newKeys) > h.maxInstruments {
		return fmt.Errorf("%w: current=%d new=%d limit=%d", errUnifiedInstrumentCapacity, len(h.instruments), len(newKeys), h.maxInstruments)
	}
	// Keep coverage and history admission under the same h.mu ordering used by
	// EvaluateReady. A rejected-capacity batch therefore cannot make coverage
	// look ready while its history was silently discarded.
	if err := h.coverage.Observe(batch, now); err != nil {
		return err
	}
	minute := minuteKey(batch.Minute)
	for _, item := range planned {
		key, event, plan := item.key, item.event, item.plan
		if len(plan) == 0 {
			delete(h.instruments, key)
			continue
		}
		record := h.instruments[key]
		if record == nil {
			record = &unifiedInstrumentHistory{history: newSparseHistory(plan)}
			h.instruments[key] = record
		} else {
			record.history.Reconfigure(plan)
		}
		record.history.Put(minute, event)
		record.lastSeen = now
	}
	return nil
}

func (h *sparseEngineState) syncRules() {
	rules, generation := h.rules.Snapshot()
	h.mu.Lock()
	defer h.mu.Unlock()
	if generation == h.ruleGeneration {
		return
	}
	next := newHistoryDependencyIndex(rules, h.includeImportant)
	h.dependencies = next
	h.ruleGeneration = generation
	for key, record := range h.instruments {
		exchange, marketType, symbol, ok := splitInstrumentKey(key)
		if !ok {
			delete(h.instruments, key)
			continue
		}
		plan := next.Plan(exchange, marketType, symbol)
		if len(plan) == 0 {
			delete(h.instruments, key)
			continue
		}
		record.history.Reconfigure(plan)
	}
}

func splitInstrumentKey(key string) (string, string, string, bool) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func (h *sparseEngineState) cleanupLocked(now time.Time) {
	if !h.lastCleanup.IsZero() && now.Sub(h.lastCleanup) < time.Hour {
		return
	}
	h.lastCleanup = now
	cutoff := now.Add(-h.ttl)
	for key, record := range h.instruments {
		if record.lastSeen.Before(cutoff) {
			delete(h.instruments, key)
		}
	}
}

func (h *sparseEngineState) Stats() sparseEngineStats {
	if h == nil {
		return sparseEngineStats{}
	}
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	stats := sparseEngineStats{Instruments: len(h.instruments), Catalogs: h.catalogs.Len(), CoverageMinutes: h.coverage.Len(time.Now().UTC())}
	for _, record := range h.instruments {
		historyStats := record.history.Stats()
		stats.Series += historyStats.Series
		stats.ValueSlots += historyStats.ValueSlots
		stats.KeySlots += historyStats.KeySlots
		stats.BackingBytes += historyStats.BackingBytes
		if historyStats.MaxDepth > stats.MaxHistoryDepth {
			stats.MaxHistoryDepth = historyStats.MaxDepth
		}
	}
	return stats
}

func (h *sparseEngineState) Get(instrument string, minute int64, metric string) (float64, bool) {
	if h == nil {
		return 0, false
	}
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	record := h.instruments[instrument]
	if record == nil {
		return 0, false
	}
	return record.history.Get(minute, metric)
}

func (h *sparseEngineState) HasInstrument(instrument string) bool {
	if h == nil {
		return false
	}
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.instruments[instrument]
	return ok
}

func (h *sparseEngineState) StreamHighWater(stream string) (compactStreamHighWater, bool) {
	if h == nil {
		return compactStreamHighWater{}, false
	}
	h.snapshotMu.RLock()
	defer h.snapshotMu.RUnlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	water, ok := h.streamHighWater[stream]
	return water, ok
}

func cloneRule(rule Rule) Rule {
	rule.Conditions = append([]Condition(nil), rule.Conditions...)
	return rule
}
