package collector

import (
	"context"
	"github.com/redis/go-redis/v9"
	"log"
	"shortlong/arbitrage/internal/market"
	"sort"
	"strconv"
	"time"
)

func (c *Collector) pollFunding(ctx context.Context) {
	for ctx.Err() == nil {
		c.mu.Lock()
		list := []market.Instrument{}
		for _, i := range c.instruments {
			if i.Market == "perpetual" {
				list = append(list, i)
			}
		}
		if c.Client.Exchange == "okx" {
			sort.Slice(list, func(i, j int) bool {
				a, b := c.funding[list[i].ID].ObservedAt, c.funding[list[j].ID].ObservedAt
				if a == b {
					return list[i].ID < list[j].ID
				}
				return a < b
			})
			list = list[:min(100, len(list))]
		}
		c.mu.Unlock()
		if len(list) == 0 {
			if !sleep(ctx, time.Second) {
				return
			}
			continue
		}
		op, cancel := context.WithTimeout(ctx, 100*time.Second)
		rates, err := c.Client.Funding(op, list)
		cancel()
		if err != nil {
			log.Printf("funding %s: %v", c.Client.Exchange, err)
		}
		c.mu.Lock()
		for id, f := range rates {
			if _, ok := c.instruments[id]; ok {
				c.funding[id] = f
			}
		}
		c.mu.Unlock()
		pause := 30 * time.Second
		if c.Client.Exchange == "okx" {
			pause = time.Second
		}
		if !sleep(ctx, pause) {
			return
		}
	}
}
func (c *Collector) enrichDepth(ctx context.Context) {
	last := map[string]time.Time{}
	key := Prefix + "depth-demand:" + c.Client.Exchange
	for ctx.Err() == nil {
		now := time.Now()
		op, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = c.Redis.ZRemRangeByScore(op, key, "-inf", strconv.FormatInt(now.Add(-20*time.Second).UnixMilli(), 10)).Err()
		ids, err := c.Redis.ZRevRangeByScore(op, key, &redis.ZRangeBy{Min: strconv.FormatInt(now.Add(-20*time.Second).UnixMilli(), 10), Max: "+inf", Count: 200}).Result()
		cancel()
		if err == nil {
			// Pick the oldest fetched active demand: many viewers share one rate-limited cache.
			chosen := ""
			for _, id := range ids {
				if now.Sub(last[id]) >= 4*time.Second && (chosen == "" || last[id].Before(last[chosen])) {
					chosen = id
				}
			}
			if chosen != "" {
				c.mu.Lock()
				i, ok := c.instruments[chosen]
				c.mu.Unlock()
				last[chosen] = now
				if ok {
					op, cancel = context.WithTimeout(ctx, 5*time.Second)
					b, er := c.Client.Depth(op, i)
					cancel()
					if er == nil {
						c.mu.Lock()
						if current, ok := c.instruments[chosen]; ok && current == i {
							c.depth[chosen] = b
						}
						c.mu.Unlock()
					} else {
						log.Printf("depth %s/%s: %v", c.Client.Exchange, i.Native, er)
					}
				}
			}
		}
		for id, at := range last {
			if now.Sub(at) > time.Minute {
				delete(last, id)
			}
		}
		if !sleep(ctx, 500*time.Millisecond) {
			return
		}
	}
}
