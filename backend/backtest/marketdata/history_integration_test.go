package marketdata

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedHistoryDeduplicatesConcurrentRequestsAndDetectsGaps(t *testing.T) {
	dsn, endpoint := os.Getenv("BACKTEST_TEST_DATABASE_URL"), os.Getenv("BACKTEST_TEST_CLICKHOUSE_URL")
	if dsn == "" || endpoint == "" {
		t.Skip("isolated PostgreSQL / ClickHouse not configured")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("requires _test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	storage := NewClickHouse(endpoint, "backtest", os.Getenv("BACKTEST_TEST_CLICKHOUSE_PASSWORD"))
	if err = storage.Init(ctx); err != nil {
		t.Fatal(err)
	}
	symbol := "QA" + strconv.FormatInt(time.Now().UnixNano(), 36) + "USDT"
	symbol = strings.ToUpper(symbol)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		start, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		end, _ := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
		fmt.Fprint(w, `{"retCode":0,"result":{"list":[`)
		first := true
		for ts := end + 1 - 300000; ts >= start; ts -= 300000 {
			if !first {
				fmt.Fprint(w, ",")
			}
			first = false
			fmt.Fprintf(w, `["%d","100","101","99","100","10"]`, ts)
		}
		fmt.Fprint(w, `]}}`)
	}))
	defer server.Close()
	h := &History{DB: pool, Storage: storage, Bybit: NewBybit(server.URL, nil)}
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := h.Load(ctx, "spot", symbol, "5m", 0, 900000, nil)
			if e == nil && len(c) != 3 {
				e = fmt.Errorf("got %d candles", len(c))
			}
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal(e)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("downloaded same candles %d times", calls.Load())
	}
	// ClickHouse FINAL removes duplicates after an ambiguous/retried insert.
	cached, err := storage.Read(ctx, "spot", symbol, "5m", 0, 900000)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Insert(ctx, "spot", symbol, "5m", cached); err != nil {
		t.Fatal(err)
	}
	again, err := storage.Read(ctx, "spot", symbol, "5m", 0, 900000)
	if err != nil || len(again) != 3 {
		t.Fatal("duplicate candles", len(again), err)
	}
	var before time.Time
	if err = pool.QueryRow(ctx, `SELECT last_used_at FROM backtest_candle_series WHERE symbol=$1`, symbol).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err = h.load(ctx, "spot", symbol, "5m", 0, 900000, nil, false); err != nil {
		t.Fatal(err)
	}
	var after time.Time
	_ = pool.QueryRow(ctx, `SELECT last_used_at FROM backtest_candle_series WHERE symbol=$1`, symbol).Scan(&after)
	if !after.Equal(before) {
		t.Fatal("background refresh extended usage retention")
	}
	// Every larger timeframe reuses the same 5m archive without another REST fetch.
	if _, err = h.Load(ctx, "spot", symbol, "5m", 0, 14400000, nil); err != nil {
		t.Fatal(err)
	}
	beforeAggregate := calls.Load()
	for _, tf := range []string{"15m", "1h", "4h"} {
		bars, e := h.Load(ctx, "spot", symbol, tf, 0, 14400000, nil)
		if e != nil || len(bars) == 0 || bars[0].Open != 100 || bars[0].Close != 100 {
			t.Fatal(tf, bars, e)
		}
	}
	if calls.Load() != beforeAggregate {
		t.Fatal("higher timeframes downloaded duplicate history")
	}
	// A provider returning fewer rows must fail, not invent flat candles.
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"retCode":0,"result":{"list":[]}}`) }))
	defer broken.Close()
	h.Bybit = NewBybit(broken.URL, nil)
	if _, err = h.Load(ctx, "spot", symbol, "5m", 14400000, 15300000, nil); err == nil {
		t.Fatal("missing candles silently accepted")
	}
	// Archive loading can record an empty source window and must terminate.
	archive, err := h.SyncRange(ctx, "spot", symbol, 14400000, 15300000)
	if err != nil || len(archive) != 0 {
		t.Fatal("empty source range fabricated", archive, err)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM backtest_candle_series WHERE symbol=$1`, symbol)
}
