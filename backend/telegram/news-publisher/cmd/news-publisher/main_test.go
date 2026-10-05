package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFormatNewsMessageBuildsReadableCardAndEscapesHTML(t *testing.T) {
	summary := "Рост <сильнее> ожиданий"
	message := formatNewsMessage(newsItem{
		Title:       "BTC < $100k & растёт",
		URL:         "https://example.com/news?a=1&b=2",
		Source:      "Test & News",
		Category:    "crypto",
		Summary:     &summary,
		Assets:      []string{"BTC", "ETH"},
		PublishedAt: time.Date(2026, 7, 16, 10, 30, 0, 0, time.UTC),
	})
	for _, expected := range []string{"КРИПТО • НОВОСТЬ", "BTC &lt; $100k &amp; растёт", "Рост &lt;сильнее&gt; ожиданий", "Test &amp; News", "#BTC", "#ETH", "#Крипто", "16 июля • 10:30 UTC"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message does not contain %q: %s", expected, message)
		}
	}
	if strings.Contains(message, "Читать источник") {
		t.Fatal("source link must be rendered as an inline button")
	}
}

func TestSourceButtonRejectsUnsafeURL(t *testing.T) {
	if sourceButton("javascript:alert(1)") != nil {
		t.Fatal("unsafe URL must not create a button")
	}
	button := sourceButton("https://example.com/news")
	if button == nil {
		t.Fatal("valid URL must create a button")
	}
}

func TestSendFallsBackToTextWhenTelegramRejectsPhoto(t *testing.T) {
	requests := make([]string, 0, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sendPhoto") {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "wrong file identifier"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 42}})
	}))
	defer srv.Close()
	image := "https://example.com/image.jpg"
	bot := telegramClient{token: "test", channel: "@channel", client: srv.Client(), apiBaseURL: srv.URL}
	messageID, err := bot.send(context.Background(), newsItem{ID: "id", Title: "News", URL: "https://example.com/news", Image: &image, PublishedAt: time.Now()})
	if err != nil || messageID != 42 {
		t.Fatalf("messageID=%d err=%v", messageID, err)
	}
	if len(requests) != 2 || !strings.HasSuffix(requests[0], "/sendPhoto") || !strings.HasSuffix(requests[1], "/sendMessage") {
		t.Fatalf("requests=%v", requests)
	}
}

func TestPhotoCaptionStaysWithinTelegramLimit(t *testing.T) {
	summary := strings.Repeat("Очень длинное описание новости. ", 100)
	caption := formatNewsCaption(newsItem{Title: strings.Repeat("Заголовок ", 100), Summary: &summary, Category: "crypto", PublishedAt: time.Now()})
	if len([]rune(caption)) > 1024 {
		t.Fatalf("caption has %d runes", len([]rune(caption)))
	}
}

func TestClaimNewsDoesNotExcludeCalendarOnlyItems(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(source)), "where n.calendar_only=false") {
		t.Fatal("claimNews must not exclude calendar-only items")
	}
}

func TestClaimNewsDoesNotPublishFreshlyInsertedStaleArticles(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	normalized := strings.Join(strings.Fields(strings.ToLower(string(source))), " ")
	if !strings.Contains(normalized, "and n.published_at >= now() - ($3 * interval '1 second')") {
		t.Fatal("new Telegram publications must be bounded by the source published_at timestamp")
	}
	if !strings.Contains(normalized, "and n.publication_eligible = true") {
		t.Fatal("Telegram must only claim news that passed publication policy")
	}
}

func TestParseCategoriesFiltersUnknownAndDuplicates(t *testing.T) {
	got := parseCategories("crypto, macro,world,crypto,other")
	if len(got) != 3 || got[0] != "crypto" || got[1] != "macro" || got[2] != "world" {
		t.Fatalf("parseCategories() = %#v", got)
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	if got := retryDelay(1); got != 15*time.Second {
		t.Fatalf("first retry = %s", got)
	}
	if got := retryDelay(100); got != 5*time.Minute {
		t.Fatalf("maximum retry = %s", got)
	}
}

func TestTruncateRunesPreservesUTF8(t *testing.T) {
	got := truncateRunes(strings.Repeat("я", 20), 10)
	if len([]rune(got)) != 10 || !strings.HasSuffix(got, "…") {
		t.Fatalf("unexpected truncation %q", got)
	}
}
