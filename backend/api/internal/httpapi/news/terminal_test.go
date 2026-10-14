package newsapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var terminalTestNow = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func terminalTestItem(id, title, category, source string, minutes int, score int) TerminalNewsItem {
	item := TerminalNewsItem{ID: id, Title: title, Category: category, SourceID: source, Source: source, URL: "https://example.org/" + id, PublishedAt: terminalTestNow.Add(-time.Duration(minutes) * time.Minute).Format(time.RFC3339), TimestampBasis: "published"}
	enrichTerminalItem(&item, false, terminalTestNow)
	if score > 0 {
		item.ImportanceScore = score
	}
	return item
}

func TestTerminalRSSUsesOnlyPublishedDatesAndBoundedExcerpt(t *testing.T) {
	source := terminalSource{TerminalSourceHealth: TerminalSourceHealth{ID: "federal_reserve", Name: "Fed"}, official: true, category: "macro"}
	rss := fmt.Sprintf(`<?xml version="1.0"?><rss><channel><language>en</language>
	<item><title>Fed monetary policy &amp; inflation</title><link>https://federalreserve.gov/news/1</link><pubDate>Wed, 14 Oct 2026 07:00:00 EDT</pubDate><description><![CDATA[<p>%s</p>]]></description></item>
	<item><title>Missing timestamp must not use ingestion time</title><link>https://federalreserve.gov/news/2</link></item>
	<item><title>Bad URL must be rejected</title><link>javascript:alert(1)</link><pubDate>Wed, 14 Oct 2026 11:00:00 GMT</pubDate></item>
	<item><title>Scheduled future release is not published news</title><link>https://federalreserve.gov/news/3</link><pubDate>Thu, 15 Oct 2026 11:00:00 GMT</pubDate></item>
	</channel></rss>`, strings.Repeat("quoted feed sentence ", 100))
	items, err := parseTerminalRSS([]byte(rss), source, terminalTestNow)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	item := items[0]
	if item.PublishedAt != "2026-10-14T11:00:00Z" {
		t.Fatalf("EDT shifted incorrectly: %s", item.PublishedAt)
	}
	if len([]rune(item.Summary)) > 421 || strings.Contains(item.Summary, "<p>") {
		t.Fatalf("unbounded or unclean excerpt: %s", item.Summary)
	}
	if item.Breaking || item.Importance != "high" || !terminalMember("USD", item.Assets...) {
		t.Fatalf("incorrect enrichment: %+v", item)
	}
	if _, err := parseTerminalRSS([]byte(`<html>rate-limit response</html>`), source, terminalTestNow); err == nil {
		t.Fatal("HTML was accepted as an empty successful RSS feed")
	}
}

func TestTerminalAtomDoesNotInventPublicationFromUpdated(t *testing.T) {
	body := []byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Ethereum protocol release</title><link rel="alternate" href="https://example.org/1"/><published>2026-10-14T08:00:00Z</published><summary>New release</summary></entry><entry><title>Only updated</title><link href="https://example.org/2"/><updated>2026-10-14T09:00:00Z</updated></entry></feed>`)
	items, err := parseTerminalRSS(body, terminalSource{category: "crypto"}, terminalTestNow)
	if err != nil || len(items) != 1 || items[0].PublishedAt != "2026-10-14T08:00:00Z" {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestTerminalGDELTPreservesIndexedTimestampAndExcludesRestrictedPublishers(t *testing.T) {
	body := []byte(`{"articles":[{"title":"Bitcoin and Ethereum ETF flows","url":"https://example.org/crypto","domain":"example.org","seendate":"20261014T110000Z","language":"English"},{"title":"Restricted publication","url":"https://www.coindesk.com/restricted","seendate":"20261014T110000Z"},{"title":"Invalid","url":"javascript:alert(1)","seendate":"20261014T110000Z"}]}`)
	items, err := parseTerminalGDELT(body, terminalSource{TerminalSourceHealth: TerminalSourceHealth{ID: "gdelt"}}, terminalTestNow)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if items[0].TimestampBasis != "indexed" || items[0].Summary != "" || items[0].Category != "crypto" || !terminalMember("BTC", items[0].Assets...) || !terminalMember("ETH", items[0].Assets...) {
		t.Fatalf("incorrect metadata: %+v", items[0])
	}
	if _, err := parseTerminalGDELT([]byte(`{"error":"Too many requests"}`), terminalSource{}, terminalTestNow); err == nil {
		t.Fatal("provider error accepted")
	}
}

func TestTerminalDeduplicatesTrackingURLsAndCrossSourceHeadlines(t *testing.T) {
	a := terminalTestItem("1", "Bitcoin ETF approved: new institutional access", "crypto", "source1", 1, 0)
	b := a
	b.URL = a.URL + "?utm_source=x&fbclid=y"
	b.SourceID = "source2"
	c := terminalTestItem("3", "BREAKING: Bitcoin ETF approved — new institutional access!", "crypto", "source3", 2, 0)
	d := terminalTestItem("4", a.Title, "crypto", "source4", 25*60, 0)
	items := deduplicateTerminal([]TerminalNewsItem{a, b, c, d}, terminalTestNow)
	if len(items) != 2 {
		t.Fatalf("expected one current headline and independent older release, got %d: %+v", len(items), items)
	}
	if items[0].ID != terminalHash(canonicalTerminalURL(a.URL))[:24] {
		t.Fatal("stable canonical id lost")
	}
	for _, raw := range []string{"javascript:alert(1)", "https://user:pass@example.org/1", "file:///tmp/article", "/relative"} {
		if canonicalTerminalURL(raw) != "" {
			t.Fatalf("unsafe url accepted %s", raw)
		}
	}
}

func TestTerminalCursorStableAcrossRefreshAndRejectsFilterChanges(t *testing.T) {
	items := []TerminalNewsItem{terminalTestItem("a", "Bitcoin price reaction", "crypto", "one", 1, 20), terminalTestItem("b", "Ethereum price reaction", "crypto", "one", 2, 30), terminalTestItem("c", "Treasury yield reaction", "markets", "two", 3, 90)}
	f, err := parseTerminalFilter(url.Values{"limit": {"1"}}, terminalTestNow)
	if err != nil {
		t.Fatal(err)
	}
	first, total, cursor := terminalPage(items, f, terminalTestNow)
	if len(first) != 1 || first[0].ID != "a" || total != 3 || cursor == nil {
		t.Fatalf("first page invalid %+v %d %v", first, total, cursor)
	}
	secondFilter, err := parseTerminalFilter(url.Values{"limit": {"1"}, "cursor": {*cursor}}, terminalTestNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	newItem := terminalTestItem("new", "New Bitcoin news", "crypto", "one", 0, 80)
	newItem.PublishedAt = terminalTestNow.Add(time.Second).Format(time.RFC3339)
	second, _, _ := terminalPage(append(items, newItem), secondFilter, terminalTestNow.Add(time.Minute))
	if len(second) != 1 || second[0].ID != "b" {
		t.Fatalf("page shifted after refresh %+v", second)
	}
	if _, err := parseTerminalFilter(url.Values{"cursor": {*cursor}, "asset": {"BTC"}}, terminalTestNow); err == nil {
		t.Fatal("cursor accepted different filters")
	}
	if _, err := parseTerminalFilter(url.Values{"cursor": {"bad"}}, terminalTestNow); err == nil {
		t.Fatal("malformed cursor accepted")
	}
	decoded, _ := base64.RawURLEncoding.DecodeString(*cursor)
	var forged terminalCursor
	_ = json.Unmarshal(decoded, &forged)
	forged.ID = "forged-anchor"
	data, _ := json.Marshal(forged)
	if _, err := parseTerminalFilter(url.Values{"cursor": {base64.RawURLEncoding.EncodeToString(data)}}, terminalTestNow); err == nil {
		t.Fatal("tampered signed cursor accepted")
	}
}

func TestTerminalFilteringAndImportanceOrder(t *testing.T) {
	items := []TerminalNewsItem{terminalTestItem("btc", "Bitcoin ETF trading opens", "crypto", "one", 1, 30), terminalTestItem("macro", "Fed interest rate decision", "macro", "two", 2, 90), terminalTestItem("eth", "Ethereum network upgrade", "crypto", "one", 3, 40)}
	f, _ := parseTerminalFilter(url.Values{"asset": {"BTC"}, "q": {"etf"}, "source": {"one"}, "category": {"crypto"}}, terminalTestNow)
	page, total, _ := terminalPage(items, f, terminalTestNow)
	if len(page) != 1 || total != 1 || page[0].ID != "btc" {
		t.Fatalf("filters failed %+v", page)
	}
	f, _ = parseTerminalFilter(url.Values{"sort": {"important"}}, terminalTestNow)
	page, _, _ = terminalPage(items, f, terminalTestNow)
	if page[0].ID != "macro" || page[1].ID != "eth" {
		t.Fatalf("wrong importance order %+v", page)
	}
	for _, query := range []url.Values{{"category": {"fake"}}, {"importance": {"extreme"}}, {"limit": {"101"}}, {"limit": {"1 OR 1=1"}}, {"asset": {"BTC'"}}, {"from": {"tomorrow"}}, {"from": {"2026-10-14"}, "to": {"2026-10-13"}}, {"q": {strings.Repeat("я", 201)}}} {
		if _, err := parseTerminalFilter(query, terminalTestNow); err == nil {
			t.Fatalf("invalid query accepted %v", query)
		}
	}
}

func TestTerminalSyncConditionalRequestsAndRateLimit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		switch n {
		case 1:
			w.Header().Set("ETag", "first")
			fmt.Fprint(w, `<rss><channel><item><title>Bitcoin ETF approval update</title><link>https://example.org/1</link><pubDate>Wed, 14 Oct 2026 11:00:00 GMT</pubDate></item></channel></rss>`)
		case 2:
			if r.Header.Get("If-None-Match") != "first" {
				t.Errorf("conditional header missing")
			}
			w.WriteHeader(http.StatusNotModified)
		default:
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer server.Close()
	s := newTerminalService(nil)
	s.now = func() time.Time { return terminalTestNow }
	s.sources = []terminalSource{{TerminalSourceHealth: TerminalSourceHealth{ID: "test", Name: "Test source", URL: server.URL, Enabled: true, IntervalSeconds: 300}, kind: "rss", category: "crypto"}}
	s.syncSource(context.Background(), 0, s.sources[0])
	if len(s.items) != 1 || s.sources[0].LastSuccess == nil || s.sources[0].ItemCount != 1 {
		t.Fatalf("initial sync failed %+v", s.sources[0])
	}
	s.syncSource(context.Background(), 0, s.sources[0])
	if len(s.items) != 1 || s.sources[0].ItemCount != 1 {
		t.Fatal("304 erased retained data")
	}
	s.syncSource(context.Background(), 0, s.sources[0])
	if len(s.items) != 1 || s.sources[0].LastError == nil || s.sources[0].nextAttempt != terminalTestNow.Add(time.Hour) || s.healthLocked(terminalTestNow)[0].Status != "error" {
		t.Fatal("rate-limit retry or cached fallback broken")
	}
}

func TestTerminalSourceHealthIsRealAndDisabledSourcesStayDisabled(t *testing.T) {
	t.Setenv("NEWS_TERMINAL_LICENSED_SOURCES", "")
	s := newTerminalService(nil)
	for _, h := range s.healthLocked(terminalTestNow) {
		if terminalMember(h.ID, "coindesk", "cointelegraph", "cryptoslate", "decrypt", "bbc") && (h.Enabled || h.Status != "disabled" || h.Reason == "") {
			t.Fatalf("restricted source enabled %+v", h)
		}
		if h.LastSuccess != nil || h.LastAttempt != nil {
			t.Fatal("fabricated initial source health")
		}
	}
	s.sources[0].LastSuccess = nullableTerminalTime(terminalTestNow.Add(-24 * time.Hour))
	if s.healthLocked(terminalTestNow)[0].Status != "stale" {
		t.Fatal("stale source not marked")
	}
}

func TestTerminalHandlerValidatesBeforeStartingNetwork(t *testing.T) {
	s := newTerminalService(nil)
	recorder := httptest.NewRecorder()
	s.handleList(recorder, httptest.NewRequest(http.MethodGet, "/news/terminal?sort=arbitrary", nil))
	if recorder.Code != http.StatusBadRequest || s.syncing {
		t.Fatal("invalid query triggered network work")
	}
	var response map[string]any
	if json.Unmarshal(recorder.Body.Bytes(), &response) != nil || response["error"] != "invalid_sort" {
		t.Fatal("unexpected validation response")
	}
}

// Explicit opt-in smoke test for provider availability. Ordinary unit tests do
// not use the network or fabricate data in the running terminal.
func TestTerminalLiveSources(t *testing.T) {
	if os.Getenv("NEWS_TERMINAL_LIVE_TEST") != "1" {
		t.Skip("set NEWS_TERMINAL_LIVE_TEST=1 to verify live providers")
	}
	s := newTerminalService(nil)
	s.synchronize()
	ok, itemCount := 0, len(s.items)
	for _, h := range s.healthLocked(s.now()) {
		if !h.Enabled {
			continue
		}
		t.Logf("%s status=%s count=%d error=%v", h.ID, h.Status, h.ItemCount, h.LastError)
		if h.Status == "ok" {
			ok++
		}
	}
	if ok < 2 {
		t.Fatalf("fewer than two independent real feeds available: %d", ok)
	}
	if itemCount == 0 {
		t.Fatal("no recent real news returned")
	}
	for _, item := range s.items {
		if canonicalTerminalURL(item.URL) == "" || item.ID == "" || item.PublishedAt == "" {
			t.Fatalf("invalid live news metadata %+v", item)
		}
	}
	t.Logf("normalized real news items=%d", itemCount)
}
