// Public read-only diagnostics for depth, funding and alternative sources.
package main

import (
	"context"
	"encoding/json"
	"os"
	"shortlong/arbitrage/internal/alternative"
	"shortlong/arbitrage/internal/exchanges"
	"shortlong/arbitrage/internal/market"
	"sync"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	p := alternative.New()
	go p.Run(ctx)
	var wg sync.WaitGroup
	var mu sync.Mutex
	rows := []map[string]any{}
	for _, venue := range market.Exchanges {
		for _, kind := range []string{"spot", "perpetual"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c := exchanges.New(venue, "BTC,ETH")
				row := map[string]any{"exchange": venue, "market": kind}
				list, err := c.Catalog(ctx, kind)
				if err == nil {
					if len(list) > 0 {
						b, e := c.Depth(ctx, list[0])
						if e != nil {
							err = e
						} else {
							row["bids"] = len(b.Bids)
							row["asks"] = len(b.Asks)
							row["sourceAgeMs"] = time.Now().UnixMilli() - b.QuoteAt()
						}
					}
					if kind == "perpetual" {
						rates, e := c.Funding(ctx, list)
						row["funding"] = len(rates)
						if e != nil {
							row["fundingError"] = e.Error()
						}
					}
				}
				if err != nil {
					row["error"] = err.Error()
				}
				mu.Lock()
				rows = append(rows, row)
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
	}
	d := p.Snapshot()
	json.NewEncoder(os.Stdout).Encode(map[string]any{"depth": rows, "externalSources": d.Sources, "pools": len(d.Pools), "usdtUsd": d.USDTUSD, "usdcUsd": d.USDCUSD})
}
