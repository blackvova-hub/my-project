package backtestapi

import (
	"backend/internal/auth"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	bt "shortlong/backtest"
	"shortlong/backtest/jobs"
	"shortlong/backtest/marketdata"
	"strconv"
	"time"
)

type API struct {
	authenticate  func(*http.Request) (auth.User, error)
	store         jobs.Store
	bybit         *marketdata.Bybit
	allowedOrigin string
	replayArchive minuteReader
	replaySlots   chan struct{}
}
type userKey struct{}
type planKey struct{}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func New(a *auth.Store, db *pgxpool.Pool, rdb *redis.Client, bybitURL, frontendURL string) *API {
	origin := ""
	if u, err := url.Parse(frontendURL); err == nil && u.Host != "" {
		origin = u.Scheme + "://" + u.Host
	}
	return &API{authenticate: a.Authenticate, store: jobs.Store{Pool: db}, bybit: marketdata.NewBybit(bybitURL, rdb), allowedOrigin: origin,
		replayArchive: marketdata.NewClickHouse(os.Getenv("CLICKHOUSE_URL"), os.Getenv("CLICKHOUSE_USER"), os.Getenv("CLICKHOUSE_PASSWORD")), replaySlots: make(chan struct{}, 2)}
}
func (a *API) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.requireAuth)
		r.Get("/replay/symbols", a.replaySymbols)
		r.Get("/replay/candles", a.replayCandles)
		r.Get("/backtests/symbols", a.symbols)
		r.Get("/backtests", a.list)
		r.Post("/backtests", a.create)
		r.Get("/backtests/{id}", a.get)
		r.Post("/backtests/{id}/cancel", a.cancel)
		r.Get("/backtests/{id}/trades", a.trades)
		r.Get("/backtests/{id}/trades.csv", a.csv)
	})
}
func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := a.authenticate(r)
		if err != nil {
			respond(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		// JSON mutations plus browser Fetch Metadata block cross-site form requests.
		if r.Method == http.MethodPost && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			respond(w, 403, map[string]string{"error": "cross_site_request"})
			return
		}
		if r.Method == http.MethodPost {
			contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if contentType != "application/json" {
				respond(w, 415, map[string]string{"error": "Ожидается application/json"})
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && origin != a.allowedOrigin {
				respond(w, 403, map[string]string{"error": "cross_site_request"})
				return
			}
		}
		if id := chi.URLParam(r, "id"); id != "" && !uuidPattern.MatchString(id) {
			respond(w, 404, map[string]string{"error": "Тест не найден"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		ctx = context.WithValue(ctx, planKey{}, user.Plan)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, userKey{}, user.ID)))
	})
}
func user(r *http.Request) string { return r.Context().Value(userKey{}).(string) }
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		respond(w, 404, map[string]string{"error": "Тест не найден"})
		return
	}
	log.Printf("backtest api: %v", err)
	respond(w, 503, map[string]string{"error": "Сервис тестирования временно недоступен"})
}
func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RequestKey string `json:"requestKey"`
		bt.Request
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		respond(w, 400, map[string]string{"error": "Некорректный JSON стратегии"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		respond(w, 400, map[string]string{"error": "Ожидается один JSON-объект"})
		return
	}
	if !uuidPattern.MatchString(input.RequestKey) {
		respond(w, 400, map[string]string{"error": "Некорректный идентификатор запроса"})
		return
	}
	plan, _ := r.Context().Value(planKey{}).(string)
	if err := input.Request.ValidateForPlan(time.Now().UTC(), plan); err != nil {
		respond(w, 422, map[string]string{"error": err.Error()})
		return
	}
	id, err := a.store.Create(r.Context(), user(r), input.RequestKey, input.Request)
	if errors.Is(err, jobs.ErrActive) || errors.Is(err, jobs.ErrCapacity) {
		respond(w, 429, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, 202, map[string]string{"jobId": id})
}
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.List(r.Context(), user(r))
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, map[string]any{"jobs": list})
}
func (a *API) get(w http.ResponseWriter, r *http.Request) {
	j, err := a.store.Get(r.Context(), user(r), chi.URLParam(r, "id"))
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, j)
}
func (a *API) cancel(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Cancel(r.Context(), user(r), chi.URLParam(r, "id")); err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (a *API) symbols(w http.ResponseWriter, r *http.Request) {
	market := r.URL.Query().Get("market")
	if market != "spot" && market != "linear" {
		respond(w, 422, map[string]string{"error": "Некорректный рынок"})
		return
	}
	symbols, err := a.bybit.Symbols(r.Context(), market)
	if err != nil {
		respond(w, 502, map[string]string{"error": err.Error()})
		return
	}
	respond(w, 200, map[string]any{"symbols": symbols})
}
func (a *API) trades(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	j, err := a.store.Get(r.Context(), user(r), id)
	if err != nil {
		failure(w, err)
		return
	}
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		page, err = strconv.Atoi(p)
		if err != nil || page < 1 || page > 2500 {
			respond(w, 422, map[string]string{"error": "Некорректная страница"})
			return
		}
	}
	list, err := a.store.Trades(r.Context(), user(r), id, (page-1)*20, 20)
	if err != nil {
		failure(w, err)
		return
	}
	total := 0
	if j.Result != nil {
		total = j.Result.Metrics.TradeCount
	}
	respond(w, 200, map[string]any{"trades": list, "total": total, "page": page})
}
func (a *API) csv(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	j, err := a.store.Get(r.Context(), user(r), id)
	if err != nil {
		failure(w, err)
		return
	}
	if j.Status != "completed" {
		respond(w, 409, map[string]string{"error": "Тест ещё не завершён"})
		return
	}
	trades, err := a.store.Trades(r.Context(), user(r), id, 0, 50000)
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="backtest-`+id+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"Symbol", "Direction", "Entry UTC", "Exit UTC", "Entry price", "Exit price", "Quantity", "Fees USDT", "PnL USDT", "PnL %", "Reason"})
	num := func(v float64) string { return strconv.FormatFloat(v, 'f', 8, 64) }
	for _, t := range trades {
		_ = writer.Write([]string{t.Symbol, t.Direction, time.UnixMilli(t.EntryTime).UTC().Format(time.RFC3339), time.UnixMilli(t.ExitTime).UTC().Format(time.RFC3339), num(t.EntryPrice), num(t.ExitPrice), num(t.Quantity), num(t.Fees), num(t.PnL), num(t.PnLPct), t.Reason})
	}
	writer.Flush()
}
