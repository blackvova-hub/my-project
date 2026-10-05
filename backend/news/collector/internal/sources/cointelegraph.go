package sources

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

type Cointelegraph struct {
	URL    string
	Limit  int
	Client HTTPClient
}

func (s *Cointelegraph) ID() string { return "cointelegraph" }

func (s *Cointelegraph) Fetch(ctx context.Context) ([]Item, error) {
	rawURL := strings.TrimSpace(s.URL)
	if rawURL == "" {
		rawURL = "https://cointelegraph.com/rss"
	}
	body, headers, err := Get(ctx, s.Client, rawURL, "application/rss+xml, application/xml;q=0.9, text/xml;q=0.8", 4<<20)
	if err != nil {
		return nil, err
	}
	contentType := strings.ToLower(headers.Get("Content-Type"))
	if strings.Contains(contentType, "text/html") && !strings.Contains(strings.ToLower(string(body[:min(len(body), 512)])), "<rss") {
		return nil, fmt.Errorf("cointelegraph endpoint returned HTML instead of RSS")
	}
	parser := gofeed.NewParser()
	parser.UserAgent = UserAgent
	feed, err := parser.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse cointelegraph RSS: %w", err)
	}
	limit := s.Limit
	if limit <= 0 {
		limit = 50
	}
	out := make([]Item, 0, min(limit, len(feed.Items)))
	for _, row := range feed.Items {
		if len(out) >= limit {
			break
		}
		if strings.TrimSpace(row.Title) == "" || strings.TrimSpace(row.Link) == "" {
			continue
		}
		published := time.Time{}
		fallback := false
		if row.PublishedParsed != nil {
			published = row.PublishedParsed.UTC()
		} else if row.UpdatedParsed != nil {
			published = row.UpdatedParsed.UTC()
			fallback = true
		} else {
			// No time.Now fallback: an undated item is unsafe to publish as current.
			continue
		}
		image := ""
		if row.Image != nil {
			image = strings.TrimSpace(row.Image.URL)
		}
		out = append(out, Item{Category: "crypto", Title: strings.TrimSpace(row.Title), URL: strings.TrimSpace(row.Link), Source: "Cointelegraph", SourceID: s.ID(), SourceType: "rss", PublishedAt: published, PublishedFallback: fallback, EventType: "market_news", Summary: CleanHTML(first(row.Description, row.Content)), Image: image, OriginalLanguage: "en"})
	}
	return out, nil
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var _ Source = (*Cointelegraph)(nil)
var _ = http.MethodGet
