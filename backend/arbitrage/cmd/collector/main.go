package main

import (
	"context"
	"github.com/redis/go-redis/v9"
	"log"
	"os"
	"os/signal"
	"shortlong/arbitrage/internal/collector"
	"shortlong/arbitrage/internal/exchanges"
	"shortlong/arbitrage/internal/market"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	exchange := os.Getenv("EXCHANGE")
	if !market.KnownExchange(exchange) {
		log.Fatal("EXCHANGE must name one supported venue")
	}
	assets := os.Getenv("ARBITRAGE_ASSETS")
	if assets == "" {
		assets = market.DefaultAssets
	}
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD"), DialTimeout: 5 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal(err)
	}
	collector.New(exchanges.New(exchange, assets), rdb).Run(ctx)
}
