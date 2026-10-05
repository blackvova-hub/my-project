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

type FearGreed struct {
	URL       string
	DailyHour int
	Client    HTTPClient
	Now       func() time.Time
}

func (s *FearGreed) ID() string { return "fear_greed" }

type fearGreedResponse struct {
	Data []struct {
		Value          string `json:"value"`
		Classification string `json:"value_classification"`
		Timestamp      string `json:"timestamp"`
	} `json:"data"`
}

func (s *FearGreed) Fetch(ctx context.Context) ([]Item, error) {
	rawURL := strings.TrimSpace(s.URL)
	if rawURL == "" {
		rawURL = "https://api.alternative.me/fng/?limit=2"
	}
	body, _, err := Get(ctx, s.Client, rawURL, "application/json", 1<<20)
	if err != nil {
		return nil, err
	}
	var response fearGreedResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode Fear & Greed: %w", err)
	}
	if len(response.Data) == 0 {
		return nil, nil
	}
	current := response.Data[0]
	value, err := strconv.Atoi(current.Value)
	seconds, timeErr := strconv.ParseInt(current.Timestamp, 10, 64)
	if err != nil || timeErr != nil || seconds <= 0 {
		return nil, fmt.Errorf("invalid Fear & Greed value or timestamp")
	}
	published := time.Unix(seconds, 0).UTC()
	changedClass, changedValue := false, false
	if len(response.Data) > 1 {
		previous, parseErr := strconv.Atoi(response.Data[1].Value)
		changedValue = parseErr == nil && absInt(value-previous) >= 5
		changedClass = !strings.EqualFold(strings.TrimSpace(current.Classification), strings.TrimSpace(response.Data[1].Classification))
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	dailyHour := s.DailyHour
	if dailyHour < 0 || dailyHour > 23 {
		dailyHour = 12
	}
	dailyWindow := now.Hour() == dailyHour
	if !changedClass && !changedValue && !dailyWindow {
		return nil, nil
	}
	reasons := make([]string, 0, 3)
	if changedClass {
		reasons = append(reasons, "classification changed")
	}
	if changedValue {
		reasons = append(reasons, "index moved at least 5 points")
	}
	if dailyWindow {
		reasons = append(reasons, "daily snapshot")
	}
	title := fmt.Sprintf("Crypto Fear & Greed Index: %d (%s)", value, current.Classification)
	link := "https://alternative.me/crypto/fear-and-greed-index/?date=" + url.QueryEscape(published.Format("2006-01-02"))
	return []Item{{Category: "crypto", Title: title, URL: link, Source: "Alternative.me", SourceID: s.ID(), SourceType: "api", PublishedAt: published, EventAt: TimePtr(published), EventType: "market_sentiment", Summary: strings.Join(reasons, "; ") + ".", OriginalLanguage: "en"}}, nil
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

var _ Source = (*FearGreed)(nil)
