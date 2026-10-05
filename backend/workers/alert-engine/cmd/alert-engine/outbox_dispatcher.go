package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	outboxDispatcherAdvisoryLock int64 = 0x4f5554424f584449 // "OUTBOXDI"
	outboxWakeChannel                  = "internal:outbox:wakeup"
	outboxFallbackInterval             = 350 * time.Millisecond
	outboxLeadershipRetry              = 2 * time.Second
	outboxClaimLease                   = time.Minute
)

var workerRedisPublishErrors atomic.Uint64

func newOutboxClaimToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func notifyOutbox(ctx context.Context, rdb *redis.Client) {
	if rdb == nil {
		return
	}
	if err := rdb.Publish(ctx, outboxWakeChannel, "1").Err(); err != nil && ctx.Err() == nil {
		log.Printf("outbox wake publish failed: %v", err)
	}
}

func startOutboxDispatcher(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, cfg Config) {
	if db == nil || rdb == nil {
		return
	}

	for ctx.Err() == nil {
		conn, err := db.Acquire(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("outbox dispatcher acquire connection failed: %v", err)
			}
			if !waitForDispatcherRetry(ctx) {
				return
			}
			continue
		}
		var leader bool
		err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, outboxDispatcherAdvisoryLock).Scan(&leader)
		if err != nil || !leader {
			conn.Release()
			if err != nil && ctx.Err() == nil {
				log.Printf("outbox dispatcher leader election failed: %v", err)
			}
			if !waitForDispatcherRetry(ctx) {
				return
			}
			continue
		}

		log.Printf("outbox dispatcher became leader")
		serveOutboxDispatcherLeader(ctx, conn, db, rdb, cfg)
		unlockCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, unlockErr := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, outboxDispatcherAdvisoryLock)
		cancel()
		if unlockErr != nil && ctx.Err() == nil {
			log.Printf("outbox dispatcher unlock failed: %v", unlockErr)
		}
		conn.Release()
	}
}

func waitForDispatcherRetry(ctx context.Context) bool {
	timer := time.NewTimer(outboxLeadershipRetry)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func serveOutboxDispatcherLeader(ctx context.Context, leader *pgxpool.Conn, db *pgxpool.Pool, rdb *redis.Client, cfg Config) {
	pubsub := rdb.Subscribe(ctx, outboxWakeChannel)
	defer pubsub.Close()
	wake := pubsub.Channel()
	fallback := time.NewTicker(outboxFallbackInterval)
	heartbeat := time.NewTicker(5 * time.Second)
	defer fallback.Stop()
	defer heartbeat.Stop()
	flushOutboxes(ctx, db, rdb, cfg)
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-wake:
			if !ok {
				return
			}
			flushOutboxes(ctx, db, rdb, cfg)
		case <-fallback.C:
			flushOutboxes(ctx, db, rdb, cfg)
		case <-heartbeat.C:
			if err := leader.QueryRow(ctx, `SELECT 1`).Scan(new(int)); err != nil {
				if ctx.Err() == nil {
					log.Printf("outbox dispatcher lost leadership connection: %v", err)
				}
				return
			}
		}
	}
}

func flushOutboxes(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, cfg Config) {
	if err := flushSignalOutbox(ctx, db, rdb, cfg); err != nil && ctx.Err() == nil {
		log.Printf("signal outbox dispatcher failed: %v", err)
	}
	if err := flushImportantEventOutbox(ctx, db, rdb, cfg); err != nil && ctx.Err() == nil {
		log.Printf("important-event outbox dispatcher failed: %v", err)
	}
}

func deliverImportantEventOutbox(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, cfg Config) error {
	notifyOutbox(ctx, rdb)
	return nil
}
