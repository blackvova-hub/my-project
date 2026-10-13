package chartconfigapi

import (
	"backend/internal/auth"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func terminalFixture() json.RawMessage {
	windows := make([]map[string]any, 9)
	for n := range windows {
		windows[n] = map[string]any{"id": fmt.Sprintf("window-%d", n+1), "symbol": "BTCUSDT", "exchange": "bybit", "market": "linear", "interval": "1h", "indicators": []string{"ema", "rsi"}, "instances": []any{}, "comparisons": []any{}, "studySettings": map[string]any{"rsi": map[string]any{"period": 21}}, "panels": map[string]any{"order": []string{"rsi", "volume"}, "heights": map[string]int{"rsi": 120}}}
	}
	raw, _ := json.Marshal(map[string]any{"version": 3, "layout": 9, "active": 0, "windows": windows, "sidebarWidth": 340})
	return raw
}
func TestTerminalValidation(t *testing.T) {
	valid := terminalFixture()
	if err := validateTerminal(valid); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"null", "{}", strings.Replace(string(valid), `"version":3`, `"version":99`, 1), strings.Replace(string(valid), `"layout":9`, `"layout":10`, 1), strings.Replace(string(valid), `"symbol":"BTCUSDT"`, `"symbol":"../secret"`, 1)} {
		if validateTerminal(json.RawMessage(bad)) == nil {
			t.Fatalf("accepted invalid terminal: %.50s", bad)
		}
	}
	config := defaultConfig()
	config.Terminal = valid
	if _, err := normalizeConfig(config); err != nil {
		t.Fatal(err)
	}
}
func TestTerminalPersistenceAndStaleRevision(t *testing.T) {
	dsn := os.Getenv("CHART_TERMINAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated terminal PostgreSQL not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("charts_terminal_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `CREATE TABLE user_chart_configurations(user_id uuid PRIMARY KEY,config jsonb NOT NULL,updated_at timestamptz NOT NULL DEFAULT now())`)
	if err != nil {
		t.Fatal(err)
	}
	api := &API{db: pool}
	user := auth.User{ID: "11111111-1111-1111-1111-111111111111"}
	put := func(body any, legacy bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("PUT", "/api/chart/terminal", strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(context.WithValue(r.Context(), ctxUserKey, user))
		w := httptest.NewRecorder()
		if legacy {
			api.handlePut(w, r)
		} else {
			api.handleTerminalPut(w, r)
		}
		return w
	}
	payload := map[string]any{"workspace": terminalFixture(), "revision": "", "drawing_sets": map[string]any{"window-1:bybit:linear:BTCUSDT:1h": []ChartDrawing{{ID: 1, Type: "horizontal", Points: []DrawingPoint{{Time: 1000, Price: 100}}}}}}
	first := put(payload, false)
	if first.Code != 200 {
		t.Fatalf("first save %d %s", first.Code, first.Body.String())
	}
	var response struct {
		Revision  string          `json:"revision"`
		Workspace json.RawMessage `json:"workspace"`
	}
	json.Unmarshal(first.Body.Bytes(), &response)
	payload["revision"] = response.Revision
	second := put(payload, false)
	if second.Code != 200 {
		t.Fatalf("second save %d %s", second.Code, second.Body.String())
	}
	stale := put(payload, false)
	if stale.Code != 409 {
		t.Fatalf("stale revision status %d", stale.Code)
	}
	legacy := defaultConfig()
	legacy.DrawingSets = map[string][]ChartDrawing{}
	savedLegacy := put(legacy, true)
	if savedLegacy.Code != 200 {
		t.Fatal(savedLegacy.Body.String())
	}
	r := httptest.NewRequest("GET", "/api/chart/terminal", nil)
	r = r.WithContext(context.WithValue(r.Context(), ctxUserKey, user))
	out := httptest.NewRecorder()
	api.handleTerminalGet(out, r)
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	var restored struct {
		Workspace json.RawMessage `json:"workspace"`
	}
	json.Unmarshal(out.Body.Bytes(), &restored)
	var actual, expected any
	json.Unmarshal(restored.Workspace, &actual)
	json.Unmarshal(terminalFixture(), &expected)
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(expected)
	if string(a) != string(b) {
		t.Fatal("legacy drawing save lost terminal workspace")
	}
}
