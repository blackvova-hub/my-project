package analytics

import (
	"backend/internal/auth"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Opt-in integration check: all writes live in a new, isolated schema, which is
// dropped after the test. It never changes application users or account history.
func TestDatabaseIsolationAndIdempotency(t *testing.T) {
	url := os.Getenv("ANALYTICS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("integration database not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("analytics_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migration, err := os.ReadFile("../../database/migrations/069_trader_analytics.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	a := &API{db: db, origin: "https://test.local", authenticate: func(r *http.Request) (auth.User, error) { return auth.User{ID: r.Header.Get("X-Test-Owner")}, nil }}
	var idA, idB string
	for _, owner := range []string{"a", "b"} {
		var id string
		if err = db.QueryRow(ctx, `INSERT INTO analytics_connections(user_id,exchange,account_type,name,credentials,fingerprint) VALUES($1,'bybit','unified','fixture',$2,$1) RETURNING id::text`, owner, []byte("encrypted-fixture")).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if owner == "a" {
			idA = id
		} else {
			idB = id
		}
	}
	f1 := fill("open", "BUY", 1, 100, 1, 1000)
	f1.ConnectionID = idA
	f2 := fill("close", "SELL", 1, 110, 1, 2000)
	f2.ConnectionID = idA
	result := syncResult{Events: []sourceEvent{{ID: f1.ID, Kind: "fill", At: f1.At, Currency: "USDT", Amount: "0", Normalized: f1, Raw: map[string]string{"original": "retained"}}, {ID: f2.ID, Kind: "fill", At: f2.At, Currency: "USDT", Amount: "0", Normalized: f2, Raw: map[string]string{"original": "retained"}}}, Snapshot: Snapshot{ConnectionID: idA, At: 3000, Equity: 108, Wallet: 108, Assets: []Asset{}, Positions: []Position{}}, Warnings: []string{}}
	// Simulate correcting a previously imported normalization, while retaining
	// the source event and its stable identity.
	incorrect := f2
	incorrect.FeeKnown = false
	result.Events[1].Normalized = incorrect
	if err = a.commitSync(ctx, Connection{ID: idA}, time.UnixMilli(3000), result); err != nil {
		t.Fatal(err)
	}
	result.Events[1].Normalized = f2
	for i := 0; i < 2; i++ {
		if err = a.commitSync(ctx, Connection{ID: idA}, time.UnixMilli(3000), result); err != nil {
			t.Fatal(err)
		}
	}
	da, err := a.load(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	dbb, err := a.load(ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	if len(da.Trades) != 1 || !da.Trades[0].Complete || len(da.Snapshots) != 1 || len(dbb.Trades) != 0 || len(dbb.Connections) != 1 || dbb.Connections[0].ID != idB {
		t.Fatal("deduplication or owner isolation failed")
	}
	closeTo(t, da.Trades[0].Net, 8)
	router := chi.NewRouter()
	a.Routes(router)
	request := func(method, path, owner, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Test-Owner", owner)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://test.local")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	note := fmt.Sprintf(`{"tradeId":%q,"tag":"verified","strategy":"test"}`, da.Trades[0].ID)
	if w := request("PUT", "/analytics/notes", "b", note); w.Code != 404 {
		t.Fatalf("foreign annotation: %d", w.Code)
	}
	if w := request("PUT", "/analytics/notes", "a", note); w.Code != 200 {
		t.Fatalf("own annotation: %d %s", w.Code, w.Body.String())
	}
	if w := request("DELETE", "/analytics/connections/"+idA, "b", ""); w.Code != 404 {
		t.Fatal("foreign deletion accepted")
	}
	if w := request("GET", "/analytics", "a", ""); w.Code != 200 || strings.Contains(w.Body.String(), "encrypted-fixture") || strings.Contains(w.Body.String(), "credentials") {
		t.Fatal("dataset response leaked credentials or failed")
	}
	if _, err = db.Exec(ctx, `UPDATE analytics_connections SET last_sync=now()-interval '1 hour',status='ready' WHERE id=$1`, idA); err != nil {
		t.Fatal(err)
	}
	if w := request("POST", "/analytics/connections/"+idA+"/sync", "b", `{"fullHistory":true}`); w.Code != 409 {
		t.Fatal("foreign history import accepted")
	}
	if w := request("POST", "/analytics/connections/"+idA+"/sync", "a", `{"fullHistory":true}`); w.Code != 202 {
		t.Fatalf("history import failed: %d %s", w.Code, w.Body.String())
	}
	var queued bool
	if err = db.QueryRow(ctx, `SELECT status='queued' AND synced_through IS NULL AND coverage_from<now()-interval '700 days' FROM analytics_connections WHERE id=$1`, idA).Scan(&queued); err != nil || !queued {
		t.Fatal("history range was not reset", err)
	}
	if w := request("POST", "/analytics/connections/"+idA+"/sync", "a", `{"fullHistory":true}`); w.Code != 409 {
		t.Fatal("duplicate queued history import accepted")
	}
	var retained int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM analytics_events WHERE connection_id=$1`, idA).Scan(&retained); err != nil || retained != 2 {
		t.Fatal("backfill erased existing history", err, retained)
	}
	if w := request("DELETE", "/analytics/connections/"+idA, "a", ""); w.Code != 200 {
		t.Fatal("own deletion failed")
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM analytics_events`).Scan(&count); err != nil || count != 0 {
		t.Fatal("event cascade failed", err, count)
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM analytics_snapshots`).Scan(&count); err != nil || count != 0 {
		t.Fatal("snapshot cascade failed", err, count)
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM analytics_trade_notes`).Scan(&count); err != nil || count != 0 {
		t.Fatal("annotation cleanup failed", err, count)
	}
}
