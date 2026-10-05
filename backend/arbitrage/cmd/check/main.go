// Optional real-network smoke check. Never submits orders or uses private API keys.
package main

import (
	"context"
	"encoding/json"
	"os"
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
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				c := exchanges.New(e, market.DefaultAssets)
				r := result{Exchange: e, Market: k}
				list, err := c.Catalog(ctx, k)
				r.Instruments = len(list)
				if err != nil {
					r.Error = err.Error()
				} else {
					if k == "perpetual" {
						f, er := c.Funding(ctx, list)
						r.Funding = len(f)
						if er != nil {
							r.Error = er.Error()
						}
					}
					seen := map[string]bool{}
					err = c.Stream(ctx, k, list, func(b market.Book) {
						seen[b.Instrument.ID] = true
						if len(seen) == len(list) {
							cancel()
						}
					}, func(bool) {})
					r.Books = len(seen)
					if err != nil && ctx.Err() == nil {
						r.Error = err.Error()
					}
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
