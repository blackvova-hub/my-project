package macroapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxResponseBytes = 16 << 20
const macroInterval = 6 * time.Hour
const calendarInterval = time.Hour
const marketInterval = 5 * time.Minute
const sentimentInterval = time.Hour

type API struct {
	db            *pgxpool.Pool
	client        *http.Client
	mu            sync.RWMutex
	state         snapshot
	now           func() time.Time
	lastRun       map[string]time.Time
	lastPersisted map[string]string
	running       bool
	fredKey       string
	blsKey        string
}

func New(db *pgxpool.Pool) *API {
	a := &API{db: db, client: &http.Client{Timeout: 18 * time.Second}, now: time.Now, lastRun: map[string]time.Time{}, lastPersisted: map[string]string{}, fredKey: strings.TrimSpace(os.Getenv("FRED_API_KEY")), blsKey: strings.TrimSpace(os.Getenv("BLS_API_KEY")), state: snapshot{Series: map[string]Series{}, Events: []Event{}, Markets: []MarketItem{}, Sources: map[string]SourceHealth{}}}
	for _, s := range catalog() {
		a.state.Series[s.ID] = s
	}
	for _, h := range []SourceHealth{
		{ID: "fred", Name: "FRED · официальные макроданные", URL: "https://fred.stlouisfed.org/", Status: "pending", Message: "Ожидание первой синхронизации"},
		{ID: "bls-data", Name: "BLS · резервный API наблюдений", URL: "https://www.bls.gov/developers/", Status: "pending", Message: "Используется при недоступности FRED"},
		{ID: "ecb", Name: "ECB · SDMX", URL: "https://data.ecb.europa.eu/", Status: "pending", Message: "Ожидание первой синхронизации"},
		{ID: "bls-calendar", Name: "BLS · календарь публикаций", URL: blsCalendarURL, Status: "pending", Message: "Ожидание первой синхронизации"},
		{ID: "bea-calendar", Name: "BEA · календарь публикаций", URL: beaCalendarURL, Status: "pending", Message: "Ожидание первой синхронизации"},
		{ID: "fed-calendar", Name: "Federal Reserve · FOMC", URL: fedCalendarURL, Status: "pending", Message: "Ожидание первой синхронизации"},
		{ID: "crypto-markets", Name: "Bybit / Binance · spot", URL: "https://bybit-exchange.github.io/docs/v5/market/tickers", Status: "pending", Message: "Ожидание первой синхронизации"},
		{ID: "alternative-sentiment", Name: "Alternative.me · Bitcoin Fear & Greed", URL: sentimentSourceURL, Status: "pending", Message: "Ожидание первой синхронизации; дневной индекс настроений Bitcoin"},
		{ID: "licensed-markets", Name: "Индексы и золото", URL: "https://fred.stlouisfed.org/legal/terms/", Status: "unavailable", Message: "Для S&P 500, Nasdaq, ICE DXY и золота нужен источник с разрешением на распространение котировок; значения не подставляются."},
	} {
		a.state.Sources[h.ID] = h
	}
	a.loadPersistent()
	return a
}

// Run owns all upstream refreshes. Reading HTTP endpoints never consumes an
// upstream quota, and startup restores the last successful persisted snapshot.
func (a *API) Run(ctx context.Context) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return
	}
	a.running = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.running = false; a.mu.Unlock() }()
	a.refresh(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.refresh(ctx)
		}
	}
}

func (a *API) refresh(ctx context.Context) {
	var wg sync.WaitGroup
	refreshed := false
	jobs := []struct {
		key      string
		interval time.Duration
		run      func(context.Context)
	}{
		{"fred", macroInterval, a.refreshFRED}, {"ecb", macroInterval, a.refreshECB},
		{"bls-calendar", calendarInterval, func(c context.Context) { a.refreshICS(c, "bls-calendar", blsCalendarURL, "BLS") }},
		{"bea-calendar", calendarInterval, func(c context.Context) { a.refreshICS(c, "bea-calendar", beaCalendarURL, "BEA") }},
		{"fed-calendar", calendarInterval, a.refreshFOMC}, {"crypto-markets", marketInterval, a.refreshMarkets},
		{"alternative-sentiment", sentimentInterval, a.refreshSentiment},
	}
	for _, j := range jobs {
		a.mu.Lock()
		last := a.lastRun[j.key]
		due := refreshDue(j.key, a.state.Sources[j.key].Status, last, a.now(), j.interval)
		if due {
			a.lastRun[j.key] = a.now()
		}
		a.mu.Unlock()
		if due {
			refreshed = true
			wg.Add(1)
			go func(run func(context.Context)) { defer wg.Done(); run(ctx) }(j.run)
		}
	}
	wg.Wait()
	if refreshed {
		a.persist(ctx)
	}
}

// Calendars have no daily observation API quota. Recover from transient network
// failures after five minutes while preserving the hourly successful schedule
// and the six-hour FRED/BLS observation request budgets across restarts.
func refreshDue(key, status string, last, now time.Time, interval time.Duration) bool {
	if strings.HasSuffix(key, "-calendar") && (status == "unavailable" || status == "stale") && interval > 5*time.Minute {
		interval = 5 * time.Minute
	}
	return last.IsZero() || now.Sub(last) >= interval
}

func (a *API) get(ctx context.Context, rawURL string) ([]byte, error) {
	return a.request(ctx, http.MethodGet, rawURL, nil)
}
func (a *API) request(ctx context.Context, method, rawURL string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, errors.New("Некорректный адрес источника")
	}
	req.Header.Set("User-Agent", "ShortLong-NewsMacro/1.0 (+https://shortlong.ru)")
	req.Header.Set("Accept", "application/json,text/csv,text/calendar,text/html;q=0.8")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Источник не ответил за отведённое время или недоступен")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("Источник вернул HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return nil, errors.New("Не удалось прочитать ответ источника")
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("Ответ источника превышает допустимый размер")
	}
	return data, nil
}

func (a *API) health(id string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h := a.state.Sources[id]
	h.LastAttempt = ptr(a.now().UTC().Format(time.RFC3339))
	if err == nil {
		h.LastSuccess = h.LastAttempt
		h.Status = "ok"
		h.Message = "Синхронизировано; дата наблюдения может отличаться от даты загрузки"
	} else {
		h.Message = err.Error()
		h.Status = "unavailable"
		if h.LastSuccess != nil {
			h.Status = "stale"
			h.Message += ". Сохранены последние успешно полученные данные"
		}
	}
	a.state.Sources[id] = h
}

func (a *API) applyPoints(id string, points []Point, source, sourceURL string) {
	points = normalizePoints(points)
	if len(points) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.state.Series[id]
	if !ok {
		return
	}
	// A quota-limited BLS fallback covers ten years. Preserve older already
	// verified history while accepting the provider's revisions inside its
	// returned window.
	if len(s.Points) > 0 && points[0].Date > s.Points[0].Date {
		older := []Point{}
		for _, p := range s.Points {
			if p.Date < points[0].Date {
				older = append(older, p)
			}
		}
		points = append(older, points...)
	}
	s.Points = points
	s.Source = source
	s.SourceURL = sourceURL
	s.LastUpdated = ptr(a.now().UTC().Format(time.RFC3339))
	s.Status = "fresh"
	s.Latest = ptr(points[len(points)-1].Value)
	s.ObservationDate = ptr(points[len(points)-1].Date)
	// Publication lag differs by frequency; a successful network request
	// must not turn a years-old discontinued observation into fresh data.
	latestDate, _ := time.Parse("2006-01-02", *s.ObservationDate)
	maxAge := time.Duration(0)
	switch s.Frequency {
	case "daily":
		maxAge = 7 * 24 * time.Hour
	case "weekly":
		maxAge = 21 * 24 * time.Hour
	case "monthly":
		maxAge = 80 * 24 * time.Hour
	case "quarterly":
		maxAge = 200 * 24 * time.Hour
	}
	if maxAge > 0 && a.now().Sub(latestDate) > maxAge {
		s.Status = "stale"
	}
	if len(points) > 1 {
		s.Previous = ptr(points[len(points)-2].Value)
		s.Change = ptr(*s.Latest - *s.Previous)
	}
	a.state.Series[id] = s
}

func (a *API) staleSeries(ids []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ids {
		s := a.state.Series[id]
		if len(s.Points) > 0 {
			s.Status = "stale"
		}
		a.state.Series[id] = s
	}
}

func normalizePoints(points []Point) []Point {
	seen := map[string]float64{}
	for _, p := range points {
		if _, err := time.Parse("2006-01-02", p.Date); err == nil && finite(p.Value) {
			seen[p.Date] = p.Value
		}
	}
	result := make([]Point, 0, len(seen))
	for date, value := range seen {
		result = append(result, Point{date, value})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result
}

func (a *API) Routes(r chi.Router) {
	r.Get("/macro/overview", a.handleOverview)
	r.Get("/macro/series", a.handleSeries)
	r.Get("/macro/calendar", a.handleCalendar)
	r.Get("/macro/markets", a.handleMarkets)
}

func (a *API) clone() snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s := snapshot{Series: map[string]Series{}, Sources: map[string]SourceHealth{}, Events: append([]Event{}, a.state.Events...), Markets: append([]MarketItem{}, a.state.Markets...)}
	if a.state.Sentiment != nil {
		copy := *a.state.Sentiment
		s.Sentiment = &copy
	}
	for k, v := range a.state.Series {
		s.Series[k] = v
	}
	for k, v := range a.state.Sources {
		s.Sources[k] = v
	}
	return s
}

func sourceList(s snapshot) []SourceHealth {
	h := make([]SourceHealth, 0, len(s.Sources))
	for _, v := range s.Sources {
		h = append(h, v)
	}
	sort.Slice(h, func(i, j int) bool { return h[i].ID < h[j].ID })
	return h
}

// asOf is the last successful collection of the relevant providers, never
// the HTTP response clock. Reopening the UI cannot make old data look new.
func syncAsOf(s snapshot, ids ...string) string {
	latest := time.Time{}
	for _, id := range ids {
		h, ok := s.Sources[id]
		if !ok || h.LastSuccess == nil {
			continue
		}
		at, err := time.Parse(time.RFC3339, *h.LastSuccess)
		if err == nil && at.After(latest) {
			latest = at
		}
	}
	if latest.IsZero() {
		return ""
	}
	return latest.UTC().Format(time.RFC3339)
}
func (a *API) handleOverview(w http.ResponseWriter, r *http.Request) {
	s := a.clone()
	items := make([]Series, 0, len(s.Series))
	for _, c := range catalog() {
		v := s.Series[c.ID]
		if len(v.Points) > 90 {
			v.Points = v.Points[len(v.Points)-90:]
		}
		items = append(items, v)
	}
	now := a.now().UTC()
	events := filterEvents(s.Events, now.AddDate(0, 0, -7), now.AddDate(0, 1, 0), false, now)
	a.write(w, map[string]any{"series": items, "events": events, "sources": sourceList(s), "asOf": syncAsOf(s, "fred", "bls-data", "ecb", "bls-calendar", "bea-calendar", "fed-calendar")})
}
func (a *API) handleSeries(w http.ResponseWriter, r *http.Request) {
	s := a.clone()
	v, ok := s.Series[r.URL.Query().Get("id")]
	if !ok {
		http.Error(w, "Unknown macro series", http.StatusBadRequest)
		return
	}
	rangeName := r.URL.Query().Get("range")
	if rangeName == "" {
		rangeName = "2y"
	}
	years := 0
	switch rangeName {
	case "1y":
		years = 1
	case "2y":
		years = 2
	case "5y":
		years = 5
	case "all":
	default:
		http.Error(w, "range must be 1y, 2y, 5y or all", http.StatusBadRequest)
		return
	}
	if years > 0 {
		from := a.now().AddDate(-years, 0, 0).Format("2006-01-02")
		points := []Point{}
		for _, p := range v.Points {
			if p.Date >= from {
				points = append(points, p)
			}
		}
		v.Points = points
	}
	asOf := ""
	if v.LastUpdated != nil {
		asOf = *v.LastUpdated
	}
	a.write(w, map[string]any{"series": v, "sources": sourceList(s), "asOf": asOf})
}
func (a *API) handleCalendar(w http.ResponseWriter, r *http.Request) {
	from, to, dateOnly, err := calendarBounds(r.URL.Query(), a.now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s := a.clone()
	a.write(w, map[string]any{"events": filterEvents(s.Events, from, to, dateOnly, a.now()), "sources": sourceList(s), "asOf": syncAsOf(s, "bls-calendar", "bea-calendar", "fed-calendar")})
}
func (a *API) handleMarkets(w http.ResponseWriter, r *http.Request) {
	s := a.clone()
	items := append([]MarketItem{}, s.Markets...)
	for _, id := range []string{"DTWEXBGS", "DGS2", "DGS10", "ECB_EURUSD"} {
		v := s.Series[id]
		p := v.Points
		if len(p) > 30 {
			p = p[len(p)-30:]
		}
		items = append(items, MarketItem{Symbol: id, Name: v.Title, AssetClass: v.Category, Price: v.Latest, ChangePercent: nil, Unit: v.Unit, Source: v.Source, SourceURL: v.SourceURL, ObservedAt: v.ObservationDate, Points: p, Status: v.Status, Note: "Официальное наблюдение; изменение к предыдущей публикации доступно в Macro Analysis. Это не котировка в реальном времени."})
	}
	for _, v := range []struct{ id, name, kind string }{{"SPX", "S&P 500", "equities"}, {"NDX", "NASDAQ 100", "equities"}, {"DXY", "ICE US Dollar Index", "currencies"}, {"XAUUSD", "Золото", "commodities"}} {
		unit := "пункты индекса"
		if v.id == "XAUUSD" {
			unit = "USD / тройская унция"
		}
		items = append(items, MarketItem{Symbol: v.id, Name: v.name, AssetClass: v.kind, Unit: unit, Source: "Источник котировок не подключён", SourceURL: "https://fred.stlouisfed.org/legal/terms/", Points: []Point{}, Status: "unavailable", Note: "Нет доступного источника с разрешением на распространение этих котировок. Для доллара отдельно доступен широкий индекс Federal Reserve; он не является DXY."})
	}
	a.write(w, map[string]any{"items": items, "sentiment": s.Sentiment, "sources": sourceList(s), "asOf": syncAsOf(s, "fred", "bls-data", "ecb", "crypto-markets", "alternative-sentiment")})
}
func (a *API) write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=30")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("macro: response encoding failed")
	}
}

func calendarBounds(q url.Values, now time.Time) (time.Time, time.Time, bool, error) {
	fromRaw, toRaw := q.Get("from"), q.Get("to")
	if fromRaw == "" {
		fromRaw = now.UTC().Format("2006-01-02")
	}
	if toRaw == "" {
		toRaw = now.AddDate(0, 1, 0).UTC().Format("2006-01-02")
	}
	dateOnly := len(fromRaw) == 10 && len(toRaw) == 10
	layout := time.RFC3339
	if dateOnly {
		layout = "2006-01-02"
	}
	from, e1 := time.Parse(layout, fromRaw)
	to, e2 := time.Parse(layout, toRaw)
	if e1 != nil || e2 != nil {
		return from, to, dateOnly, errors.New("from/to must both be YYYY-MM-DD or RFC3339 timestamps")
	}
	if dateOnly {
		to = to.AddDate(0, 0, 1)
	}
	if !to.After(from) || to.Sub(from) > 370*24*time.Hour {
		return from, to, dateOnly, errors.New("Calendar range must be positive and at most 370 days")
	}
	return from, to, dateOnly, nil
}

func filterEvents(events []Event, from, to time.Time, _ bool, now time.Time) []Event {
	result := []Event{}
	for _, e := range events {
		at, err := time.Parse("2006-01-02", e.Date)
		if e.ScheduledAt != nil {
			at, err = time.Parse(time.RFC3339, *e.ScheduledAt)
		}
		if err != nil {
			continue
		}
		include := !at.Before(from) && at.Before(to)
		if e.TimePrecision == "date" {
			include = e.Date >= from.UTC().Format("2006-01-02") && e.Date <= to.Add(-time.Nanosecond).UTC().Format("2006-01-02")
		}
		if !include {
			continue
		}
		if e.Status != "cancelled" && e.Status != "released" {
			if e.TimePrecision == "exact" {
				if !at.After(now) {
					e.Status = "elapsed"
				} else {
					if e.Status != "postponed" {
						e.Status = "upcoming"
					}
				}
			} else if e.Date < now.UTC().Format("2006-01-02") {
				e.Status = "elapsed"
			}
		}
		result = append(result, e)
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i].Date, result[j].Date
		if result[i].ScheduledAt != nil {
			a = *result[i].ScheduledAt
		}
		if result[j].ScheduledAt != nil {
			b = *result[j].ScheduledAt
		}
		return a < b
	})
	return result
}
