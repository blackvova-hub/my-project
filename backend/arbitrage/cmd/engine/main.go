package main

import (
	"context"
	"github.com/redis/go-redis/v9"
	"log"
	"net/http"
	"os"
	"os/signal"
	"shortlong/arbitrage/internal/market"
	"shortlong/arbitrage/internal/service"
	"sort"
	"syscall"
	"time"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	r := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD"), PoolSize: 8})
	defer r.Close()
	s := service.New(r)
	if raw := os.Getenv("ARBITRAGE_ASSETS"); raw != "" {
		s.Universe = nil
		for a := range market.AssetSet(raw) {
			s.Universe = append(s.Universe, a)
		}
		sort.Strings(s.Universe)
	}
	go s.Run(ctx)
	listen := os.Getenv("HTTP_ADDR")
	if listen == "" {
		listen = ":8093"
	}
	server := &http.Server{Addr: listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() {
		<-ctx.Done()
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = server.Shutdown(c)
	}()
	log.Printf("arbitrage engine listening on %s", listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
