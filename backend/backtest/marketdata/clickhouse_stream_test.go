package marketdata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCandleHistoryStreamsBeyondSmallQueryLimit(t *testing.T) {
	padding := strings.Repeat(" ", 64*1024)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprintln(w, `{"bucket":0,"time":0,"open":1,"high":2,"low":1,"close":2,"volume":3}`)
		// A year of minute history exceeds 64 MiB. Whitespace lets this
		// regression exercise that wire size without retaining 500k test bars.
		for i := 0; i < 1024; i++ {
			if _, err := io.WriteString(w, padding); err != nil {
				return
			}
		}
		fmt.Fprintln(w, `{"bucket":60000,"time":60000,"open":2,"high":3,"low":1,"close":2,"volume":4}`)
	}))
	defer server.Close()
	for _, minuteArchive := range []bool{false, true} {
		t.Run(fmt.Sprint(minuteArchive), func(t *testing.T) {
			ch := NewClickHouse(server.URL, "", "")
			ch.MinuteArchive = minuteArchive
			before := calls.Load()
			rows, err := ch.Read(context.Background(), "linear", "LYTEUSDT", "1m", 0, 120000)
			if err != nil || len(rows) != 2 || rows[1].Time != 60000 || rows[1].Volume != 4 {
				t.Fatalf("large response was truncated: %d rows, %v", len(rows), err)
			}
			if calls.Load() != before+1 {
				t.Fatal("streaming decoder repeated the HTTP query")
			}
		})
	}
}

type streamTestTransport func(*http.Request) (*http.Response, error)

func (f streamTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedStreamBody struct {
	io.Reader
	closed bool
}

func (b *trackedStreamBody) Close() error { b.closed = true; return nil }

func TestCandleHistoryClosesFailedStreamsAndRejectsTruncatedJSON(t *testing.T) {
	for _, minuteArchive := range []bool{false, true} {
		for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
			body := &trackedStreamBody{Reader: strings.NewReader(`{"time":0,"bucket":0,"open":1,"high":2,"low":1,"close":2,"volume":3}` + "\n" + `{"time":60000`)}
			calls := 0
			ch := NewClickHouse("http://storage.test", "", "")
			ch.MinuteArchive = minuteArchive
			ch.HTTP = &http.Client{Transport: streamTestTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Body: body, Header: http.Header{}}, nil
			})}
			if _, err := ch.Read(context.Background(), "linear", "LYTEUSDT", "1m", 0, 120000); err == nil {
				t.Fatalf("minute=%t status=%d: failed stream was accepted", minuteArchive, status)
			}
			if !body.closed || calls != 1 {
				t.Fatalf("minute=%t status=%d: closed=%t requests=%d", minuteArchive, status, body.closed, calls)
			}
		}
	}
}
