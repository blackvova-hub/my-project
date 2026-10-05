package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Bithumb struct {
	URL    string
	Limit  int
	Client HTTPClient
}

func (s *Bithumb) ID() string { return "bithumb" }

type bithumbNotice struct {
	Categories  []string `json:"categories"`
	Title       string   `json:"title"`
	URL         string   `json:"pc_url"`
	PublishedAt string   `json:"published_at"`
	ModifiedAt  string   `json:"modified_at"`
}

func (s *Bithumb) Fetch(ctx context.Context) ([]Item, error) {
	rawURL := strings.TrimSpace(s.URL)
	if rawURL == "" {
		rawURL = "https://api.bithumb.com/v1/notices"
	}
	limit := s.Limit
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("count", strconv.Itoa(limit))
	u.RawQuery = q.Encode()
	body, _, err := Get(ctx, s.Client, u.String(), "application/json", 4<<20)
	if err != nil {
		return nil, err
	}
	var rows []bithumbNotice
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode Bithumb notices: %w", err)
	}
	kst := time.FixedZone("KST", 9*60*60)
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		eventType := exchangeNoticeType(row.Title)
		if eventType == "" {
			continue
		}
		published, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(row.PublishedAt), kst)
		fallback := false
		if err != nil {
			published, err = time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(row.ModifiedAt), kst)
			fallback = err == nil
		}
		if err != nil {
			continue
		}
		// A notice cannot be published in the future. This protects the feed
		// when the exchange returns a scheduled timestamp or a bad timezone.
		if published.After(time.Now().UTC().Add(5 * time.Minute)) {
			continue
		}
		var eventAt *time.Time
		if value, found := ExchangeAnnouncementEventTime(row.Title, published.UTC(), kst); found {
			eventAt = TimePtr(value)
		}
		out = append(out, Item{Category: "crypto", Title: strings.TrimSpace(row.Title), URL: strings.TrimSpace(row.URL), Source: "Bithumb", SourceID: s.ID(), SourceType: "official_exchange", PublishedAt: published.UTC(), PublishedFallback: fallback, EventAt: eventAt, EventType: eventType, OriginalLanguage: "ko", Assets: ExchangeAnnouncementAssets(row.Title)})
	}
	return out, nil
}

var _ Source = (*Bithumb)(nil)
