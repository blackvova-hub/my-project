package voiceapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// A pair is an entry in the catalogue, not a provisioned LiveKit room.
type Pair struct {
	ID    string `json:"id"`
	Base  string `json:"base"`
	Quote string `json:"quote"`
}
type Source struct {
	Name      string    `json:"name"`
	Pairs     int       `json:"pairs"`
	Available bool      `json:"available"`
	Complete  bool      `json:"complete"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type catalogue struct {
	mu        sync.RWMutex
	pairs     []Pair
	sources   []Source
	bySource  map[string][]Pair
	client    *http.Client
	cacheFile string
}

func (c *catalogue) loadCache() {
	if c.cacheFile == "" {
		return
	}
	file, err := os.Open(c.cacheFile)
	if err != nil {
		return
	}
	defer file.Close()
	var saved struct {
		At       time.Time         `json:"at"`
		BySource map[string][]Pair `json:"bySource"`
		Sources  []Source          `json:"sources"`
	}
	if json.NewDecoder(io.LimitReader(file, 16<<20)).Decode(&saved) != nil || time.Since(saved.At) > 24*time.Hour || saved.At.After(time.Now().Add(time.Minute)) {
		return
	}
	var all []Pair
	for _, source := range saved.Sources {
		if time.Since(source.UpdatedAt) > 24*time.Hour {
			continue
		}
		if _, ok := map[string]bool{"Binance": true, "Bybit": true, "OKX": true, "Bitget": true, "Gate.io": true, "KuCoin": true}[source.Name]; !ok {
			continue
		}
		items := uniquePairs(saved.BySource[source.Name])
		c.bySource[source.Name] = items
		all = append(all, items...)
		source.Pairs = len(items)
		source.Available = false
		source.Complete = false
		c.sources = append(c.sources, source)
	}
	c.pairs = uniquePairs(all)
}
func (c *catalogue) saveCacheLocked() {
	if c.cacheFile == "" || len(c.pairs) == 0 {
		return
	}
	data, err := json.Marshal(struct {
		At       time.Time         `json:"at"`
		BySource map[string][]Pair `json:"bySource"`
		Sources  []Source          `json:"sources"`
	}{time.Now().UTC(), c.bySource, c.sources})
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(c.cacheFile), 0o755) != nil {
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(c.cacheFile), ".voice-catalogue-*")
	if err != nil {
		return
	}
	name := temporary.Name()
	defer os.Remove(name)
	_, err = temporary.Write(data)
	closeErr := temporary.Close()
	if err == nil && closeErr == nil {
		_ = os.Rename(name, c.cacheFile)
	}
}

var assetRE = regexp.MustCompile(`^[\p{L}\p{N}]{1,32}$`)

func canonicalPair(base, quote string) (Pair, bool) {
	base, quote = strings.ToUpper(strings.TrimSpace(base)), strings.ToUpper(strings.TrimSpace(quote))
	if !assetRE.MatchString(base) || !assetRE.MatchString(quote) || base == quote {
		return Pair{}, false
	}
	// Contract lot multipliers refer to the same coin, not a separate currency.
	for _, prefix := range []string{"1000000", "10000", "1000"} {
		if strings.HasPrefix(base, prefix) && len(base) > len(prefix) && base[len(prefix)] >= 'A' && base[len(prefix)] <= 'Z' {
			base = strings.TrimPrefix(base, prefix)
			break
		}
	}
	return Pair{ID: base + "-" + quote, Base: base, Quote: quote}, true
}
func uniquePairs(items []Pair) []Pair {
	set := map[string]Pair{}
	for _, item := range items {
		if p, ok := canonicalPair(item.Base, item.Quote); ok {
			set[p.ID] = p
		}
	}
	result := make([]Pair, 0, len(set))
	for _, p := range set {
		result = append(result, p)
	}
	// Familiar pairs first; the remaining catalogue is alphabetical.
	priority := map[string]int{"BTC-USDT": 1, "ETH-USDT": 2, "SOL-USDT": 3, "XRP-USDT": 4, "DOGE-USDT": 5, "BTC-USDC": 6, "ETH-USDC": 7, "ADA-USDT": 8, "AVAX-USDT": 9, "LINK-USDT": 10}
	sort.Slice(result, func(i, j int) bool {
		a, b := priority[result[i].ID], priority[result[j].ID]
		if a != b {
			if a == 0 {
				return false
			}
			if b == 0 {
				return true
			}
			return a < b
		}
		return result[i].ID < result[j].ID
	})
	return result
}
func (c *catalogue) snapshot() ([]Pair, []Source) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]Pair{}, c.pairs...), append([]Source{}, c.sources...)
}
func (c *catalogue) contains(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, p := range c.pairs {
		if p.ID == id {
			return true
		}
	}
	return false
}
func (c *catalogue) get(ctx context.Context, address string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("catalogue status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
}
func envURL(key, fallback string) string {
	if v := strings.TrimRight(os.Getenv(key), "/"); v != "" {
		return v
	}
	return fallback
}
func (c *catalogue) binance(ctx context.Context) ([]Pair, error) {
	var all []Pair
	var failures []error
	for _, endpoint := range []string{envURL("BINANCE_SPOT_BASE_URL", "https://api.binance.com") + "/api/v3/exchangeInfo", envURL("BINANCE_FUTURES_BASE_URL", "https://fapi.binance.com") + "/fapi/v1/exchangeInfo", "https://dapi.binance.com/dapi/v1/exchangeInfo"} {
		var payload struct {
			Symbols []struct {
				Base           string `json:"baseAsset"`
				Quote          string `json:"quoteAsset"`
				Status         string `json:"status"`
				ContractStatus string `json:"contractStatus"`
			} `json:"symbols"`
		}
		if err := c.get(ctx, endpoint, &payload); err != nil {
			failures = append(failures, err)
			continue
		}
		for _, s := range payload.Symbols {
			if s.Status == "TRADING" || s.ContractStatus == "TRADING" {
				all = append(all, Pair{Base: s.Base, Quote: s.Quote})
			}
		}
	}
	return uniquePairs(all), errors.Join(failures...)
}
func (c *catalogue) bybit(ctx context.Context) ([]Pair, error) {
	var all []Pair
	for _, category := range []string{"spot", "linear", "inverse"} {
		cursor := ""
		seen := map[string]bool{}
		for page := 0; page < 30; page++ {
			address := envURL("BYBIT_BASE_URL", "https://api.bybit.com") + "/v5/market/instruments-info?category=" + category + "&limit=1000&cursor=" + url.QueryEscape(cursor)
			var p struct {
				Code   int `json:"retCode"`
				Result struct {
					Cursor string `json:"nextPageCursor"`
					List   []struct {
						Base       string `json:"baseCoin"`
						Quote      string `json:"quoteCoin"`
						Status     string `json:"status"`
						SymbolType string `json:"symbolType"`
					} `json:"list"`
				} `json:"result"`
			}
			if err := c.get(ctx, address, &p); err != nil {
				return nil, err
			}
			if p.Code != 0 {
				return nil, fmt.Errorf("bybit %d", p.Code)
			}
			for _, s := range p.Result.List {
				if s.Status == "Trading" && !strings.Contains(strings.ToLower(s.SymbolType), "stock") && !strings.Contains(strings.ToLower(s.SymbolType), "xstock") {
					all = append(all, Pair{Base: s.Base, Quote: s.Quote})
				}
			}
			if category == "spot" || p.Result.Cursor == "" {
				break
			}
			if seen[p.Result.Cursor] {
				return nil, fmt.Errorf("repeated catalogue cursor")
			}
			cursor = p.Result.Cursor
			seen[cursor] = true
		}
	}
	return uniquePairs(all), nil
}
func (c *catalogue) okx(ctx context.Context) ([]Pair, error) {
	var all []Pair
	for _, kind := range []string{"SPOT", "SWAP", "FUTURES"} {
		var p struct {
			Code string `json:"code"`
			Data []struct {
				Base       string `json:"baseCcy"`
				Quote      string `json:"quoteCcy"`
				Underlying string `json:"uly"`
				Family     string `json:"instFamily"`
				State      string `json:"state"`
			} `json:"data"`
		}
		if err := c.get(ctx, "https://www.okx.com/api/v5/public/instruments?instType="+kind, &p); err != nil {
			return nil, err
		}
		if p.Code != "0" {
			return nil, fmt.Errorf("okx %s", p.Code)
		}
		for _, s := range p.Data {
			if s.State != "live" {
				continue
			}
			if s.Base == "" {
				key := s.Underlying
				if key == "" {
					key = s.Family
				}
				parts := strings.Split(key, "-")
				if len(parts) >= 2 {
					s.Base, s.Quote = parts[0], parts[1]
				}
			}
			all = append(all, Pair{Base: s.Base, Quote: s.Quote})
		}
	}
	return uniquePairs(all), nil
}
func (c *catalogue) bitget(ctx context.Context) ([]Pair, error) {
	var all []Pair
	for _, kind := range []string{"spot", "USDT-FUTURES", "USDC-FUTURES", "COIN-FUTURES"} {
		endpoint := "https://api.bitget.com/api/v2/mix/market/contracts?productType=" + kind
		if kind == "spot" {
			endpoint = "https://api.bitget.com/api/v2/spot/public/symbols"
		}
		var p struct {
			Code string `json:"code"`
			Data []struct {
				Base         string `json:"baseCoin"`
				Quote        string `json:"quoteCoin"`
				Status       string `json:"status"`
				SymbolStatus string `json:"symbolStatus"`
			} `json:"data"`
		}
		if err := c.get(ctx, endpoint, &p); err != nil {
			return nil, err
		}
		if p.Code != "00000" {
			return nil, fmt.Errorf("bitget %s", p.Code)
		}
		for _, s := range p.Data {
			if s.Status == "online" || s.SymbolStatus == "normal" {
				all = append(all, Pair{Base: s.Base, Quote: s.Quote})
			}
		}
	}
	return uniquePairs(all), nil
}
func (c *catalogue) gate(ctx context.Context) ([]Pair, error) {
	var all []Pair
	var failures []error
	var spot []struct {
		Base   string `json:"base"`
		Quote  string `json:"quote"`
		Status string `json:"trade_status"`
	}
	if err := c.get(ctx, "https://api.gateio.ws/api/v4/spot/currency_pairs", &spot); err != nil {
		failures = append(failures, err)
	}
	for _, s := range spot {
		if s.Status == "tradable" || s.Status == "buyable" || s.Status == "sellable" {
			all = append(all, Pair{Base: s.Base, Quote: s.Quote})
		}
	}
	for _, settle := range []string{"usdt", "btc"} {
		var p []struct {
			Name      string `json:"name"`
			Delisting bool   `json:"in_delisting"`
		}
		if err := c.get(ctx, "https://api.gateio.ws/api/v4/futures/"+settle+"/contracts", &p); err != nil {
			failures = append(failures, err)
			continue
		}
		for _, s := range p {
			parts := strings.Split(s.Name, "_")
			if !s.Delisting && len(parts) == 2 {
				all = append(all, Pair{Base: parts[0], Quote: parts[1]})
			}
		}
	}
	return uniquePairs(all), errors.Join(failures...)
}
func (c *catalogue) kucoin(ctx context.Context) ([]Pair, error) {
	var all []Pair
	var p struct {
		Code string `json:"code"`
		Data []struct {
			Base    string `json:"baseCurrency"`
			Quote   string `json:"quoteCurrency"`
			Enabled bool   `json:"enableTrading"`
		} `json:"data"`
	}
	if err := c.get(ctx, "https://api.kucoin.com/api/v2/symbols", &p); err != nil {
		return nil, err
	}
	if p.Code != "200000" {
		return nil, fmt.Errorf("kucoin %s", p.Code)
	}
	for _, s := range p.Data {
		if s.Enabled {
			all = append(all, Pair{Base: s.Base, Quote: s.Quote})
		}
	}
	var f struct {
		Code string `json:"code"`
		Data []struct {
			Base   string `json:"baseCurrency"`
			Quote  string `json:"quoteCurrency"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := c.get(ctx, "https://api-futures.kucoin.com/api/v1/contracts/active", &f); err != nil {
		return nil, err
	}
	if f.Code != "200000" {
		return nil, fmt.Errorf("kucoin %s", f.Code)
	}
	for _, s := range f.Data {
		if s.Status == "Open" {
			if s.Base == "XBT" {
				s.Base = "BTC"
			}
			all = append(all, Pair{Base: s.Base, Quote: s.Quote})
		}
	}
	return uniquePairs(all), nil
}
func (c *catalogue) refresh(ctx context.Context) {
	loaders := []struct {
		name string
		load func(context.Context) ([]Pair, error)
	}{{"Binance", c.binance}, {"Bybit", c.bybit}, {"OKX", c.okx}, {"Bitget", c.bitget}, {"Gate.io", c.gate}, {"KuCoin", c.kucoin}}
	type result struct {
		name  string
		pairs []Pair
		err   error
	}
	ch := make(chan result, len(loaders))
	for _, l := range loaders {
		go func() { items, err := l.load(ctx); ch <- result{l.name, items, err} }()
	}
	results := map[string]result{}
	for range loaders {
		r := <-ch
		results[r.name] = r
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var all []Pair
	var sources []Source
	previous := map[string]Source{}
	for _, s := range c.sources {
		previous[s.Name] = s
	}
	for _, l := range loaders {
		r := results[l.name]
		s := previous[l.name]
		s.Name = l.name
		s.Available = len(r.pairs) > 0
		s.Complete = s.Available && r.err == nil
		if s.Available {
			if s.Complete {
				c.bySource[l.name] = r.pairs
			} else {
				c.bySource[l.name] = uniquePairs(append(c.bySource[l.name], r.pairs...))
			}
			s.UpdatedAt = time.Now().UTC()
		}
		s.Pairs = len(c.bySource[l.name])
		sources = append(sources, s)
		all = append(all, c.bySource[l.name]...)
	}
	c.pairs, c.sources = uniquePairs(all), sources
	c.saveCacheLocked()
}
