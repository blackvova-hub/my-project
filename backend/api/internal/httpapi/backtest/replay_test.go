package backtestapi

import (
	"backend/internal/auth"
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"net/url"
	bt "shortlong/backtest"
	"testing"
	"time"
)

func TestReplayRangeBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	base := url.Values{"exchange": {"bybit"}, "market": {"linear"}, "symbol": {"BTCUSDT"}, "timeframe": {"5m"}, "from": {"1789776000000"}, "to": {"1789862400000"}}
	for _, tc := range []struct {
		key, value string
		valid      bool
	}{{"exchange", "binance", true}, {"market", "spot", true}, {"timeframe", "1m", true}, {"exchange", "evil", false}, {"market", "inverse", false}, {"symbol", "BTC';DROP", false}, {"timeframe", "1s", false}, {"from", "-1", false}, {"from", "NaN", false}, {"to", "1789776000000", false}, {"to", "9999999999999", false}, {"from", "0", false}, {"from", "1789776000001", false}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			q := url.Values{}
			for k, v := range base {
				q[k] = append([]string{}, v...)
			}
			q.Set(tc.key, tc.value)
			_, _, _, err := replayRange(q, now)
			if (err == nil) != tc.valid {
				t.Fatalf("error %v, want valid %v", err, tc.valid)
			}
		})
	}
}

type replayReaderStub struct {
	exchange, market string
	fail             bool
}

func (s *replayReaderStub) ReadMinutes(_ context.Context, exchange, market, symbol, tf string, from, to int64) ([]bt.Candle, error) {
	s.exchange = exchange
	s.market = market
	if s.fail {
		return nil, errors.New("offline")
	}
	return []bt.Candle{{Time: from, Open: 10, High: 12, Low: 9, Close: 11, Volume: 4}}, nil
}
func TestReplayAuthReadOnlyAndGaps(t *testing.T) {
	for _, tc := range []struct {
		name                string
		authenticated, fail bool
		want                int
	}{{"auth", false, false, 401}, {"read", true, false, 200}, {"offline", true, true, 503}} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &replayReaderStub{fail: tc.fail}
			a := &API{replayArchive: reader, authenticate: func(*http.Request) (auth.User, error) {
				if !tc.authenticated {
					return auth.User{}, errors.New("session")
				}
				return auth.User{ID: "user"}, nil
			}}
			router := chi.NewRouter()
			a.Routes(router)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/replay/candles?exchange=binance&market=spot&symbol=BTCUSDT&timeframe=5m&from=1789776000000&to=1789776600000", nil))
			if w.Code != tc.want {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			if tc.want == 200 {
				var body struct {
					Candles []bt.Candle
					Missing int
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if len(body.Candles) != 1 || body.Missing != 1 || reader.exchange != "binance" || reader.market != "spot" {
					t.Fatalf("unexpected response %+v", body)
				}
			}
		})
	}
}
