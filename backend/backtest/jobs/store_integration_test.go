package jobs

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"os"
	bt "shortlong/backtest"
	"strings"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) (context.Context, Store, string) {
	t.Helper()
	dsn := os.Getenv("BACKTEST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("BACKTEST_TEST_DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("integration database must end in _test")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var id string
	if err = pool.QueryRow(ctx, `INSERT INTO users(email,password_hash) VALUES(gen_random_uuid()::text||'@example.invalid','test-only') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal("apply API migrations first: ", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, id) })
	return ctx, Store{Pool: pool}, id
}
func testRequest() bt.Request {
	return bt.Request{Exchange: "bybit", Market: "spot", Symbols: []string{"BTCUSDT"}, Timeframe: "1h", Strategy: bt.Strategy{Version: 1}}
}
func newKey(t *testing.T, ctx context.Context, s Store) string {
	t.Helper()
	var key string
	if err := s.Pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	return key
}
func TestAdmissionOwnershipAndLeaseFencing(t *testing.T) {
	ctx, s, user := testStore(t)
	key := newKey(t, ctx, s)
	id, err := s.Create(ctx, user, key, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Create(ctx, user, key, testRequest())
	if err != nil || again != id {
		t.Fatal("idempotency", again, err)
	}
	if _, err = s.Create(ctx, user, newKey(t, ctx, s), testRequest()); !errors.Is(err, ErrActive) {
		t.Fatal("active limit", err)
	}
	if _, err = s.Get(ctx, newKey(t, ctx, s), id); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("cross-user access", err)
	}
	_, token, err := s.Claim(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Claim(ctx, id); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("duplicate claimed", err)
	}
	if err = s.Cancel(ctx, user, id); err != nil {
		t.Fatal(err)
	}
	if err = s.Heartbeat(ctx, id, token, "calculating", 50); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("cancel failed to fence heartbeat", err)
	}
	if err = s.Complete(ctx, id, token, &bt.Result{}); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("cancel failed to fence completion", err)
	}
	id, err = s.Create(ctx, user, newKey(t, ctx, s), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	_, token, err = s.Claim(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	result := &bt.Result{EngineVersion: bt.EngineVersion, Metrics: bt.Metrics{TradeCount: 1}, Trades: []bt.Trade{{Symbol: "BTCUSDT", PnL: 42}}}
	if err = s.Complete(ctx, id, token, result); err != nil {
		t.Fatal(err)
	}
	j, err := s.Get(ctx, user, id)
	if err != nil || j.Result.Metrics.TradeCount != 1 || j.Progress != 100 {
		t.Fatal(j, err)
	}
	trades, err := s.Trades(ctx, user, id, 0, 20)
	if err != nil || len(trades) != 1 || trades[0].PnL != 42 {
		t.Fatal(trades, err)
	}
	other, err := s.Trades(ctx, newKey(t, ctx, s), id, 0, 20)
	if err != nil || len(other) != 0 {
		t.Fatal("trade ownership", other, err)
	}
}
func TestConcurrentAdmissionOnlyOneActive(t *testing.T) {
	ctx, s, user := testStore(t)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	keys := []string{}
	for i := 0; i < 8; i++ {
		keys = append(keys, newKey(t, ctx, s))
	}
	for _, key := range keys {
		wg.Add(1)
		go func(key string) { defer wg.Done(); _, err := s.Create(ctx, user, key, testRequest()); results <- err }(key)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrActive) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("%d active requests accepted", success)
	}
}
func TestQueueOutboxRecoveryAndExpiredLease(t *testing.T) {
	ctx, s, user := testStore(t)
	addr := os.Getenv("BACKTEST_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("BACKTEST_TEST_REDIS_ADDR not set")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	q := Queue{Store: s, Redis: rdb}
	if err := q.Init(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := s.Create(ctx, user, newKey(t, ctx, s), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if err = q.Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	message, err := q.Next(ctx, "integration-"+user)
	if err != nil {
		t.Fatal(err)
	}
	if message.Values["jobId"] != id {
		t.Fatal("unexpected queue delivery", message.Values)
	}
	_, token, err := s.Claim(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Pool.Exec(ctx, `UPDATE backtest_jobs SET heartbeat_at=$2 WHERE id=$1`, id, time.Now().Add(-3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = q.Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Heartbeat(ctx, id, token, "calculating", 90); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("stale worker retained lease", err)
	}
	if err = q.Ack(ctx, message.ID); err != nil {
		t.Fatal(err)
	}
	message, err = q.Next(ctx, "integration-"+user)
	if err != nil {
		t.Fatal(err)
	}
	if message.Values["jobId"] != id {
		t.Fatal(message)
	}
	_, newToken, err := s.Claim(ctx, id)
	if err != nil || newToken == token {
		t.Fatal("lease did not rotate", err)
	}
	if err = s.Complete(ctx, id, newToken, &bt.Result{}); err != nil {
		t.Fatal(err)
	}
	if err = q.Ack(ctx, message.ID); err != nil {
		t.Fatal(err)
	}
}
