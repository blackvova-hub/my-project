package sources

import (
	"context"
	"fmt"
	"strings"

	"github.com/mmcdole/gofeed"
)

type RSSFeed struct {
	URL        string
	Source     string
	SourceID   string
	Category   string
	Limit      int
	Client     HTTPClient
	Keep       func(title, summary string) bool
	EventType  func(title, summary, link string) string
	SourceType string
}

func FetchRSS(ctx context.Context, cfg RSSFeed) ([]Item, error) {
	body, _, err := Get(ctx, cfg.Client, cfg.URL, "application/rss+xml, application/xml;q=0.9, text/xml;q=0.8", 4<<20)
	if err != nil {
		return nil, err
	}
	parser := gofeed.NewParser()
	parser.UserAgent = UserAgent
	feed, err := parser.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse %s RSS: %w", cfg.SourceID, err)
	}
	limit := cfg.Limit
	if limit <= 0 {
		limit = 50
	}
	out := make([]Item, 0, min(limit, len(feed.Items)))
	for _, row := range feed.Items {
		if len(out) >= limit {
			break
		}
		title := strings.TrimSpace(row.Title)
		link := strings.TrimSpace(first(row.Link, row.GUID))
		summary := CleanHTML(first(row.Description, row.Content))
		if title == "" || link == "" || cfg.Keep != nil && !cfg.Keep(title, summary) {
			continue
		}
		publishedFallback := false
		var published = row.PublishedParsed
		if published == nil {
			published = row.UpdatedParsed
			publishedFallback = published != nil
		}
		if published == nil {
			continue
		}
		eventType := "market_news"
		if cfg.EventType != nil {
			eventType = cfg.EventType(title, summary, link)
		}
		image := ""
		if row.Image != nil {
			image = strings.TrimSpace(row.Image.URL)
		}
		sourceType := strings.TrimSpace(cfg.SourceType)
		if sourceType == "" {
			sourceType = "rss"
		}
		out = append(out, Item{Category: cfg.Category, Title: title, URL: link, Source: cfg.Source, SourceID: cfg.SourceID, SourceType: sourceType, PublishedAt: published.UTC(), PublishedFallback: publishedFallback, EventType: eventType, Summary: summary, Image: image, OriginalLanguage: "en"})
	}
	return out, nil
}
