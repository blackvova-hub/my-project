package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Bybit struct {
	URL    string
	Limit  int
	Client HTTPClient
}

func (s *Bybit) ID() string { return "bybit" }

type bybitResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List []struct {
			Title              string   `json:"title"`
			Description        string   `json:"description"`
			URL                string   `json:"url"`
			DateTimestamp      int64    `json:"dateTimestamp"`
			PublishTime        int64    `json:"publishTime"`
			StartDataTimestamp int64    `json:"startDateTimestamp"`
			StartTimestamp     int64    `json:"startDataTimestamp"`
			Tags               []string `json:"tags"`
			Type               struct {
				Key   string `json:"key"`
				Title string `json:"title"`
			} `json:"type"`
		} `json:"list"`
	} `json:"result"`
}

func (s *Bybit) Fetch(ctx context.Context) ([]Item, error) {
	limit := s.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rawURL := strings.TrimSpace(s.URL)
	if rawURL == "" {
		rawURL = "https://api.bybit.com/v5/announcements/index"
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	// The old HTML announcements page is not the API and can return an HTTP/2
	// frame to an HTTP/1 client. Always use the structured official API when a
	// stale deployment configuration still points at that page.
	if strings.EqualFold(u.Host, "announcements.bybit.com") {
		rawURL = "https://api.bybit.com/v5/announcements/index"
		u, err = url.Parse(rawURL)
		if err != nil {
			return nil, err
		}
	}
	requests := []struct{ key, value string }{{"type", "new_crypto"}, {"type", "delistings"}, {"type", "maintenance_updates"}, {"tag", "Launchpool"}}
	responses := make([]bybitResponse, 0, len(requests))
	errors := make([]string, 0)
	for _, filter := range requests {
		filtered := *u
		q := filtered.Query()
		if q.Get("locale") == "" {
			q.Set("locale", "en-US")
		}
		q.Set("limit", strconv.Itoa(limit))
		q.Set(filter.key, filter.value)
		filtered.RawQuery = q.Encode()
		body, _, fetchErr := Get(ctx, s.Client, filtered.String(), "application/json", 4<<20)
		if fetchErr != nil {
			errors = append(errors, fetchErr.Error())
			continue
		}
		var response bybitResponse
		if decodeErr := json.Unmarshal(body, &response); decodeErr != nil || response.RetCode != 0 {
			errors = append(errors, fmt.Sprintf("decode/filter %s: retCode=%d err=%v", filter.value, response.RetCode, decodeErr))
			continue
		}
		responses = append(responses, response)
	}
	out := make([]Item, 0, len(responses)*limit)
	seen := map[string]bool{}
	for _, response := range responses {
		for _, row := range response.Result.List {
			if seen[row.URL] {
				continue
			}
			seen[row.URL] = true
			published, ok := MillisTime(row.PublishTime)
			fallback := false
			if !ok {
				published, ok = MillisTime(row.DateTimestamp)
				fallback = ok
			}
			if !ok {
				continue
			}
			eventType := ExchangeAnnouncementType(row.Title, row.Type.Key+" "+strings.Join(row.Tags, " "))
			if eventType == "" {
				continue
			}
			var eventAt *time.Time
			if value, found := MillisTime(max64(row.StartDataTimestamp, row.StartTimestamp)); found {
				eventAt = TimePtr(value)
			} else if value, found := ExchangeAnnouncementEventTime(row.Title+" "+row.Description, published, time.UTC); found {
				eventAt = TimePtr(value)
			}
			out = append(out, Item{Category: "crypto", Title: strings.TrimSpace(row.Title), URL: strings.TrimSpace(row.URL), Source: "Bybit", SourceID: s.ID(), SourceType: "official_exchange", PublishedAt: published, PublishedFallback: fallback, EventAt: eventAt, EventType: eventType, Summary: CleanHTML(row.Description), OriginalLanguage: "en", Assets: ExchangeAnnouncementAssets(row.Title)})
		}
	}
	return out, partialError(len(responses), errors)
}

var announcedTime = regexp.MustCompile(`(?i)(?:at|from|on)\s+(20\d{2}[-/]\d{1,2}[-/]\d{1,2}[ T]\d{1,2}:\d{2}(?::\d{2})?\s*(?:UTC|Z)?|[A-Z][a-z]+\s+\d{1,2},?\s+20\d{2},?\s+\d{1,2}:\d{2}\s*(?:UTC|GMT))`)
var numericAnnouncementTime = regexp.MustCompile(`(?i)(20\d{2})[-/.](\d{1,2})[-/.](\d{1,2})[^0-9]{0,12}(\d{1,2}):(\d{2})`)
var shortAnnouncementTime = regexp.MustCompile(`(?i)(\d{1,2})[/.-](\d{1,2})[^0-9]{0,12}(\d{1,2}):(\d{2})`)
var koreanAnnouncementTime = regexp.MustCompile(`(\d{1,2})\s*월\s*(\d{1,2})\s*일(?:\s*\([^)]*\))?[^0-9]{0,12}(\d{1,2}):(\d{2})`)

func parseAnnouncedTime(text string) (time.Time, bool) {
	match := announcedTime.FindStringSubmatch(CleanHTML(text))
	if len(match) < 2 {
		return time.Time{}, false
	}
	value := strings.TrimSpace(strings.NewReplacer("UTC", "", "GMT", "", "Z", "").Replace(match[1]))
	for _, layout := range []string{"2006-1-2 15:04:05", "2006-1-2 15:04", "2006/1/2 15:04", "January 2, 2006, 15:04", "January 2 2006 15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func ExchangeAnnouncementEventTime(text string, published time.Time, location *time.Location) (time.Time, bool) {
	if parsed, ok := parseAnnouncedTime(text); ok {
		return parsed, true
	}
	if location == nil {
		location = time.UTC
	}
	clean := CleanHTML(text)
	if match := numericAnnouncementTime.FindStringSubmatch(clean); len(match) == 6 {
		return buildAnnouncementTime(match[1], match[2], match[3], match[4], match[5], location)
	}
	for _, expression := range []*regexp.Regexp{shortAnnouncementTime, koreanAnnouncementTime} {
		if match := expression.FindStringSubmatch(clean); len(match) == 5 {
			parsed, ok := buildAnnouncementTime(strconv.Itoa(published.In(location).Year()), match[1], match[2], match[3], match[4], location)
			if !ok {
				return time.Time{}, false
			}
			if parsed.Before(published.Add(-12 * time.Hour)) {
				parsed = parsed.AddDate(1, 0, 0)
			}
			return parsed, true
		}
	}
	return time.Time{}, false
}

func buildAnnouncementTime(year, month, day, hour, minute string, location *time.Location) (time.Time, bool) {
	values := make([]int, 5)
	for index, raw := range []string{year, month, day, hour, minute} {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return time.Time{}, false
		}
		values[index] = parsed
	}
	if values[1] < 1 || values[1] > 12 || values[2] < 1 || values[2] > 31 || values[3] > 23 || values[4] > 59 {
		return time.Time{}, false
	}
	parsed := time.Date(values[0], time.Month(values[1]), values[2], values[3], values[4], 0, 0, location)
	if parsed.Month() != time.Month(values[1]) || parsed.Day() != values[2] {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

var _ Source = (*Bybit)(nil)
