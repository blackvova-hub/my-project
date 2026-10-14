package macroapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestFREDCSVSkipsMissingAndDeduplicates(t *testing.T) {
	data := []byte("observation_date,CPIAUCSL,DGS10\n2026-01-01,330,.\n2026-02-01,NaN,4.2\n2026-01-01,331,4.1\ninvalid,333,4.4\n2026-03-01,332,+Inf\n")
	points, err := parseFREDCSV(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := points["CPIAUCSL"]; len(got) != 2 || got[0].Value != 331 || got[1].Date != "2026-03-01" {
		t.Fatalf("bad CPI observations: %+v", got)
	}
	if got := points["DGS10"]; len(got) != 2 || got[0].Value != 4.1 {
		t.Fatalf("missing values were invented: %+v", got)
	}
	if _, err = parseFREDCSV([]byte("<html>access denied</html>")); err == nil {
		t.Fatal("HTML must be rejected")
	}
}

func TestCalendarRecoveryPreservesProviderBudgets(t *testing.T) {
	now := time.Date(2026, 10, 14, 4, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		key, status   string
		age, interval time.Duration
		want          bool
	}{
		{"bea-calendar", "unavailable", 4 * time.Minute, calendarInterval, false},
		{"bea-calendar", "unavailable", 5 * time.Minute, calendarInterval, true},
		{"bls-calendar", "stale", 5 * time.Minute, calendarInterval, true},
		{"bea-calendar", "ok", 5 * time.Minute, calendarInterval, false},
		{"fred", "unavailable", 5 * time.Minute, macroInterval, false},
		{"bls-data", "unavailable", 5 * time.Minute, macroInterval, false},
	} {
		if got := refreshDue(tc.key, tc.status, now.Add(-tc.age), now, tc.interval); got != tc.want {
			t.Fatalf("%s/%s: got %v want %v", tc.key, tc.status, got, tc.want)
		}
	}
}

func TestFREDMixedFrequencyZIP(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, body := range map[string]string{"README.txt": "Source: government data via FRED", "monthly.csv": "observation_date,CPIAUCSL\n2026-08-01,334.131\n", "daily,_7-day.csv": "observation_date,DFEDTARU\n2026-10-13,4.0\n"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	points, err := parseFREDDownload(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(points["CPIAUCSL"]) != 1 || len(points["DFEDTARU"]) != 1 {
		t.Fatalf("mixed-frequency archive dropped series: %+v", points)
	}
}

func TestBLSAnnualAverageIsNotAnExtraMonth(t *testing.T) {
	data := []byte(`{"status":"REQUEST_SUCCEEDED","Results":{"series":[{"seriesID":"LNS14000000","data":[{"year":"2026","period":"M13","value":"4.2"},{"year":"2026","period":"M02","value":"4.1"},{"year":"2026","period":"M01","value":"4.0"},{"year":"2026","period":"M03","value":"-"}]}]}}`)
	got, err := parseBLS(data)
	if err != nil {
		t.Fatal(err)
	}
	p := got["LNS14000000"]
	if len(p) != 2 || p[0].Date != "2026-01-01" {
		t.Fatalf("bad BLS data: %+v", p)
	}
}

func TestECBCSVHonorsHeaderOrderAndMissingValues(t *testing.T) {
	data := []byte("OBS_VALUE,KEY,TIME_PERIOD\n2.5,ECB_DFR,2026-01-01\n,ECB_DFR,2026-02-01\n2.25,ECB_DFR,2026-03-01\n")
	p, err := parseECBCSV(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 2 || p[1].Value != 2.25 {
		t.Fatalf("bad ECB data %+v", p)
	}
}

func TestICSUnfoldsAndUsesDST(t *testing.T) {
	ics := `BEGIN:VCALENDAR
X-WR-TIMEZONE:America/New_York
BEGIN:VEVENT
UID:before-dst
SUMMARY:Consumer Price Index\, February
  2026
DTSTART;TZID=America/New_York:20260306T083000
END:VEVENT
BEGIN:VEVENT
UID:after-dst
SUMMARY:Employment Situation\, March 2026
DTSTART;TZID=America/New_York:20260309T083000
END:VEVENT
BEGIN:VEVENT
UID:date-only
SUMMARY:Statistical release
DTSTART;VALUE=DATE:20260310
END:VEVENT
BEGIN:VEVENT
UID:unknown-timezone
SUMMARY:Unknown timezone event
DTSTART;TZID=Not/A_Zone:20260310T083000
END:VEVENT
END:VCALENDAR`
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	events, err := parseICS([]byte(ics), "BLS", blsCalendarURL, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("unknown timezone must be excluded: %+v", events)
	}
	byTitle := map[string]Event{}
	for _, e := range events {
		byTitle[e.Title] = e
	}
	before := byTitle["Consumer Price Index, February 2026"]
	after := byTitle["Employment Situation, March 2026"]
	if before.ScheduledAt == nil || *before.ScheduledAt != "2026-03-06T13:30:00Z" || after.ScheduledAt == nil || *after.ScheduledAt != "2026-03-09T12:30:00Z" {
		t.Fatalf("DST incorrect: before %+v after %+v", before, after)
	}
	if before.ReferencePeriod == nil || *before.ReferencePeriod != "2026-02" {
		t.Fatal("reference period lost")
	}
	if before.Forecast != nil || before.Actual != nil || before.Previous != nil {
		t.Fatal("calendar schedule must not invent released values")
	}
	if e := byTitle["Statistical release"]; e.ScheduledAt != nil || e.TimePrecision != "date" {
		t.Fatal("date-only release must not invent intraday time")
	}
}

func TestBLSDeclaredEasternAlias(t *testing.T) {
	ics := "BEGIN:VCALENDAR\nX-WR-TIMEZONE:US-Eastern\nBEGIN:VEVENT\nUID:bls-cpi\nSUMMARY:Consumer Price Index\nDTSTART;TZID=US-Eastern:20261014T083000\nEND:VEVENT\nEND:VCALENDAR"
	events, err := parseICS([]byte(ics), "BLS", blsCalendarURL, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ScheduledAt == nil || *events[0].ScheduledAt != "2026-10-14T12:30:00Z" {
		t.Fatalf("BLS alias not resolved %+v", events)
	}
}

func TestPassedScheduleDoesNotClaimConfirmedPublication(t *testing.T) {
	at := "2026-10-14T12:30:00Z"
	events := []Event{{ID: "cpi", Date: "2026-10-14", ScheduledAt: &at, TimePrecision: "exact", Status: "upcoming"}}
	from, _ := time.Parse("2006-01-02", "2026-10-14")
	got := filterEvents(events, from, from.AddDate(0, 0, 1), false, from.Add(15*time.Hour))
	if len(got) != 1 || got[0].Status != "elapsed" || got[0].Actual != nil {
		t.Fatalf("unverified release claimed %+v", got)
	}
}

func TestFOMCDateOnlyAndCrossMonth(t *testing.T) {
	data := `<h4>2026 FOMC Meetings</h4><div class="row fomc-meeting"><div class="fomc-meeting__month">April</div><div class="fomc-meeting__date">28-29</div></div><h4>2027 FOMC Meetings</h4><div class="row fomc-meeting"><div class="fomc-meeting__month">April/May</div><div class="fomc-meeting__date">30-1*</div></div>`
	events, err := parseFOMC([]byte(data), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Date != "2026-04-29" || events[1].Date != "2027-05-01" {
		t.Fatalf("wrong decision dates %+v", events)
	}
	for _, e := range events {
		if e.ScheduledAt != nil || e.TimePrecision != "date" {
			t.Fatal("FOMC page contains no exact time")
		}
	}
}

func TestCalendarBoundsRFC3339ExclusiveAndLocalDST(t *testing.T) {
	q := url.Values{"from": {"2026-03-08T00:00:00-05:00"}, "to": {"2026-03-09T00:00:00-04:00"}}
	from, to, _, err := calendarBounds(q, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if to.Sub(from) != 23*time.Hour {
		t.Fatal("DST day must contain23 hours")
	}
	start, end := from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339)
	events := []Event{{ID: "start", Date: "2026-03-08", ScheduledAt: &start, TimePrecision: "exact", Status: "upcoming"}, {ID: "end", Date: "2026-03-09", ScheduledAt: &end, TimePrecision: "exact", Status: "upcoming"}}
	got := filterEvents(events, from, to, false, from)
	if len(got) != 1 || got[0].ID != "start" {
		t.Fatalf("expected [from,to), got %+v", got)
	}
	_, _, _, err = calendarBounds(url.Values{"from": {"2026-01-01"}, "to": {"2028-01-01"}}, time.Now())
	if err == nil {
		t.Fatal("oversized date range accepted")
	}
}

func TestDedupAndRescheduledEventRetainsPriorTime(t *testing.T) {
	a := New(nil)
	priorAt, newAt := "2026-10-14T12:30:00Z", "2026-10-15T12:30:00Z"
	prior := Event{ID: "bea-123", Source: "BEA", Title: "GDP", Date: "2026-10-14", ScheduledAt: &priorAt, TimePrecision: "exact", Status: "upcoming"}
	a.applyEvents("BEA", []Event{prior, prior})
	if len(a.state.Events) != 1 {
		t.Fatal("duplicate scheduled event")
	}
	updated := prior
	updated.Date = "2026-10-15"
	updated.ScheduledAt = &newAt
	a.applyEvents("BEA", []Event{updated})
	e := a.state.Events[0]
	if e.PreviousScheduledAt == nil || *e.PreviousScheduledAt != priorAt || e.Status != "postponed" {
		t.Fatalf("rescheduling lost: %+v", e)
	}
}

func TestStaleCacheAndFrequencyAwareAge(t *testing.T) {
	a := New(nil)
	a.now = func() time.Time { return time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC) }
	a.applyPoints("CPIAUCSL", []Point{{"2026-09-01", 331}}, "BLS", "https://bls.gov")
	if a.state.Series["CPIAUCSL"].Status != "fresh" {
		t.Fatal("monthly publication lag should not be mistaken for outage")
	}
	a.applyPoints("DGS10", []Point{{"2026-09-01", 4.3}}, "Treasury", "https://treasury.gov")
	if a.state.Series["DGS10"].Status != "stale" {
		t.Fatal("old daily yield should not be called fresh")
	}
	a.health("fred", nil)
	a.health("fred", errors.New("HTTP503"))
	if a.state.Sources["fred"].Status != "stale" || a.state.Sources["fred"].LastSuccess == nil {
		t.Fatal("source success was lost after failure")
	}
	a.staleSeries([]string{"CPIAUCSL"})
	if a.state.Series["CPIAUCSL"].Latest == nil || *a.state.Series["CPIAUCSL"].Latest != 331 {
		t.Fatal("failure must preserve last successful data")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEndpointsNeverFetchUpstreamAndInvalidRanges(t *testing.T) {
	a := New(nil)
	var calls atomic.Int32
	a.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, errors.New("unexpected fetch") })
	r := chi.NewRouter()
	a.Routes(r)
	for _, path := range []string{"/macro/overview", "/macro/calendar?from=2026-10-01&to=2026-10-31", "/macro/series?id=CPIAUCSL&range=all", "/macro/markets"} {
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusOK {
			t.Fatalf("%s: %s", path, res.Body)
		}
		var body map[string]json.RawMessage
		if json.Unmarshal(res.Body.Bytes(), &body) != nil || body["sources"] == nil || body["asOf"] == nil {
			t.Fatal("broken contract")
		}
	}
	for _, path := range []string{"/macro/series?id=../secret", "/macro/series?id=CPIAUCSL&range=200y", "/macro/calendar?from=garbage"} {
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != 400 {
			t.Fatal("invalid input accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("cache reads consumed upstream requests")
	}
}

func TestProviderErrorsDoNotExposeCredentials(t *testing.T) {
	a := New(nil)
	a.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("transport failed: " + r.URL.String())
	})
	_, err := a.get(context.Background(), "https://api.stlouisfed.org/test?api_key=SUPERSECRET")
	if err == nil || strings.Contains(err.Error(), "SUPERSECRET") {
		t.Fatal("credential leaked into returned provider error")
	}
}

func TestResponseAsOfIsSuccessfulSyncNotRequestTime(t *testing.T) {
	a := New(nil)
	fixed := time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return fixed }
	calendarSync := "2026-10-14T01:00:00Z"
	macroSync := "2026-10-14T02:00:00Z"
	a.state.Sources["bea-calendar"] = SourceHealth{ID: "bea-calendar", Status: "ok", LastSuccess: &calendarSync}
	a.state.Sources["fred"] = SourceHealth{ID: "fred", Status: "ok", LastSuccess: &macroSync}
	a.applyPoints("CPIAUCSL", []Point{{"2026-08-01", 334.131}}, "BLS via FRED", "https://fred.stlouisfed.org/series/CPIAUCSL")
	series := a.state.Series["CPIAUCSL"]
	series.LastUpdated = &macroSync
	a.state.Series[series.ID] = series
	router := chi.NewRouter()
	a.Routes(router)
	for path, expected := range map[string]string{"/macro/overview": macroSync, "/macro/calendar?from=2026-10-01&to=2026-10-31": calendarSync, "/macro/series?id=CPIAUCSL": macroSync, "/macro/markets": macroSync} {
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		var body struct {
			AsOf string `json:"asOf"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.AsOf != expected {
			t.Fatalf("%s falsely updates collection time to%s", path, body.AsOf)
		}
	}
	if syncAsOf(New(nil).clone(), "fred") != "" {
		t.Fatal("pending sources have no successful sync timestamp")
	}
}

func TestSentimentUsesSourceClassificationAndRejectsInvalidValues(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	ts := strconv.FormatInt(now.Add(-12*time.Hour).Unix(), 10)
	old := strconv.FormatInt(now.Add(-36*time.Hour).Unix(), 10)
	data := []byte(`{"data":[{"value":"62","value_classification":"Greed","timestamp":"` + ts + `"},{"value":"57","value_classification":"Greed","timestamp":"` + old + `"},{"value":"101","timestamp":"` + ts + `"},{"value":"NaN","timestamp":"` + ts + `"}],"metadata":{"error":null}}`)
	item, err := parseSentiment(data, now)
	if err != nil {
		t.Fatal(err)
	}
	if item.Price == nil || *item.Price != 62 || item.Classification == nil || *item.Classification != "Greed" || len(item.Points) != 2 || item.Source != "Alternative.me" || item.Status != "fresh" {
		t.Fatalf("bad real-source sentiment contract %+v", item)
	}
	if _, err := parseSentiment([]byte(`{"data":[{"value":"-1","timestamp":"`+ts+`"}],"metadata":{"error":null}}`), now); err == nil {
		t.Fatal("invalid source sentiment must not become neutral0")
	}
	if _, err := parseSentiment([]byte(`{"data":[],"metadata":{"error":"quota"}}`), now); err == nil {
		t.Fatal("provider error swallowed")
	}
	stale, err := parseSentiment(data, now.Add(3*24*time.Hour))
	if err != nil || stale.Status != "stale" {
		t.Fatal("old daily sentiment called current")
	}
}

func TestSentimentFailureKeepsCacheSeparateFromCryptoQuotes(t *testing.T) {
	a := New(nil)
	at := time.Now().UTC().Format(time.RFC3339)
	a.state.Markets = []MarketItem{{Symbol: "BTC", Price: ptr(60000.0)}}
	a.state.Sentiment = &MarketItem{Symbol: "CRYPTO_FEAR_GREED", Price: ptr(62.0), ObservedAt: &at, Status: "fresh", Points: []Point{}}
	a.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unavailable") })
	a.refreshSentiment(context.Background())
	s := a.clone()
	if len(s.Markets) != 1 || s.Markets[0].Symbol != "BTC" || s.Sentiment == nil || s.Sentiment.Status != "stale" || *s.Sentiment.Price != 62 {
		t.Fatal("sentiment error overwrote independent quote cache")
	}
}

func TestLiveSentiment(t *testing.T) {
	if os.Getenv("SENTIMENT_LIVE_TEST") != "1" {
		t.Skip("set SENTIMENT_LIVE_TEST=1 to verify only Alternative.me")
	}
	a := New(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a.refreshSentiment(ctx)
	s := a.clone()
	if s.Sentiment == nil {
		t.Fatalf("Alternative.me unavailable: %s", s.Sources["alternative-sentiment"].Message)
	}
	t.Logf("value=%v observed=%s points=%d status=%s source=%s", *s.Sentiment.Price, *s.Sentiment.ObservedAt, len(s.Sentiment.Points), s.Sentiment.Status, s.Sentiment.Source)
}

func TestBybitQuoteIsRealSourceTimeAnd24hChange(t *testing.T) {
	a := New(nil)
	a.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"retCode":0,"time":1791979200000,"result":{"list":[{"symbol":"BTCUSDT","lastPrice":"60000","price24hPcnt":"-0.025"}]}}`
		if strings.Contains(r.URL.Path, "kline") {
			body = `{"retCode":0,"result":{"list":[["1791936000000","1","2","1","60000","1","1"],["1791849600000","1","2","1","61000","1","1"]]}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	item, err := a.bybitMarket(context.Background(), "BTC", "Bitcoin")
	if err != nil {
		t.Fatal(err)
	}
	if item.Price == nil || *item.Price != 60000 || item.ChangePercent == nil || *item.ChangePercent != -2.5 || len(item.Points) != 2 || item.Points[0].Date >= item.Points[1].Date || item.ObservedAt == nil {
		t.Fatalf("bad quote %+v", item)
	}
}

// Opt-in deployment smoke check; it calls only real configured providers and
// leaves ordinary unit tests independent of networks and provider quotas.
func TestLiveProviders(t *testing.T) {
	if os.Getenv("MACRO_LIVE_TEST") != "1" {
		t.Skip("set MACRO_LIVE_TEST=1 for real provider verification")
	}
	a := New(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	a.refresh(ctx)
	s := a.clone()
	usable := 0
	for _, series := range s.Series {
		if series.Latest != nil {
			usable++
			t.Logf("series=%s points=%d latest=%v observation=%s status=%s", series.ID, len(series.Points), *series.Latest, *series.ObservationDate, series.Status)
		}
	}
	for _, source := range sourceList(s) {
		t.Logf("source=%s status=%s message=%s", source.ID, source.Status, source.Message)
	}
	t.Logf("calendar events=%d markets=%d usableSeries=%d", len(s.Events), len(s.Markets), usable)
	if usable == 0 {
		t.Fatal("all real macro providers are unavailable")
	}
	if len(s.Events) == 0 {
		t.Fatal("all real calendar providers are unavailable")
	}
	for _, event := range s.Events {
		if event.Forecast != nil {
			t.Fatal("official calendar must not invent consensus forecasts")
		}
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("full cached JSON bytes=%d", len(data))
}
