package sources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFederalRegisterUsesPublicationAndSigningDates(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/list":
			_, _ = fmt.Fprintf(w, `{"results":[{"title":"Digital Assets Order","document_number":"2026-00001","json_url":%q,"publication_date":"2026-07-21"}]}`, server.URL+"/detail")
		case "/detail":
			_, _ = fmt.Fprintf(w, `{"title":"Digital Assets Order","document_number":"2026-00001","html_url":"https://www.federalregister.gov/d/2026-00001","raw_text_url":%q,"subtype":"Executive Order","publication_date":"2026-07-21","signing_date":"2026-07-20"}`, server.URL+"/text")
		case "/text":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("This executive order establishes policy for digital assets and stablecoins."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	items, err := (&FederalRegister{URL: server.URL + "/list", Limit: 10}).Fetch(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	item := items[0]
	if item.SourceType != "official_government" || item.EventType != "major_statement" ||
		item.PublishedAt.Format("2006-01-02") != "2026-07-21" || item.EventAt == nil || item.EventAt.Format("2006-01-02") != "2026-07-20" {
		t.Fatalf("unexpected item: %+v", item)
	}
}

func fixtureServer(t *testing.T, name, contentType string) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
}

type rewriteURLClient struct {
	base string
}

func (c rewriteURLClient) Do(req *http.Request) (*http.Response, error) {
	target, err := url.Parse(c.base)
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.URL.Scheme = target.Scheme
	clone.URL.Host = target.Host
	clone.URL.Path = target.Path
	clone.URL.RawPath = target.RawPath
	return http.DefaultClient.Do(clone)
}

func TestBybitSeparatesPublicationAndEventTime(t *testing.T) {
	server := fixtureServer(t, "bybit.json", "application/json")
	defer server.Close()
	items, err := (&Bybit{URL: server.URL, Limit: 20}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items", len(items))
	}
	item := items[0]
	if item.EventAt == nil || item.PublishedAt.Equal(*item.EventAt) {
		t.Fatalf("publication and event time were not separated: %+v", item)
	}
	if item.PublishedAt.UnixMilli() != 1784632381000 || item.EventAt.UnixMilli() != 1784714400000 {
		t.Fatalf("unexpected timestamps published=%s event=%s", item.PublishedAt, item.EventAt)
	}
	if item.EventType != "spot_listing" || item.SourceType != "official_exchange" || len(item.Assets) != 1 || item.Assets[0] != "TEST" || item.PublishedFallback {
		t.Fatalf("unexpected classification: %+v", item)
	}
}

func TestUpbitUsesFirstListedAt(t *testing.T) {
	server := fixtureServer(t, "upbit.json", "application/json")
	defer server.Close()
	items, err := (&Upbit{URL: server.URL}).Fetch(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	want := time.Date(2026, 7, 21, 7, 30, 0, 0, time.UTC)
	if !items[0].PublishedAt.Equal(want) || items[0].EventType != "spot_listing" || items[0].SourceType != "official_exchange" || len(items[0].Assets) != 2 {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}

func TestBithumbOfficialAPIUsesKSTPublication(t *testing.T) {
	server := fixtureServer(t, "bithumb.json", "application/json")
	defer server.Close()
	items, err := (&Bithumb{URL: server.URL}).Fetch(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	want := time.Date(2026, 7, 21, 7, 20, 56, 0, time.UTC)
	if !items[0].PublishedAt.Equal(want) || items[0].EventType != "spot_listing" || items[0].SourceType != "official_exchange" || len(items[0].Assets) != 1 || items[0].Assets[0] != "TEST" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}

func TestBybitNormalizesLegacyHTMLEndpoint(t *testing.T) {
	server := fixtureServer(t, "bybit.json", "application/json")
	defer server.Close()
	items, err := (&Bybit{URL: "https://announcements.bybit.com/en/?category=new_crypto", Client: rewriteURLClient{base: server.URL}}).Fetch(context.Background())
	if err != nil || len(items) == 0 {
		t.Fatalf("legacy endpoint normalization failed: items=%d err=%v", len(items), err)
	}
}

func TestExchangeAnnouncementClassifiesFuturesAndAssets(t *testing.T) {
	if got := ExchangeAnnouncementType("Bybit Will List TESTUSDT Perpetual Contract", "new_crypto"); got != "futures_listing" {
		t.Fatalf("type=%q", got)
	}
	assets := ExchangeAnnouncementAssets("New Listing: TEST/USDT")
	if len(assets) != 1 || assets[0] != "TEST" {
		t.Fatalf("assets=%v", assets)
	}
}

func TestWalletMaintenanceIsNotTradingSuspension(t *testing.T) {
	if got := exchangeNoticeType("NEO N3 네트워크 디지털 자산 입출금 일시 중단 안내"); got != "" {
		t.Fatalf("wallet maintenance classified as %q", got)
	}
	if got := ExchangeAnnouncementType("Scheduled wallet maintenance for BTC", "maintenance_updates"); got != "" {
		t.Fatalf("wallet maintenance classified as %q", got)
	}
	if got := ExchangeAnnouncementType("Exchange will suspend trading for TEST", ""); got != "trading_suspension" {
		t.Fatalf("real trading suspension classified as %q", got)
	}
	if got := exchangeNoticeType("TEST 거래 일시 중단"); got != "trading_suspension" {
		t.Fatalf("real trading suspension classified as %q", got)
	}
}

func TestExchangeAnnouncementParsesKoreanShortDateInKST(t *testing.T) {
	published := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	got, ok := ExchangeAnnouncementEventTime("입출금 일시 중단 안내 (07/22 18:00 ~)", published, time.FixedZone("KST", 9*60*60))
	want := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("event_at=%s ok=%v want=%s", got, ok, want)
	}
}

func TestExchangeAnnouncementParsesKoreanNamedDate(t *testing.T) {
	published := time.Date(2026, 7, 21, 1, 0, 0, 0, time.UTC)
	got, ok := ExchangeAnnouncementEventTime("거래지원 종료 7월 22일 12:10", published, time.FixedZone("KST", 9*60*60))
	want := time.Date(2026, 7, 22, 3, 10, 0, 0, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("event_at=%s ok=%v want=%s", got, ok, want)
	}
}

func TestWhiteHouseUsesOfficialGovernmentContract(t *testing.T) {
	server := fixtureServer(t, "white_house.xml", "application/rss+xml")
	defer server.Close()
	items, err := (&WhiteHouse{URL: server.URL, Limit: 10}).Fetch(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	item := items[0]
	if item.SourceType != "official_government" || item.EventType != "major_statement" || len(item.Entities) != 1 {
		t.Fatalf("unexpected item: %+v", item)
	}
}

func TestFearGreedPublishesFivePointMove(t *testing.T) {
	server := fixtureServer(t, "fear_greed.json", "application/json")
	defer server.Close()
	source := &FearGreed{URL: server.URL, DailyHour: 12, Now: func() time.Time { return time.Date(2026, 7, 21, 5, 0, 0, 0, time.UTC) }}
	items, err := source.Fetch(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	if items[0].EventType != "market_sentiment" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}

func TestSECRequiresExplicitRelevantSubject(t *testing.T) {
	if secRelevant("SEC publishes office hours", "general administrative update") {
		t.Fatal("generic SEC item passed")
	}
	if !secRelevant("SEC clarifies crypto asset securities rules", "digital asset markets") {
		t.Fatal("crypto regulation was rejected")
	}
}
