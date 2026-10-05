package sources

import (
	"context"
	"strings"
)

type SEC struct {
	URLs   []string
	Limit  int
	Client HTTPClient
}

func (s *SEC) ID() string { return "sec" }

func (s *SEC) Fetch(ctx context.Context) ([]Item, error) {
	urls := s.URLs
	if len(urls) == 0 {
		urls = []string{"https://www.sec.gov/news/pressreleases.rss", "https://www.sec.gov/enforcement-litigation/litigation-releases/rss"}
	}
	var out []Item
	var errs []string
	successes := 0
	for _, rawURL := range urls {
		items, err := FetchRSS(ctx, RSSFeed{URL: rawURL, Source: "U.S. Securities and Exchange Commission", SourceID: s.ID(), SourceType: "official_government", Category: "macro", Limit: s.Limit, Client: s.Client, Keep: secRelevant, EventType: secEventType})
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		successes++
		for i := range items {
			items[i].Entities = []string{"U.S. Securities and Exchange Commission"}
			if secCrypto(items[i].Title, items[i].Summary) {
				items[i].Category = "crypto"
			}
		}
		out = append(out, items...)
	}
	return out, partialError(successes, errs)
}

func secCrypto(title, summary string) bool {
	text := strings.ToLower(title + " " + summary)
	return containsAny(text, "bitcoin", "ethereum", "crypto", "digital asset", "token", "blockchain", "coinbase", "microstrategy", "strategy", "circle", "robinhood")
}

func secRelevant(title, summary string) bool {
	text := strings.ToLower(title + " " + summary)
	for _, keyword := range []string{"bitcoin", "ethereum", "crypto", "digital asset", "token", "blockchain", "coinbase", "strategy", "microstrategy", "circle", "robinhood", "exchange-traded fund", " etf", "securities market", "market structure"} {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func secEventType(title, summary, _ string) string {
	text := strings.ToLower(title + " " + summary)
	if strings.Contains(text, "etf") || strings.Contains(text, "exchange-traded fund") {
		return "etf"
	}
	if strings.Contains(text, "charges") || strings.Contains(text, "litigation") || strings.Contains(text, "settlement") || strings.Contains(text, "enforcement") {
		return "enforcement"
	}
	return "regulation"
}

var _ Source = (*SEC)(nil)
