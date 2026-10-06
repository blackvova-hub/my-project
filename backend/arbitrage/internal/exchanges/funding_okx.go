package exchanges

import (
	"context"
	"math"
	"net/url"
	"shortlong/arbitrage/internal/market"
	"strconv"
	"sync"
	"time"
)

// Bounded concurrency with one global start limiter, shared by all workers.
// Each rate receives its own response timestamp, never the scan start time.
func (c *Client) okxFunding(ctx context.Context, instruments []market.Instrument) (map[string]market.Funding, error) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	jobs := make(chan market.Instrument)
	out := map[string]market.Funding{}
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
				v, err := c.request(ctx, "GET", c.BasePerpetual, "/api/v5/public/funding-rate?instId="+url.QueryEscape(i.Native))
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					continue
				}
				rows := arr(rec(v)["data"])
				if len(rows) == 0 {
					continue
				}
				x := rec(rows[0])
				next := int64(number(x, "fundingTime"))
				interval := int64(number(x, "nextFundingTime")) - next
				rate, err := strconv.ParseFloat(text(x["fundingRate"]), 64)
				now := time.Now().UnixMilli()
				if err == nil && market.Finite(rate) && math.Abs(rate) <= .1 && next > now && interval >= 3600000 && interval <= 86400000 {
					mu.Lock()
					out[i.ID] = market.Funding{Rate: rate, NextAt: next, IntervalMS: interval, ObservedAt: now}
					mu.Unlock()
				}
			}
		}()
	}
	for _, i := range instruments {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return out, ctx.Err()
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
	return out, firstErr
}
