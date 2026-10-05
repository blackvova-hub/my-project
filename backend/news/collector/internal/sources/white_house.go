package sources

import (
	"context"
	"strings"
)

type WhiteHouse struct {
	URL    string
	Limit  int
	Client HTTPClient
}

func (s *WhiteHouse) ID() string { return "whitehouse" }

func (s *WhiteHouse) Fetch(ctx context.Context) ([]Item, error) {
	rawURL := strings.TrimSpace(s.URL)
	if rawURL == "" {
		rawURL = "https://www.whitehouse.gov/briefing-room/feed/"
	}
	items, err := FetchRSS(ctx, RSSFeed{
		URL: rawURL, Source: "The White House", SourceID: s.ID(), SourceType: "official_government",
		Category: "macro", Limit: s.Limit, Client: s.Client, Keep: whiteHouseRelevant,
		EventType: func(_, _, _ string) string { return "major_statement" },
	})
	for i := range items {
		items[i].Entities = []string{"The White House"}
	}
	return items, err
}

func whiteHouseRelevant(title, summary string) bool {
	return officialGovernmentMarketRelevant(title + " " + summary)
}

var _ Source = (*WhiteHouse)(nil)
