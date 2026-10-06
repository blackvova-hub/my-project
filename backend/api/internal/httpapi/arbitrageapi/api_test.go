package arbitrageapi

import (
	"backend/internal/auth"
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testAPI(upstream string) *API {
	return &API{origin: "https://site.test", upstream: upstream, client: &http.Client{Timeout: time.Second}, authenticate: func(*http.Request) (auth.User, error) { return auth.User{ID: "user-a"}, nil }}
}
func TestVerificationGatewayRequiresAuthenticationAndValidRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/verification" || r.URL.Query().Get("amount") != "100" {
			t.Error(r.URL)
		}
		respond(w, 200, map[string]any{"netProfit": nil})
	}))
	defer upstream.Close()
	a := testAPI(upstream.URL)
	if w := request(a, "GET", "/arbitrage/verification?id=invalid&amount=100", "", nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
	path := "/arbitrage/verification?id=0123456789abcdef01234567&amount=100"
	if w := request(a, "GET", path, "", nil); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code)
	}
	a.authenticate = func(*http.Request) (auth.User, error) { return auth.User{}, errors.New("unauthenticated") }
	if w := request(a, "GET", path, "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func router(a *API) http.Handler { r := chi.NewRouter(); a.Routes(r); return r }
func request(a *API, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	router(a).ServeHTTP(w, r)
	return w
}
func settings() Settings {
	return Settings{Strategy: "spot-perpetual", Amount: 1000, MinSpread: .1, MaxAgeMS: 10000, HorizonHours: 24, Exchanges: []string{"binance", "bybit"}, Assets: []string{}, InterExchange: true, Sort: "spread", Limit: 100}
}
func TestStrategiesAndAvailabilityPreferences(t *testing.T) {
	s := settings()
	s.Budget = true
	s.Network = "all"
	s.LPRangePct = 10
	s.MinAvailable = 100
	for _, strategy := range []string{"spot-perpetual", "spot-spot", "perpetual-perpetual", "cex-cex", "funding", "xemm", "redemption", "hedged-lp"} {
		s.Strategy = strategy
		if !validSettings(s) {
			t.Fatal(strategy)
		}
	}
	for _, strategy := range []string{"funding|xemm", "", "made-up"} {
		s.Strategy = strategy
		if validSettings(s) {
			t.Fatal(strategy)
		}
	}
	s.Strategy = "spot-spot"
	for _, threshold := range []float64{-1, math.NaN(), math.Inf(1), 1000001} {
		s.MinAvailable = threshold
		if validSettings(s) {
			t.Fatal(threshold)
		}
	}
	s.MinAvailable = 0
	if !validSettings(s) {
		t.Fatal("zero should disable availability filtering")
	}
}
func TestUnicodeSearch(t *testing.T) {
	s := settings()
	s.Simple = true
	for _, value := range []string{"", "BTC", "1INCH", "哈基米"} {
		s.Search = value
		if !validSettings(s) {
			t.Fatalf("rejected %q", value)
		}
	}
	for _, value := range []string{"BTC/USDT", "BTC ETH", strings.Repeat("哈", 11)} {
		s.Search = value
		if validSettings(s) {
			t.Fatalf("accepted %q", value)
		}
	}
}
func TestAuthenticationAndCSRF(t *testing.T) {
	a := testAPI("")
	a.authenticate = func(*http.Request) (auth.User, error) { return auth.User{}, errors.New("unauthorized") }
	if w := request(a, "GET", "/arbitrage/opportunities", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	a = testAPI("")
	for _, h := range []map[string]string{{"Origin": "https://evil.test", "Content-Type": "application/json"}, {"Sec-Fetch-Site": "cross-site", "Content-Type": "application/json"}} {
		if w := request(a, "PUT", "/arbitrage/preferences", `{}`, h); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	if w := request(a, "PUT", "/arbitrage/preferences", `{}`, nil); w.Code != 415 {
		t.Fatal(w.Code)
	}
}
func TestProxyFixedDestinationAndFailures(t *testing.T) {
	var target string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"rows":[],"transport":"live"}`)
	}))
	defer up.Close()
	a := testAPI(up.URL)
	w := request(a, "GET", "/arbitrage/opportunities?amount=1000&url=http://evil.test", "", nil)
	if w.Code != 200 || !strings.HasPrefix(target, "/opportunities?") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, target)
	}
	if w = request(a, "GET", "/arbitrage/history?id=../../private", "", nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "not JSON") }))
	defer bad.Close()
	a.upstream = bad.URL
	if w = request(a, "GET", "/arbitrage/opportunities", "", nil); w.Code != 502 {
		t.Fatal(w.Code)
	}
	a.upstream = "http://127.0.0.1:1"
	if w = request(a, "GET", "/arbitrage/opportunities", "", nil); w.Code != 503 {
		t.Fatal(w.Code)
	}
}
func TestSettingsAndStrictBodyValidation(t *testing.T) {
	s := settings()
	if !validSettings(s) {
		t.Fatal("default invalid")
	}
	s.Exchanges = []string{"binance", "binance"}
	if validSettings(s) {
		t.Fatal("duplicate exchange accepted")
	}
	a := testAPI("")
	for _, body := range []string{`{"settings":null}`, `{"settings":{},"favorites":[]}`, `{"settings":{},"admin":true}`, `{} {}`, strings.Repeat("x", 33000)} {
		if w := request(a, "PUT", "/arbitrage/preferences", body, map[string]string{"Content-Type": "application/json"}); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
func TestPreferencesIsolatedDatabase(t *testing.T) {
	database := os.Getenv("ARBITRAGE_TEST_DATABASE_URL")
	if database == "" {
		t.Skip("set ARBITRAGE_TEST_DATABASE_URL for isolated schema integration")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "arbitrage_test_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE") }()
	cfg, err := pgxpool.ParseConfig(database)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sql, err := os.ReadFile("../../database/migrations/070_arbitrage.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	a := testAPI("")
	a.db = db
	s := settings()
	body, _ := json.Marshal(Preferences{Settings: &s, Favorites: []string{"0123456789abcdef01234567"}})
	if w := request(a, "PUT", "/arbitrage/preferences", string(body), map[string]string{"Content-Type": "application/json", "Origin": "https://site.test"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := request(a, "GET", "/arbitrage/preferences", "", nil)
	var p Preferences
	if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Settings == nil || len(p.Favorites) != 1 {
		t.Fatal(w.Body.String())
	}
	a.authenticate = func(*http.Request) (auth.User, error) { return auth.User{ID: "user-b"}, nil }
	w = request(a, "GET", "/arbitrage/preferences", "", nil)
	if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Settings != nil || len(p.Favorites) != 0 {
		t.Fatal("cross-user settings leaked", w.Body.String())
	}
	s.Amount = 500
	body, _ = json.Marshal(Preferences{Settings: &s, Favorites: []string{}})
	if w = request(a, "PUT", "/arbitrage/preferences", string(body), map[string]string{"Content-Type": "application/json"}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	a.authenticate = func(*http.Request) (auth.User, error) { return auth.User{ID: "user-a"}, nil }
	w = request(a, "GET", "/arbitrage/preferences", "", nil)
	if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Settings.Amount != 1000 || len(p.Favorites) != 1 {
		t.Fatal("cross-user write", w.Body.String())
	}
}
