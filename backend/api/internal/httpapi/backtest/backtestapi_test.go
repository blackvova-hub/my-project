package backtestapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/auth"
	"github.com/go-chi/chi/v5"
	bt "shortlong/backtest"
)

func testBody() string {
	to := time.Now().UTC().Truncate(24 * time.Hour)
	r := bt.Request{Exchange: "bybit", Market: "linear", Symbols: []string{"BTCUSDT"}, Timeframe: "1h", From: to.Add(-24 * time.Hour), To: to, Strategy: bt.Strategy{Version: 1, Direction: "long", InitialCapital: 10000, PositionSizePct: 20, StopLossPct: 2, Entry: bt.Condition{Kind: "compare", Operator: "gt", Left: &bt.Operand{Kind: "close"}, Right: &bt.Operand{Kind: "constant"}}}}
	raw, _ := json.Marshal(struct {
		RequestKey string `json:"requestKey"`
		bt.Request
	}{"12345678-1234-4234-8234-123456789012", r})
	return string(raw)
}
func TestRequestBoundaryRejectsUnauthenticatedCrossSiteAndInvalidPayload(t *testing.T) {
	cases := []struct {
		name, body, contentType, origin, site string
		authenticated                         bool
		want                                  int
	}{
		{name: "authentication", body: testBody(), contentType: "application/json", want: 401},
		{name: "cross site", body: testBody(), contentType: "application/json", origin: "https://evil.example", site: "cross-site", authenticated: true, want: 403},
		{name: "sibling origin", body: testBody(), contentType: "application/json", origin: "https://other.example", site: "same-site", authenticated: true, want: 403},
		{name: "form submission", body: testBody(), contentType: "text/plain", authenticated: true, want: 415},
		{name: "malformed JSON", body: "{", contentType: "application/json", authenticated: true, want: 400},
		{name: "trailing JSON", body: testBody() + "{}", contentType: "application/json", authenticated: true, want: 400},
		{name: "unknown field", body: strings.Replace(testBody(), `"exchange":`, `"script":"evil","exchange":`, 1), contentType: "application/json", authenticated: true, want: 400},
		{name: "oversized body", body: strings.Repeat(" ", 32769) + testBody(), contentType: "application/json", authenticated: true, want: 400},
		{name: "invalid symbol", body: strings.Replace(testBody(), "BTCUSDT", "BTC;DROP", 1), contentType: "application/json", authenticated: true, want: 422},
		{name: "invalid interval", body: strings.Replace(testBody(), `"1h"`, `"1m"`, 1), contentType: "application/json", authenticated: true, want: 422},
		{name: "invalid key", body: strings.Replace(testBody(), "12345678-1234-4234-8234-123456789012", "invalid", 1), contentType: "application/json", authenticated: true, want: 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := &API{allowedOrigin: "https://short-and-long.ru", authenticate: func(*http.Request) (auth.User, error) {
				if !tc.authenticated {
					return auth.User{}, errors.New("no session")
				}
				return auth.User{ID: "12345678-1234-4234-8234-123456789012"}, nil
			}}
			router := chi.NewRouter()
			api.Routes(router)
			req := httptest.NewRequest(http.MethodPost, "/backtests", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Sec-Fetch-Site", tc.site)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}
func TestMalformedJobIDNeverReachesDatabase(t *testing.T) {
	api := &API{authenticate: func(*http.Request) (auth.User, error) {
		return auth.User{ID: "12345678-1234-4234-8234-123456789012"}, nil
	}}
	router := chi.NewRouter()
	api.Routes(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/backtests/not-a-uuid", nil))
	if response.Code != 404 {
		t.Fatal(response.Code)
	}
}

func TestAuthenticatedPlanCannotBeOverriddenByClient(t *testing.T) {
	for _, tc := range []struct {
		name, plan, body string
		status           int
	}{
		{"free two pairs", "free", strings.Replace(testBody(), `["BTCUSDT"]`, `["BTCUSDT","ETHUSDT"]`, 1), 422},
		{"missing plan falls back to free", "", strings.Replace(testBody(), `["BTCUSDT"]`, `["BTCUSDT","ETHUSDT"]`, 1), 422},
		{"standard six pairs", "standard", strings.Replace(testBody(), `["BTCUSDT"]`, `["BTCUSDT","ETHUSDT","SOLUSDT","ADAUSDT","XRPUSDT","DOGEUSDT"]`, 1), 422},
		{"pro eleven pairs", "pro", strings.Replace(testBody(), `["BTCUSDT"]`, `["AAUSDT","BBUSDT","CCUSDT","DDUSDT","EEUSDT","FFUSDT","GGUSDT","HHUSDT","IIUSDT","JJUSDT","KKUSDT"]`, 1), 422},
		{"forged body plan", "free", strings.Replace(testBody(), `"exchange":`, `"plan":"pro","exchange":`, 1), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &API{authenticate: func(*http.Request) (auth.User, error) {
				return auth.User{ID: "12345678-1234-4234-8234-123456789012", Plan: tc.plan}, nil
			}}
			router := chi.NewRouter()
			api.Routes(router)
			req := httptest.NewRequest(http.MethodPost, "/backtests?plan=pro", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Plan", "pro")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
