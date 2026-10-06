package collector

import (
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"log"
	"shortlong/arbitrage/internal/exchanges"
	"shortlong/arbitrage/internal/market"
	"sync"
	"time"
)

const Prefix = "arbitrage:v1:"

type Collector struct {
	Client      *exchanges.Client
	Redis       *redis.Client
	mu          sync.Mutex
	books       map[string]market.Book
	status      map[string]market.Status
	dirty       map[string]bool
	funding     map[string]market.Funding
	connections map[string][]bool
}

func New(client *exchanges.Client, rdb *redis.Client) *Collector {
	return &Collector{Client: client, Redis: rdb, books: map[string]market.Book{}, status: map[string]market.Status{}, dirty: map[string]bool{}, funding: map[string]market.Funding{}, connections: map[string][]bool{}}
}
func (c *Collector) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, kind := range []string{"spot", "perpetual"} {
		wg.Add(1)
		go func(kind string) { defer wg.Done(); c.runMarket(ctx, kind) }(kind)
	}
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-t.C:
			if err := c.publish(ctx); err != nil {
				log.Printf("arbitrage collector transport: %v", err)
			}
		}
	}
}

// Conservative per-connection topic limits; reconnects affect only one shard.
func Shards(exchange string, list []market.Instrument) [][]market.Instrument {
	size := map[string]int{"binance": 200, "bybit": 100, "okx": 100, "bitget": 40, "gateio": 50, "kucoin": 40}[exchange]
	if size == 0 {
		size = 40
	}
	out := [][]market.Instrument{}
	for start := 0; start < len(list); start += size {
		out = append(out, list[start:min(start+size, len(list))])
	}
	return out
}
func (c *Collector) shardState(kind string, index int, list []market.Instrument, up bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	active := c.connections[kind]
	active[index] = up
	st := c.status[kind]
	st.Connected = 0
	for _, connected := range active {
		if connected {
			st.Connected++
		}
	}
	st.State = "connecting"
	if st.Connected > 0 {
		st.State = "live"
	}
	st.Message = ""
	if st.Connected < st.Connections {
		st.Message = "Some streams are connecting or reconnecting"
	}
	st.UpdatedAt = time.Now().UnixMilli()
	c.status[kind] = st
	if !up {
		for _, i := range list {
			if b, ok := c.books[i.ID]; ok {
				b.Valid = false
				c.books[i.ID] = b
				c.dirty[i.ID] = true
			}
		}
	}
}
func (c *Collector) runMarket(ctx context.Context, kind string) {
	backoff := time.Second
	for ctx.Err() == nil {
		c.setStatus(kind, "connecting", "", 0, true)
		list, err := c.Client.Catalog(ctx, kind)
		if err != nil {
			c.setStatus(kind, "error", err.Error(), 0, true)
			log.Printf("catalog %s/%s: %v", c.Client.Exchange, kind, err)
			if !sleep(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		shards := Shards(c.Client.Exchange, list)
		c.mu.Lock()
		// A complete redis snapshot also removes delisted instruments.
		for id, b := range c.books {
			if b.Instrument.Market == kind {
				delete(c.books, id)
			}
		}
		c.connections[kind] = make([]bool, len(shards))
		c.status[kind] = market.Status{Exchange: c.Client.Exchange, Market: kind, State: "connecting", Instruments: len(list), Connections: len(shards), UpdatedAt: time.Now().UnixMilli()}
		c.mu.Unlock()
		sessionCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		var workers sync.WaitGroup
		for index, shard := range shards {
			if !sleep(sessionCtx, 750*time.Millisecond) {
				break
			}
			workers.Add(1)
			go func(index int, shard []market.Instrument) {
				defer workers.Done()
				retry := time.Second
				for sessionCtx.Err() == nil {
					err := c.Client.Stream(sessionCtx, kind, shard, c.onBook, func(up bool) {
						c.shardState(kind, index, shard, up)
						if up {
							retry = time.Second
						}
					})
					c.shardState(kind, index, shard, false)
					if err != nil && sessionCtx.Err() == nil {
						log.Printf("stream %s/%s shard %d: %v", c.Client.Exchange, kind, index, err)
					}
					if !sleep(sessionCtx, retry) {
						return
					}
					retry = min(retry*2, 30*time.Second)
				}
			}(index, shard)
		}
		<-sessionCtx.Done()
		cancel()
		workers.Wait()
	}
}
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
func (c *Collector) setStatus(kind, state, message string, count int, invalidate bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status[kind] = market.Status{Exchange: c.Client.Exchange, Market: kind, State: state, Message: message, Instruments: count, UpdatedAt: time.Now().UnixMilli()}
	if invalidate {
		for id, b := range c.books {
			if b.Instrument.Market == kind {
				b.Valid = false
				c.books[id] = b
				c.dirty[id] = true
			}
		}
	}
}
func (c *Collector) onBook(b market.Book) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if prev, ok := c.books[b.Instrument.ID]; ok && prev.Valid {
		bybitReset := b.Instrument.Exchange == "bybit" && b.Sequence == 1 && b.SourceAt >= prev.SourceAt && b.SourceAt > 0
		if b.Sequence > 0 && prev.Sequence > 0 && b.Sequence < prev.Sequence && !bybitReset {
			return
		}
		if b.SourceAt > 0 && prev.SourceAt > b.SourceAt {
			return
		}
	}
	if f, ok := c.funding[b.Instrument.ID]; ok {
		copy := f
		b.Funding = &copy
	}
	c.books[b.Instrument.ID] = b
	c.dirty[b.Instrument.ID] = true
}
func (c *Collector) publish(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c.mu.Lock()
	snapshot := make(map[string]market.Book, len(c.books))
	statuses := make(map[string]market.Status, len(c.status))
	for id, b := range c.books {
		snapshot[id] = b
	}
	for k, st := range c.status {
		statuses[k] = st
	}
	clear(c.dirty)
	c.mu.Unlock()
	values := make(map[string]any, len(snapshot))
	for id, b := range snapshot {
		raw, err := json.Marshal(b)
		if err != nil {
			return err
		}
		values[id] = raw
		if b.Valid && b.Fresh(time.Now(), 30*time.Second) {
			st := statuses[b.Instrument.Market]
			st.Books++
			st.LastBookAt = max(st.LastBookAt, b.ReceivedAt)
			statuses[b.Instrument.Market] = st
		}
	}
	// Atomic replacement avoids stale delisted fields and uses one HSET rather
	// than thousands of Redis commands. Original quote times are preserved.
	pipe := c.Redis.TxPipeline()
	key := Prefix + "books:" + c.Client.Exchange
	pipe.Del(ctx, key)
	if len(values) > 0 {
		pipe.HSet(ctx, key, values)
		pipe.Expire(ctx, key, 30*time.Second)
	}
	for kind, st := range statuses {
		st.UpdatedAt = time.Now().UnixMilli()
		data, _ := json.Marshal(st)
		pipe.Set(ctx, Prefix+"status:"+c.Client.Exchange+":"+kind, data, 10*time.Second)
	}
	_, err := pipe.Exec(ctx)
	return err
}
