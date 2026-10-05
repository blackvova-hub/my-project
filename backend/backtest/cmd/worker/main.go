package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"log"
	"os"
	"os/signal"
	bt "shortlong/backtest"
	"shortlong/backtest/engine"
	"shortlong/backtest/jobs"
	"shortlong/backtest/marketdata"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	config, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	config.MaxConns = 12
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		log.Fatal("postgres unavailable")
	}
	rdb := redis.NewClient(&redis.Options{Addr: env("REDIS_ADDR", "redis:6379"), Password: os.Getenv("REDIS_PASSWORD")})
	defer rdb.Close()
	storage := marketdata.NewClickHouse(env("CLICKHOUSE_URL", "http://clickhouse:8123"), env("CLICKHOUSE_USER", "backtest"), os.Getenv("CLICKHOUSE_PASSWORD"))
	if err = storage.Init(ctx); err != nil {
		log.Fatal(err)
	}
	if err = storage.InitIndicators(ctx); err != nil {
		log.Fatal(err)
	}
	history := &marketdata.History{DB: pool, Storage: storage, Bybit: marketdata.NewBybit(os.Getenv("BYBIT_BASE_URL"), rdb)}
	queue := jobs.Queue{Store: jobs.Store{Pool: pool}, Redis: rdb}
	if err = queue.Init(ctx); err != nil {
		log.Fatal(err)
	}
	concurrency, err := strconv.Atoi(env("BACKTEST_CONCURRENCY", "2"))
	if err != nil || concurrency < 1 || concurrency > 8 {
		log.Fatal("BACKTEST_CONCURRENCY must be 1..8")
	}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			if err := queue.Dispatch(ctx); err != nil && ctx.Err() == nil {
				log.Printf("queue dispatch: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	for i := 0; i < concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			var nonce [12]byte
			if _, err := rand.Read(nonce[:]); err != nil {
				log.Printf("consumer identity: %v", err)
				return
			}
			consumer := "bt-" + hex.EncodeToString(nonce[:])
			for ctx.Err() == nil {
				message, err := queue.Next(ctx, consumer)
				if errors.Is(err, redis.Nil) {
					continue
				}
				if err != nil {
					if ctx.Err() == nil {
						log.Printf("queue read: %v", err)
						select {
						case <-ctx.Done():
							return
						case <-time.After(time.Second):
						}
					}
					continue
				}
				id, _ := message.Values["jobId"].(string)
				if id != "" {
					execute(ctx, queue.Store, history, id)
				}
				if ctx.Err() == nil {
					if err := queue.Ack(ctx, message.ID); err != nil {
						log.Printf("queue ack: %v", err)
					}
				}
			}
		}()
	}
	log.Printf("backtest worker ready concurrency=%d engine=%s", concurrency, bt.EngineVersion)
	workers.Wait()
}

func execute(parent context.Context, store jobs.Store, history *marketdata.History, id string) {
	request, token, err := store.Claim(parent, id)
	if jobs.IsUnclaimable(err) {
		return
	}
	if err != nil {
		log.Printf("claim: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	var progress atomic.Int64
	var calculating atomic.Bool
	var metricsPhase atomic.Value
	metricsPhase.Store("")
	progress.Store(1)
	done := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				phase := "loading"
				if value := metricsPhase.Load().(string); value != "" {
					phase = value
				}
				if calculating.Load() {
					phase = "calculating"
				}
				beatCtx, beatCancel := context.WithTimeout(ctx, 5*time.Second)
				err := store.Heartbeat(beatCtx, id, token, phase, int(progress.Load()))
				beatCancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { close(done); <-heartbeatDone }()
	compute := func() (*bt.Result, error) {
		if err := request.Validate(time.Now().UTC()); err != nil {
			return nil, err
		}
		symbols, err := history.Bybit.Symbols(ctx, request.Market)
		if err != nil {
			return nil, err
		}
		available := map[string]bool{}
		for _, symbol := range symbols {
			available[symbol] = true
		}
		for _, symbol := range request.Symbols {
			if !available[symbol] {
				return nil, errors.New("Выбранная торговая пара недоступна на этом рынке Bybit")
			}
		}
		data := make(map[string][]bt.Candle, len(request.Symbols))
		step := bt.Interval(request.Timeframe).Milliseconds()
		from := request.From.UnixMilli() - int64(request.WarmupBars())*step
		for i, symbol := range request.Symbols {
			metricsPhase.Store("")
			candles, err := history.Load(ctx, request.Market, symbol, request.Timeframe, from, request.To.UnixMilli(), func(p int) { progress.Store(int64(1 + (i*100+p/2)*69/(len(request.Symbols)*100))) })
			if err != nil {
				return nil, err
			}
			if err = history.Enrich(ctx, request, symbol, candles, func(kind string, p int) {
				metricsPhase.Store("loading_" + kind)
				progress.Store(int64(1 + (i*100+50+p/2)*69/(len(request.Symbols)*100)))
			}); err != nil {
				return nil, err
			}
			data[symbol] = candles
		}
		calculating.Store(true)
		return engine.Run(ctx, request, data, func(p int) { progress.Store(int64(70 + p*29/100)) })
	}
	result, err := compute()
	if parent.Err() != nil {
		return
	} // leave the lease for crash/shutdown recovery
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer finishCancel()
	if err != nil {
		message := err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			message = "Расчёт превысил 30 минут. Сократите период"
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("job %s failed: %v", id, err)
		if e := store.Fail(finishCtx, id, token, message); e != nil {
			log.Printf("persist failure: %v", e)
		}
		return
	}
	if err = store.Complete(finishCtx, id, token, result); err != nil {
		log.Printf("persist result %s: %v", id, err)
	} else {
		log.Printf("job %s complete trades=%d", id, result.Metrics.TradeCount)
	}
}
