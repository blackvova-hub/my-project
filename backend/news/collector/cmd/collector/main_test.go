package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSanitizeNewsItemForPostgresRemovesNULFromEveryTextField(t *testing.T) {
	item := sanitizeNewsItemForPostgres(NewsItem{
		Category: "cr\x00ypto", Title: "Bit\x00coin", TitleNorm: "bit\x00coin",
		URL: "https://example.com/a\x00b", URLHash: "ha\x00sh", Source: "so\x00urce",
		Summary: "sum\x00mary", Image: "im\x00age", OriginalTitle: "original\x00 title",
		OriginalText: "original\x00 text", OriginalLanguage: "e\x00n",
		TranslatedTitle: "translated\x00 title", TranslatedSummary: "translated\x00 summary",
		SourceID: "source\x00id", SourceType: "source\x00type", EventType: "event\x00type",
		Entities: []string{"entity\x00one"}, Assets: []string{"BT\x00C"},
	})
	values := []string{
		item.Category, item.Title, item.TitleNorm, item.URL, item.URLHash, item.Source,
		item.Summary, item.Image, item.OriginalTitle, item.OriginalText, item.OriginalLanguage,
		item.TranslatedTitle, item.TranslatedSummary, item.SourceID, item.SourceType, item.EventType,
		item.Entities[0], item.Assets[0],
	}
	for _, value := range values {
		if strings.ContainsRune(value, '\x00') {
			t.Fatalf("NUL remained in sanitized value %q", value)
		}
	}
	if item.Title != "Bitcoin" || item.Summary != "summary" || item.Assets[0] != "BTC" {
		t.Fatalf("unexpected sanitized item: %+v", item)
	}
}

func TestFetchBinanceAnnouncements(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"code":"000000",
			"success":true,
			"data":{"articles":[
				{"code":"abc123","title":"Binance Will List TEST","releaseDate":1710000000000},
				{"code":"ignored","title":"Second item","releaseDate":1710000001000}
			]}
		}`)
	}))
	defer server.Close()

	items, err := fetchBinanceAnnouncements(context.Background(), Config{
		BinanceListingsURL: server.URL,
		BinanceLimit:       1,
	})
	if err != nil {
		t.Fatalf("fetch Binance announcements: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	item := items[0]
	if item.Title != "Binance Will List TEST" || item.Source != "Binance" {
		t.Fatalf("unexpected item: %+v", item)
	}
	if !item.CalendarOnly {
		t.Fatal("Binance announcement must be a calendar-only event")
	}
	if item.URL != "https://www.binance.com/en/support/announcement/detail/abc123" {
		t.Fatalf("unexpected URL: %s", item.URL)
	}
	wantPublished := time.UnixMilli(1710000000000).UTC()
	if !item.Published.Equal(wantPublished) {
		t.Fatalf("published=%s want=%s", item.Published, wantPublished)
	}
	if item.SourceType != "official_exchange" || item.EventType != "spot_listing" || len(item.Assets) != 1 || item.Assets[0] != "TEST" {
		t.Fatalf("unexpected official contract: %+v", item)
	}
}

func TestOfficialExchangeFilterBypassIsNarrow(t *testing.T) {
	at := time.Now().UTC().Add(time.Hour)
	valid := NewsItem{SourceType: "official_exchange", EventType: "spot_listing", EventAt: &at, Assets: []string{"TEST"}}
	if !validOfficialExchangeAnnouncement(valid) {
		t.Fatal("valid official announcement rejected")
	}
	valid.EventAt = nil
	if validOfficialExchangeAnnouncement(valid) {
		t.Fatal("announcement without event_at bypassed filter")
	}
}

func TestPublicationPolicyRejectsOffTopicCryptoAndMalformedPayloads(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		item NewsItem
		want bool
	}{
		{name: "bitcoin market news", item: NewsItem{Category: "crypto", Title: "Bitcoin falls below $66,000"}, want: true},
		{name: "unrelated AI story", item: NewsItem{Category: "crypto", Title: "AI models escaped a sandbox and hit Hugging Face"}, want: false},
		{name: "ticker is not a substring", item: NewsItem{Category: "crypto", Title: "Together we build better software"}, want: false},
		{name: "financial macro", item: NewsItem{Category: "macro", Title: "Federal Reserve announces an interest rate cut"}, want: true},
		{name: "malformed editor json", item: NewsItem{Category: "crypto", Title: "Binance lists BTC", Summary: `{"node":"root","children":[]}`}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPublicationEligible(Config{}, tt.item, now); got != tt.want {
				t.Fatalf("eligible=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestOfficialAnnouncementRequiresUpcomingEvent(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	at := now.Add(24 * time.Hour)
	item := NewsItem{Category: "crypto", SourceType: "official_exchange", EventType: "spot_listing", EventAt: &at, Assets: []string{"TEST"}}
	if !validOfficialExchangeAnnouncementAt(item, now) {
		t.Fatal("upcoming official announcement rejected")
	}
	at = now.Add(8 * 24 * time.Hour)
	item.EventAt = &at
	if validOfficialExchangeAnnouncementAt(item, now) {
		t.Fatal("announcement outside seven-day horizon accepted")
	}
}

func TestFetchBinanceAnnouncementsRejectsFailedPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"code":"100001","success":false,"data":{"articles":[]}}`)
	}))
	defer server.Close()

	_, err := fetchBinanceAnnouncements(context.Background(), Config{
		BinanceListingsURL: server.URL,
	})
	if err == nil {
		t.Fatal("expected Binance payload error")
	}
}

func TestFetchFreshRSSKeepsOnlyCurrentWuBlockchainItems(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0"><channel>
<title>Wu Blockchain - Crypto News &amp; Insights</title>
<item><title>Fresh item</title><link>https://wublockchain.xyz/news/fresh</link><pubDate>%s</pubDate><description>Fresh summary</description></item>
<item><title>Stale item</title><link>https://wublockchain.xyz/news/stale</link><pubDate>%s</pubDate></item>
<item><title>Future item</title><link>https://wublockchain.xyz/news/future</link><pubDate>%s</pubDate></item>
</channel></rss>`,
			now.Add(-2*time.Hour).Format(time.RFC1123Z),
			now.Add(-73*time.Hour).Format(time.RFC1123Z),
			now.Add(time.Hour).Format(time.RFC1123Z),
		)
	}))
	defer server.Close()

	items, err := fetchFreshRSS(context.Background(), server.URL, "crypto", 50, 72*time.Hour)
	if err != nil {
		t.Fatalf("fetch fresh RSS: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want one current item: %+v", len(items), items)
	}
	item := items[0]
	if item.Title != "Fresh item" || item.Category != "crypto" || item.Source != "Wu Blockchain - Crypto News & Insights" {
		t.Fatalf("unexpected WuBlockchain item: %+v", item)
	}
	if item.Summary != "Fresh summary" {
		t.Fatalf("summary=%q", item.Summary)
	}
}

func TestBuildSourcesPrioritizesWuBlockchain(t *testing.T) {
	sources := buildSources(Config{
		WuBlockchainRSSURL:      "https://www.wublockchain.xyz/rss",
		WuBlockchainLimit:       50,
		WuBlockchainMaxAgeHours: 72,
		CoinDeskRSSURL:          "https://example.com/coindesk.xml",
	})
	if len(sources) != 2 || sources[0].name != "wublockchain_rss" {
		t.Fatalf("unexpected source order: %+v", sources)
	}
}

func TestSimilarTitleDeduplicatesCrossSourceRewording(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{
			name: "same words in another order",
			a:    "Bitcoin spot ETFs record 500 million dollars in daily inflows",
			b:    "Daily inflows in Bitcoin spot ETFs record 500 million dollars",
			want: true,
		},
		{
			name: "one source adds context",
			a:    "Bitcoin spot ETFs record 500 million dollars in daily inflows",
			b:    "Bitcoin spot ETFs record 500 million dollars in daily net inflows today",
			want: true,
		},
		{
			name: "different asset is another story",
			a:    "Bitcoin spot ETFs record 500 million dollars in daily inflows",
			b:    "Ethereum spot ETFs record 500 million dollars in daily inflows",
			want: false,
		},
		{
			name: "different amount is another update",
			a:    "Bitcoin spot ETFs record 500 million dollars in daily inflows",
			b:    "Bitcoin spot ETFs record 900 million dollars in daily inflows",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSimilarTitle(normalizeTitle(tt.a), normalizeTitle(tt.b))
			if got != tt.want {
				t.Fatalf("isSimilarTitle()=%v want=%v", got, tt.want)
			}
		})
	}
}
