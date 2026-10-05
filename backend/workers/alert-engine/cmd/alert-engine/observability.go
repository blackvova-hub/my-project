package main

import (
	"context"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func residentSetSizeBytes() int64 {
	if runtime.GOOS != "linux" {
		return -1
	}
	raw, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return -1
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return -1
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return -1
	}
	return pages * int64(os.Getpagesize())
}

type databaseObservation struct {
	xactCommit   int64
	xactRollback int64
	at           time.Time
	initialized  bool
}

func startWorkerObservability(ctx context.Context, e *Engine, db *pgxpool.Pool, rdb *redis.Client, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	previousDB := databaseObservation{}
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			observeWorker(ctx, now, e, db, rdb, &previousDB)
		}
	}
}

func observeWorker(ctx context.Context, now time.Time, e *Engine, db *pgxpool.Pool, rdb *redis.Client, previousDB *databaseObservation) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	history := e.unifiedHistory.Stats()
	rules, _ := e.rules.Snapshot()
	log.Printf("worker resources heap_live_bytes=%d heap_goal_bytes=%d heap_released_bytes=%d rss_bytes=%d goroutines=%d active_dependency_instruments=%d allocated_histories=%d allocated_series=%d total_allocated_points=%d estimated_history_bytes=%d max_history_depth=%d rules_count=%d slot=%s slot_dependency_count=%d catalogs=%d coverage_minutes=%d redis_publish_errors=%d",
		mem.HeapAlloc, mem.NextGC, mem.HeapReleased, residentSetSizeBytes(), runtime.NumGoroutine(),
		history.Instruments, history.Instruments, history.Series, history.ValueSlots, history.BackingBytes,
		history.MaxHistoryDepth, len(rules), e.cfg.RulesSlotFilter, history.Series, history.Catalogs,
		history.CoverageMinutes, workerRedisPublishErrors.Load())

	if rdb != nil {
		observeRedisStreams(ctx, now, e.cfg, rdb)
	}
	if db != nil && (strings.TrimSpace(e.cfg.RulesSlotFilter) == "" || strings.EqualFold(e.cfg.RulesSlotFilter, "SLOT_1")) {
		observeDatabase(ctx, now, db, previousDB)
	}
}

func observeRedisStreams(parent context.Context, now time.Time, cfg Config, rdb *redis.Client) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	for _, stream := range cfg.MarketStreams {
		xlen, xlenErr := rdb.XLen(ctx, stream).Result()
		oldestAge := int64(-1)
		messages, oldestErr := rdb.XRangeN(ctx, stream, "-", "+", 1).Result()
		if oldestErr == nil && len(messages) == 1 {
			if idMs, err := strconv.ParseInt(strings.SplitN(messages[0].ID, "-", 2)[0], 10, 64); err == nil {
				oldestAge = now.UnixMilli() - idMs
			}
		}
		pel := int64(-1)
		pending, pendingErr := rdb.XPending(ctx, stream, cfg.MarketConsumerGroup).Result()
		if pendingErr == nil && pending != nil {
			pel = pending.Count
		}
		log.Printf("worker stream stream=%s group=%s xlen=%d oldest_age_ms=%d pel=%d xlen_error=%v oldest_error=%v pel_error=%v",
			stream, cfg.MarketConsumerGroup, xlen, oldestAge, pel, xlenErr, oldestErr, pendingErr)
	}
}

func observeDatabase(parent context.Context, now time.Time, db *pgxpool.Pool, previous *databaseObservation) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	var signalPending, importantPending, commits, rollbacks int64
	if err := db.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM signal_delivery_outbox WHERE stream_published_at IS NULL OR telegram_processed_at IS NULL),
			(SELECT count(*) FROM important_event_outbox WHERE published_at IS NULL),
			xact_commit,
			xact_rollback
		FROM pg_stat_database
		WHERE datname = current_database()
	`).Scan(&signalPending, &importantPending, &commits, &rollbacks); err != nil {
		log.Printf("worker database observation error=%v", err)
		return
	}
	rate := float64(0)
	if previous != nil && previous.initialized {
		seconds := now.Sub(previous.at).Seconds()
		deltaCommit := commits - previous.xactCommit
		deltaRollback := rollbacks - previous.xactRollback
		if seconds > 0 && deltaCommit >= 0 && deltaRollback >= 0 {
			rate = float64(deltaCommit+deltaRollback) / seconds
		}
	}
	if previous != nil {
		*previous = databaseObservation{xactCommit: commits, xactRollback: rollbacks, at: now, initialized: true}
	}
	log.Printf("worker database signal_outbox_pending=%d important_outbox_pending=%d transactions_per_second=%.3f xact_commit=%d xact_rollback=%d",
		signalPending, importantPending, rate, commits, rollbacks)
}
