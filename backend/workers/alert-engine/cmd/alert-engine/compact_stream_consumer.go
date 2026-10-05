package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func (e *Engine) consumeCompactStreams(ctx context.Context, db *pgxpool.Pool) error {
	if e == nil || e.unifiedHistory == nil || e.rdb == nil || len(e.cfg.MarketStreams) == 0 {
		return nil
	}
	if !e.compactCheckpointingEnabled() {
		for _, stream := range e.cfg.MarketStreams {
			if err := ensureGroup(ctx, e.rdb, stream, e.cfg.MarketConsumerGroup); err != nil {
				return err
			}
		}
	}
	if err := e.assertCompactOwner(ctx); err != nil {
		return err
	}
	consumer := e.cfg.ConsumerName
	log.Printf("stream consumer started streams=%s group=%s consumer=%s", strings.Join(e.cfg.MarketStreams, ","), e.cfg.MarketConsumerGroup, consumer)
	backoff := 200 * time.Millisecond
	lastStats := time.Now().UTC()
	deduper := newCompactBatchDeduper(10*time.Minute, 10_000)
	reclaimErrors := make(chan error, 1)
	go func() { reclaimErrors <- e.reclaimCompactStreamsLoop(ctx, db, consumer, deduper) }()
	for ctx.Err() == nil {
		select {
		case reclaimErr := <-reclaimErrors:
			reclaimErrors = nil
			if reclaimErr != nil && !errors.Is(reclaimErr, context.Canceled) {
				return reclaimErr
			}
		default:
		}
		if err := e.assertCompactOwner(ctx); err != nil {
			return err
		}
		readStreams := append([]string(nil), e.cfg.MarketStreams...)
		for range e.cfg.MarketStreams {
			readStreams = append(readStreams, ">")
		}
		streams, err := e.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{Group: e.cfg.MarketConsumerGroup, Consumer: consumer, Streams: readStreams, Count: int64(e.cfg.StreamReadCount), Block: time.Duration(e.cfg.ReadBlockMs) * time.Millisecond}).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			log.Printf("compact stream xreadgroup error: %v (backoff=%s)", err, backoff)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			if backoff < 3*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 200 * time.Millisecond
		if err := e.assertCompactOwner(ctx); err != nil {
			return err
		}
		for _, stream := range streams {
			if err := e.processCompactStreamBatchWithRuleRefresh(ctx, db, stream.Stream, stream.Messages, false, consumer, deduper); err != nil {
				return fmt.Errorf("process compact stream batch %s: %w", stream.Stream, err)
			}
		}
		if time.Since(lastStats) >= time.Minute {
			if e.unifiedHistory != nil {
				stats := e.unifiedHistory.Stats()
				log.Printf("sparse history instruments=%d series=%d points=%d backing_bytes=%d max_depth=%d catalogs=%d coverage_minutes=%d", stats.Instruments, stats.Series, stats.ValueSlots, stats.BackingBytes, stats.MaxHistoryDepth, stats.Catalogs, stats.CoverageMinutes)
			}
			lastStats = time.Now().UTC()
		}
	}
	return ctx.Err()
}

func (e *Engine) reclaimCompactStreamsLoop(ctx context.Context, db *pgxpool.Pool, consumer string, deduper *compactBatchDeduper) error {
	if e.cfg.PendingClaimEverySeconds <= 0 {
		return nil
	}
	ticker := time.NewTicker(time.Duration(e.cfg.PendingClaimEverySeconds) * time.Second)
	defer ticker.Stop()
	idle := time.Duration(max1(e.cfg.PendingMinIdleSeconds)) * time.Second
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := e.assertCompactOwner(ctx); err != nil {
				return err
			}
			for _, stream := range e.cfg.MarketStreams {
				messages, _, err := e.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
					Stream: stream, Group: e.cfg.MarketConsumerGroup, Consumer: consumer,
					MinIdle: idle, Start: "0-0", Count: int64(e.cfg.StreamReadCount),
				}).Result()
				if err != nil {
					if err != redis.Nil && !errors.Is(err, context.Canceled) {
						log.Printf("compact stream xautoclaim stream=%s error=%v", stream, err)
					}
					continue
				}
				if err := e.assertCompactOwner(ctx); err != nil {
					return err
				}
				if err := e.processCompactStreamBatchWithRuleRefresh(ctx, db, stream, messages, true, consumer, deduper); err != nil {
					return fmt.Errorf("process reclaimed compact batch %s: %w", stream, err)
				}
			}
		}
	}
}

func (e *Engine) processCompactStreamBatchWithRuleRefresh(ctx context.Context, db *pgxpool.Pool, stream string, messages []redis.XMessage, reclaimed bool, consumer string, deduper *compactBatchDeduper) error {
	err := e.processCompactStreamBatch(ctx, db, stream, messages, reclaimed, consumer, deduper)
	if !errors.Is(err, errCompactBootstrapRulesChanged) {
		return err
	}
	if err := e.rebootstrapRules(ctx); err != nil {
		return fmt.Errorf("rebootstrap changed rules: %w", err)
	}
	return e.processCompactStreamBatch(ctx, db, stream, messages, reclaimed, consumer, deduper)
}

func (e *Engine) rebootstrapRules(ctx context.Context) error {
	return e.rebootstrapRulesFrom(ctx, newRedisCompactBootstrapSource(e.rdb))
}

func (e *Engine) rebootstrapRulesFrom(ctx context.Context, source compactStreamBootstrapSource) error {
	e.streamProcessing.Lock()
	defer e.streamProcessing.Unlock()
	if err := e.unifiedHistory.requireBootstrapDependenciesStable(); err == nil {
		return nil
	} else if !errors.Is(err, errCompactBootstrapRulesChanged) {
		return err
	}
	next := newSparseEngineState(e.rules, e.cfg.ImportantEventsEnabled, e.cfg.MaxInstruments)
	report, err := BootstrapCompactStreams(ctx, source, e.compactOwner, next, e.cfg.MarketStreams, e.cfg.MarketConsumerGroup, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := e.unifiedHistory.replaceRuntimeState(next); err != nil {
		return err
	}
	e.outcomeScheduler = newOutcomeScheduler(defaultOutcomeSchedulerTTL, defaultOutcomeSchedulerMaxUnits)
	log.Printf("sparse rule refresh replay complete streams=%d ready=%v", len(report.Streams), report.Ready)
	return nil
}

func (e *Engine) assertCompactOwner(ctx context.Context) error {
	if e == nil || !e.compactCheckpointingEnabled() {
		return nil
	}
	if e.compactOwner == nil {
		return errCompactBootstrapOwnerRequired
	}
	if err := e.compactOwner.AssertOwned(ctx); err != nil {
		return fmt.Errorf("compact stream owner lost: %w", err)
	}
	return nil
}

func compactMessageJSON(value any) ([]byte, bool) {
	switch raw := value.(type) {
	case string:
		if strings.TrimSpace(raw) == "" {
			return nil, false
		}
		return []byte(raw), true
	case []byte:
		if len(raw) == 0 {
			return nil, false
		}
		return raw, true
	case json.RawMessage:
		if len(raw) == 0 {
			return nil, false
		}
		return []byte(raw), true
	default:
		return nil, false
	}
}
