package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
)

type config struct {
	DatabaseURL     string
	BotToken        string
	ChannelID       string
	PollInterval    time.Duration
	StartupLookback time.Duration
	ClaimTimeout    time.Duration
	BatchSize       int
	Categories      []string
}

type newsItem struct {
	ID          string
	Title       string
	URL         string
	Source      string
	Category    string
	Summary     *string
	Image       *string
	EventType   string
	Assets      []string
	PublishedAt time.Time
	Attempts    int
}

type telegramClient struct {
	token      string
	channel    string
	client     *http.Client
	apiBaseURL string
}

type telegramAPIError struct {
	status      int
	description string
}

func (e *telegramAPIError) Error() string {
	return fmt.Sprintf("Telegram HTTP %d: %s", e.status, e.description)
}

type telegramResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      struct {
		MessageID int64 `json:"message_id"`
	} `json:"result"`
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect PostgreSQL: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping PostgreSQL: %v", err)
	}
	if err := ensureSchema(ctx, pool); err != nil {
		log.Fatalf("prepare publication state: %v", err)
	}

	bot := &telegramClient{
		token:      cfg.BotToken,
		channel:    cfg.ChannelID,
		client:     &http.Client{Timeout: 15 * time.Second},
		apiBaseURL: "https://api.telegram.org",
	}
	log.Printf("telegram news publisher started channel=%s categories=%s", cfg.ChannelID, strings.Join(cfg.Categories, ","))

	publish := func() {
		if err := publishBatch(ctx, pool, bot, cfg); err != nil && ctx.Err() == nil {
			log.Printf("publish batch: %v", err)
		}
	}
	publish()
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Print("telegram news publisher stopped")
			return
		case <-ticker.C:
			publish()
		}
	}
}

func loadConfig() (config, error) {
	cfg := config{
		BotToken:        strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		ChannelID:       strings.TrimSpace(os.Getenv("TELEGRAM_CHANNEL_ID")),
		PollInterval:    envDuration("NEWS_PUBLISH_INTERVAL", 30*time.Second),
		StartupLookback: envDuration("NEWS_STARTUP_LOOKBACK", 2*time.Hour),
		ClaimTimeout:    envDuration("NEWS_CLAIM_TIMEOUT", 5*time.Minute),
		BatchSize:       envInt("NEWS_PUBLISH_BATCH_SIZE", 10),
		Categories:      parseCategories(envString("NEWS_PUBLISH_CATEGORIES", "crypto,macro")),
	}
	cfg.DatabaseURL = firstNonEmpty(os.Getenv("PG_DSN"), os.Getenv("DATABASE_URL"))
	if cfg.DatabaseURL == "" {
		password := os.Getenv("POSTGRES_PASSWORD")
		if password != "" {
			host := envString("PG_HOST", "postgres")
			port := envString("PG_PORT", "5432")
			user := envString("PG_USER", "postgres")
			database := envString("PG_DATABASE", "alerts")
			dsn := &url.URL{Scheme: "postgres", User: url.UserPassword(user, password), Host: host + ":" + port, Path: database}
			query := dsn.Query()
			query.Set("sslmode", envString("PG_SSLMODE", "disable"))
			dsn.RawQuery = query.Encode()
			cfg.DatabaseURL = dsn.String()
		}
	}
	if cfg.DatabaseURL == "" {
		return config{}, errors.New("PG_DSN, DATABASE_URL or POSTGRES_PASSWORD is required")
	}
	if cfg.BotToken == "" {
		return config{}, errors.New("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.ChannelID == "" {
		return config{}, errors.New("TELEGRAM_CHANNEL_ID is required (for example @channel_name or -1001234567890)")
	}
	if cfg.PollInterval < 5*time.Second {
		return config{}, errors.New("NEWS_PUBLISH_INTERVAL must be at least 5s")
	}
	if cfg.StartupLookback <= 0 || cfg.ClaimTimeout <= 0 {
		return config{}, errors.New("lookback and claim timeout must be positive")
	}
	if cfg.BatchSize < 1 || cfg.BatchSize > 50 {
		return config{}, errors.New("NEWS_PUBLISH_BATCH_SIZE must be between 1 and 50")
	}
	if len(cfg.Categories) == 0 {
		return config{}, errors.New("NEWS_PUBLISH_CATEGORIES must contain crypto, macro and/or world")
	}
	return cfg, nil
}

func ensureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS telegram_news_publications (
			news_id UUID NOT NULL REFERENCES news_items(id) ON DELETE CASCADE,
			channel_id TEXT NOT NULL,
			telegram_message_id BIGINT,
			attempts INTEGER NOT NULL DEFAULT 0,
			claimed_at TIMESTAMPTZ,
			next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			published_at TIMESTAMPTZ,
			last_error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (news_id, channel_id)
		);
		CREATE INDEX IF NOT EXISTS idx_telegram_news_publications_pending
			ON telegram_news_publications(channel_id, next_attempt_at)
			WHERE published_at IS NULL;
	`)
	return err
}

func publishBatch(ctx context.Context, pool *pgxpool.Pool, bot *telegramClient, cfg config) error {
	items, err := claimNews(ctx, pool, cfg)
	if err != nil {
		return err
	}
	for _, item := range items {
		messageID, sendErr := bot.send(ctx, item)
		if sendErr != nil {
			delay := retryDelay(item.Attempts + 1)
			if err := markFailed(ctx, pool, item, cfg.ChannelID, sendErr, delay); err != nil {
				return fmt.Errorf("record Telegram error for news %s: %w", item.ID, err)
			}
			log.Printf("news publication failed id=%s retry_in=%s: %v", item.ID, delay, sendErr)
			continue
		}
		if err := markPublished(ctx, pool, item.ID, cfg.ChannelID, messageID); err != nil {
			return fmt.Errorf("mark news %s published: %w", item.ID, err)
		}
		log.Printf("news published id=%s message_id=%d", item.ID, messageID)
	}
	return nil
}

func claimNews(ctx context.Context, pool *pgxpool.Pool, cfg config) ([]newsItem, error) {
	rows, err := pool.Query(ctx, `
		WITH candidates AS (
			SELECT n.id
			FROM news_items n
			LEFT JOIN telegram_news_publications p
			  ON p.news_id=n.id AND p.channel_id=$1
			WHERE n.category=ANY($2::text[])
			  AND n.publication_eligible = TRUE
			  AND n.published_at <= now()
			  AND (
				(
				  p.news_id IS NULL
				  AND n.created_at >= now() - ($3 * interval '1 second')
				  AND n.published_at >= now() - ($3 * interval '1 second')
				)
				OR (
					p.published_at IS NULL
					AND p.next_attempt_at <= now()
					AND (p.claimed_at IS NULL OR p.claimed_at < now() - ($4 * interval '1 second'))
				)
			  )
			ORDER BY n.created_at ASC
			LIMIT $5
		), claimed AS (
			INSERT INTO telegram_news_publications (news_id, channel_id, claimed_at)
			SELECT id, $1, now() FROM candidates
			ON CONFLICT (news_id, channel_id) DO UPDATE
			SET claimed_at=now()
			WHERE telegram_news_publications.published_at IS NULL
			  AND telegram_news_publications.next_attempt_at <= now()
			  AND (telegram_news_publications.claimed_at IS NULL OR telegram_news_publications.claimed_at < now() - ($4 * interval '1 second'))
			RETURNING news_id, attempts
		)
		SELECT n.id::text, n.title, n.url, n.source, n.category, n.summary, n.image,
		       COALESCE(n.event_type, ''), COALESCE(n.assets, '[]'::jsonb),
		       n.published_at, c.attempts
		FROM claimed c
		JOIN news_items n ON n.id=c.news_id
		ORDER BY n.created_at ASC
	`, cfg.ChannelID, cfg.Categories, int64(cfg.StartupLookback/time.Second), int64(cfg.ClaimTimeout/time.Second), cfg.BatchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]newsItem, 0, cfg.BatchSize)
	for rows.Next() {
		var item newsItem
		if err := rows.Scan(&item.ID, &item.Title, &item.URL, &item.Source, &item.Category, &item.Summary, &item.Image, &item.EventType, &item.Assets, &item.PublishedAt, &item.Attempts); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func markPublished(ctx context.Context, pool *pgxpool.Pool, newsID, channelID string, messageID int64) error {
	_, err := pool.Exec(ctx, `
		UPDATE telegram_news_publications
		SET telegram_message_id=$3, published_at=now(), claimed_at=NULL,
			attempts=attempts+1, last_error=NULL
		WHERE news_id=$1::uuid AND channel_id=$2
	`, newsID, channelID, messageID)
	return err
}

func markFailed(ctx context.Context, pool *pgxpool.Pool, item newsItem, channelID string, sendErr error, delay time.Duration) error {
	message := sendErr.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	_, err := pool.Exec(ctx, `
		UPDATE telegram_news_publications
		SET attempts=attempts+1, claimed_at=NULL,
			next_attempt_at=now() + ($3 * interval '1 second'), last_error=$4
		WHERE news_id=$1::uuid AND channel_id=$2 AND published_at IS NULL
	`, item.ID, channelID, int64(delay/time.Second), message)
	return err
}

func (b *telegramClient) send(ctx context.Context, item newsItem) (int64, error) {
	markup := sourceButton(item.URL)
	if imageURL := validHTTPURL(pointerValue(item.Image)); imageURL != "" {
		fields := map[string]any{
			"chat_id":    b.channel,
			"photo":      imageURL,
			"caption":    formatNewsCaption(item),
			"parse_mode": "HTML",
		}
		if markup != nil {
			fields["reply_markup"] = markup
		}
		messageID, err := b.request(ctx, "sendPhoto", fields)
		if err == nil {
			return messageID, nil
		}
		var apiErr *telegramAPIError
		if !errors.As(err, &apiErr) || apiErr.status != http.StatusBadRequest {
			return 0, err
		}
		log.Printf("telegram rejected news image id=%s; sending text fallback: %v", item.ID, err)
	}
	fields := map[string]any{
		"chat_id":                  b.channel,
		"text":                     formatNewsMessage(item),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if markup != nil {
		fields["reply_markup"] = markup
	}
	return b.request(ctx, "sendMessage", fields)
}

func (b *telegramClient) request(ctx context.Context, method string, fields map[string]any) (int64, error) {
	payload, err := json.Marshal(fields)
	if err != nil {
		return 0, err
	}
	baseURL := strings.TrimRight(b.apiBaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	endpoint := baseURL + "/bot" + b.token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	var result telegramResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("Telegram HTTP %d returned invalid JSON", resp.StatusCode)
	}
	if resp.StatusCode/100 != 2 || !result.OK {
		description := strings.TrimSpace(result.Description)
		if description == "" {
			description = http.StatusText(resp.StatusCode)
		}
		return 0, &telegramAPIError{status: resp.StatusCode, description: description}
	}
	return result.Result.MessageID, nil
}

func formatNewsMessage(item newsItem) string {
	return formatNewsCard(item, 2600)
}

func formatNewsCaption(item newsItem) string {
	return formatNewsCard(item, 620)
}

func formatNewsCard(item newsItem, summaryLimit int) string {
	title := html.EscapeString(truncateRunes(cleanText(item.Title), 320))
	summary := cleanText(pointerValue(item.Summary))
	badge, categoryTag := newsBadge(item)
	parts := []string{badge, "<b>" + title + "</b>"}
	if summary != "" && !strings.EqualFold(summary, cleanText(item.Title)) {
		parts = append(parts, html.EscapeString(truncateRunes(summary, summaryLimit)))
	}
	if tags := newsTags(item.Assets, categoryTag); tags != "" {
		parts = append(parts, tags)
	}
	source := html.EscapeString(truncateRunes(prettySource(item.Source), 80))
	meta := "🕒 " + formatTelegramTime(item.PublishedAt)
	if source != "" {
		meta = "🏛 " + source + "  •  " + meta
	}
	parts = append(parts, "<i>"+meta+"</i>")
	return strings.Join(parts, string([]byte{10, 10}))
}

func newsBadge(item newsItem) (string, string) {
	eventType := strings.ToLower(strings.TrimSpace(item.EventType))
	switch {
	case strings.EqualFold(item.Category, "macro"):
		return "📊 <b>МАКРО • СОБЫТИЕ</b>", "#Макро"
	case strings.Contains(eventType, "hack") || strings.Contains(eventType, "exploit") || strings.Contains(eventType, "security"):
		return "🚨 <b>КРИПТО • БЕЗОПАСНОСТЬ</b>", "#Безопасность"
	case strings.Contains(eventType, "enforcement") || strings.Contains(eventType, "regulation") || strings.Contains(eventType, "sanction") || strings.Contains(eventType, "policy"):
		return "⚖️ <b>РЕГУЛИРОВАНИЕ • РЫНКИ</b>", "#Регулирование"
	case strings.HasPrefix(eventType, "exchange_"):
		return "📣 <b>БИРЖИ • ОБЪЯВЛЕНИЕ</b>", "#Биржи"
	case strings.EqualFold(item.Category, "world"):
		return "🌍 <b>МИР • РЫНКИ</b>", "#Рынки"
	default:
		return "⚡️ <b>КРИПТО • НОВОСТЬ</b>", "#Крипто"
	}
}

func newsTags(assets []string, categoryTag string) string {
	tags := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, asset := range assets {
		asset = sanitizeTag(asset)
		if asset == "" || seen[asset] {
			continue
		}
		seen[asset] = true
		tags = append(tags, "#"+asset)
		if len(tags) == 3 {
			break
		}
	}
	if categoryTag != "" {
		tags = append(tags, categoryTag)
	}
	return strings.Join(tags, "  ")
}

func sanitizeTag(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	runes := make([]rune, 0, len(value))
	for _, r := range value {
		if r == '_' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r >= 'А' && r <= 'Я' {
			runes = append(runes, r)
		}
		if len(runes) == 16 {
			break
		}
	}
	return string(runes)
}

func prettySource(value string) string {
	cleaned := cleanText(value)
	known := map[string]string{
		"sec": "SEC", "fed": "Federal Reserve", "bls": "BLS", "doj": "DOJ",
		"whitehouse": "White House", "wublockchain": "WuBlockchain",
		"coindesk": "CoinDesk", "binance": "Binance", "bybit": "Bybit",
		"coinbase": "Coinbase", "upbit": "Upbit", "bithumb": "Bithumb",
	}
	if display := known[strings.ToLower(cleaned)]; display != "" {
		return display
	}
	return cleaned
}

func formatTelegramTime(value time.Time) string {
	months := [...]string{"янв.", "февр.", "мар.", "апр.", "мая", "июня", "июля", "авг.", "сент.", "окт.", "нояб.", "дек."}
	value = value.UTC()
	return fmt.Sprintf("%d %s • %02d:%02d UTC", value.Day(), months[value.Month()-1], value.Hour(), value.Minute())
}

func sourceButton(rawURL string) map[string]any {
	link := validHTTPURL(rawURL)
	if link == "" {
		return nil
	}
	return map[string]any{"inline_keyboard": [][]map[string]string{{{"text": "Читать источник ↗", "url": link}}}}
}

func validHTTPURL(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || len(raw) > 1000 {
		return ""
	}
	return raw
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func legacyFormatNewsMessage(item newsItem) string {
	title := html.EscapeString(truncateRunes(cleanText(item.Title), 500))
	summary := ""
	if item.Summary != nil {
		summary = html.EscapeString(truncateRunes(cleanText(*item.Summary), 2400))
	}
	source := html.EscapeString(truncateRunes(cleanText(item.Source), 120))
	categoryLabel, tag := "Криптовалюты", "#крипто"
	if strings.EqualFold(item.Category, "world") {
		categoryLabel, tag = "Мир", "#мир"
	}
	parts := []string{"📰 <b>" + title + "</b>"}
	if summary != "" {
		parts = append(parts, summary)
	}
	meta := "🏷 " + html.EscapeString(categoryLabel)
	if source != "" {
		meta += " · " + source
	}
	meta += " · " + item.PublishedAt.UTC().Format("02.01.2006 15:04 UTC")
	parts = append(parts, meta+"\n"+tag)
	if parsed, err := url.Parse(strings.TrimSpace(item.URL)); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && len(item.URL) <= 1000 {
		parts = append(parts, `<a href="`+html.EscapeString(item.URL)+`">Читать источник</a>`)
	}
	return strings.Join(parts, "\n\n")
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateRunes(value string, max int) string {
	if max < 1 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 15 * time.Second
	for i := 1; i < attempt && delay < 5*time.Minute; i++ {
		delay *= 2
	}
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

func parseCategories(raw string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, 3)
	for _, part := range strings.Split(raw, ",") {
		value := strings.ToLower(strings.TrimSpace(part))
		if (value == "crypto" || value == "macro" || value == "world") && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
