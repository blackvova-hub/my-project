package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	defaultOutcomeSchedulerTTL      = 30 * time.Minute
	defaultOutcomeSchedulerMaxUnits = 200_000
)

type outcomeScheduler struct {
	mu       sync.Mutex
	ttl      time.Duration
	maxUnits int
	units    int
	pending  map[int64]map[string]time.Time
}

func newOutcomeScheduler(ttl time.Duration, maxUnits int) *outcomeScheduler {
	if ttl <= 0 {
		ttl = defaultOutcomeSchedulerTTL
	}
	if maxUnits <= 0 {
		maxUnits = defaultOutcomeSchedulerMaxUnits
	}
	return &outcomeScheduler{ttl: ttl, maxUnits: maxUnits, pending: make(map[int64]map[string]time.Time)}
}

func (s *outcomeScheduler) ObserveProductionBatch(now time.Time, batch compactDecodedBatch, history *sparseEngineState, slotFilter, cooldownPrefix string, deliver func([]unifiedRuleOutcome) error) error {
	if s == nil || history == nil || deliver == nil {
		return errors.New("invalid production outcome scheduler")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(now)
	symbols := s.pending[batch.Minute]
	if symbols == nil {
		symbols = make(map[string]time.Time)
		s.pending[batch.Minute] = symbols
	}
	for _, rawSymbol := range batch.Catalog {
		symbol := strings.ToUpper(strings.TrimSpace(rawSymbol))
		if symbol == "" {
			continue
		}
		if _, exists := symbols[symbol]; exists {
			continue
		}
		if s.units >= s.maxUnits {
			return errors.New("production outcome scheduler symbol budget exhausted")
		}
		symbols[symbol] = now
		s.units++
	}
	for symbol := range symbols {
		outcomes, ready := history.EvaluateReady(batch.MarketType, symbol, batch.Minute, cooldownPrefix, now)
		if !ready {
			continue
		}
		filtered := outcomes[:0]
		for _, outcome := range outcomes {
			if outcomeMatchesSlot(outcome, slotFilter) {
				filtered = append(filtered, outcome)
			}
		}
		if err := deliver(filtered); err != nil {
			return err
		}
		delete(symbols, symbol)
		s.units--
	}
	if len(symbols) == 0 {
		delete(s.pending, batch.Minute)
	}
	return nil
}

func outcomeMatchesSlot(outcome unifiedRuleOutcome, slotFilter string) bool {
	slotFilter = strings.ToUpper(strings.TrimSpace(slotFilter))
	return slotFilter == "" || strings.ToUpper(strings.TrimSpace(outcome.Alert.ScannerSlot)) == slotFilter
}

func (s *outcomeScheduler) expireLocked(now time.Time) {
	cutoff := now.Add(-s.ttl)
	for minute, symbols := range s.pending {
		for symbol, observedAt := range symbols {
			if observedAt.Before(cutoff) {
				delete(symbols, symbol)
				s.units--
			}
		}
		if len(symbols) == 0 {
			delete(s.pending, minute)
		}
	}
}

func (s *outcomeScheduler) String() string {
	if s == nil {
		return "outcome_scheduler=nil"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("outcome_scheduler_minutes=%d outcome_scheduler_units=%d", len(s.pending), s.units)
}
