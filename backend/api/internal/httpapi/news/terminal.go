package newsapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const terminalRetention = 30 * 24 * time.Hour
const terminalDedupWindow = 24 * time.Hour
const terminalBreakingWindow = 10 * time.Minute

type TerminalNewsItem struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	URL              string   `json:"url"`
	Source           string   `json:"source"`
	SourceID         string   `json:"sourceId"`
	Category         string   `json:"category"`
	PublishedAt      string   `json:"publishedAt"`
	TimestampBasis   string   `json:"timestampBasis"`
	Summary          string   `json:"summary,omitempty"`
	Assets           []string `json:"assets"`
	Tags             []string `json:"tags"`
	Importance       string   `json:"importance"`
	ImportanceScore  int      `json:"importanceScore"`
	ImportanceReason []string `json:"importanceReason"`
	Breaking         bool     `json:"breaking"`
	Language         string   `json:"language"`
	Attribution      string   `json:"attribution,omitempty"`
}

type TerminalSourceHealth struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	URL             string  `json:"url"`
	TermsURL        string  `json:"termsUrl"`
	Status          string  `json:"status"`
	Enabled         bool    `json:"enabled"`
	LastAttempt     *string `json:"lastAttempt"`
	LastSuccess     *string `json:"lastSuccess"`
	LastError       *string `json:"lastError"`
	ItemCount       int     `json:"itemCount"`
	Attribution     string  `json:"attribution"`
	Reason          string  `json:"reason,omitempty"`
	IntervalSeconds int     `json:"intervalSeconds"`
}

type terminalSource struct {
	TerminalSourceHealth
	kind         string
	category     string
	official     bool
	etag         string
	lastModified string
	nextAttempt  time.Time
}

type terminalService struct {
	db        *pgxpool.Pool
	client    *http.Client
	mu        sync.RWMutex
	items     []TerminalNewsItem
	sources   []terminalSource
	syncing   bool
	hydrated  bool
	asOf      time.Time
	dbChecked time.Time
	storage   string
	now       func() time.Time
	ctx       context.Context
}

var terminalServices sync.Map

func (a *API) terminalService() *terminalService {
	if stored, ok := terminalServices.Load(a); ok {
		return stored.(*terminalService)
	}
	service, _ := terminalServices.LoadOrStore(a, newTerminalService(a.db))
	return service.(*terminalService)
}

// TerminalRoutes shares the existing news store and runs at most one background
// synchronization at a time. It also works with a nil pool for the development
// server: the resulting data is real provider data, retained in bounded memory.
func (a *API) TerminalRoutes(r chi.Router) {
	s := a.terminalService()
	r.Get("/news/terminal", s.handleList)
	r.Get("/news/terminal/sources", s.handleSources)
}

// TerminalRun keeps official feeds warm even when nobody has the terminal open.
// The caller owns the lifecycle and should run it in a goroutine with the API's
// shutdown context. Route registration alone still supports demand-driven sync.
func (a *API) TerminalRun(ctx context.Context) {
	s := a.terminalService()
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	s.ensureSync()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ensureSync()
		}
	}
}

func newTerminalService(db *pgxpool.Pool) *terminalService {
	return &terminalService{db: db, client: &http.Client{Timeout: 18 * time.Second}, sources: terminalSources(), items: []TerminalNewsItem{}, storage: "memory", ctx: context.Background(), now: func() time.Time { return time.Now().UTC() }}
}

func (s *terminalService) handleList(w http.ResponseWriter, r *http.Request) {
	filter, err := parseTerminalFilter(r.URL.Query(), s.now())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.ensureSync()
	s.mu.RLock()
	items := append([]TerminalNewsItem{}, s.items...)
	sources := s.healthLocked(s.now())
	syncing, storage, asOf := s.syncing, s.storage, s.asOf
	profiles := make(map[string]bool, len(s.sources))
	for _, source := range s.sources {
		profiles[source.ID] = source.official
	}
	s.mu.RUnlock()
	// Ranking is derived from current rules, including for a warm in-memory
	// cache; previously persisted importance is not treated as ground truth.
	for i := range items {
		assets := items[i].Assets
		enrichTerminalItem(&items[i], profiles[items[i].SourceID], s.now())
		for _, asset := range assets {
			if newsAssetRe.MatchString(asset) && !terminalMember(asset, items[i].Assets...) {
				items[i].Assets = append(items[i].Assets, asset)
			}
		}
	}
	truncated := false
	if s.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		stored, limitReached, readErr := s.readStored(ctx, filter)
		cancel()
		if readErr == nil {
			// Filter the shared hot cache before merging persistent history. A
			// full cache of unrelated newer rows must not evict an older match
			// fetched specifically for this query.
			allMatches := filter
			allMatches.limit, allMatches.anchor = 10001, nil
			items, _, _ = terminalPage(items, allMatches, s.now())
			stored, _, _ = terminalPage(stored, allMatches, s.now())
			if storage == "memory" {
				items = deduplicateTerminal(append(items, stored...), s.now())
			} else {
				items = deduplicateTerminal(append(stored, items...), s.now())
			}
			truncated = limitReached || len(stored) >= 10000
		}
	}
	page, total, cursor := terminalPage(items, filter, s.now())
	w.Header().Set("Cache-Control", "private, max-age=15")
	writeJSON(w, http.StatusOK, map[string]any{"items": page, "sources": sources, "asOf": nullableTerminalTime(asOf), "nextCursor": cursor, "total": total, "storage": storage, "syncing": syncing, "truncated": truncated, "historyWindowDays": 30})
}

func (s *terminalService) handleSources(w http.ResponseWriter, r *http.Request) {
	s.ensureSync()
	s.mu.RLock()
	response := map[string]any{"sources": s.healthLocked(s.now()), "asOf": nullableTerminalTime(s.asOf), "syncing": s.syncing}
	s.mu.RUnlock()
	w.Header().Set("Cache-Control", "private, max-age=15")
	writeJSON(w, http.StatusOK, response)
}

func (s *terminalService) healthLocked(now time.Time) []TerminalSourceHealth {
	result := make([]TerminalSourceHealth, 0, len(s.sources))
	for _, source := range s.sources {
		h := source.TerminalSourceHealth
		switch {
		case !h.Enabled:
			h.Status = "disabled"
		case h.LastError != nil:
			h.Status = "error"
		case h.LastSuccess == nil:
			h.Status = "pending"
		default:
			last, _ := time.Parse(time.RFC3339, *h.LastSuccess)
			if now.Sub(last) > time.Duration(h.IntervalSeconds)*3*time.Second {
				h.Status = "stale"
			} else {
				h.Status = "ok"
			}
		}
		result = append(result, h)
	}
	return result
}

func (s *terminalService) ensureSync() {
	s.mu.Lock()
	if s.syncing {
		s.mu.Unlock()
		return
	}
	now, needed := s.now(), !s.hydrated
	for _, source := range s.sources {
		if source.Enabled && !now.Before(source.nextAttempt) {
			needed = true
		}
	}
	if !needed && (s.db == nil || now.Sub(s.dbChecked) < time.Minute) {
		s.mu.Unlock()
		return
	}
	s.syncing = true
	s.mu.Unlock()
	go s.synchronize()
}

func (s *terminalService) synchronize() {
	defer func() {
		s.mu.Lock()
		s.syncing = false
		s.mu.Unlock()
	}()
	s.mu.RLock()
	parent := s.ctx
	s.mu.RUnlock()
	ctx, cancel := context.WithTimeout(parent, 100*time.Second)
	defer cancel()
	s.hydrate(ctx)
	s.mu.RLock()
	sources := append([]terminalSource{}, s.sources...)
	s.mu.RUnlock()
	// Official endpoints are independent. GDELT uses a single broad query rather
	// than parallel topic queries to avoid exceeding its shared rate limit.
	var wg sync.WaitGroup
	for i, source := range sources {
		if !source.Enabled || s.now().Before(source.nextAttempt) {
			continue
		}
		wg.Add(1)
		go func(index int, spec terminalSource) {
			defer wg.Done()
			s.syncSource(ctx, index, spec)
		}(i, source)
	}
	wg.Wait()
}

func (s *terminalService) syncSource(ctx context.Context, index int, source terminalSource) {
	now := s.now()
	items, headers, unchanged, err := s.fetch(ctx, source)
	s.mu.Lock()
	health := &s.sources[index]
	health.LastAttempt = nullableTerminalTime(now)
	health.nextAttempt = now.Add(time.Duration(source.IntervalSeconds) * time.Second)
	if err != nil {
		message := boundedTerminalText(err.Error(), 180)
		health.LastError = &message
		if retry := terminalRetryAfter(headers.Get("Retry-After"), now); retry.After(health.nextAttempt) {
			health.nextAttempt = retry
		}
	} else {
		health.LastSuccess, health.LastError = nullableTerminalTime(now), nil
		s.asOf = now
		if !unchanged {
			health.ItemCount = len(items)
			health.etag, health.lastModified = headers.Get("ETag"), headers.Get("Last-Modified")
			s.items = deduplicateTerminal(append(items, s.items...), now)
		}
	}
	updated := *health
	s.mu.Unlock()
	if err == nil && !unchanged && s.db != nil {
		if persistErr := s.persistItems(ctx, items); persistErr != nil {
			log.Printf("news terminal persistence unavailable: %v", persistErr)
			s.mu.Lock()
			s.storage = "memory"
			s.mu.Unlock()
		} else {
			s.mu.Lock()
			s.storage = "postgres"
			s.mu.Unlock()
		}
	}
	s.persistHealth(ctx, updated)
}

type terminalFilter struct {
	category   string
	source     string
	asset      string
	query      string
	importance string
	sort       string
	from       time.Time
	to         time.Time
	limit      int
	asOf       time.Time
	anchor     *terminalCursor
	hash       string
}

type terminalCursor struct {
	Version   int    `json:"v"`
	AsOf      string `json:"asOf"`
	Time      string `json:"time"`
	ID        string `json:"id"`
	Score     int    `json:"score"`
	Filter    string `json:"filter"`
	Signature string `json:"sig"`
}

var terminalCursorKey = func() []byte {
	if key := os.Getenv("NEWS_TERMINAL_CURSOR_SECRET"); len(key) >= 32 {
		return []byte(key)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("news terminal cursor entropy unavailable")
	}
	return key
}()

func signTerminalCursor(cursor terminalCursor) string {
	cursor.Signature = ""
	payload, _ := json.Marshal(cursor)
	mac := hmac.New(sha256.New, terminalCursorKey)
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func parseTerminalFilter(q url.Values, now time.Time) (terminalFilter, error) {
	f := terminalFilter{category: strings.ToLower(strings.TrimSpace(q.Get("category"))), source: strings.TrimSpace(q.Get("source")), asset: strings.ToUpper(strings.TrimSpace(q.Get("asset"))), query: strings.TrimSpace(q.Get("q")), importance: strings.ToLower(q.Get("importance")), sort: strings.ToLower(q.Get("sort")), limit: 60, asOf: now}
	if f.category == "" {
		f.category = "all"
	}
	if f.importance == "" {
		f.importance = "all"
	}
	if f.sort == "" {
		f.sort = "latest"
	}
	if !terminalMember(f.category, "all", "crypto", "macro", "world", "markets") {
		return f, errors.New("invalid_category")
	}
	if !terminalMember(f.importance, "all", "high", "medium", "low") {
		return f, errors.New("invalid_importance")
	}
	if !terminalMember(f.sort, "latest", "important") {
		return f, errors.New("invalid_sort")
	}
	if f.asset != "" && !newsAssetRe.MatchString(f.asset) {
		return f, errors.New("invalid_asset")
	}
	if len([]rune(f.query)) > 200 || len(f.source) > 60 {
		return f, errors.New("invalid_search")
	}
	if raw := q.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return f, errors.New("invalid_limit")
		}
		f.limit = value
	}
	for name, dest := range map[string]*time.Time{"from": &f.from, "to": &f.to} {
		if raw := q.Get(name); raw != "" {
			value, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				value, err = time.Parse("2006-01-02", raw)
				// Date-only 'to' includes that entire UTC day.
				if err == nil && name == "to" {
					value = value.Add(24 * time.Hour)
				}
			}
			if err != nil {
				return f, fmt.Errorf("invalid_%s", name)
			}
			*dest = value.UTC()
		}
	}
	if !f.from.IsZero() && !f.to.IsZero() && !f.from.Before(f.to) {
		return f, errors.New("invalid_range")
	}
	f.hash = terminalHash(strings.Join([]string{f.category, f.source, f.asset, strings.ToLower(f.query), f.importance, f.sort, f.from.Format(time.RFC3339), f.to.Format(time.RFC3339)}, "\x00"))[:16]
	if raw := q.Get("cursor"); raw != "" {
		if len(raw) > 1024 {
			return f, errors.New("invalid_cursor")
		}
		data, err := base64.RawURLEncoding.DecodeString(raw)
		var cursor terminalCursor
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.Filter != f.hash || cursor.ID == "" || cursor.Score < 0 || cursor.Score > 100 || !hmac.Equal([]byte(cursor.Signature), []byte(signTerminalCursor(cursor))) {
			return f, errors.New("invalid_cursor")
		}
		asOf, err := time.Parse(time.RFC3339Nano, cursor.AsOf)
		if err != nil || asOf.After(now.Add(time.Minute)) || now.Sub(asOf) > terminalRetention {
			return f, errors.New("expired_cursor")
		}
		if _, err := time.Parse(time.RFC3339, cursor.Time); err != nil {
			return f, errors.New("invalid_cursor")
		}
		f.asOf, f.anchor = asOf, &cursor
	}
	return f, nil
}

func terminalPage(items []TerminalNewsItem, f terminalFilter, now time.Time) ([]TerminalNewsItem, int, *string) {
	filtered := make([]TerminalNewsItem, 0, len(items))
	for _, item := range items {
		published, err := time.Parse(time.RFC3339, item.PublishedAt)
		if err != nil || published.After(f.asOf) || published.Before(now.Add(-terminalRetention)) {
			continue
		}
		if !f.from.IsZero() && published.Before(f.from) || !f.to.IsZero() && !published.Before(f.to) {
			continue
		}
		if f.category != "all" && f.category != item.Category || f.source != "" && f.source != "all" && f.source != item.SourceID {
			continue
		}
		if f.asset != "" && !terminalMember(f.asset, item.Assets...) || f.importance != "all" && f.importance != item.Importance {
			continue
		}
		if f.query != "" && !strings.Contains(strings.ToLower(item.Title+" "+item.Summary+" "+strings.Join(item.Tags, " ")+" "+strings.Join(item.Assets, " ")), strings.ToLower(f.query)) {
			continue
		}
		item.Breaking = item.Breaking && now.Sub(published) <= terminalBreakingWindow && !published.After(now)
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool { return terminalBefore(filtered[i], filtered[j], f.sort) })
	total, start := len(filtered), 0
	if f.anchor != nil {
		anchor := TerminalNewsItem{ID: f.anchor.ID, PublishedAt: f.anchor.Time, ImportanceScore: f.anchor.Score}
		for start < len(filtered) && !terminalBefore(anchor, filtered[start], f.sort) {
			start++
		}
	}
	end := min(start+f.limit, len(filtered))
	page := append([]TerminalNewsItem{}, filtered[start:end]...)
	var next *string
	if end < len(filtered) && len(page) > 0 {
		last := page[len(page)-1]
		cursor := terminalCursor{Version: 1, AsOf: f.asOf.Format(time.RFC3339Nano), Time: last.PublishedAt, ID: last.ID, Score: last.ImportanceScore, Filter: f.hash}
		cursor.Signature = signTerminalCursor(cursor)
		encoded, _ := json.Marshal(cursor)
		value := base64.RawURLEncoding.EncodeToString(encoded)
		next = &value
	}
	return page, total, next
}

func terminalBefore(a, b TerminalNewsItem, order string) bool {
	if order == "important" && a.ImportanceScore != b.ImportanceScore {
		return a.ImportanceScore > b.ImportanceScore
	}
	if a.PublishedAt != b.PublishedAt {
		return a.PublishedAt > b.PublishedAt
	}
	return a.ID < b.ID
}

func deduplicateTerminal(items []TerminalNewsItem, now time.Time) []TerminalNewsItem {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].PublishedAt != items[j].PublishedAt {
			return items[i].PublishedAt > items[j].PublishedAt
		}
		return items[i].SourceID < items[j].SourceID
	})
	urls, titles := map[string]bool{}, map[string]time.Time{}
	out := make([]TerminalNewsItem, 0, min(len(items), 10000))
	for _, item := range items {
		published, err := time.Parse(time.RFC3339, item.PublishedAt)
		if err != nil || published.Before(now.Add(-terminalRetention)) || published.After(now.Add(5*time.Minute)) {
			continue
		}
		canonical := canonicalTerminalURL(item.URL)
		if canonical == "" || urls[canonical] {
			continue
		}
		normal := normalizeTerminalHeadline(item.Title)
		if len([]rune(normal)) < 12 {
			continue
		}
		if last, exists := titles[normal]; exists && last.Sub(published) < terminalDedupWindow {
			continue
		}
		item.ID = terminalHash(canonical)[:24]
		urls[canonical], titles[normal] = true, published
		out = append(out, item)
		if len(out) == 10000 {
			break
		}
	}
	return out
}

func canonicalTerminalURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return ""
	}
	u.Scheme, u.Host, u.Fragment = strings.ToLower(u.Scheme), strings.ToLower(u.Host), ""
	u.Host = strings.TrimPrefix(u.Host, "www.")
	u.Path = strings.TrimRight(u.Path, "/")
	q := u.Query()
	for key := range q {
		if strings.HasPrefix(strings.ToLower(key), "utm_") || terminalMember(strings.ToLower(key), "fbclid", "gclid", "ref", "ref_src", "mc_cid", "mc_eid") {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func normalizeTerminalHeadline(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(cleanNewsText(value))), "breaking:")
	return strings.Join(strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
}

func boundedTerminalText(value string, maxRunes int) string {
	value = cleanNewsText(value)
	runes := []rune(value)
	if len(runes) > maxRunes {
		return strings.TrimSpace(string(runes[:maxRunes])) + "…"
	}
	return value
}

func terminalHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func terminalMember(value string, values ...string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}
func nullableTerminalTime(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func terminalRetryAfter(value string, now time.Time) time.Time {
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return now.Add(time.Duration(min(seconds, 86400)) * time.Second)
	}
	if parsed, err := http.ParseTime(value); err == nil && parsed.After(now) {
		return minTime(parsed, now.Add(24*time.Hour))
	}
	return time.Time{}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
