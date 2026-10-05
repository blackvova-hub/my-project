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

type Upbit struct {
	URL    string
	Limit  int
	Client HTTPClient
}

func (s *Upbit) ID() string { return "upbit" }

type upbitNotice struct {
	ID            json.Number `json:"id"`
	Title         string      `json:"title"`
	FirstListedAt string      `json:"first_listed_at"`
	ListedAt      string      `json:"listed_at"`
	Category      string      `json:"category"`
}

type upbitResponse struct {
	Data struct {
		Notices []upbitNotice `json:"notices"`
		List    []upbitNotice `json:"list"`
	} `json:"data"`
}

func (s *Upbit) Fetch(ctx context.Context) ([]Item, error) {
	rawURL := strings.TrimSpace(s.URL)
	if rawURL == "" {
		rawURL = "https://api-manager.upbit.com/api/v1/announcements"
	}
	limit := s.Limit
	if limit <= 0 {
		limit = 20
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("page", "1")
	q.Set("per_page", strconv.Itoa(limit))
	q.Set("os", "web")
	q.Set("category", "all")
	u.RawQuery = q.Encode()
	body, _, err := Get(ctx, s.Client, u.String(), "application/json", 4<<20)
	if err != nil {
		return nil, err
	}
	var response upbitResponse
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("decode Upbit notices: %w", err)
	}
	rows := response.Data.Notices
	if len(rows) == 0 {
		rows = response.Data.List
	}
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		if strings.Contains(row.Title, "코인모으기") {
			continue
		}
		eventType := exchangeNoticeType(row.Title)
		if eventType == "" {
			continue
		}
		published, fallback, ok := parseSourceTime(row.FirstListedAt, row.ListedAt)
		if !ok {
			continue
		}
		id := row.ID.String()
		summary := s.fetchDetail(ctx, u, id)
		var eventAt *time.Time
		if value, found := ExchangeAnnouncementEventTime(row.Title+" "+summary, published, time.FixedZone("KST", 9*60*60)); found {
			eventAt = TimePtr(value)
		}
		out = append(out, Item{Category: "crypto", Title: strings.TrimSpace(row.Title), URL: "https://upbit.com/service_center/notice?id=" + url.QueryEscape(id), Source: "Upbit", SourceID: s.ID(), SourceType: "official_exchange", PublishedAt: published, PublishedFallback: fallback, EventAt: eventAt, EventType: eventType, Summary: summary, OriginalLanguage: "ko", Assets: ExchangeAnnouncementAssets(row.Title)})
	}
	return out, nil
}

func (s *Upbit) fetchDetail(ctx context.Context, listURL *url.URL, id string) string {
	detailURL := *listURL
	detailURL.RawQuery = ""
	detailURL.Path = strings.TrimSuffix(detailURL.Path, "/") + "/" + url.PathEscape(id)
	body, _, err := Get(ctx, s.Client, detailURL.String(), "application/json", 4<<20)
	if err != nil {
		return ""
	}
	var response struct {
		Data struct {
			Body string `json:"body"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ""
	}
	return CleanHTML(response.Data.Body)
}

func parseSourceTime(primary, fallbackValue string) (time.Time, bool, bool) {
	for index, value := range []string{primary, fallbackValue} {
		value = strings.TrimSpace(value)
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-07:00", "2006-01-02 15:04:05", "2006-01-02"} {
			if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
				return parsed.UTC(), index > 0, true
			}
		}
	}
	return time.Time{}, false, false
}

func exchangeNoticeType(title string) string {
	text := strings.ToLower(title)
	switch {
	case containsAny(text, "delist", "termination of trading support", "거래지원 종료", "거래 지원 종료"):
		return "delisting"
	case containsAny(text, "listing", "new digital asset", "신규 거래지원", "신규 디지털 자산 지원", "거래지원 안내", "원화 마켓 추가"):
		return "spot_listing"
	case containsAny(text, "suspend trading", "trading suspended", "suspension of trading", "temporary trading suspension", "trading halt", "거래 일시 중단", "거래 중단"):
		return "trading_suspension"
	default:
		return ""
	}
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

var _ Source = (*Upbit)(nil)
