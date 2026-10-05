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
	Client  *exchanges.Client
	Redis   *redis.Client
	mu      sync.Mutex
	books   map[string]market.Book
	status  map[string]market.Status
	dirty   map[string]bool
	funding map[string]market.Funding
}

func New(client *exchanges.Client, rdb *redis.Client) *Collector {
	return &Collector{Client: client, Redis: rdb, books: map[string]market.Book{}, status: map[string]market.Status{}, dirty: map[string]bool{}, funding: map[string]market.Funding{}}
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
func (c *Collector) runMarket(ctx context.Context, kind string) {
	backoff := time.Second
	for ctx.Err() == nil {
		c.setStatus(kind, "connecting", "", 0, false)
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
		c.setStatus(kind, "connecting", "", len(list), true)
		sessionCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		var fw sync.WaitGroup
		if kind == "perpetual" {
			fw.Add(1)
			go func() {
				defer fw.Done()
				for sessionCtx.Err() == nil {
					funds, e := c.Client.Funding(sessionCtx, list)
					c.mu.Lock()
					if e == nil {
						for id, f := range funds {
							c.funding[id] = f
							if b, ok := c.books[id]; ok {
								copy := f
								b.Funding = &copy
								c.books[id] = b
								c.dirty[id] = true
							}
						}
					}
					c.mu.Unlock()
					if e != nil && sessionCtx.Err() == nil {
						log.Printf("funding %s: %v", c.Client.Exchange, e)
					}
					if !sleep(sessionCtx, 30*time.Second) {
						return
					}
				}
			}()
		}
		err = c.Client.Stream(sessionCtx, kind, list, c.onBook, func(up bool) {
			state := "connecting"
			if up {
				state = "live"
			}
			c.setStatus(kind, state, "", len(list), !up)
		})
		cancel()
		fw.Wait()
		if ctx.Err() != nil {
			return
		}
		message := "Соединение восстанавливается"
		if err != nil {
			message = "Поток недоступен; повторное подключение"
			log.Printf("stream %s/%s: %v", c.Client.Exchange, kind, err)
		}
		c.setStatus(kind, "error", message, len(list), true)
		if !sleep(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, 30*time.Second)
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
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	pipe := c.Redis.Pipeline()
	key := Prefix + "books:" + c.Client.Exchange
	// Full small snapshots also recover a Redis restart or key eviction without
	// waiting for an instrument to change. This does not refresh book timestamps.
	for id := range c.books {
		b, err := json.Marshal(c.books[id])
		if err != nil {
			return err
		}
		pipe.HSet(ctx, key, id, b)
	}
	pipe.Expire(ctx, key, 30*time.Second)
	for kind, s := range c.status {
		s.UpdatedAt = time.Now().UnixMilli()
		for _, b := range c.books {
			if b.Instrument.Market == kind && b.Valid {
				s.Books++
				s.LastBookAt = max(s.LastBookAt, b.ReceivedAt)
			}
		}
		data, _ := json.Marshal(s)
		pipe.Set(ctx, Prefix+"status:"+c.Client.Exchange+":"+kind, data, 10*time.Second)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	clear(c.dirty)
	return nil
}
