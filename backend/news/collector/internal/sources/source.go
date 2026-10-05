package sources

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const UserAgent = "ShortLongNewsBot/1.0 admin@short-and-long.ru"

// Item is the source-facing representation used by the legacy news pipeline.
// PublishedAt is always the publication time. EventAt is optional and must only
// contain an explicitly announced event time (listing, data release, etc.).
type Item struct {
	Category          string
	Title             string
	URL               string
	Source            string
	SourceID          string
	SourceType        string
	PublishedAt       time.Time
	PublishedFallback bool
	EventAt           *time.Time
	EventType         string
	Summary           string
	Image             string
	OriginalTitle     string
	OriginalText      string
	OriginalLanguage  string
	CalendarOnly      bool
	Importance        int
	Entities          []string
	Assets            []string
}

// RawItem is the source-provider representation before the news pipeline
// enriches or persists an item. Keep it as an alias so providers using the
// newer name remain compatible with the legacy Item-based pipeline.
type RawItem = Item

var exchangeAssetToken = regexp.MustCompile(`(?i)(?:\(([A-Z0-9]{2,15}(?:\s*,\s*[A-Z0-9]{2,15})*)\)|\b([A-Z0-9]{2,15})[-/](?:USDT|USDC|USD|KRW|BTC|ETH)\b)`)
var exchangeListToken = regexp.MustCompile(`(?i)\b(?:list|lists|listing|delist|delists|support for)\s+([A-Z0-9]{2,15})\b`)
var exchangeAssetStopWords = map[string]bool{
	"USD": true, "USDT": true, "USDC": true, "KRW": true, "SPOT": true,
	"FUTURES": true, "PERPETUAL": true, "MARGIN": true, "ETF": true,
}

func ExchangeAnnouncementAssets(title string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 2)
	for _, match := range exchangeAssetToken.FindAllStringSubmatch(title, -1) {
		value := match[1]
		if value == "" {
			value = match[2]
		}
		for _, token := range strings.Split(value, ",") {
			token = strings.ToUpper(strings.TrimSpace(token))
			if token == "" || exchangeAssetStopWords[token] || seen[token] {
				continue
			}
			seen[token] = true
			out = append(out, token)
		}
	}
	for _, match := range exchangeListToken.FindAllStringSubmatch(title, -1) {
		token := strings.ToUpper(strings.TrimSpace(match[1]))
		if token != "" && !exchangeAssetStopWords[token] && !seen[token] {
			seen[token] = true
			out = append(out, token)
		}
	}
	return out
}

func ExchangeAnnouncementType(title, hint string) string {
	text := strings.ToLower(title + " " + hint)
	if containsAny(text, "delist", "removal") {
		return "delisting"
	}
	if containsAny(text, "suspend trading", "trading suspended", "suspension of trading", "trading suspension", "trading halt", "halt trading") {
		return "trading_suspension"
	}
	if !containsAny(text, "listing", "new crypto", "new_crypto", "new digital asset") {
		return ""
	}
	if containsAny(text, "futures", "future", "perpetual", "perp", "derivatives", "contract") {
		return "futures_listing"
	}
	return "spot_listing"
}

func PostJSON(ctx context.Context, client HTTPClient, rawURL, apiKey string, payload io.Reader, maxBytes int64) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("x-soso-api-key", strings.TrimSpace(apiKey))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("POST %s: %s", rawURL, resp.Status)
	}
	if maxBytes <= 0 {
		maxBytes = 4 << 20
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBytes))
}

type Source interface {
	ID() string
	Fetch(context.Context) ([]Item, error)
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

func Get(ctx context.Context, client HTTPClient, rawURL, accept string, maxBytes int64) ([]byte, http.Header, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	if maxBytes <= 0 {
		maxBytes = 4 << 20
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	return body, resp.Header, err
}

var tags = regexp.MustCompile(`<[^>]+>`)
var spaces = regexp.MustCompile(`\s+`)

func CleanHTML(value string) string {
	value = html.UnescapeString(tags.ReplaceAllString(value, " "))
	return strings.TrimSpace(spaces.ReplaceAllString(value, " "))
}

func MillisTime(value int64) (time.Time, bool) {
	if value <= 0 {
		return time.Time{}, false
	}
	if value < 10_000_000_000 {
		return time.Unix(value, 0).UTC(), true
	}
	return time.UnixMilli(value).UTC(), true
}

func TimePtr(value time.Time) *time.Time {
	v := value.UTC()
	return &v
}
