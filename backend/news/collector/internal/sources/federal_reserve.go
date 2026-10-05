package sources

import (
	"context"
	"fmt"
	"strings"
)

type FederalReserve struct {
	URLs   []string
	Limit  int
	Client HTTPClient
}

func (s *FederalReserve) ID() string { return "federal_reserve" }

func (s *FederalReserve) Fetch(ctx context.Context) ([]Item, error) {
	urls := s.URLs
	if len(urls) == 0 {
		urls = []string{
			"https://www.federalreserve.gov/feeds/press_monetary.xml",
			"https://www.federalreserve.gov/feeds/press_enforcement.xml",
			"https://www.federalreserve.gov/feeds/press_all.xml",
			"https://www.federalreserve.gov/feeds/speeches.xml",
		}
	}
	var out []Item
	var errs []string
	successes := 0
	for _, rawURL := range urls {
		items, err := FetchRSS(ctx, RSSFeed{URL: rawURL, Source: "Federal Reserve", SourceID: s.ID(), SourceType: "official_government", Category: "macro", Limit: s.Limit, Client: s.Client, Keep: fedRelevant, EventType: fedEventType})
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		successes++
		for i := range items {
			items[i].Entities = []string{"Federal Reserve"}
		}
		out = append(out, items...)
	}
	return out, partialError(successes, errs)
}

func fedRelevant(title, summary string) bool {
	text := strings.ToLower(title + " " + summary)
	for _, keyword := range []string{"fomc", "federal funds", "interest rate", "monetary policy", "balance sheet", "economic outlook", "inflation", "labor market", "financial stability", "beige book", "enforcement", "bank capital", "stablecoin", "digital asset", "payment system"} {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func fedEventType(title, summary, link string) string {
	text := strings.ToLower(title + " " + summary + " " + link)
	switch {
	case strings.Contains(text, "enforcement"):
		return "enforcement"
	case strings.Contains(text, "beige book"):
		return "beige_book"
	case strings.Contains(text, "fomc") || strings.Contains(text, "monetary policy") || strings.Contains(text, "interest rate"):
		return "monetary_policy"
	case strings.Contains(text, "speech"):
		return "speech"
	default:
		return "macro_news"
	}
}

func partialError(successes int, errors []string) error {
	if len(errors) == 0 || successes > 0 {
		return nil
	}
	return fmt.Errorf("all endpoints failed: %s", strings.Join(errors, "; "))
}

var _ Source = (*FederalReserve)(nil)
