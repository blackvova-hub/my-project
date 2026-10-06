// Package verification caches public evidence; account facts remain unknown.
package verification

import (
	"context"
	"shortlong/arbitrage/internal/exchanges"
	"shortlong/arbitrage/internal/market"
	"sync"
	"time"
)

type cached struct {
	at    time.Time
	value any
	ready chan struct{}
}
type Provider struct {
	mu      sync.Mutex
	cache   map[string]*cached
	clients map[string]*exchanges.Client
	slots   chan struct{}
}

func New() *Provider {
	p := &Provider{cache: map[string]*cached{}, clients: map[string]*exchanges.Client{}, slots: make(chan struct{}, 8)}
	for _, e := range market.Exchanges {
		p.clients[e] = exchanges.New(e, "*")
	}
	return p
}
func (p *Provider) get(ctx context.Context, key string, fn func(context.Context) any) any {
	p.mu.Lock()
	if c := p.cache[key]; c != nil && (c.ready != nil || time.Since(c.at) < time.Minute) {
		ready := c.ready
		value := c.value
		p.mu.Unlock()
		if ready != nil {
			select {
			case <-ready:
				p.mu.Lock()
				value = c.value
				p.mu.Unlock()
			case <-ctx.Done():
				return nil
			}
		}
		return value
	}
	// Bound both retained entries and concurrent exchange requests.
	for k, c := range p.cache {
		if c.ready == nil && time.Since(c.at) >= time.Minute {
			delete(p.cache, k)
		}
	}
	if len(p.cache) >= 256 {
		p.mu.Unlock()
		return nil
	}
	c := &cached{ready: make(chan struct{})}
	p.cache[key] = c
	ready := c.ready
	p.mu.Unlock()
	var value any
	select {
	case p.slots <- struct{}{}:
		value = fn(ctx)
		<-p.slots
	case <-ctx.Done():
	}
	p.mu.Lock()
	c.value = value
	c.at = time.Now()
	c.ready = nil
	close(ready)
	p.mu.Unlock()
	return value
}
func (p *Provider) Evidence(ctx context.Context, i market.Instrument) market.VenueEvidence {
	e := market.VenueEvidence{}
	c := p.clients[i.Exchange]
	if c == nil {
		return e
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		v := p.get(ctx, i.Exchange+":"+i.Market, func(ctx context.Context) any {
			list, err := c.Catalog(ctx, i.Market)
			if err != nil {
				return nil
			}
			return list
		})
		if list, ok := v.([]market.Instrument); ok {
			found := false
			for _, item := range list {
				e.CatalogAt = max(e.CatalogAt, item.CatalogAt)
				if item.ID == i.ID {
					copy := item
					e.Instrument = &copy
					found = true
				}
			}
			e.Trading = &found
		}
	}()
	go func() {
		defer wg.Done()
		v := p.get(ctx, i.Exchange+":wallet:"+i.Asset, func(ctx context.Context) any { return c.Wallet(ctx, i.Asset) })
		if w, ok := v.(market.WalletEvidence); ok {
			e.Wallet = w
		}
	}()
	wg.Wait()
	return e
}
