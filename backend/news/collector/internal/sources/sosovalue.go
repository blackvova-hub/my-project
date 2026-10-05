package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type SoSoValue struct {
	APIKey     string
	URL        string
	CurrentURL string
	Client     HTTPClient
}

func (s *SoSoValue) ID() string { return "sosovalue_etf" }

type sosoResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		List []struct {
			Date             string  `json:"date"`
			TotalNetInflow   float64 `json:"totalNetInflow"`
			TotalValueTraded float64 `json:"totalValueTraded"`
			TotalNetAssets   float64 `json:"totalNetAssets"`
		} `json:"list"`
	} `json:"data"`
}

type sosoCurrentResponse struct {
	Code int `json:"code"`
	Data struct {
		List []struct {
			Ticker         string `json:"ticker"`
			Institute      string `json:"institute"`
			DailyNetInflow struct {
				Value          *float64 `json:"value"`
				LastUpdateDate string   `json:"lastUpdateDate"`
			} `json:"dailyNetInflow"`
		} `json:"list"`
	} `json:"data"`
}

func (s *SoSoValue) Fetch(ctx context.Context) ([]Item, error) {
	if strings.TrimSpace(s.APIKey) == "" {
		return nil, fmt.Errorf("SOSOVALUE_API_KEY is empty")
	}
	endpoint := strings.TrimSpace(s.URL)
	if endpoint == "" {
		endpoint = "https://api.sosovalue.xyz/openapi/v2/etf/historicalInflowChart"
	}
	out := make([]Item, 0, 2)
	for _, spec := range []struct{ apiType, asset, name string }{{"us-btc-spot", "BTC", "Bitcoin"}, {"us-eth-spot", "ETH", "Ethereum"}} {
		payload, _ := json.Marshal(map[string]string{"type": spec.apiType})
		body, err := PostJSON(ctx, s.Client, endpoint, s.APIKey, bytes.NewReader(payload), 4<<20)
		if err != nil {
			return out, err
		}
		var response sosoResponse
		if err := json.Unmarshal(body, &response); err != nil || response.Code != 0 {
			return out, fmt.Errorf("decode SoSoValue %s: code=%d err=%v msg=%s", spec.asset, response.Code, err, response.Msg)
		}
		if len(response.Data.List) == 0 {
			continue
		}
		row := response.Data.List[0]
		tradingDay, err := time.Parse("2006-01-02", row.Date)
		if err != nil {
			continue
		}
		direction := "inflow"
		if row.TotalNetInflow < 0 {
			direction = "outflow"
		}
		title := fmt.Sprintf("U.S. spot %s ETFs recorded a net %s of $%.2f million", spec.name, direction, math.Abs(row.TotalNetInflow)/1_000_000)
		summary := fmt.Sprintf("Trading day %s. Total value traded: $%.2f billion; total net assets: $%.2f billion.", row.Date, row.TotalValueTraded/1_000_000_000, row.TotalNetAssets/1_000_000_000)
		if breakdown := s.fundBreakdown(ctx, spec.apiType, row.Date); breakdown != "" {
			summary += " Fund flows: " + breakdown + "."
		}
		out = append(out, Item{Category: "crypto", Title: title, URL: "https://sosovalue.com/assets/etf/" + strings.ToLower(spec.asset) + "?date=" + row.Date, Source: "SoSoValue", SourceID: s.ID(), SourceType: "api", PublishedAt: tradingDay.Add(23*time.Hour + 59*time.Minute).UTC(), PublishedFallback: true, EventAt: TimePtr(tradingDay.UTC()), EventType: "etf_flow", Summary: summary, OriginalLanguage: "en"})
	}
	return out, nil
}

func (s *SoSoValue) fundBreakdown(ctx context.Context, apiType, tradingDate string) string {
	endpoint := strings.TrimSpace(s.CurrentURL)
	if endpoint == "" {
		endpoint = "https://api.sosovalue.xyz/openapi/v2/etf/currentEtfDataMetrics"
	}
	payload, _ := json.Marshal(map[string]string{"type": apiType})
	body, err := PostJSON(ctx, s.Client, endpoint, s.APIKey, bytes.NewReader(payload), 4<<20)
	if err != nil {
		return ""
	}
	var response sosoCurrentResponse
	if json.Unmarshal(body, &response) != nil || response.Code != 0 {
		return ""
	}
	type fund struct {
		name string
		flow float64
	}
	funds := make([]fund, 0, len(response.Data.List))
	for _, row := range response.Data.List {
		if row.DailyNetInflow.Value == nil || row.DailyNetInflow.LastUpdateDate != tradingDate {
			continue
		}
		name := strings.TrimSpace(row.Ticker)
		if name == "" {
			name = strings.TrimSpace(row.Institute)
		}
		funds = append(funds, fund{name: name, flow: *row.DailyNetInflow.Value})
	}
	sort.Slice(funds, func(i, j int) bool { return math.Abs(funds[i].flow) > math.Abs(funds[j].flow) })
	if len(funds) > 5 {
		funds = funds[:5]
	}
	parts := make([]string, 0, len(funds))
	for _, fund := range funds {
		parts = append(parts, fmt.Sprintf("%s %+.2fM USD", fund.name, fund.flow/1_000_000))
	}
	return strings.Join(parts, ", ")
}

var _ Source = (*SoSoValue)(nil)
