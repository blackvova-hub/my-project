package similarity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"shortlong/backtest/marketdata"
)

func TestDemandJobsScopeOwnershipRecoveryAndIdle(t *testing.T) {
	dsn := os.Getenv("SIMILARITY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated integration services required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("test database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"064_add_market_history.sql", "065_add_similarity.sql", "066_similarity_on_demand.sql"} {
		raw, e := os.ReadFile("../api/internal/database/migrations/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, string(raw)); e != nil {
			t.Fatal(e)
		}
	}
	if _, err = db.Exec(ctx, `TRUNCATE similarity_jobs`); err != nil {
		t.Fatal(err)
	}
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	config.Windows = []Window{{12, 3}, {36, 6}}
	config.Version = fmt.Sprintf("test_jobs_%d", time.Now().UnixNano())
	store := Store{db, config}
	symbol := "DEMANDTESTUSDT"
	other := "IDLETESTUSDT"
	ch := marketdata.NewClickHouse("http://127.0.0.1:28123", "backtest", "backtest-test-only")
	if err = ch.Init(ctx); err != nil {
		t.Fatal(err)
	}
	candles := fixture(500)
	if err = ch.Insert(ctx, "linear", symbol, "5m", candles); err != nil {
		t.Fatal(err)
	}
	for _, sym := range []string{symbol, other} {
		_, err = db.Exec(ctx, `INSERT INTO market_history_series(market,symbol,history_from,history_to,backfill_to,live_to,first_available,last_available) VALUES('linear',$1,$2,$3,$2,$3,$2,$4) ON CONFLICT(market,symbol,timeframe) DO UPDATE SET first_available=EXCLUDED.first_available,last_available=EXCLUDED.last_available`, sym, candles[0].Time, candles[499].Time+Step, candles[499].Time)
		if err != nil {
			t.Fatal(err)
		}
	}
	q := NewQdrant("http://127.0.0.1:26333", "", config.Version)
	defer q.call(context.Background(), "DELETE", q.path(12), nil, nil)
	cache := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379"})
	defer cache.Close()
	search := NewSearcher(store, ch, q, cache)
	jobs := NewJobs(search, Pipeline{Store: store, Storage: ch, Index: q, Outcomes: Outcomes{ch}})
	if err = store.SyncAssets(ctx); err != nil {
		t.Fatal(err)
	}
	if v, e := jobs.claim(ctx); e != nil || v != nil {
		t.Fatal("idle service created work", v, e)
	}
	var count int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM similarity_streams WHERE version=$1`, config.Version).Scan(&count)
	if count != 0 {
		t.Fatal("catalog sync scheduled history")
	}
	req := Request{Market: "linear", Symbol: symbol, Window: 12, End: candles[410].Time, Scope: "same_asset", Limit: 6}
	id, err := jobs.Create(ctx, "owner-a", req)
	if err != nil {
		t.Fatal(err)
	}
	same, err := jobs.Create(ctx, "owner-a", req)
	if err != nil || same != id {
		t.Fatal("duplicate request not coalesced", same, err)
	}
	if _, err = jobs.Get(ctx, "owner-b", id); err != pgx.ErrNoRows {
		t.Fatal("other user read job", err)
	}
	if err = jobs.Cancel(ctx, "owner-b", id); err != pgx.ErrNoRows {
		t.Fatal("other user cancelled job", err)
	}
	req2 := req
	req2.End -= 3 * Step
	if _, err = jobs.Create(ctx, "owner-a", req2); err != ErrQueueFull {
		t.Fatal("active-user limit", err)
	}
	claimed, err := jobs.claim(ctx)
	if err != nil || claimed == nil {
		t.Fatal(claimed, err)
	}
	jobs.execute(ctx, *claimed)
	completed, err := jobs.Get(ctx, "owner-a", id)
	if err != nil || completed.Status != "completed" {
		t.Fatal("job not completed", completed, err)
	}
	if len(completed.Result.Matches) != 6 {
		t.Fatal("matches", completed.Result)
	}
	for _, m := range completed.Result.Matches {
		if m.Symbol != symbol || m.End > req.End-int64(req.Window)*Step {
			t.Fatal("scope or lookahead", m)
		}
		if len(m.Outcomes) == 0 {
			t.Fatal("outcomes not calculated on demand")
		}
		for _, o := range m.Outcomes {
			if o.AvailableAt > req.End {
				t.Fatal("future leakage")
			}
		}
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM similarity_streams WHERE version=$1 AND (symbol<>$2 OR span<>12 OR kind<>'features')`, config.Version, symbol).Scan(&count); err != nil || count != 0 {
		t.Fatal("unrequested indexing", count, err)
	}
	if p, e := store.Pending(ctx, req, []string{symbol}); e != nil || p != 0 {
		t.Fatal("coverage", p, e)
	}
	if v, e := jobs.claim(ctx); e != nil || v != nil {
		t.Fatal("idle work after request", v, e)
	}

	handler := Handler(search, q, jobs)
	for _, tc := range []struct {
		owner, id string
		want      int
	}{{"owner-a", id, 200}, {"owner-b", id, 404}, {"", id, 401}, {"owner-a", "bad", 404}} {
		r := httptest.NewRequest("GET", "/jobs/"+tc.id, nil)
		r.Header.Set("X-Similarity-User", tc.owner)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("job access %s: %d", tc.owner, w.Code)
		}
	}
	id2, err := jobs.Create(ctx, "owner-a", req2)
	if err != nil {
		t.Fatal(err)
	}
	if err = jobs.Cancel(ctx, "owner-a", id2); err != nil {
		t.Fatal(err)
	}
	if v, e := jobs.claim(ctx); e != nil || v != nil {
		t.Fatal("cancelled job claimed", v, e)
	}
	req3 := req
	req3.End -= 6 * Step
	id3, err := jobs.Create(ctx, "owner-a", req3)
	if err != nil {
		t.Fatal(err)
	}
	old, err := jobs.claim(ctx)
	if err != nil || old == nil {
		t.Fatal(old, err)
	}
	_, err = db.Exec(ctx, `UPDATE similarity_jobs SET lease_until=now()-interval '1 second' WHERE id=$1::uuid`, id3)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := jobs.claim(ctx)
	if err != nil || fresh == nil || fresh.Token == old.Token {
		t.Fatal("lease recovery", fresh, err)
	}
	if err = jobs.touch(ctx, *old, 99, "stale"); err != context.Canceled {
		t.Fatal("stale owner updated", err)
	}
	stopped, stop := context.WithCancel(ctx)
	stop()
	jobs.execute(stopped, *fresh)
	resumed, err := jobs.Get(ctx, "owner-a", id3)
	if err != nil || resumed.Status != "queued" {
		t.Fatal("shutdown lost work", resumed, err)
	}
	if err = jobs.Cancel(ctx, "owner-a", id3); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(req3)
	r := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(string(payload)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated create", w.Code)
	}
}
