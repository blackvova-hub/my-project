package main

import (
	"context"
	"flag"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"log"
	"net/http"
	"os"
	"os/signal"
	"shortlong/backtest/marketdata"
	sim "shortlong/similarity"
	"sync"
	"syscall"
	"time"
)

func env(k, d string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return d
}
func main() {
	health := flag.Bool("healthcheck", false, "Check readiness")
	importFile := flag.String("import-assets", "", "Import asset metadata JSON without resetting cursors")
	purge := flag.Bool("purge-derived", false, "Explicitly delete derived similarity indexes, outcomes and jobs; preserve source candles")
	flag.Parse()
	if *health {
		c := &http.Client{Timeout: 4 * time.Second}
		r, e := c.Get("http://127.0.0.1:8092/healthz")
		if e != nil {
			os.Exit(1)
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	config, e := sim.ConfigFromEnv()
	if e != nil {
		log.Fatal(e)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pc, e := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if e != nil {
		log.Fatal("Invalid DATABASE_URL")
	}
	pc.MaxConns = int32(config.Concurrency + 8)
	db, e := pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		log.Fatal("PostgreSQL pool unavailable")
	}
	defer db.Close()
	store := sim.Store{DB: db, Config: config}
	if *importFile != "" {
		raw, e := os.ReadFile(*importFile)
		if e != nil {
			log.Fatal(e)
		}
		if e = sim.ImportAssets(ctx, db, raw); e != nil {
			log.Fatal(e)
		}
		log.Print("asset metadata imported")
		return
	}
	cache := redis.NewClient(&redis.Options{Addr: env("REDIS_ADDR", "redis:6379"), Password: os.Getenv("REDIS_PASSWORD")})
	defer cache.Close()
	ch := marketdata.NewClickHouse(env("CLICKHOUSE_URL", "http://clickhouse:8123"), env("CLICKHOUSE_USER", "backtest"), os.Getenv("CLICKHOUSE_PASSWORD"))
	q := sim.NewQdrant(env("QDRANT_URL", "http://qdrant:6333"), os.Getenv("QDRANT_API_KEY"), config.Version)
	out := sim.Outcomes{Storage: ch}
	if *purge {
		cleanup, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		if err := sim.PurgeDerived(cleanup, store, q, ch, cache); err != nil {
			log.Fatal(err)
		}
		return
	}
	startup, cancel := context.WithTimeout(ctx, 2*time.Minute)
	e = sim.SeedAssets(startup, db)
	if e == nil {
		e = q.Ping(startup)
	}
	if e == nil {
		e = store.SyncAssets(startup)
	}
	cancel()
	if e != nil {
		log.Fatal(e)
	}
	search := sim.NewSearcher(store, ch, q, cache)
	search.History = &marketdata.History{DB: db, Storage: ch, Bybit: marketdata.NewBybit(os.Getenv("BYBIT_BASE_URL"), cache)}
	p := sim.Pipeline{Store: store, Storage: ch, Index: q, Outcomes: out}
	jobs := sim.NewJobs(search, p)
	server := &http.Server{Addr: ":8092", Handler: sim.Handler(search, q, jobs), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 70 * time.Second, IdleTimeout: 60 * time.Second}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if e := server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Print(e)
			stop()
		}
	}()
	for i := 0; i < config.Concurrency; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); jobs.Run(ctx) }()
	}
	log.Printf("similarity started mode=on_demand version=%s concurrency=%d", config.Version, config.Concurrency)
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = server.Shutdown(shutdown)
	cancel()
	wg.Wait()
}
