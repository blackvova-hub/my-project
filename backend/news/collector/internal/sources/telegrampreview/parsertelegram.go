package parsertelegram

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"news-bot/internal/sources"
)

const (
	DefaultChannelURL = "https://t.me/s/cryptoattack24"
	defaultSourceID   = "telegram_cryptoattack24"
	defaultLimit      = 25
	maxResponseBytes  = 8 << 20
)

var digestPattern = regexp.MustCompile(`(?i)(?:^|\s)(?:дайджест|digest)\b`)

func isRussianCoinTag(tag string) bool {
	return strings.Contains(`|биткоин|эфириум|эфир|солана|доджкоин|риппл|кардано|полкадот|тон|трон|аваланш|сибаину|`, `|`+strings.ToLower(tag)+`|`)
}

// Parser reads the public HTML preview of a Telegram channel. It deliberately
// does not use the Telegram API: one conditional HTTP request per interval is
// enough to pick up new posts and avoids API/flood limits.
type Parser struct {
	URL    string
	Limit  int
	Client sources.HTTPClient
	Now    func() time.Time

	mu           sync.Mutex
	etag         string
	lastModified string
}

func New(channelURL string, limit int) *Parser {
	if strings.TrimSpace(channelURL) == "" {
		channelURL = DefaultChannelURL
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	return &Parser{
		URL:    normalizeChannelURL(channelURL),
		Limit:  limit,
		Client: &http.Client{Timeout: 12 * time.Second},
		Now:    time.Now,
	}
}

func (p *Parser) ID() string { return defaultSourceID }

func (p *Parser) Fetch(ctx context.Context) ([]sources.Item, error) {
	if p == nil {
		return nil, fmt.Errorf("telegram parser is nil")
	}
	endpoint := normalizeChannelURL(p.URL)
	if endpoint == "" {
		return nil, fmt.Errorf("telegram channel URL is empty")
	}

	p.mu.Lock()
	client := p.Client
	etag := p.etag
	lastModified := p.lastModified
	p.mu.Unlock()
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", sources.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("telegram GET %s: rate limited", endpoint)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram GET %s: %s", endpoint, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.etag = strings.TrimSpace(resp.Header.Get("ETag"))
	p.lastModified = strings.TrimSpace(resp.Header.Get("Last-Modified"))
	p.mu.Unlock()

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	limit := p.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	items := make([]sources.Item, 0, limit)
	seen := make(map[string]struct{}, limit)
	doc.Find(".tgme_widget_message_wrap").EachWithBreak(func(_ int, wrap *goquery.Selection) bool {
		post := wrap.Find(".tgme_widget_message").First()
		if post.Length() == 0 {
			post = wrap
		}
		item, ok := parsePost(post, p.Now)
		if !ok || shouldSkipDigest(item.OriginalText) {
			return true
		}
		if _, exists := seen[item.URL]; exists {
			return true
		}
		seen[item.URL] = struct{}{}
		items = append(items, item)
		return len(items) < limit
	})
	return items, nil
}

func parsePost(post *goquery.Selection, now func() time.Time) (sources.Item, bool) {
	text := cleanTelegramText(post.Find(".tgme_widget_message_text").Text())
	if text == "" {
		return sources.Item{}, false
	}
	dateNode := post.Find("a.tgme_widget_message_date").First()
	datetime, hasDatetime := dateNode.Attr("datetime")
	published, ok := parseTelegramTime(datetime)
	if !hasDatetime || !ok {
		return sources.Item{}, false
	}

	postKey, _ := post.Attr("data-post")
	permalink, _ := dateNode.Attr("href")
	permalink = normalizePostURL(permalink)
	if permalink == "" {
		permalink = postURLFromKey(postKey)
	}
	if permalink == "" {
		return sources.Item{}, false
	}

	if now == nil {
		now = time.Now
	}
	if published.After(now().UTC().Add(10 * time.Minute)) {
		published = now().UTC()
	}
	title := telegramTitle(text)
	return sources.Item{
		Category:         "crypto",
		Title:            title,
		URL:              permalink,
		Source:           "КриптоАтака 24",
		SourceID:         defaultSourceID,
		SourceType:       "telegram",
		PublishedAt:      published,
		EventType:        "market_news",
		Summary:          text,
		OriginalTitle:    title,
		OriginalText:     text,
		OriginalLanguage: "ru",
	}, true
}

func normalizeChannelURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultChannelURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	path := strings.TrimSuffix(u.Path, "/")
	if !strings.HasPrefix(path, "/s/") {
		path = "/s" + path
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func normalizePostURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Host == "" {
		u, err = url.Parse("https://t.me" + raw)
		if err != nil {
			return ""
		}
	}
	u.Path = strings.Replace(u.Path, "/s/", "/", 1)
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "/")
}

func postURLFromKey(key string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(key), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return "https://t.me/" + parts[0] + "/" + parts[1]
}

func parseTelegramTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05Z07:00"} {
		if value, err := time.Parse(layout, raw); err == nil {
			return value.UTC(), true
		}
	}
	return time.Time{}, false
}

func shouldSkipDigest(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return digestPattern.MatchString(lower) ||
		strings.Contains(lower, "события на завтра") ||
		strings.Contains(lower, "главное за сегодня") ||
		strings.Contains(lower, "главное за выходные") ||
		strings.Contains(lower, "итоги недели")
}

func cleanTelegramText(text string) string {
	var out strings.Builder
	text = strings.ReplaceAll(text, "\u00a0", " ")
	for _, r := range text {
		if isEmoji(r) || r == '\uFE0E' || r == '\uFE0F' || r == '\u200D' {
			if out.Len() > 0 {
				out.WriteByte(' ')
			}
			continue
		}
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			continue
		}
		out.WriteRune(r)
	}
	return cleanHashtags(strings.Join(strings.Fields(out.String()), " "))
}

func cleanHashtags(text string) string {
	runes := []rune(text)
	var out []rune
	for i := 0; i < len(runes); {
		if runes[i] != '#' {
			out = append(out, runes[i])
			i++
			continue
		}
		end := i + 1
		cyrillic := false
		for end < len(runes) && (unicode.IsLetter(runes[end]) || unicode.IsDigit(runes[end]) || runes[end] == '_') {
			if unicode.In(runes[end], unicode.Cyrillic) {
				cyrillic = true
			}
			end++
		}
		if end == i+1 {
			out = append(out, runes[i])
			i++
			continue
		}
		tag := ``
		for tagIndex := i + 1; tagIndex < end; tagIndex++ {
			tag += string(runes[tagIndex])
		}
		if !cyrillic || isRussianCoinTag(tag) {
			out = append(out, runes[i:end]...)
		}
		i = end
	}
	return strings.Join(strings.Fields(string(out)), " ")
}

func telegramTitle(text string) string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) <= 180 {
		return text
	}
	for index, r := range text {
		if index >= 120 && (r == '.' || r == '!' || r == '?') {
			return strings.TrimSpace(text[:index+len(string(r))])
		}
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:180])) + "…"
}

func isEmoji(r rune) bool {
	return r >= 0x1F000 && r <= 0x1FAFF ||
		r >= 0x2600 && r <= 0x27BF ||
		r >= 0x2300 && r <= 0x23FF
}
