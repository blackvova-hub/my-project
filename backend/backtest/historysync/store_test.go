package historysync

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	bt "shortlong/backtest"
	"shortlong/backtest/marketdata"
)

func TestCalendarAndClosedBoundary(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 34, 0, 0, time.UTC)
	if got := time.UnixMilli(HistoryStart(now, 6)).UTC().Format("2006-01-02"); got != "2026-02-28" {
		t.Fatal(got)
	}
	if got := time.UnixMilli(ClosedEnd(time.Date(2026, 9, 10, 12, 5, 4, 0, time.UTC))).UTC().Format("15:04:05"); got != "12:00:00" {
		t.Fatal(got)
	}
}

func TestHalfDayRefreshBoundary(t *testing.T) {
	for _, tc := range []struct{ now, want string }{
		{"2026-09-14T00:00:09Z", "2026-09-13T12:00:00Z"},
		{"2026-09-14T00:00:10Z", "2026-09-14T00:00:00Z"},
		{"2026-09-14T11:59:59Z", "2026-09-14T00:00:00Z"},
		{"2026-09-14T12:00:09Z", "2026-09-14T00:00:00Z"},
		{"2026-09-14T12:00:10Z", "2026-09-14T12:00:00Z"},
		{"2026-09-14T22:15:00+10:00", "2026-09-14T12:00:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		got := time.UnixMilli(RefreshEnd(now, 12*time.Hour)).UTC().Format(time.RFC3339)
		if got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.now, got, tc.want)
		}
	}
}

func TestArchiveLeaseResumeMissingCoverageAndLivePriority(t *testing.T) {
	dsn := os.Getenv("BACKTEST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated DB not configured")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("requires _test DB")
	}
	ctx := context.Background()
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	s := Store{Pool: p}
	// This package owns only archive metadata in the isolated database.
	if _, err = p.Exec(ctx, `TRUNCATE market_history_gaps,market_history_series`); err != nil {
		t.Fatal(err)
	}
	instruments := []marketdata.Instrument{{Symbol: "QA1USDT"}, {Symbol: "QA2USDT", LaunchTime: 4 * Step}}
	if err = s.Register(ctx, "spot", instruments, Step, 8*Step); err != nil {
		t.Fatal(err)
	}
	tasks := make(chan *Task, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); task, e := s.Claim(ctx, 8*Step); tasks <- task; errs <- e }()
	}
	wg.Wait()
	close(tasks)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	claimed := []*Task{}
	for task := range tasks {
		if task == nil {
			t.Fatal("no claim")
		}
		claimed = append(claimed, task)
	}
	if claimed[0].Symbol == claimed[1].Symbol {
		t.Fatal("duplicate concurrent claim")
	}
	first := claimed[0]
	if first.Kind != "backfill" {
		t.Fatal(first)
	}
	// Simulate an interrupted process: expired lease gets a fresh token.
	_, err = p.Exec(ctx, `UPDATE market_history_series SET lease_until=now()-interval '1 second' WHERE symbol=$1`, first.Symbol)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := s.Claim(ctx, 8*Step)
	if err != nil || resumed == nil || resumed.Symbol != first.Symbol || resumed.Token == first.Token {
		t.Fatal(resumed, err)
	}
	if err = s.Finish(ctx, *first, nil); err == nil {
		t.Fatal("stale worker advanced cursor")
	}
	sparse := []bt.Candle{{Time: resumed.To - Step, Open: 100, High: 101, Low: 99, Close: 100, Volume: 1}}
	if err = s.Finish(ctx, *resumed, sparse); err != nil {
		t.Fatal(err)
	}
	var missing int
	if err = p.QueryRow(ctx, `SELECT missing_candles FROM market_history_gaps WHERE symbol=$1`, resumed.Symbol).Scan(&missing); err != nil || missing != int((resumed.To-resumed.From)/Step)-1 {
		t.Fatal(missing, err)
	}
	if err = s.Finish(ctx, *claimed[1], nil); err != nil {
		t.Fatal(err)
	}
	// Live catch-up wins over repairs and continues at the durable frontier.
	live, err := s.Claim(ctx, 10*Step)
	if err != nil || live == nil || live.Kind != "live" || live.From != 8*Step || live.To != 10*Step {
		t.Fatal(live, err)
	}
	if err = s.Fail(ctx, *live, fmt.Errorf("provider timeout")); err != nil {
		t.Fatal(err)
	}
	var message string
	if err = p.QueryRow(ctx, `SELECT last_error FROM market_history_series WHERE symbol=$1`, live.Symbol).Scan(&message); err != nil || message != "provider timeout" {
		t.Fatal(message, err)
	}
	st, err := s.Status(ctx, 10*Step)
	if err != nil || len(st) != 1 || st[0].Scanned != 2 || st[0].MissingBars <= 0 {
		t.Fatal(st, err)
	}
	// Catalog refresh must preserve scanned history and schedule new instruments.
	if err = s.Register(ctx, "spot", instruments, Step, 11*Step); err != nil {
		t.Fatal(err)
	}
	// A graceful stop releases work without manufacturing a provider error.
	graceful, err := s.Claim(ctx, 10*Step)
	if err != nil || graceful == nil {
		t.Fatal(graceful, err)
	}
	if err = s.Release(ctx, *graceful); err != nil {
		t.Fatal(err)
	}
	var failures int
	if err = p.QueryRow(ctx, `SELECT failures FROM market_history_series WHERE symbol=$1`, graceful.Symbol).Scan(&failures); err != nil || failures != 0 {
		t.Fatal(failures, err)
	}
	var cursor int64
	if err = p.QueryRow(ctx, `SELECT live_to FROM market_history_series WHERE symbol=$1`, live.Symbol).Scan(&cursor); err != nil || cursor != 8*Step {
		t.Fatal(cursor, err)
	}
}
