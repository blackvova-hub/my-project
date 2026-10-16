package marketdata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	bt "shortlong/backtest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAvailableTailUsesFirstProviderCandleAndCompleteTimeframes(t *testing.T) {
	const hour = int64(time.Hour / time.Millisecond)
	first := time.Date(2026, 8, 13, 9, 15, 0, 0, time.UTC).UnixMilli()
	from, to := first/hour*hour, (first/hour+7)*hour
	for _, sourceTF := range []string{"1m", "5m"} {
		t.Run(sourceTF, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("interval") != strconv.FormatInt(candleStep(sourceTF)/60000, 10) {
					t.Error("wrong source timeframe")
				}
				fmt.Fprint(w, `{"retCode":0,"result":{"list":[`)
				for ts := to - candleStep(sourceTF); ts >= first; ts -= candleStep(sourceTF) {
					if ts != to-candleStep(sourceTF) {
						fmt.Fprint(w, ",")
					}
					fmt.Fprintf(w, `["%d","100","103","99","102","2"]`, ts)
				}
				fmt.Fprint(w, `]}}`)
			}))
			defer server.Close()
			raw, err := NewBybit(server.URL, nil).Candles(context.Background(), "linear", "LYTEUSDT", sourceTF, from, to)
			if err != nil {
				t.Fatal(err)
			}
			for _, tf := range []string{"5m", "15m", "1h", "4h"} {
				step := bt.Interval(tf).Milliseconds()
				end := to / step * step
				cut := int((end - first) / candleStep(sourceTF))
				bars, err := availableTail(raw[:cut], "LYTEUSDT", tf, from/step*step, end, candleStep(sourceTF))
				wantStart := (first + step - 1) / step * step
				wantCount := int((end - wantStart) / step)
				if err != nil || len(bars) != wantCount || len(bars) == 0 || bars[0].Time != wantStart || bars[len(bars)-1].Time != end-step {
					t.Fatalf("%s: got %+v, err=%v; expected %d complete bars from %d", tf, bars, err, wantCount, wantStart)
				}
				if bars[0].Open != 100 || bars[0].High != 103 || bars[0].Low != 99 || bars[0].Close != 102 || bars[0].Volume != float64(step/candleStep(sourceTF))*2 {
					t.Fatalf("%s: incorrect aggregation: %+v", tf, bars[0])
				}
			}
			if sourceTF == "1m" {
				minutes, err := availableTail(raw, "LYTEUSDT", "1m", from, to, MinuteStep)
				if err != nil || len(minutes) != len(raw) || minutes[0].Time != first || minutes[0].Volume != 2 {
					t.Fatalf("real minute rows changed: %d, %v", len(minutes), err)
				}
			}
		})
	}
}

func TestAvailableTailRejectsInternalAndTrailingGapsIncludingDiscardedBucket(t *testing.T) {
	const hour = int64(time.Hour / time.Millisecond)
	for _, sourceStep := range []int64{MinuteStep, 300000} {
		first, to := 9*hour+15*MinuteStep, 12*hour
		base := availableTestCandles(first, to, sourceStep)
		for _, missing := range []int{1, int((10*hour - first) / sourceStep), len(base) - 1} {
			rows := append([]bt.Candle{}, base[:missing]...)
			rows = append(rows, base[missing+1:]...)
			_, err := availableTail(rows, "LYTEUSDT", "1h", 9*hour, to, sourceStep)
			date := time.UnixMilli(base[missing].Time).UTC().Format("02.01.2006 15:04")
			if err == nil || !strings.Contains(err.Error(), "LYTEUSDT") || !strings.Contains(err.Error(), date) {
				t.Fatalf("step=%d missing=%d: expected symbol and missing date %s, got %v", sourceStep, missing, date, err)
			}
		}
	}
}

func TestAvailableTailEmptyAndPartialOnly(t *testing.T) {
	for _, sourceStep := range []int64{MinuteStep, 300000} {
		for _, rows := range [][]bt.Candle{nil, availableTestCandles(15*MinuteStep, 3600000, sourceStep)} {
			bars, err := availableTail(rows, "LYTEUSDT", "1h", 0, 3600000, sourceStep)
			if err != nil || len(bars) != 0 {
				t.Fatalf("partial first hour fabricated: %+v, %v", bars, err)
			}
		}
	}
}

func TestLoadAvailableCompleteCacheDoesNotAcquireDatabaseOrProvider(t *testing.T) {
	for _, minuteArchive := range []bool{false, true} {
		t.Run(fmt.Sprint(minuteArchive), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("param_from") != "0" || r.URL.Query().Get("param_to") != "7200000" {
					t.Error("wrong requested range")
				}
				for ts := int64(0); ts < 7200000; ts += 3600000 {
					fmt.Fprintf(w, `{"bucket":%d,"time":%d,"open":100,"high":103,"low":99,"close":102,"volume":2}`+"\n", ts, ts)
				}
			}))
			defer server.Close()
			storage := NewClickHouse(server.URL, "", "")
			storage.MinuteArchive = minuteArchive
			// Nil DB and Bybit ensure the complete-cache path is self-contained.
			history := &History{Storage: storage}
			bars, err := history.LoadAvailable(context.Background(), "linear", "LYTEUSDT", "1h", 0, 7200000, nil)
			if err != nil || len(bars) != 2 || calls != 1 {
				t.Fatalf("cache path: %+v, %v, calls=%d", bars, err, calls)
			}
		})
	}
}

func TestLoadAvailableRejectsUnalignedRangeBeforeStorageAccess(t *testing.T) {
	history := &History{}
	for _, tc := range []struct {
		tf       string
		from, to int64
	}{{"1h", 60000, 3600000}, {"4h", 0, 3600000}, {"1h", 3600000, 3600000}, {"bad", 0, 3600000}} {
		if _, err := history.LoadAvailable(context.Background(), "linear", "LYTEUSDT", tc.tf, tc.from, tc.to, nil); err == nil {
			t.Fatalf("invalid range accepted: %+v", tc)
		}
	}
}

func availableTestCandles(from, to, step int64) []bt.Candle {
	rows := []bt.Candle{}
	for ts := from; ts < to; ts += step {
		rows = append(rows, bt.Candle{Time: ts, Open: 100, High: 103, Low: 99, Close: 102, Volume: 2})
	}
	return rows
}
