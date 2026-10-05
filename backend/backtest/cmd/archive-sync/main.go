package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"log"
	"net/http"
	"os"
	"os/signal"
	bt "shortlong/backtest"
	"shortlong/backtest/archivesync"
	"shortlong/backtest/historysync"
	"shortlong/backtest/marketdata"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type provider interface {
	Instruments(context.Context, string) ([]marketdata.Instrument, error)
	Candles(context.Context, string, string, string, int64, int64) ([]bt.Candle, error)
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func integer(k string, d, lo, hi int) int {
	v, e := strconv.Atoi(env(k, strconv.Itoa(d)))
	if e != nil || v < lo || v > hi {
		log.Fatalf("invalid %s", k)
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
	status := flag.Bool("status", false, "Print durable archive progress")
	health := flag.Bool("healthcheck", false, "Check dependencies and catalog registration")
	flag.Parse()
	if *health {
		client := http.Client{Timeout: 5 * time.Second}
		r, e := client.Get("http://127.0.0.1:8091/healthz")
		if e != nil {
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
	months := integer("MARKET_HISTORY_MONTHS", 12, 1, 120)
	perMarket := integer("ARCHIVE_WORKERS_PER_MARKET", 2, 1, 4)
	refresh := time.Duration(integer("ARCHIVE_REFRESH_MINUTES", 60, 5, 720)) * time.Minute
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("Invalid DATABASE_URL")
	}
	cfg.MaxConns = int32(perMarket*4 + 4)
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	store := archivesync.Store{Pool: db}
	if *status {
		st, e := store.Status(ctx)
		if e != nil {
			log.Fatal(e)
		}
		json.NewEncoder(os.Stdout).Encode(st)
		return
	}
	rdb := redis.NewClient(&redis.Options{Addr: env("REDIS_ADDR", "redis:6379"), Password: os.Getenv("REDIS_PASSWORD")})
	defer rdb.Close()
	ch := marketdata.NewClickHouse(env("CLICKHOUSE_URL", "http://clickhouse:8123"), env("CLICKHOUSE_USER", "backtest"), os.Getenv("CLICKHOUSE_PASSWORD"))
	if err = ch.InitMinutes(ctx); err != nil {
		log.Fatal(err)
	}
	if _, err = store.Status(ctx); err != nil {
		log.Fatal("Archive migration missing: ", err)
	}
	providers := map[string]provider{"bybit": marketdata.NewBybit(os.Getenv("BYBIT_BASE_URL"), rdb), "binance": marketdata.NewBinance(os.Getenv("BINANCE_SPOT_BASE_URL"), os.Getenv("BINANCE_FUTURES_BASE_URL"), rdb)}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		check, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		st, e := store.Status(check)
		if e != nil || len(st) != 4 || rdb.Ping(check).Err() != nil || ch.Ping(check) != nil {
			http.Error(w, "archive dependencies/catalog not ready", 503)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		check, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		st, e := store.Status(check)
		if e != nil {
			http.Error(w, "status unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(st)
	})
	// Internal, bounded archive reads for verification and future API integration.
	mux.HandleFunc("/candles", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		from, e1 := strconv.ParseInt(q.Get("from"), 10, 64)
		to, e2 := strconv.ParseInt(q.Get("to"), 10, 64)
		if e1 != nil || e2 != nil || to-from > 7*86400000 || to <= from {
			http.Error(w, "invalid range (max 7 days)", 400)
			return
		}
		tf := q.Get("timeframe")
		if tf == "" {
			tf = "1m"
		}
		check, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		rows, e := ch.ReadMinutes(check, q.Get("exchange"), q.Get("market"), q.Get("symbol"), tf, from, to)
		if e != nil {
			http.Error(w, "archive query failed", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rows)
	})
	server := &http.Server{Addr: ":8091", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if e := server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Print(e)
			stop()
		}
	}()
	var wg sync.WaitGroup
	for exchange, p := range providers {
		for _, market := range []string{"spot", "linear"} {
			wg.Add(1)
			go func(exchange, market string, p provider) {
				defer wg.Done()
				for ctx.Err() == nil {
					work, cancel := context.WithTimeout(ctx, 2*time.Minute)
					instruments, e := p.Instruments(work, market)
					if e == nil {
						e = store.Register(work, exchange, market, instruments, historysync.HistoryStart(time.Now(), months), archivesync.ClosedEnd(time.Now()))
					}
					cancel()
					delay := 12 * time.Hour
					if e != nil {
						log.Printf("catalog %s/%s: %v", exchange, market, e)
						delay = 5 * time.Minute
					} else {
						log.Printf("catalog %s/%s: %d series, %d months, 1m", exchange, market, len(instruments), months)
					}
					if !pause(ctx, delay) {
						return
					}
				}
			}(exchange, market, p)
			for i := 0; i < perMarket; i++ {
				wg.Add(1)
				go func(exchange, market string, p provider) {
					defer wg.Done()
					for ctx.Err() == nil {
						// Batch live refreshes to avoid one tiny insert per series per minute on HDD.
						end := time.Now().Add(-10 * time.Second).UTC().Truncate(refresh).UnixMilli()
						t, e := store.Claim(ctx, exchange, market, end)
						if e != nil {
							log.Printf("claim %s/%s: %v", exchange, market, e)
							if !pause(ctx, 5*time.Second) {
								return
							}
							continue
						}
						if t == nil {
							if !pause(ctx, 5*time.Second) {
								return
							}
							continue
						}
						work, cancel := context.WithTimeout(ctx, 2*time.Minute)
						rows, e := p.Candles(work, t.Market, t.Symbol, "1m", t.From, t.To)
						if e == nil {
							e = ch.InsertMinutes(work, t.Exchange, t.Market, t.Symbol, rows)
						}
						if e == nil {
							e = store.Finish(work, *t, rows)
						}
						cancel()
						if e != nil {
							release, done := context.WithTimeout(context.Background(), 5*time.Second)
							cause := e
							if ctx.Err() != nil {
								cause = nil
							} else {
								log.Printf("range %s/%s/%s %s: %v", exchange, market, t.Symbol, t.Kind, e)
							}
							if e2 := store.Release(release, *t, cause); e2 != nil {
								log.Printf("release: %v", e2)
							}
							done()
						}
					}
				}(exchange, market, p)
			}
		}
	}
	log.Printf("minute archive started: 4 markets, workers=%d, months=%d, refresh=%s", perMarket*4, months, refresh)
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	server.Shutdown(shutdown)
	cancel()
	wg.Wait()
}
