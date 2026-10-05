package similarity

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"net/http"
	"net/http/httptest"
	"os"
	"shortlong/backtest/marketdata"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countedIndex struct {
	VectorIndex
	calls atomic.Int32
}

func (i *countedIndex) Search(ctx context.Context, n int, v []float64, m string, s []string, b int64, l int) ([]Candidate, error) {
	i.calls.Add(1)
	time.Sleep(150 * time.Millisecond)
	return i.VectorIndex.Search(ctx, n, v, m, s, b, l)
}
func TestIntegrationPipelineSearchAndCoalescing(t *testing.T) {
	dsn := os.Getenv("SIMILARITY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated integration services required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	raw, e := os.ReadFile("../api/internal/database/migrations/065_add_similarity.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	symbol := "SIMTESTUSDT"
	if _, e = db.Exec(ctx, `DELETE FROM similarity_streams WHERE symbol=$1;`, symbol); e != nil {
		t.Fatal(e)
	}
	t.Setenv("SIMILARITY_WINDOWS", "12:3")
	config, e := ConfigFromEnv()
	if e != nil {
		t.Fatal(e)
	}
	config.Version = fmt.Sprintf("test_%d", time.Now().UnixNano())
	store := Store{db, config}
	if e = ImportAssets(ctx, db, []byte(`[{"market":"linear","symbol":"SIMTESTUSDT","sectors":["test"],"isAlt":true,"isStablecoin":false,"enabled":true}]`)); e != nil {
		t.Fatal(e)
	}
	ch := marketdata.NewClickHouse("http://127.0.0.1:28123", "backtest", "backtest-test-only")
	if e = ch.Init(ctx); e != nil {
		t.Fatal(e)
	}
	candles := fixture(500)
	if e = ch.Insert(ctx, "linear", symbol, "5m", candles); e != nil {
		t.Fatal(e)
	}
	q := NewQdrant("http://127.0.0.1:26333", "", config.Version)
	if e = q.Init(ctx, config.Windows); e != nil {
		t.Fatal(e)
	}
	defer q.call(context.Background(), "DELETE", q.path(12), nil, nil)
	out := Outcomes{ch}
	if e = out.Init(ctx); e != nil {
		t.Fatal(e)
	}
	pipeline := Pipeline{store, ch, q, out}
	from := (candles[0].Time + 24*Step + 3*Step - 1) / (3 * Step) * (3 * Step)
	to := candles[400].Time
	missing, e := pipeline.Process(ctx, Task{Market: "linear", Symbol: symbol, Kind: "features", Span: 12, Stride: 3, From: from, To: to})
	if e != nil || missing {
		t.Fatal(missing, e)
	}
	missing, e = pipeline.Process(ctx, Task{Market: "linear", Symbol: symbol, Kind: "outcomes", Span: 12, Stride: 3, From: from, To: to})
	if e != nil || missing {
		t.Fatal(missing, e)
	}
	index := &countedIndex{VectorIndex: q}
	cache := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379"})
	defer cache.Close()
	search := NewSearcher(store, ch, index, cache)
	req := Request{Market: "linear", Symbol: symbol, Window: 12, End: candles[410].Time, Scope: "same_asset", Limit: 6}
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, e := search.Search(ctx, req)
			if e != nil {
				errs <- e
				return
			}
			var result Response
			if e = json.Unmarshal(raw, &result); e != nil {
				errs <- e
				return
			}
			if len(result.Matches) != 6 {
				errs <- fmt.Errorf("matches %d", len(result.Matches))
				return
			}
			for _, m := range result.Matches {
				if m.End > req.End-int64(req.Window)*Step || len(m.Chart) == 0 {
					errs <- fmt.Errorf("lookahead or absent chart")
					return
				}
				for _, o := range m.Outcomes {
					if o.AvailableAt > req.End {
						errs <- fmt.Errorf("future outcome leak")
					}
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if index.calls.Load() != 1 {
		t.Fatalf("40 requests performed %d ANN searches", index.calls.Load())
	}
	t.Run("fill only missing query candles and reuse archive", func(t *testing.T) {
		freshSymbol := fmt.Sprintf("FRESH%dUSDT", time.Now().UnixMilli())
		bars := fixture(504)
		if err := ch.Insert(ctx, "linear", freshSymbol, "5m", bars[:500]); err != nil {
			t.Fatal(err)
		}
		var downloads atomic.Int32
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			downloads.Add(1)
			p := r.URL.Query()
			if r.URL.Path != "/v5/market/kline" || p.Get("symbol") != freshSymbol || p.Get("category") != "linear" || p.Get("interval") != "5" || p.Get("start") != fmt.Sprint(bars[500].Time) || p.Get("end") != fmt.Sprint(bars[503].Time+Step-1) {
				t.Errorf("unexpected download: %s", r.URL)
			}
			rows := [][]string{}
			for _, c := range bars[500:] {
				rows = append(rows, []string{fmt.Sprint(c.Time), fmt.Sprint(c.Open), fmt.Sprint(c.High), fmt.Sprint(c.Low), fmt.Sprint(c.Close), fmt.Sprint(c.Volume)})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{"list": rows}})
		}))
		defer provider.Close()
		search.History = &marketdata.History{DB: db, Storage: ch, Bybit: marketdata.NewBybit(provider.URL, cache)}
		defer func() { search.History = nil }()
		query := Request{Market: "linear", Symbol: freshSymbol, Window: 12, End: bars[503].Time + Step, Scope: "all_crypto", Limit: 6}
		for i := 0; i < 2; i++ {
			result, err := search.compute(ctx, query, []string{symbol})
			if err != nil || len(result.Matches) == 0 {
				t.Fatalf("search after refresh: %v; matches=%d", err, len(result.Matches))
			}
		}
		if downloads.Load() != 1 {
			t.Fatalf("two computations downloaded %d times", downloads.Load())
		}
	})
	// SQL claims are exclusive; expired lease cannot advance a fresh lease's cursor.
	_, e = db.Exec(ctx, `INSERT INTO similarity_streams(version,market,symbol,kind,span,stride,target_from,target_to,scanned_from,scanned_to) VALUES($1,'linear',$2,'features',12,3,$3,$4,$4,$4)`, config.Version, symbol, from, to)
	if e != nil {
		t.Fatal(e)
	}
	a, e := store.Claim(ctx, req, []string{symbol})
	if e != nil || a == nil {
		t.Fatal(a, e)
	}
	b, e := store.Claim(ctx, req, []string{symbol})
	if e != nil || b != nil {
		t.Fatal("double claim", b, e)
	}
	_, e = db.Exec(ctx, `UPDATE similarity_streams SET lease_until=now()-interval '1 second' WHERE id=$1`, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	b, e = store.Claim(ctx, req, []string{symbol})
	if e != nil || b == nil {
		t.Fatal(e)
	}
	if e = store.Finish(ctx, *a, false); e == nil {
		t.Fatal("old owner advanced cursor")
	}
	if e = store.Finish(ctx, *b, true); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = db.QueryRow(ctx, `SELECT count(*) FROM similarity_repairs WHERE stream_id=$1`, b.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	// Discover both backward archive growth and new closes without resetting work.
	migration, e := os.ReadFile("../api/internal/database/migrations/064_add_market_history.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, string(migration)); e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(ctx, `INSERT INTO market_history_series(market,symbol,history_from,history_to,backfill_to,live_to,first_available,last_available) VALUES('linear',$1,$2,$3,$2,$3,$2,$4) ON CONFLICT(market,symbol,timeframe) DO UPDATE SET first_available=EXCLUDED.first_available,last_available=EXCLUDED.last_available`, symbol, candles[0].Time, candles[499].Time+Step, candles[499].Time)
	if e != nil {
		t.Fatal(e)
	}
	var left, right int64
	if e = db.QueryRow(ctx, `SELECT scanned_from,scanned_to FROM similarity_streams WHERE id=$1`, b.ID).Scan(&left, &right); e != nil {
		t.Fatal(e)
	}
	if e = store.Discover(ctx, Request{Market: "linear", Window: 12, End: candles[499].Time + 24*Step}, []string{symbol}); e != nil {
		t.Fatal(e)
	}
	var newLeft, newRight, target int64
	if e = db.QueryRow(ctx, `SELECT scanned_from,scanned_to,target_to FROM similarity_streams WHERE id=$1`, b.ID).Scan(&newLeft, &newRight, &target); e != nil {
		t.Fatal(e)
	}
	if left != newLeft || right != newRight || target <= right {
		t.Fatal("discovery reset progress or missed new closes")
	}
	var firstBefore int64
	_ = db.QueryRow(ctx, `SELECT target_from FROM similarity_streams WHERE id=$1`, b.ID).Scan(&firstBefore)
	_, e = db.Exec(ctx, `UPDATE market_history_series SET first_available=first_available-86400000 WHERE market='linear' AND symbol=$1`, symbol)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.Discover(ctx, Request{Market: "linear", Window: 12, End: candles[499].Time + 24*Step}, []string{symbol}); e != nil {
		t.Fatal(e)
	}
	var firstAfter int64
	_ = db.QueryRow(ctx, `SELECT target_from FROM similarity_streams WHERE id=$1`, b.ID).Scan(&firstAfter)
	if firstAfter >= firstBefore {
		t.Fatal("older archive was not discovered")
	}
}
