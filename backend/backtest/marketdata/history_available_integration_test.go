package marketdata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHistoryLoadAvailableRealStorage(t *testing.T) {
	dsn, endpoint := os.Getenv("BACKTEST_TEST_DATABASE_URL"), os.Getenv("BACKTEST_TEST_CLICKHOUSE_URL")
	if dsn == "" || endpoint == "" {
		t.Skip("isolated PostgreSQL / ClickHouse not configured")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("requires _test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Match the other history integration tests: PostgreSQL migrations must
	// already be applied to this isolated database, not to a production schema.
	if _, err := pool.Exec(ctx, `SELECT market,symbol,timeframe FROM backtest_candle_series LIMIT 0`); err != nil {
		t.Fatalf("prepare backtest migrations in the isolated database: %v", err)
	}
	for _, tc := range []struct {
		minuteArchive bool
		timeframe     string
	}{{false, "1h"}, {true, "1h"}, {false, "1m"}, {true, "1m"}} {
		t.Run(fmt.Sprintf("minute_archive_%t_%s", tc.minuteArchive, tc.timeframe), func(t *testing.T) {
			storage := NewClickHouse(endpoint, "backtest", os.Getenv("BACKTEST_TEST_CLICKHOUSE_PASSWORD"))
			storage.MinuteArchive = tc.minuteArchive
			if err := storage.Init(ctx); err != nil {
				t.Fatal(err)
			}
			suffix := strings.ToUpper(strconv.FormatInt(time.Now().UnixNano(), 36))
			symbol, brokenSymbol := "QA"+suffix+"USDT", "QB"+suffix+"USDT"
			t.Cleanup(func() {
				cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if _, err := pool.Exec(cleanup, `DELETE FROM backtest_candle_series WHERE market=$1 AND symbol IN ($2,$3)`, "linear", symbol, brokenSymbol); err != nil {
					t.Errorf("clean up test series metadata: %v", err)
				}
			})
			const hour = int64(time.Hour / time.Millisecond)
			from := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
			to, cutoff, hole := from+3*hour, from+15*MinuteStep, from+90*MinuteStep
			sourceTF, sourceStep := "5m", int64(300000)
			if tc.minuteArchive || tc.timeframe == "1m" {
				sourceTF, sourceStep = "1m", MinuteStep
			}
			var existingRows, holeRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				requestedSymbol := query.Get("symbol")
				if query.Get("interval") != strconv.FormatInt(sourceStep/MinuteStep, 10) || (requestedSymbol != symbol && requestedSymbol != brokenSymbol) {
					http.Error(w, "unexpected provider request", http.StatusBadRequest)
					return
				}
				start, _ := strconv.ParseInt(query.Get("start"), 10, 64)
				end, _ := strconv.ParseInt(query.Get("end"), 10, 64)
				if requestedSymbol == brokenSymbol && start <= hole && end >= hole {
					holeRequests.Add(1)
				}
				fmt.Fprint(w, `{"retCode":0,"result":{"list":[`)
				first := true
				for ts := start; ts <= end; ts += sourceStep {
					if ts < cutoff || (requestedSymbol == brokenSymbol && ts == hole) {
						continue
					}
					if requestedSymbol == symbol {
						existingRows.Add(1)
					}
					if !first {
						fmt.Fprint(w, ",")
					}
					first = false
					fmt.Fprintf(w, `["%d","100","101","99","100","10"]`, ts)
				}
				fmt.Fprint(w, `]}}`)
			}))
			defer server.Close()
			history := &History{DB: pool, Storage: storage, Bybit: NewBybit(server.URL, nil)}
			first, barStep := from+hour, hour
			if tc.timeframe == "1m" {
				first, barStep = cutoff, MinuteStep
			}
			bars, err := history.LoadAvailable(ctx, "linear", symbol, tc.timeframe, from, to, nil)
			if err != nil || len(bars) != int((to-first)/barStep) {
				t.Fatalf("available tail: got %d bars, err=%v", len(bars), err)
			}
			for i, bar := range bars {
				if bar.Time != first+int64(i)*barStep || bar.Volume != float64(barStep/sourceStep)*10 {
					t.Fatalf("partial first hour was exposed or aggregation changed: %+v", bars)
				}
			}
			raw, err := storage.Read(ctx, "linear", symbol, sourceTF, from, to)
			if err != nil || len(raw) != int((to-cutoff)/sourceStep) || raw[0].Time != cutoff {
				t.Fatalf("archive must retain real rows without fabricated prefix: %d rows, err=%v", len(raw), err)
			}
			downloadedRows := existingRows.Load()
			if downloadedRows != int32(len(raw)) {
				t.Fatalf("downloaded %d provider rows, archived %d", downloadedRows, len(raw))
			}
			if _, err := history.Load(ctx, "linear", symbol, tc.timeframe, from, to, nil); err == nil {
				t.Fatal("strict Load accepted the unavailable prefix")
			}
			strictBars, err := history.Load(ctx, "linear", symbol, tc.timeframe, first, to, nil)
			if err != nil || len(strictBars) != len(bars) {
				t.Fatalf("strict Load failed on the complete real tail: %d rows, %v", len(strictBars), err)
			}
			again, err := history.LoadAvailable(ctx, "linear", symbol, tc.timeframe, from, to, nil)
			if err != nil || len(again) != len(bars) || again[0].Time != bars[0].Time {
				t.Fatalf("second available load: %+v, err=%v", again, err)
			}
			if existingRows.Load() != downloadedRows {
				t.Fatal("cached provider candles were downloaded again; only the unavailable prefix may be retried")
			}
			for attempt := 0; attempt < 2; attempt++ {
				before := holeRequests.Load()
				_, err := history.LoadAvailable(ctx, "linear", brokenSymbol, tc.timeframe, from, to, nil)
				missingDate := time.UnixMilli(hole).UTC().Format("02.01.2006 15:04")
				if err == nil || !strings.Contains(err.Error(), brokenSymbol) || !strings.Contains(err.Error(), missingDate) {
					t.Fatalf("attempt %d: source hole must fail with symbol/date, got %v", attempt+1, err)
				}
				if holeRequests.Load() <= before {
					t.Fatalf("attempt %d did not try to repair the missing source candle", attempt+1)
				}
			}
		})
	}
}
