package similarity

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"shortlong/backtest/marketdata"
)

func TestChartsReadRawHistoryForSixSavedMatches(t *testing.T) {
	var calls atomic.Int32
	start := int64(1700000100000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		if q.Get("param_tf") != "5m" || q.Get("param_symbol") != "BTCUSDT" || q.Get("param_from") != fmt.Sprint(start) || q.Get("param_to") != fmt.Sprint(start+24*Step) {
			t.Errorf("wrong raw candle bounds: %v", q)
		}
		for i := range 24 {
			fmt.Fprintf(w, `{"time":%d,"open":100,"high":110,"low":90,"close":105,"volume":10}`+"\n", start+int64(i)*Step)
		}
	}))
	defer server.Close()
	s := NewSearcher(Store{}, marketdata.NewClickHouse(server.URL, "", ""), nil, nil)
	result := Response{Query: Request{Window: 12, End: start + 24*Step}}
	for i := range 8 {
		result.Matches = append(result.Matches, Match{ID: fmt.Sprint(i), Market: "linear", Symbol: "BTCUSDT", Start: start, End: start + 12*Step, Chart: []ChartBar{{Time: 1, Open: 999}}})
	}
	charts, err := s.Charts(context.Background(), result)
	if err != nil || len(charts) != 6 || calls.Load() != 6 {
		t.Fatalf("six bounded reads: %v %d %d", err, len(charts), calls.Load())
	}
	for i, chart := range charts {
		if chart.ID != fmt.Sprint(i) || len(chart.Candles) != 24 || chart.Candles[0].Open != 100 {
			t.Fatal("saved compressed chart used instead of raw candles", chart)
		}
		for j, c := range chart.Candles {
			if c.Time != (start+int64(j)*Step)/1000 {
				t.Fatal("raw cadence changed")
			}
		}
	}
}

func TestChartsRejectUnboundedHistory(t *testing.T) {
	s := NewSearcher(Store{}, nil, nil, nil)
	for _, result := range []Response{
		{Query: Request{Window: 10000}},
		{Query: Request{Window: 12}, Matches: []Match{{Start: 1, End: 10000 * Step}}},
	} {
		if _, err := s.Charts(context.Background(), result); err == nil {
			t.Fatal("invalid chart bounds accepted")
		}
	}
}
