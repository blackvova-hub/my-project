package service

import (
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"net/http/httptest"
	"os"
	"shortlong/arbitrage/internal/collector"
	"shortlong/arbitrage/internal/engine"
	"shortlong/arbitrage/internal/market"
	"testing"
	"time"
)

func sample(e, k string, ask, bid float64, now time.Time) market.Book {
	i := market.Instrument{Exchange: e, Market: k, Native: "BTCUSDT", Asset: "BTC", PriceScale: 1, QuantityScale: 1, Step: .001, TakerFee: .001}
	i.ID = i.Key()
	return market.Book{Instrument: i, Asks: []market.Level{{Price: ask, Quantity: 100}}, Bids: []market.Level{{Price: bid, Quantity: 100}}, ReceivedAt: now.UnixMilli(), SourceAt: now.UnixMilli(), Valid: true}
}
func TestDatasetDropsStaleStateAndValidatesHTTP(t *testing.T) {
	now := time.Now()
	s := New(nil)
	s.at = now.UnixMilli()
	s.transport = "live"
	s.books = []market.Book{sample("binance", "spot", 100, 99, now), sample("bybit", "perpetual", 102, 101, now)}
	q := engine.Defaults()
	d := s.Dataset(q, now)
	if len(d.Rows) != 1 {
		t.Fatal(d)
	}
	d = s.Dataset(q, now.Add(4*time.Second))
	if len(d.Rows) != 0 || d.Transport != "stale" {
		t.Fatal(d)
	}
	for _, path := range []string{"/opportunities?amount=NaN", "/history?id=evil"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
func TestThirtySecondAgeOptionRetainsOlderQuotes(t *testing.T) {
	now := time.Now()
	s := New(nil)
	s.at = now.UnixMilli()
	s.transport = "live"
	s.books = []market.Book{sample("binance", "spot", 100, 99, now.Add(-20*time.Second)), sample("bybit", "perpetual", 102, 101, now.Add(-20*time.Second))}
	q := engine.Defaults()
	if len(s.Dataset(q, now).Rows) != 0 {
		t.Fatal("old quotes admitted by default")
	}
	q.MaxAgeMS = 30000
	if len(s.Dataset(q, now).Rows) != 1 {
		t.Fatal("30-second option ignored")
	}
}
func TestRedisCollectorCoverageDurationHistoryAndDisconnect(t *testing.T) {
	addr := os.Getenv("ARBITRAGE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set ARBITRAGE_TEST_REDIS_ADDR for Redis integration")
	}
	r := redis.NewClient(&redis.Options{Addr: addr, DB: 14})
	defer r.Close()
	ctx := context.Background()
	if err := r.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	books := []market.Book{sample("binance", "spot", 100, 99, now), sample("bybit", "perpetual", 102, 101, now)}
	keys := []string{}
	for _, b := range books {
		key := collector.Prefix + "books:" + b.Instrument.Exchange
		status := collector.Prefix + "status:" + b.Instrument.Exchange + ":" + b.Instrument.Market
		keys = append(keys, key, status)
		raw, _ := json.Marshal(b)
		if err := r.HSet(ctx, key, b.Instrument.ID, raw).Err(); err != nil {
			t.Fatal(err)
		}
		v, _ := json.Marshal(market.Status{Exchange: b.Instrument.Exchange, Market: b.Instrument.Market, State: "live", Instruments: 1, Books: 1, UpdatedAt: now.UnixMilli(), LastBookAt: now.UnixMilli()})
		if err := r.Set(ctx, status, v, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { _ = r.Del(ctx, keys...).Err() }()
	s := New(r)
	if err := s.Refresh(ctx, now); err != nil {
		t.Fatal(err)
	}
	q := engine.Defaults()
	d := s.Dataset(q, now)
	if len(d.Rows) != 1 || len(d.Sources) != 12 || d.Rows[0].Since != now.UnixMilli() {
		t.Fatal(d)
	}
	id := d.Rows[0].ID
	keys = append(keys, collector.Prefix+"history:"+id)
	if err := s.Sample(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if s.Dataset(q, now.Add(time.Second)).Rows[0].Since != now.UnixMilli() {
		t.Fatal("duration reset")
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/history?id="+id, nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var h struct {
		Points []Point `json:"points"`
	}
	if json.Unmarshal(w.Body.Bytes(), &h) != nil || len(h.Points) != 1 {
		t.Fatal(w.Body.String())
	}
	_ = r.Del(ctx, collector.Prefix+"status:bybit:perpetual").Err()
	if err := s.Refresh(ctx, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(s.Dataset(q, now.Add(2*time.Second)).Rows) != 0 {
		t.Fatal("disconnected quotes remained live")
	}
	if len(s.since) != 0 {
		t.Fatal("duration not reset after disconnect")
	}
}
