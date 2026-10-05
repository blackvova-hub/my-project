package parsertelegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParserExtractsTextKeepsEnglishHashtagsAndSkipsDigest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`<!doctype html><html><body>
		<div class="tgme_widget_message_wrap"><div class="tgme_widget_message" data-post="cryptoattack24/101">
		<div class="tgme_widget_message_text">🚀 #BTC #крипта Strategy купила BTC.<br>Подробности на рынке.</div>
		<a class="tgme_widget_message_date" href="https://t.me/cryptoattack24/101" datetime="2026-07-25T12:34:00+00:00"></a>
		<img src="private-image.jpg">
		</div></div>
		<div class="tgme_widget_message_wrap"><div class="tgme_widget_message" data-post="cryptoattack24/100">
		<div class="tgme_widget_message_text">☕️ Дайджест за сегодня и события на завтра</div>
		<a class="tgme_widget_message_date" href="https://t.me/cryptoattack24/100" datetime="2026-07-25T11:00:00+00:00"></a>
		</div></div>
		</body></html>`))
	}))
	defer server.Close()

	parser := New(server.URL+"/s/cryptoattack24", 10)
	parser.Now = func() time.Time { return time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC) }
	items, err := parser.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want one non-digest post", len(items))
	}
	item := items[0]
	if item.URL != "https://t.me/cryptoattack24/101" {
		t.Fatalf("URL = %q", item.URL)
	}
	if !strings.Contains(item.Summary, "#BTC") || strings.Contains(item.Summary, "#крипта") {
		t.Fatalf("cleaned summary = %q", item.Summary)
	}
	if strings.Contains(item.Summary, "🚀") || item.Image != "" {
		t.Fatalf("media was not removed: summary=%q image=%q", item.Summary, item.Image)
	}
}

func TestParserUsesConditionalRequestForUnchangedPage(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`<div class="tgme_widget_message_wrap"><div class="tgme_widget_message" data-post="cryptoattack24/1"><div class="tgme_widget_message_text">#BTC Новость</div><a class="tgme_widget_message_date" href="https://t.me/cryptoattack24/1" datetime="2026-07-25T12:00:00Z"></a></div></div>`))
	}))
	defer server.Close()

	parser := New(server.URL, 10)
	if _, err := parser.Fetch(context.Background()); err != nil {
		t.Fatalf("first Fetch() error = %v", err)
	}
	items, err := parser.Fetch(context.Background())
	if err != nil {
		t.Fatalf("second Fetch() error = %v", err)
	}
	if len(items) != 0 || requests != 2 {
		t.Fatalf("second fetch items=%d requests=%d, want 0 and 2", len(items), requests)
	}
}

func TestCleanHashtagsRemovesCyrillicTagsOnly(t *testing.T) {
	got := cleanTelegramText("#BTC #ETH #крипта #новости")
	if got != "#BTC #ETH" {
		t.Fatalf("clean hashtags = %q", got)
	}
}
