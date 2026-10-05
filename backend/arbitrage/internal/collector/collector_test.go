package collector

import (
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"os"
	"shortlong/arbitrage/internal/exchanges"
	"shortlong/arbitrage/internal/market"
	"testing"
	"time"
)

func quote(now time.Time) market.Book {
	i := market.Instrument{Exchange: "bybit", Market: "spot", Native: "BTCUSDT", Asset: "BTC", PriceScale: 1, QuantityScale: 1, Step: .001, TakerFee: .001}
	i.ID = i.Key()
	return market.Book{Instrument: i, Bids: []market.Level{{Price: 99, Quantity: 1}}, Asks: []market.Level{{Price: 100, Quantity: 1}}, SourceAt: now.UnixMilli(), ReceivedAt: now.UnixMilli(), Sequence: 40, Valid: true}
}
func TestDisconnectInvalidatesAndOldFramesCannotReplaceBooks(t *testing.T) {
	now := time.Now()
	c := New(exchanges.New("bybit", market.DefaultAssets), nil)
	b := quote(now)
	c.onBook(b)
	old := quote(now.Add(-time.Second))
	old.Sequence = 41
	c.onBook(old)
	if c.books[b.Instrument.ID].ReceivedAt != b.ReceivedAt {
		t.Fatal("older source frame admitted")
	}
	reset := quote(now.Add(time.Second))
	reset.Sequence = 1
	c.onBook(reset)
	if c.books[b.Instrument.ID].Sequence != 1 {
		t.Fatal("Bybit service restart snapshot ignored")
	}
	c.setStatus("spot", "error", "offline", 1, true)
	if c.books[b.Instrument.ID].Valid {
		t.Fatal("disconnect did not invalidate")
	}
	c.onBook(b)
	if !c.books[b.Instrument.ID].Valid {
		t.Fatal("new session not admitted")
	}
}
func TestFundingAttachmentDoesNotRefreshOrderBook(t *testing.T) {
	now := time.Now()
	c := New(exchanges.New("bybit", market.DefaultAssets), nil)
	b := quote(now.Add(-20 * time.Second))
	c.funding[b.Instrument.ID] = market.Funding{Rate: .0001, ObservedAt: now.UnixMilli()}
	c.onBook(b)
	v := c.books[b.Instrument.ID]
	if v.Funding == nil || v.ReceivedAt != b.ReceivedAt || v.Fresh(now, 10*time.Second) {
		t.Fatal("funding made stale quotes fresh")
	}
}
func TestRedisRepublishAfterLostHashKeepsOriginalTimestamp(t *testing.T) {
	addr := os.Getenv("ARBITRAGE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("Redis integration not configured")
	}
	r := redis.NewClient(&redis.Options{Addr: addr, DB: 13})
	defer r.Close()
	ctx := context.Background()
	c := New(exchanges.New("bybit", market.DefaultAssets), r)
	b := quote(time.Now())
	c.onBook(b)
	c.setStatus("spot", "live", "", 1, false)
	keys := []string{Prefix + "books:bybit", Prefix + "status:bybit:spot"}
	defer func() { _ = r.Del(ctx, keys...).Err() }()
	if err := c.publish(ctx); err != nil {
		t.Fatal(err)
	}
	_ = r.Del(ctx, keys[0]).Err()
	if err := c.publish(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := r.HGet(ctx, keys[0], b.Instrument.ID).Result()
	if err != nil {
		t.Fatal(err)
	}
	var restored market.Book
	if json.Unmarshal([]byte(raw), &restored) != nil || restored.ReceivedAt != b.ReceivedAt {
		t.Fatal("timestamp fabricated during recovery")
	}
}
