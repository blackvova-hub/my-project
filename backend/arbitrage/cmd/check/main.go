// Optional real-network smoke check. Never submits orders or uses private API keys.
package main

import (
	"context"
	"encoding/json"
	"os"
	"shortlong/arbitrage/internal/collector"
	"shortlong/arbitrage/internal/exchanges"
	"shortlong/arbitrage/internal/market"
	"sync"
	"time"
)

type result struct {
	Exchange    string `json:"exchange"`
	Market      string `json:"market"`
	Instruments int    `json:"instruments"`
	Books       int    `json:"books"`
	Funding     int    `json:"funding"`
	Error       string `json:"error,omitempty"`
}

func main() {
	var wg sync.WaitGroup
	var mu sync.Mutex
	out := []result{}
	for _, e := range market.Exchanges {
		for _, k := range []string{"spot", "perpetual"} {
			wg.Add(1)
			go func(e, k string) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				c := exchanges.New(e, market.DefaultAssets)
				r := result{Exchange: e, Market: k}
				list, err := c.Catalog(ctx, k)
				r.Instruments = len(list)
				if err != nil {
					r.Error = err.Error()
				} else {
					seen := map[string]bool{}
					var seenMu sync.Mutex
					var streams sync.WaitGroup
					for _, shard := range collector.Shards(e, list) {
						if ctx.Err() != nil {
							break
						}
						streams.Add(1)
						go func(shard []market.Instrument) {
							defer streams.Done()
							streamErr := c.Stream(ctx, k, shard, func(b market.Book) {
								seenMu.Lock()
								defer seenMu.Unlock()
								if b.Valid {
									seen[b.Instrument.ID] = true
								}
								if len(seen) == len(list) {
									cancel()
								}
							}, func(bool) {})
							if streamErr != nil && ctx.Err() == nil {
								seenMu.Lock()
								r.Error = streamErr.Error()
								seenMu.Unlock()
							}
						}(shard)
						time.Sleep(750 * time.Millisecond)
					}
					streams.Wait()
					r.Books = len(seen)
				}
				mu.Lock()
				out = append(out, r)
				mu.Unlock()
			}(e, k)
		}
	}
	wg.Wait()
	_ = json.NewEncoder(os.Stdout).Encode(out)
}
