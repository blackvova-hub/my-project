package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

type DefiLlama struct {
	ProtocolsURL string
	Limit        int
	MinChangePct float64
	Client       HTTPClient
	Now          func() time.Time
}

func (s *DefiLlama) ID() string { return "defillama" }

type llamaProtocol struct {
	Name     string  `json:"name"`
	Slug     string  `json:"slug"`
	Category string  `json:"category"`
	TVL      float64 `json:"tvl"`
	Change1D float64 `json:"change_1d"`
}

type llamaOverview struct {
	Total24h float64 `json:"total24h"`
	Change1D float64 `json:"change_1d"`
}

type llamaStablecoins struct {
	Assets []struct {
		Name               string             `json:"name"`
		Symbol             string             `json:"symbol"`
		Circulating        map[string]float64 `json:"circulating"`
		CirculatingPrevDay map[string]float64 `json:"circulatingPrevDay"`
	} `json:"peggedAssets"`
}

func (s *DefiLlama) Fetch(ctx context.Context) ([]Item, error) {
	rawURL := strings.TrimSpace(s.ProtocolsURL)
	if rawURL == "" {
		rawURL = "https://api.llama.fi/protocols"
	}
	body, _, err := Get(ctx, s.Client, rawURL, "application/json", 16<<20)
	if err != nil {
		return nil, err
	}
	var protocols []llamaProtocol
	if err := json.Unmarshal(body, &protocols); err != nil {
		return nil, fmt.Errorf("decode DefiLlama protocols: %w", err)
	}
	minChange := s.MinChangePct
	if minChange <= 0 {
		minChange = 10
	}
	limit := s.Limit
	if limit <= 0 {
		limit = 25
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	out := make([]Item, 0, limit)
	for _, protocol := range protocols {
		if len(out) >= limit {
			break
		}
		if strings.TrimSpace(protocol.Name) == "" || protocol.TVL <= 0 || math.Abs(protocol.Change1D) < minChange {
			continue
		}
		direction := "increased"
		if protocol.Change1D < 0 {
			direction = "decreased"
		}
		title := fmt.Sprintf("%s TVL %s %.1f%% in 24 hours", protocol.Name, direction, math.Abs(protocol.Change1D))
		summary := fmt.Sprintf("DefiLlama reports TVL of $%.2f million. Category: %s.", protocol.TVL/1_000_000, protocol.Category)
		link := "https://defillama.com/protocol/" + url.PathEscape(protocol.Slug) + "?snapshot=" + now.Format("2006-01-02")
		out = append(out, Item{Category: "crypto", Title: title, URL: link, Source: "DefiLlama", SourceID: s.ID(), SourceType: "api", PublishedAt: now, PublishedFallback: true, EventAt: TimePtr(now), EventType: "defi_tvl_change", Summary: summary, OriginalLanguage: "en"})
	}
	for _, metric := range []struct{ name, eventType, url, dataType string }{
		{"DEX volume", "dex_volume_change", "https://api.llama.fi/overview/dexs", "dailyVolume"},
		{"protocol fees", "defi_fees_change", "https://api.llama.fi/overview/fees", "dailyFees"},
		{"DeFi open interest", "defi_open_interest_change", "https://api.llama.fi/overview/open-interest", "dailyOpenInterest"},
	} {
		if item, ok := s.overviewItem(ctx, now, minChange, metric.name, metric.eventType, metric.url, metric.dataType); ok {
			out = append(out, item)
		}
	}
	out = append(out, s.stablecoinItems(ctx, now, minChange)...)
	return out, nil
}

func (s *DefiLlama) overviewItem(ctx context.Context, now time.Time, minChange float64, name, eventType, endpoint, dataType string) (Item, bool) {
	u := endpoint + "?excludeTotalDataChart=true&excludeTotalDataChartBreakdown=true&dataType=" + url.QueryEscape(dataType)
	body, _, err := Get(ctx, s.Client, u, "application/json", 5<<20)
	if err != nil {
		return Item{}, false
	}
	var row llamaOverview
	if json.Unmarshal(body, &row) != nil || row.Total24h <= 0 || math.Abs(row.Change1D) < minChange {
		return Item{}, false
	}
	direction := "increased"
	if row.Change1D < 0 {
		direction = "decreased"
	}
	title := fmt.Sprintf("DefiLlama: %s %s %.1f%% in 24 hours", name, direction, math.Abs(row.Change1D))
	summary := fmt.Sprintf("Latest 24-hour value: $%.2f billion.", row.Total24h/1_000_000_000)
	return Item{Category: "crypto", Title: title, URL: "https://defillama.com/?metric=" + url.QueryEscape(eventType) + "&snapshot=" + now.Format("2006-01-02"), Source: "DefiLlama", SourceID: s.ID(), SourceType: "api", PublishedAt: now, PublishedFallback: true, EventAt: TimePtr(now), EventType: eventType, Summary: summary, OriginalLanguage: "en"}, true
}

func (s *DefiLlama) stablecoinItems(ctx context.Context, now time.Time, minChange float64) []Item {
	body, _, err := Get(ctx, s.Client, "https://stablecoins.llama.fi/stablecoins?includePrices=true", "application/json", 5<<20)
	if err != nil {
		return nil
	}
	var response llamaStablecoins
	if json.Unmarshal(body, &response) != nil {
		return nil
	}
	out := make([]Item, 0)
	for _, asset := range response.Assets {
		current, previous := asset.Circulating["peggedUSD"], asset.CirculatingPrevDay["peggedUSD"]
		if current < 10_000_000 || previous <= 0 {
			continue
		}
		change := (current - previous) / previous * 100
		if math.Abs(change) < minChange {
			continue
		}
		direction := "increased"
		if change < 0 {
			direction = "decreased"
		}
		title := fmt.Sprintf("%s stablecoin supply %s %.1f%% in 24 hours", asset.Symbol, direction, math.Abs(change))
		out = append(out, Item{Category: "crypto", Title: title, URL: "https://defillama.com/stablecoin/" + url.PathEscape(asset.Name) + "?snapshot=" + now.Format("2006-01-02"), Source: "DefiLlama", SourceID: s.ID(), SourceType: "api", PublishedAt: now, PublishedFallback: true, EventAt: TimePtr(now), EventType: "stablecoin_supply_change", Summary: fmt.Sprintf("Current circulating supply: $%.2f million.", current/1_000_000), OriginalLanguage: "en"})
	}
	return out
}

var _ Source = (*DefiLlama)(nil)
