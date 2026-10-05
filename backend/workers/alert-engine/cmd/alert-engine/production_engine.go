package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// processProductionBatch runs only after the compact batch has been admitted
// into dependency-filtered history. All externally visible effects use the
// durable PostgreSQL outboxes, so replaying a Redis delivery is idempotent.
func (e *Engine) processProductionBatch(ctx context.Context, db *pgxpool.Pool, batch compactDecodedBatch, now time.Time) error {
	if e == nil || e.unifiedHistory == nil || e.outcomeScheduler == nil {
		return errors.New("production sparse engine is not initialized")
	}
	if err := e.unifiedHistory.requireBootstrapDependenciesStable(); err != nil {
		return err
	}
	if e.cfg.ImportantEventsEnabled && batch.Kind == "c" {
		for _, event := range batch.Events {
			if err := e.processSparseImportantEvent(ctx, db, event); err != nil {
				return err
			}
		}
	}
	return e.outcomeScheduler.ObserveProductionBatch(now, batch, e.unifiedHistory, e.cfg.RulesSlotFilter, e.cfg.CooldownPrefix, func(outcomes []unifiedRuleOutcome) error {
		return e.deliverUnifiedOutcomes(ctx, db, outcomes)
	})
}

func (e *Engine) processSparseImportantEvent(ctx context.Context, db *pgxpool.Pool, event CandleEvent) error {
	if !importantSampleFresh(event.TS, time.Now().UTC()) {
		return nil
	}
	history := e.unifiedHistory
	history.snapshotMu.RLock()
	history.mu.Lock()
	record := history.instruments[instrumentCacheKey(event.Exchange, event.MarketType, event.Symbol)]
	if record == nil || record.history == nil {
		history.mu.Unlock()
		history.snapshotMu.RUnlock()
		return nil
	}
	venues := make(map[string]metricHistory, 2)
	for _, venue := range []string{"bybit", "binance"} {
		if candidate := history.instruments[instrumentCacheKey(venue, event.MarketType, event.Symbol)]; candidate != nil {
			venues[venue] = candidate.history
		}
	}
	evaluation := e.buildSixEventEvaluation(ctx, db, event, record.history, venues, minuteKey(event.TS))
	history.mu.Unlock()
	history.snapshotMu.RUnlock()
	if err := e.persistImportantEvents(ctx, db, event, evaluation); err != nil {
		return fmt.Errorf("important events detection failed instrument=%s: %w", instrumentCacheKey(event.Exchange, event.MarketType, event.Symbol), err)
	}
	return nil
}

func unifiedEvaluationDetails(outcome unifiedRuleOutcome) []map[string]any {
	details := make([]map[string]any, 0, len(outcome.Conditions))
	for _, condition := range outcome.Conditions {
		if !condition.EffectiveMatched {
			continue
		}
		evaluation := condition.Evaluation
		detail := map[string]any{
			"path": condition.Indicator, "cmp": evaluation.Cmp, "want": evaluation.Want,
			"got": evaluation.Got, "negate": condition.Negate,
		}
		if condition.Indicator == "liquidationsCombined" {
			detail["bybitUsd"], detail["binanceUsd"] = evaluation.BybitLiqSum, evaluation.BinanceLiqSum
			detail["okxUsd"], detail["bitgetUsd"], detail["gateioUsd"] = evaluation.OKXLiqSum, evaluation.BitgetLiqSum, evaluation.GateIOLiqSum
		}
		details = append(details, detail)
	}
	return details
}

func (e *Engine) deliverUnifiedOutcomes(ctx context.Context, db *pgxpool.Pool, outcomes []unifiedRuleOutcome) error {
	if len(outcomes) == 0 {
		return nil
	}
	queuedCooldowns := make(map[string]struct{}, len(outcomes))
	deliveries := make([]pendingSignalFanoutDelivery, 0, len(outcomes))
	for _, outcome := range outcomes {
		if !outcome.Available || !outcome.Matched || !outcomeMatchesSlot(outcome, e.cfg.RulesSlotFilter) {
			continue
		}
		alert := outcome.Alert
		cooldownExchange := alert.Exchange
		if alert.CombinedExchanges {
			cooldownExchange = strings.Join(combinedLiquidationVenues, "+")
		}
		if _, duplicate := queuedCooldowns[outcome.CooldownKey]; duplicate {
			continue
		}
		active, err := isCoinCooldownActive(ctx, e.rdb, e.cfg.CooldownPrefix, alert.UserID, cooldownExchange, alert.MarketType, alert.Symbol)
		if err != nil {
			return fmt.Errorf("check coin cooldown failed: %w", err)
		}
		if active {
			continue
		}
		inserted, deliveryID, err := insertSignal(ctx, db, alert, unifiedEvaluationDetails(outcome))
		if err != nil {
			return fmt.Errorf("insert signal failed: %w", err)
		}
		queuedCooldowns[outcome.CooldownKey] = struct{}{}
		deliveries = append(deliveries, pendingSignalFanoutDelivery{
			deliveryID: deliveryID, alert: alert, cooldownExchange: cooldownExchange,
			ruleInstrument: instrumentCacheKey(alert.Exchange, alert.MarketType, alert.Symbol), inserted: inserted,
		})
	}
	if len(deliveries) == 0 {
		return nil
	}
	notifyOutbox(ctx, e.rdb)
	results := waitSignalOutboxDeliveryBatch(ctx, db, deliveries, 10*time.Second)
	var failures []error
	for _, delivery := range deliveries {
		if err := results[delivery.deliveryID]; err != nil {
			failures = append(failures, fmt.Errorf("deliver signal %d through dispatcher: %w", delivery.deliveryID, err))
			continue
		}
		_, err := tryAcquireCoinCooldown(ctx, e.rdb, e.cfg.CooldownPrefix, delivery.alert.UserID, delivery.cooldownExchange, delivery.alert.MarketType, delivery.alert.Symbol)
		if err != nil {
			log.Printf("coin cooldown error userId=%d symbol=%s: %v", delivery.alert.UserID, delivery.alert.Symbol, err)
		}
		log.Printf("alert userId=%d ruleId=%d instrument=%s pct=%.4f window=%d inserted=%v", delivery.alert.UserID, delivery.alert.RuleID, delivery.ruleInstrument, delivery.alert.ChangePercent, delivery.alert.WindowMinutes, delivery.inserted)
	}
	return errors.Join(failures...)
}
