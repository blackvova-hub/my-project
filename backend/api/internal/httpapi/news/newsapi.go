package newsapi

import (
	"encoding/json"
	"html"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type API struct {
	db *pgxpool.Pool
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)
var newsAssetRe = regexp.MustCompile(`^[A-Z0-9._-]{1,20}$`)

func New(db *pgxpool.Pool) *API {
	return &API{db: db}
}

func (a *API) Routes(r chi.Router) {
	r.Get("/news", a.handleList)
	r.Get("/news/calendar", a.handleCalendar)
	r.Get("/news/summary", a.handleSummary)
	r.Get("/news/important-events", a.handleImportantEvents)
}

type newsItem struct {
	ID                string          `json:"id"`
	Title             string          `json:"title"`
	URL               string          `json:"url"`
	Source            string          `json:"source"`
	PublishedAt       string          `json:"publishedAt"`
	Summary           *string         `json:"summary,omitempty"`
	Image             *string         `json:"image,omitempty"`
	Category          string          `json:"category"`
	CalendarOnly      bool            `json:"calendarOnly"`
	OriginalTitle     string          `json:"originalTitle,omitempty"`
	OriginalText      string          `json:"originalText,omitempty"`
	OriginalLanguage  string          `json:"originalLanguage,omitempty"`
	TranslatedTitle   *string         `json:"translatedTitle,omitempty"`
	TranslatedSummary *string         `json:"translatedSummary,omitempty"`
	SourceID          string          `json:"sourceId,omitempty"`
	SourceType        string          `json:"sourceType,omitempty"`
	EventAt           *string         `json:"eventAt,omitempty"`
	EventType         string          `json:"eventType,omitempty"`
	Entities          json.RawMessage `json:"entities"`
	Assets            json.RawMessage `json:"assets"`
}

type calendarDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type newsSummary struct {
	Summary     string `json:"summary"`
	PeriodStart string `json:"periodStart"`
	PeriodEnd   string `json:"periodEnd"`
}

type importantEvent struct {
	ID               string          `json:"id"`
	EventType        string          `json:"eventType"`
	Family           string          `json:"family"`
	Confidence       int             `json:"confidence"`
	Status           string          `json:"status"`
	Title            string          `json:"title"`
	Details          string          `json:"details"`
	Exchange         string          `json:"exchange,omitempty"`
	MarketType       string          `json:"marketType,omitempty"`
	Symbol           string          `json:"symbol,omitempty"`
	Direction        string          `json:"direction,omitempty"`
	AmountUSD        *float64        `json:"amountUsd,omitempty"`
	ChangePercent    *float64        `json:"changePercent,omitempty"`
	BaselineValue    *float64        `json:"baselineValue,omitempty"`
	BaselineRatio    *float64        `json:"baselineRatio,omitempty"`
	Percentile       *float64        `json:"percentile,omitempty"`
	WindowMinutes    int             `json:"windowMinutes"`
	OccurrenceCount  int             `json:"occurrenceCount"`
	FirstSeenAt      string          `json:"firstSeenAt"`
	LastSeenAt       string          `json:"lastSeenAt"`
	LastObservedAt   string          `json:"lastObservedAt"`
	ResolvedAt       *string         `json:"resolvedAt,omitempty"`
	EventAt          string          `json:"eventAt"`
	Metrics          json.RawMessage `json:"metrics"`
	ObservationCount int             `json:"observationCount"`
	FactorCount      int             `json:"factorCount"`
	ConfirmedVenues  []string        `json:"confirmedVenues,omitempty"`
	VenueCount       int             `json:"venueCount"`
	SourceKind       string          `json:"sourceKind,omitempty"`
	SourceRef        string          `json:"sourceRef,omitempty"`
	CatalystEventID  *string         `json:"catalystEventId,omitempty"`
	EntityID         *string         `json:"entityId,omitempty"`
	ClusterStartAt   *string         `json:"clusterStartAt,omitempty"`
	ClusterEndAt     *string         `json:"clusterEndAt,omitempty"`
}

func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("category")))
	if category == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "category_required"})
		return
	}
	if category != "crypto" && category != "world" && category != "market" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_category"})
		return
	}
	asset := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset")))
	if asset != "" && !newsAssetRe.MatchString(asset) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_asset"})
		return
	}
	searchName := strings.TrimSpace(r.URL.Query().Get("searchName"))
	if len(searchName) > 120 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_search_name"})
		return
	}

	tzOffset := parseTZOffset(r.URL.Query().Get("tzOffset"))
	var fromDate *time.Time
	var toDate *time.Time
	if rawDate := strings.TrimSpace(r.URL.Query().Get("date")); rawDate != "" {
		parsed, err := time.Parse("2006-01-02", rawDate)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_date"})
			return
		}
		start := parsed.Add(time.Minute * time.Duration(tzOffset)).UTC()
		end := start.Add(24 * time.Hour)
		fromDate = &start
		toDate = &end
	}

	limit := parseInt(r.URL.Query().Get("limit"), 60)
	if limit < 1 || limit > 200 {
		limit = 60
	}

	query := `
	    SELECT id, title, url, source, published_at, summary, image, category, calendar_only, original_title, original_text, original_language, translated_title, translated_summary, source_id, source_type, event_at, event_type, entities, assets
	    FROM news_items
	    WHERE (
	      ($1 = 'market' AND category IN ('crypto', 'macro'))
	      OR ($1 <> 'market' AND category = $1)
	    )
	      AND publication_eligible = TRUE
	  `
	args := []any{category}
	if fromDate != nil && toDate != nil {
		query += " AND published_at >= $2 AND published_at < $3"
		args = append(args, *fromDate, *toDate)
	}
	assetPredicates := make([]string, 0, 3)
	if asset != "" {
		args = append(args, asset)
		placeholder := "$" + strconv.Itoa(len(args))
		assetPredicates = append(assetPredicates,
			"assets @> jsonb_build_array("+placeholder+"::text)",
			"to_tsvector('simple', COALESCE(title, '') || ' ' || COALESCE(summary, '') || ' ' || COALESCE(original_title, '')) @@ plainto_tsquery('simple', "+placeholder+")",
		)
	}
	if searchName != "" {
		args = append(args, searchName)
		placeholder := "$" + strconv.Itoa(len(args))
		assetPredicates = append(assetPredicates,
			"to_tsvector('simple', COALESCE(title, '') || ' ' || COALESCE(summary, '') || ' ' || COALESCE(original_title, '')) @@ plainto_tsquery('simple', "+placeholder+")",
		)
	}
	if len(assetPredicates) > 0 {
		query += " AND (" + strings.Join(assetPredicates, " OR ") + ")"
	}
	query += " ORDER BY published_at DESC LIMIT $" + strconv.Itoa(len(args)+1)
	args = append(args, limit)

	rows, err := a.db.Query(r.Context(), query, args...)
	if err != nil {
		log.Printf("newsapi list query failed: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "news_fetch_failed"})
		return
	}
	defer rows.Close()

	items := make([]newsItem, 0, limit)
	for rows.Next() {
		var item newsItem
		var publishedAt time.Time
		var summary *string
		var image *string
		var eventAt *time.Time
		var translatedTitle *string
		var translatedSummary *string
		if err := rows.Scan(&item.ID, &item.Title, &item.URL, &item.Source, &publishedAt, &summary, &image, &item.Category, &item.CalendarOnly, &item.OriginalTitle, &item.OriginalText, &item.OriginalLanguage, &translatedTitle, &translatedSummary, &item.SourceID, &item.SourceType, &eventAt, &item.EventType, &item.Entities, &item.Assets); err != nil {
			log.Printf("newsapi list scan failed: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "news_fetch_failed"})
			return
		}
		item.PublishedAt = publishedAt.UTC().Format(time.RFC3339)
		item.TranslatedTitle = translatedTitle
		item.TranslatedSummary = translatedSummary
		if eventAt != nil {
			v := eventAt.UTC().Format(time.RFC3339)
			item.EventAt = &v
		}
		if summary != nil {
			clean := cleanNewsText(*summary)
			item.Summary = &clean
		} else {
			item.Summary = nil
		}
		item.Image = image
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) handleCalendar(w http.ResponseWriter, r *http.Request) {
	fromRaw := strings.TrimSpace(r.URL.Query().Get("from"))
	toRaw := strings.TrimSpace(r.URL.Query().Get("to"))
	if fromRaw == "" || toRaw == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "range_required"})
		return
	}
	fromDate, err := time.Parse("2006-01-02", fromRaw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_from"})
		return
	}
	toDate, err := time.Parse("2006-01-02", toRaw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_to"})
		return
	}
	if toDate.Before(fromDate) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_range"})
		return
	}
	category := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("category")))
	if category != "" && category != "crypto" && category != "world" && category != "market" && category != "all" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_category"})
		return
	}

	tzOffset := parseTZOffset(r.URL.Query().Get("tzOffset"))
	from := fromDate.Add(time.Minute * time.Duration(tzOffset)).UTC()
	to := toDate.Add(24 * time.Hour).Add(time.Minute * time.Duration(tzOffset)).UTC()

	query := `
    SELECT date_trunc('day', published_at - ($3 * interval '1 minute')) AS day, COUNT(*)
    FROM news_items
	    WHERE published_at >= $1 AND published_at < $2
	      AND publication_eligible = TRUE
	  `
	args := []any{from, to, tzOffset}
	if category == "market" {
		query += " AND category IN ('crypto', 'macro')"
	} else if category == "crypto" || category == "world" {
		query += " AND category = $4"
		args = append(args, category)
	}
	query += " GROUP BY day ORDER BY day"

	rows, err := a.db.Query(r.Context(), query, args...)
	if err != nil {
		log.Printf("newsapi calendar query failed: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "calendar_fetch_failed"})
		return
	}
	defer rows.Close()

	items := make([]calendarDay, 0)
	for rows.Next() {
		var day time.Time
		var count int
		if err := rows.Scan(&day, &count); err != nil {
			log.Printf("newsapi calendar scan failed: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "calendar_fetch_failed"})
			return
		}
		items = append(items, calendarDay{Date: day.UTC().Format("2006-01-02"), Count: count})
	}

	writeJSON(w, http.StatusOK, map[string]any{"days": items})
}

func (a *API) handleImportantEvents(w http.ResponseWriter, r *http.Request) {
	limit := parseInt(r.URL.Query().Get("limit"), 20)
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	hours := parseInt(r.URL.Query().Get("hours"), 24)
	if hours < 1 {
		hours = 1
	}
	if hours > 720 {
		hours = 720
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT e.id,e.event_type,e.family,e.confidence,e.status,e.title,e.details,
		       COALESCE(e.exchange,''),COALESCE(e.market_type,''),COALESCE(e.symbol,''),COALESCE(e.direction,''),
		       e.amount_usd,e.change_percent,e.baseline_value,e.baseline_ratio,e.percentile,e.window_minutes,e.occurrence_count,
		       e.first_seen_at,e.last_seen_at,e.last_observed_at,e.resolved_at,e.event_at,e.metadata,
		       e.observation_count,e.factor_count,e.confirmed_venues,e.venue_count,
		       COALESCE(e.source_kind,''),COALESCE(e.source_ref,''),e.catalyst_event_id,e.entity_id,e.cluster_start_at,e.cluster_end_at
		FROM important_events e
		WHERE e.event_at >= now() - ($1 * interval '1 hour')
		  AND e.event_at <= now() + interval '7 days'
		  AND e.status IN ('confirmed','resolved')
		  AND e.event_type IN (
		    'price_shock','open_interest_shock','large_aggressive_trade',
		    'exchange_announcement','onchain_transfer','major_statement'
		  )
		ORDER BY CASE WHEN e.status='confirmed' THEN 1 ELSE 2 END, e.last_seen_at DESC
		LIMIT $2`, hours, limit)
	if err != nil {
		log.Printf("important events query failed: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "important_events_fetch_failed"})
		return
	}
	defer rows.Close()
	items := make([]importantEvent, 0, limit)
	for rows.Next() {
		var item importantEvent
		var eventAt, first, last, observed time.Time
		var resolved, clusterStart, clusterEnd *time.Time
		var venues []byte
		if err := rows.Scan(&item.ID, &item.EventType, &item.Family, &item.Confidence, &item.Status, &item.Title, &item.Details, &item.Exchange, &item.MarketType, &item.Symbol, &item.Direction, &item.AmountUSD, &item.ChangePercent, &item.BaselineValue, &item.BaselineRatio, &item.Percentile, &item.WindowMinutes, &item.OccurrenceCount, &first, &last, &observed, &resolved, &eventAt, &item.Metrics, &item.ObservationCount, &item.FactorCount, &venues, &item.VenueCount, &item.SourceKind, &item.SourceRef, &item.CatalystEventID, &item.EntityID, &clusterStart, &clusterEnd); err != nil {
			log.Printf("important events scan failed: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "important_events_fetch_failed"})
			return
		}
		_ = json.Unmarshal(venues, &item.ConfirmedVenues)
		item.EventAt = eventAt.UTC().Format(time.RFC3339)
		item.FirstSeenAt = first.UTC().Format(time.RFC3339)
		item.LastSeenAt = last.UTC().Format(time.RFC3339)
		item.LastObservedAt = observed.UTC().Format(time.RFC3339)
		if resolved != nil {
			v := resolved.UTC().Format(time.RFC3339)
			item.ResolvedAt = &v
		}
		if clusterStart != nil {
			v := clusterStart.UTC().Format(time.RFC3339)
			item.ClusterStartAt = &v
		}
		if clusterEnd != nil {
			v := clusterEnd.UTC().Format(time.RFC3339)
			item.ClusterEndAt = &v
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "important_events_fetch_failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "generatedAt": time.Now().UTC().Format(time.RFC3339)})
}

func (a *API) handleSummary(w http.ResponseWriter, r *http.Request) {
	dateRaw := strings.TrimSpace(r.URL.Query().Get("date"))
	var summary newsSummary
	var periodStart time.Time
	var periodEnd time.Time
	if dateRaw != "" {
		err := a.db.QueryRow(r.Context(), `
			SELECT summary, period_start, period_end
			FROM news_summaries
			WHERE period_end::date = $1::date
			ORDER BY period_end DESC
			LIMIT 1
		`, dateRaw).Scan(&summary.Summary, &periodStart, &periodEnd)
		if err != nil {
			if err == pgx.ErrNoRows {
				writeJSON(w, http.StatusOK, map[string]any{"summary": "", "periodStart": "", "periodEnd": ""})
				return
			}
			log.Printf("newsapi summary query failed: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "summary_fetch_failed"})
			return
		}
	} else {
		err := a.db.QueryRow(r.Context(), `
			SELECT summary, period_start, period_end
			FROM news_summaries
			ORDER BY period_end DESC
			LIMIT 1
		`).Scan(&summary.Summary, &periodStart, &periodEnd)
		if err != nil {
			if err == pgx.ErrNoRows {
				writeJSON(w, http.StatusOK, map[string]any{"summary": "", "periodStart": "", "periodEnd": ""})
				return
			}
			log.Printf("newsapi summary query failed: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "summary_fetch_failed"})
			return
		}
	}
	summary.PeriodStart = periodStart.UTC().Format(time.RFC3339)
	summary.PeriodEnd = periodEnd.UTC().Format(time.RFC3339)
	writeJSON(w, http.StatusOK, summary)
}

func parseInt(raw string, def int) int {
	if strings.TrimSpace(raw) == "" {
		return def
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return v
}

func parseTZOffset(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	if v < -720 {
		return -720
	}
	if v > 840 {
		return 840
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func cleanNewsText(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	text = html.UnescapeString(text)
	text = htmlTagRe.ReplaceAllString(text, " ")
	text = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ", "\u00a0", " ").Replace(text)
	return strings.Join(strings.Fields(text), " ")
}
