// Package arbitrageapi is an authenticated gateway, not an exchange collector.
package arbitrageapi

import (
	"backend/internal/auth"
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"math"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type API struct {
	db               *pgxpool.Pool
	authenticate     func(*http.Request) (auth.User, error)
	origin, upstream string
	client           *http.Client
}
type ownerKey struct{}
type Settings struct {
	Simple        bool     `json:"simple"`
	Search        string   `json:"search,omitempty"`
	Strategy      string   `json:"strategy"`
	Amount        float64  `json:"amount"`
	MinSpread     float64  `json:"minSpread"`
	MaxAgeMS      int64    `json:"maxAgeMs"`
	HorizonHours  int      `json:"horizonHours"`
	ExitSpread    float64  `json:"exitSpread"`
	FeeSpot       *float64 `json:"feeSpot,omitempty"`
	FeePerpetual  *float64 `json:"feePerpetual,omitempty"`
	Exchanges     []string `json:"exchanges"`
	Assets        []string `json:"assets"`
	InterExchange bool     `json:"interExchange"`
	Sort          string   `json:"sort"`
	Limit         int      `json:"limit"`
}
type Preferences struct {
	Settings  *Settings `json:"settings"`
	Favorites []string  `json:"favorites"`
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)
var assetPattern = regexp.MustCompile(`^[A-Z0-9]{1,30}$`)
var searchPattern = regexp.MustCompile(`^[\p{L}\p{N}]{1,30}$`)

func validSettings(s Settings) bool {
	if len(s.Search) > 30 || (s.Search != "" && !searchPattern.MatchString(s.Search)) {
		return false
	}
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	if s.Strategy != "spot-perpetual" && s.Strategy != "perpetual-perpetual" && s.Strategy != "spot-spot" {
		return false
	}
	if !finite(s.Amount) || s.Amount < 10 || s.Amount > 1000000 || !finite(s.MinSpread) || s.MinSpread < -10 || s.MinSpread > 100 || !finite(s.ExitSpread) || math.Abs(s.ExitSpread) > 50 || s.MaxAgeMS < 1000 || s.MaxAgeMS > 30000 || s.HorizonHours < 1 || s.HorizonHours > 168 || s.Limit < 1 || s.Limit > 300 {
		return false
	}
	for _, f := range []*float64{s.FeeSpot, s.FeePerpetual} {
		if f != nil && (!finite(*f) || *f < 0 || *f > 2) {
			return false
		}
	}
	if s.Sort != "spread" && s.Sort != "net" && s.Sort != "funding" && s.Sort != "duration" {
		return false
	}
	if len(s.Exchanges) < 1 || len(s.Exchanges) > 6 || len(s.Assets) > 100 {
		return false
	}
	seen := map[string]bool{}
	for _, e := range s.Exchanges {
		if seen[e] || !strings.Contains("|binance|bybit|okx|bitget|gateio|kucoin|", "|"+e+"|") {
			return false
		}
		seen[e] = true
	}
	for _, a := range s.Assets {
		if !assetPattern.MatchString(a) {
			return false
		}
	}
	return true
}
func New(store *auth.Store, db *pgxpool.Pool, upstream, origin string) *API {
	if upstream == "" {
		upstream = "http://arbitrage_engine:8093"
	}
	return &API{db: db, authenticate: store.Authenticate, origin: strings.TrimRight(origin, "/"), upstream: strings.TrimRight(upstream, "/"), client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, msg string) {
	respond(w, code, map[string]string{"error": msg})
}
func (a *API) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.authenticate(r)
		if err != nil {
			fail(w, 401, "Войдите для доступа к арбитражу")
			return
		}
		if r.Method != "GET" {
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != a.origin) {
				fail(w, 403, "Cross-site request rejected")
				return
			}
			media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if media != "application/json" {
				fail(w, 415, "JSON required")
				return
			}
		}
		ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), ownerKey{}, u.ID), 10*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (a *API) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.guard)
		r.Get("/arbitrage/opportunities", a.opportunities)
		r.Get("/arbitrage/history", a.history)
		r.Get("/arbitrage/preferences", a.preferences)
		r.Put("/arbitrage/preferences", a.save)
	})
}
func (a *API) proxy(w http.ResponseWriter, r *http.Request, path string) {
	if len(r.URL.RawQuery) > 8192 {
		fail(w, 400, "Слишком длинный запрос")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), "GET", a.upstream+path+"?"+r.URL.RawQuery, nil)
	if err != nil {
		fail(w, 503, "Сервис арбитража недоступен")
		return
	}
	res, err := a.client.Do(req)
	if err != nil {
		fail(w, 503, "Сервис арбитража недоступен")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 400 {
		fail(w, 503, "Сервис арбитража недоступен")
		return
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if err != nil || len(body) > 8<<20 || !json.Valid(body) {
		fail(w, 502, "Некорректный ответ сервиса арбитража")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(body)
}
func (a *API) opportunities(w http.ResponseWriter, r *http.Request) { a.proxy(w, r, "/opportunities") }
func (a *API) history(w http.ResponseWriter, r *http.Request) {
	if !idPattern.MatchString(r.URL.Query().Get("id")) {
		fail(w, 400, "Некорректная связка")
		return
	}
	a.proxy(w, r, "/history")
}
func (a *API) preferences(w http.ResponseWriter, r *http.Request) {
	var settings, favorites []byte
	err := a.db.QueryRow(r.Context(), `SELECT settings,favorites FROM arbitrage_preferences WHERE user_id=$1`, r.Context().Value(ownerKey{})).Scan(&settings, &favorites)
	if err == pgx.ErrNoRows {
		respond(w, 200, map[string]any{"settings": nil, "favorites": []string{}})
		return
	}
	if err != nil {
		fail(w, 503, "Не удалось загрузить настройки")
		return
	}
	// The initial empty object is never treated as a valid search configuration.
	var s Settings
	var p Preferences
	if json.Unmarshal(settings, &s) == nil && validSettings(s) {
		p.Settings = &s
	}
	_ = json.Unmarshal(favorites, &p.Favorites)
	if p.Favorites == nil {
		p.Favorites = []string{}
	}
	respond(w, 200, p)
}
func (a *API) save(w http.ResponseWriter, r *http.Request) {
	var p Preferences
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(&struct{}{}) != io.EOF || p.Settings == nil || !validSettings(*p.Settings) || len(p.Favorites) > 200 {
		fail(w, 400, "Некорректные настройки")
		return
	}
	ids := map[string]bool{}
	for _, id := range p.Favorites {
		if !idPattern.MatchString(id) || ids[id] {
			fail(w, 400, "Некорректное избранное")
			return
		}
		ids[id] = true
	}
	if p.Favorites == nil {
		p.Favorites = []string{}
	}
	settings, _ := json.Marshal(p.Settings)
	favorites, _ := json.Marshal(p.Favorites)
	_, err := a.db.Exec(r.Context(), `INSERT INTO arbitrage_preferences(user_id,settings,favorites) VALUES($1,$2,$3) ON CONFLICT(user_id) DO UPDATE SET settings=EXCLUDED.settings,favorites=EXCLUDED.favorites,updated_at=now()`, r.Context().Value(ownerKey{}), settings, favorites)
	if err != nil {
		fail(w, 503, "Не удалось сохранить настройки")
		return
	}
	respond(w, 200, p)
}
