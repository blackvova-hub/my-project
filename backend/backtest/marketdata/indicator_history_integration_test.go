package marketdata

import (
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	bt "shortlong/backtest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIndicatorHistoryDeduplicatesAndAligns(t *testing.T) {
	dsn, endpoint := os.Getenv("BACKTEST_TEST_DATABASE_URL"), os.Getenv("BACKTEST_TEST_CLICKHOUSE_URL")
	if dsn == "" || endpoint == "" {
		t.Skip("isolated PostgreSQL / ClickHouse not configured")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("requires _test database")
	}
	ctx := context.Background()
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	storage := NewClickHouse(endpoint, "backtest", os.Getenv("BACKTEST_TEST_CLICKHOUSE_PASSWORD"))
	if err = storage.InitIndicators(ctx); err != nil {
		t.Fatal(err)
	}
	symbol := strings.ToUpper("QA" + strconv.FormatInt(time.Now().UnixNano(), 36) + "USDT")
	var calls atomic.Int32
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.HasPrefix(r.URL.Path, "/trading/") {
			gzipWriter := gzip.NewWriter(w)
			defer gzipWriter.Close()
			date := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/trading/"+symbol+"/"+symbol), ".csv.gz")
			day, err := time.Parse("2006-01-02", date)
			if err != nil {
				t.Error(err)
				return
			}
			fmt.Fprintln(gzipWriter, "timestamp,symbol,side,size,price")
			for hour := 0; hour < 24; hour++ {
				ts := day.Unix() + int64(hour)*3600
				fmt.Fprintf(gzipWriter, "%d,%s,Buy,3,100\n%d,%s,Sell,1,100\n", ts, symbol, ts, symbol)
			}
			return
		}
		from, _ := strconv.ParseInt(r.URL.Query().Get("startTime"), 10, 64)
		to, _ := strconv.ParseInt(r.URL.Query().Get("endTime"), 10, 64)
		fmt.Fprint(w, `{"retCode":0,"result":{"list":[`)
		first := true
		for ts := from; ts <= to; ts += 3600000 {
			if missing.Load() {
				continue
			}
			if !first {
				fmt.Fprint(w, ",")
			}
			first = false
			fmt.Fprintf(w, `{"timestamp":"%d","openInterest":"%d"}`, ts, ts/3600000+1000)
		}
		fmt.Fprint(w, `]}}`)
	}))
	defer server.Close()
	provider := NewBybit(server.URL, nil)
	provider.ArchiveURL = server.URL
	provider.ArchiveHTTP = server.Client()
	history := &History{DB: db, Storage: storage, Bybit: provider}
	// Three consumers request the same day concurrently. Exactly one archive and
	// one OI response should be downloaded; the others read the shared cache.
	var wg sync.WaitGroup
	failures := make(chan error, 3)
	for k := 0; k < 3; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bars := make([]bt.Candle, 6)
			for i := range bars {
				bars[i].Time = int64(i) * 4 * 3600000
			}
			e := history.enrichRange(ctx, "linear", symbol, "trades", "4h", 0, 86400000, bars, nil)
			if e == nil {
				e = history.enrichRange(ctx, "linear", symbol, "oi", "4h", 0, 86400000, bars, nil)
			}
			if e == nil && (bars[0].Metrics["tradeBuyVolume"] != 1200 || bars[0].Metrics["tradeSellVolume"] != 400 || bars[0].Metrics["openInterest"] != 1004 || bars[5].Metrics["openInterest"] != 1024) {
				e = fmt.Errorf("wrong candle-close alignment: %+v", bars)
			}
			failures <- e
		}()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("duplicate downloads: %d", calls.Load())
	}
	// Extending the range downloads the new day and does not redownload day 1.
	bars := []bt.Candle{{Time: 86400000}}
	if err = history.enrichRange(ctx, "linear", symbol, "trades", "1h", 86400000, 90000000, bars, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatal("cached day redownloaded")
	}
	// A missing provider sample must fail instead of inventing OI = 0.
	missing.Store(true)
	if err = history.enrichRange(ctx, "linear", symbol, "oi", "1h", 90000000, 93600000, []bt.Candle{{Time: 90000000}}, nil); err == nil {
		t.Fatal("missing OI silently filled")
	}
	// A strategy using only OHLCV never reaches the network or metric storage.
	before := calls.Load()
	if err = history.Enrich(ctx, bt.Request{Timeframe: "1h", Strategy: bt.Strategy{Entry: bt.Condition{Kind: "indicator", Indicator: "price", Measure: "change_pct", WindowHours: 24}}}, symbol, nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Fatal("unrequested metrics loaded")
	}
}
