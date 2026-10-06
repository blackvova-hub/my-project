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
func TestShardsKeepAllInstrumentsAndIsolateDisconnect(t *testing.T) {
	list := make([]market.Instrument, 2301)
	for _, e := range market.Exchanges {
		shards := Shards(e, list)
		total := 0
		for _, s := range shards {
			total += len(s)
			if len(s) > 200 {
				t.Fatal("oversized subscription", e)
			}
		}
		if total != len(list) || len(shards) < 2 {
			t.Fatal("catalog truncated", e)
		}
	}
	c := New(exchanges.New("bybit", market.DefaultAssets), nil)
	first := quote(time.Now())
	second := first
	second.Instrument.Native = "ETHUSDT"
	second.Instrument.ID = second.Instrument.Key()
	c.onBook(first)
	c.onBook(second)
	c.connections["spot"] = []bool{true, true}
	c.status["spot"] = market.Status{Connections: 2, Connected: 2, State: "live"}
	c.shardState("spot", 0, []market.Instrument{first.Instrument}, false)
	if c.books[first.Instrument.ID].Valid || !c.books[second.Instrument.ID].Valid || c.status["spot"].Connected != 1 || c.status["spot"].State != "live" {
		t.Fatal("one disconnect invalidates unrelated shard")
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
func TestRedisDeepSnapshotIsAtomicExpiresAndDisconnectInvalidates(t *testing.T) {
	addr := os.Getenv("ARBITRAGE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("Redis integration not configured")
	}
	r := redis.NewClient(&redis.Options{Addr: addr, DB: 13})
	defer r.Close()
	ctx := context.Background()
	c := New(exchanges.New("bybit", "BTC"), r)
	b := quote(time.Now())
	c.onBook(b)
	deep := b
	deep.Asks = []market.Level{{Price: 101, Quantity: 4}, {Price: 102, Quantity: 5}}
	deep.DepthSource = "rest-50"
	c.depth[b.Instrument.ID] = deep
	c.setStatus("spot", "live", "", 1, false)
	key := Prefix + "books:bybit"
	defer r.Del(ctx, key, Prefix+"status:bybit:spot")
	read := func() market.Book {
		t.Helper()
		if err := c.publish(ctx); err != nil {
			t.Fatal(err)
		}
		raw, err := r.HGet(ctx, key, b.Instrument.ID).Result()
		if err != nil {
			t.Fatal(err)
		}
		var got market.Book
		if json.Unmarshal([]byte(raw), &got) != nil {
			t.Fatal("bad JSON")
		}
		return got
	}
	got := read()
	if len(got.Asks) != 2 || got.Asks[0].Price != 101 || got.SourceAt != deep.SourceAt {
		t.Fatal("mixed BBO and deeper levels")
	}
	deep.SourceAt = time.Now().Add(-6 * time.Second).UnixMilli()
	deep.ReceivedAt = deep.SourceAt
	c.depth[b.Instrument.ID] = deep
	got = read()
	if len(got.Asks) != 1 || got.Asks[0].Price != 100 {
		t.Fatal("expired REST snapshot still used")
	}
	deep = b
	deep.DepthSource = "rest-50"
	c.depth[b.Instrument.ID] = deep
	c.connections["spot"] = []bool{true}
	c.shardState("spot", 0, []market.Instrument{b.Instrument}, false)
	got = read()
	if got.Valid || len(c.depth) != 0 {
		t.Fatal("disconnect retained usable depth")
	}
}
