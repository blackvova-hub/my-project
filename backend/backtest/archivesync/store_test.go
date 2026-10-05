package archivesync

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	bt "shortlong/backtest"
	"shortlong/backtest/marketdata"
	"strings"
	"testing"
	"time"
)

func TestClosedMinuteBoundary(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 3, 9, 0, time.UTC)
	if got := time.UnixMilli(ClosedEnd(now)); got.Minute() != 2 || got.Second() != 0 {
		t.Fatal(got)
	}
	if got := time.UnixMilli(ClosedEnd(now.Add(time.Second))); got.Minute() != 3 {
		t.Fatal(got)
	}
}

func TestArchiveLeaseIsolationAndRetry(t *testing.T) {
	dsn := os.Getenv("BACKTEST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL not configured")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("requires _test database")
	}
	ctx := context.Background()
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	migration, e := os.ReadFile("../../api/internal/database/migrations/067_minute_archive.sql")
	if e != nil {
		t.Fatal(e)
	}
	var exists bool
	if e = db.QueryRow(ctx, `SELECT to_regclass('candle_archive_series') IS NOT NULL`).Scan(&exists); e != nil {
		t.Fatal(e)
	}
	if !exists {
		if _, e = db.Exec(ctx, string(migration)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec(ctx, `TRUNCATE candle_archive_gaps,candle_archive_series`); e != nil {
		t.Fatal(e)
	}
	s := Store{Pool: db}
	for _, ex := range []string{"bybit", "binance"} {
		for _, m := range []string{"spot", "linear"} {
			if e = s.Register(ctx, ex, m, []marketdata.Instrument{{Symbol: "BTCUSDT"}}, Step, 6*Step); e != nil {
				t.Fatal(e)
			}
		}
	}
	first, e := s.Claim(ctx, "bybit", "spot", 6*Step)
	if e != nil || first == nil {
		t.Fatalf("%v %v", first, e)
	}
	second, e := s.Claim(ctx, "bybit", "spot", 6*Step)
	if e != nil || second != nil {
		t.Fatal("double lease", second, e)
	}
	other, e := s.Claim(ctx, "binance", "spot", 6*Step)
	if e != nil || other == nil {
		t.Fatal("exchange collision", e)
	}
	if _, e = db.Exec(ctx, `UPDATE candle_archive_series SET lease_until=now()-interval '1 second' WHERE exchange='bybit' AND market='spot'`); e != nil {
		t.Fatal(e)
	}
	replacement, e := s.Claim(ctx, "bybit", "spot", 6*Step)
	if e != nil || replacement == nil {
		t.Fatal(e)
	}
	rows := []bt.Candle{{Time: Step}, {Time: 2 * Step}, {Time: 3 * Step}, {Time: 4 * Step}, {Time: 5 * Step}}
	if e = s.Finish(ctx, *first, rows); e == nil {
		t.Fatal("stale worker advanced cursor")
	}
	if e = s.Finish(ctx, *replacement, rows[:4]); e != nil {
		t.Fatal(e)
	}
	var missing int
	if e = db.QueryRow(ctx, `SELECT missing_candles FROM candle_archive_gaps WHERE exchange='bybit' AND market='spot'`).Scan(&missing); e != nil || missing != 1 {
		t.Fatal(missing, e)
	}
	if e = s.Release(ctx, *other, nil); e != nil {
		t.Fatal(e)
	}
	again, e := s.Claim(ctx, "binance", "spot", 6*Step)
	if e != nil || again == nil || again.From != other.From {
		t.Fatal("resume failure", again, e)
	}
	if e = s.Finish(ctx, *again, rows); e != nil {
		t.Fatal(e)
	}
	st, e := s.Status(ctx)
	if e != nil || len(st) != 4 {
		t.Fatal(st, e)
	}
}
