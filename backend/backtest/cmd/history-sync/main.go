package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"shortlong/backtest/historysync"
	"shortlong/backtest/marketdata"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func integer(key string, fallback, lo, hi int) int {
	v, err := strconv.Atoi(env(key, strconv.Itoa(fallback)))
	if err != nil || v < lo || v > hi {
		log.Fatalf("%s must be %d..%d", key, lo, hi)
	}
	return v
}
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func main() {
	status := flag.Bool("status", false, "Print durable loading status as JSON")
	health := flag.Bool("healthcheck", false, "Check the local service readiness")
	flag.Parse()
	if *health {
		client := &http.Client{Timeout: 3 * time.Second}
		r, err := client.Get("http://127.0.0.1:8091/healthz")
		if err != nil {
			os.Exit(1)
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	concurrency := integer("MARKET_HISTORY_CONCURRENCY", 12, 1, 32)
	months := integer("MARKET_HISTORY_MONTHS", 6, 1, 12)
	refreshHours := integer("MARKET_HISTORY_REFRESH_HOURS", 12, 1, 24)
	refreshInterval := time.Duration(refreshHours) * time.Hour
	config, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("Invalid DATABASE_URL")
	}
	config.MaxConns = int32(concurrency + 4)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		log.Fatal("Cannot create postgres pool")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		log.Fatal("Postgres unavailable")
	}
	store := historysync.Store{Pool: pool}
	readStatus := func(ctx context.Context) ([]historysync.Status, error) {
		now := time.Now()
		closed := historysync.ClosedEnd(now)
		target := historysync.RefreshEnd(now, refreshInterval)
		st, err := store.Status(ctx, closed)
		for i := range st {
			st[i].RefreshHours = refreshHours
			st[i].RefreshThrough = target
			st[i].NextRefreshAt = target + refreshInterval.Milliseconds() + 10000
			st[i].ScheduledLagSeconds = max(0, st[i].LiveLagSeconds-(closed-target)/1000)
		}
		return st, err
	}
	if _, err = store.Status(ctx, historysync.ClosedEnd(time.Now())); err != nil {
		log.Fatal("Archive schema unavailable: apply API migration 064_add_market_history.sql")
	}
	if *status {
		st, err := readStatus(ctx)
		if err != nil {
			log.Fatal(err)
		}
		_ = json.NewEncoder(os.Stdout).Encode(st)
		return
	}
	rdb := redis.NewClient(&redis.Options{Addr: env("REDIS_ADDR", "redis:6379"), Password: os.Getenv("REDIS_PASSWORD")})
	defer rdb.Close()
	storage := marketdata.NewClickHouse(env("CLICKHOUSE_URL", "http://clickhouse:8123"), env("CLICKHOUSE_USER", "backtest"), os.Getenv("CLICKHOUSE_PASSWORD"))
	if err = storage.Init(ctx); err != nil {
		log.Fatal(err)
	}
	provider := marketdata.NewBybit(os.Getenv("BYBIT_BASE_URL"), rdb)
	history := &marketdata.History{DB: pool, Storage: storage, Bybit: provider}
	markets := strings.Split(env("MARKET_HISTORY_MARKETS", "spot,linear"), ",")
	for i, m := range markets {
		markets[i] = strings.TrimSpace(m)
		if markets[i] != "spot" && markets[i] != "linear" {
			log.Fatal("MARKET_HISTORY_MARKETS must contain spot and/or linear")
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE market_history_series SET active=false WHERE NOT (market=ANY($1::text[]))`, markets); err != nil {
		log.Fatal("Cannot apply market selection")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		check, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if pool.Ping(check) != nil || rdb.Ping(check).Err() != nil || storage.Ping(check) != nil {
			http.Error(w, "dependency unavailable", 503)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		check, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		st, err := readStatus(check)
		if err != nil {
			http.Error(w, "status unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})
	server := &http.Server{Addr: ":8091", Handler: mux, ReadHeaderTimeout: 3 * time.Second}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("health server: %v", err)
			stop()
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			catalogFailed := false
			for _, market := range markets {
				refresh, cancel := context.WithTimeout(ctx, 2*time.Minute)
				instruments, e := provider.Instruments(refresh, market)
				if e == nil {
					e = store.Register(refresh, market, instruments, historysync.HistoryStart(time.Now(), months), historysync.ClosedEnd(time.Now()))
				}
				cancel()
				if e != nil {
					catalogFailed = true
					log.Printf("catalog %s: %v", market, e)
				} else {
					log.Printf("catalog market=%s instruments=%d months=%d timeframe=5m", market, len(instruments), months)
				}
			}
			// Refresh the catalog with the next batch; retry transient failures sooner.
			next := time.UnixMilli(historysync.RefreshEnd(time.Now(), refreshInterval)).Add(refreshInterval + 10*time.Second)
			delay := time.Until(next)
			if catalogFailed {
				delay = min(delay, 10*time.Minute)
			}
			if !pause(ctx, delay) {
				return
			}
		}
	}()
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				task, e := store.Claim(ctx, historysync.RefreshEnd(time.Now(), refreshInterval))
				if e != nil {
					if ctx.Err() == nil {
						log.Printf("claim: %v", e)
					}
					if !pause(ctx, 3*time.Second) {
						return
					}
					continue
				}
				if task == nil {
					if !pause(ctx, time.Minute) {
						return
					}
					continue
				}
				work, cancel := context.WithTimeout(ctx, 2*time.Minute)
				candles, e := history.SyncRange(work, task.Market, task.Symbol, task.From, task.To)
				if e == nil {
					e = store.Finish(work, *task, candles)
				}
				cancel()
				if e != nil {
					release, done := context.WithTimeout(context.Background(), 5*time.Second)
					if ctx.Err() != nil {
						_ = store.Release(release, *task)
					} else {
						log.Printf("range market=%s symbol=%s kind=%s from=%d to=%d: %v", task.Market, task.Symbol, task.Kind, task.From, task.To, e)
						_ = store.Fail(release, *task, e)
					}
					done()
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for pause(ctx, time.Minute) {
			st, e := readStatus(ctx)
			if e != nil {
				log.Printf("status: %v", e)
				continue
			}
			raw, _ := json.Marshal(st)
			log.Printf("progress %s", raw)
		}
	}()
	log.Printf("market history started concurrency=%d months=%d markets=%v refresh_hours=%d", concurrency, months, markets, refreshHours)
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = server.Shutdown(shutdown)
	cancel()
	wg.Wait()
}
