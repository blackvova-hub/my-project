package marketdata

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestArchiveFormatsAndHourlyBoundaries(t *testing.T) {
	for _, market := range []string{"spot", "linear"} {
		t.Run(market, func(t *testing.T) {
			raw := "timestamp,symbol,side,size,price\n0.001,BTCUSDT,Buy,2,100\n3599.999,BTCUSDT,Sell,1,120\n3600,BTCUSDT,Buy,3,110\n"
			if market == "spot" {
				raw = "timestamp,side,volume,price\n1,buy,2,100\n3599999,sell,1,120\n3600000,buy,3,110\n"
			}
			points, err := parseTradeDay(context.Background(), strings.NewReader(raw), market, "BTCUSDT", 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(points) != 48 || points[0].Value != 200 || points[1].Value != 120 || points[2].Value != 330 || points[3].Value != 0 {
				t.Fatalf("incorrect hourly aggregation: %+v", points[:4])
			}
		})
	}
}
func TestInvalidTradeArchivesAreNotAccepted(t *testing.T) {
	for name, body := range map[string]string{
		"side": "0,X,1,1", "notFinite": "0,buy,NaN,1", "wrongDay": "86400000,buy,1,1", "missingColumn": "0,buy,1", "negative": "0,buy,-1,1",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTradeDay(context.Background(), strings.NewReader("timestamp,side,volume,price\n"+body+"\n"), "spot", "BTCUSDT", 0)
			if err == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := parseTradeDay(ctx, strings.NewReader("timestamp,side,volume,price\n0,buy,1,1\n"), "spot", "BTCUSDT", 0); err == nil {
		t.Fatal("cancel ignored")
	}
}
func TestTradeArchiveHTTPAndNotFound(t *testing.T) {
	var zipped bytes.Buffer
	gz := gzip.NewWriter(&zipped)
	_, _ = gz.Write([]byte("timestamp,symbol,side,size,price\n0,BTCUSDT,Buy,2,100\n"))
	_ = gz.Close()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/trading/BTCUSDT/BTCUSDT1970-01-01.csv.gz" {
			t.Errorf("path: %s", r.URL.Path)
		}
		w.Write(zipped.Bytes())
	}))
	defer server.Close()
	bybit := &Bybit{ArchiveURL: server.URL, ArchiveHTTP: server.Client()}
	points, err := bybit.TradeDay(context.Background(), "linear", "BTCUSDT", 0)
	if err != nil || len(points) != 48 || requests != 1 {
		t.Fatalf("%v points=%d calls=%d", err, len(points), requests)
	}
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	bybit.ArchiveURL = missing.URL
	if _, err = bybit.TradeDay(context.Background(), "linear", "BTCUSDT", 0); err == nil {
		t.Fatal("404 interpreted as zero flow")
	}
}
func TestOpenInterestPagingAndRange(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if q.Get("intervalTime") != "1h" || q.Get("startTime") != "0" || q.Get("endTime") != "7199999" {
			t.Errorf("unexpected query %s", r.URL.RawQuery)
		}
		if q.Get("cursor") == "" {
			fmt.Fprint(w, `{"retCode":0,"result":{"list":[{"timestamp":"3600000","openInterest":"105"}],"nextPageCursor":"next"}}`)
		} else {
			fmt.Fprint(w, `{"retCode":0,"result":{"list":[{"timestamp":"0","openInterest":"100"}],"nextPageCursor":"older"}}`)
		}
	}))
	defer server.Close()
	bybit := &Bybit{BaseURL: server.URL, HTTP: server.Client()}
	points, err := bybit.OpenInterest(context.Background(), "BTCUSDT", 0, 7200000)
	if err != nil || len(points) != 2 || calls != 2 {
		t.Fatalf("%v points=%+v calls=%d", err, points, calls)
	}
}
func TestOpenInterestInvalidNumber(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"retCode": 0, "result": map[string]any{"list": []any{map[string]string{"timestamp": "0", "openInterest": "NaN"}}}})
	}))
	defer server.Close()
	bybit := &Bybit{BaseURL: server.URL, HTTP: server.Client()}
	if _, err := bybit.OpenInterest(context.Background(), "BTCUSDT", 0, 3600000); err == nil {
		t.Fatal("non-finite OI accepted")
	}
}
