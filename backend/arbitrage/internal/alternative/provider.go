// Package alternative supplies public USD references and reviewed Uniswap pools.
// It never holds wallets, signing keys or permissions to redeem/trade.
package alternative

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"os"
	"shortlong/arbitrage/internal/market"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const factory = "0x1f98431c8ad98523631ae4a59f267346ea31f984"

type Provider struct {
	HTTP      *http.Client
	KrakenURL string
	YieldsURL string
	RPC       map[string]string
	mu        sync.RWMutex
	data      market.AlternativeData
}

func New() *Provider {
	return &Provider{HTTP: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, KrakenURL: "https://api.kraken.com", YieldsURL: "https://yields.llama.fi", RPC: map[string]string{"Ethereum": env("ARBITRAGE_ETH_RPC", "https://ethereum-rpc.publicnode.com"), "Arbitrum": env("ARBITRAGE_ARB_RPC", "https://arbitrum-one-rpc.publicnode.com")}}
}
func env(k, d string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return d
}
func (p *Provider) Snapshot() market.AlternativeData {
	p.mu.RLock()
	defer p.mu.RUnlock()
	d := p.data
	d.Pools = append([]market.LPPool{}, d.Pools...)
	d.Sources = append([]market.ExternalStatus{}, d.Sources...)
	return d
}
func (p *Provider) request(ctx context.Context, url string, body any, out any) error {
	method := "GET"
	var r io.Reader
	if body != nil {
		method = "POST"
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ShortLongArbitrage/2.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.HTTP.Do(req)
	if err != nil {
		return errors.New("public source request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("public source HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 24<<20)).Decode(out)
}
func (p *Provider) status(name string, err error, count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := market.ExternalStatus{Name: name, State: "live", ObservedAt: time.Now().UnixMilli(), Count: count}
	if err != nil {
		st.Count = 0
		st.State = "error"
		st.Message = err.Error()
	}
	for n, s := range p.data.Sources {
		if s.Name == name {
			p.data.Sources[n] = st
			return
		}
	}
	p.data.Sources = append(p.data.Sources, st)
}
func (p *Provider) usd(ctx context.Context, asset string) (market.USDQuote, error) {
	var d struct {
		Error  []string `json:"error"`
		Result map[string]struct {
			Bids [][]json.RawMessage `json:"bids"`
			Asks [][]json.RawMessage `json:"asks"`
		} `json:"result"`
	}
	if err := p.request(ctx, p.KrakenURL+"/0/public/Depth?pair="+asset+"USD&count=10", nil, &d); err != nil {
		return market.USDQuote{}, err
	}
	if len(d.Error) > 0 {
		return market.USDQuote{}, errors.New("USD reference rejected")
	}
	parse := func(v json.RawMessage) float64 {
		var s string
		if json.Unmarshal(v, &s) == nil {
			f, _ := strconv.ParseFloat(s, 64)
			return f
		}
		return 0
	}
	for _, r := range d.Result {
		if len(r.Bids) > 0 && len(r.Asks) > 0 && len(r.Bids[0]) >= 2 && len(r.Asks[0]) >= 2 {
			b, a := parse(r.Bids[0][0]), parse(r.Asks[0][0])
			if market.Finite(b) && market.Finite(a) && b > 0 && a >= b && a < 2 {
				return market.USDQuote{Bid: b, Ask: a, ObservedAt: time.Now().UnixMilli()}, nil
			}
		}
	}
	return market.USDQuote{}, errors.New("invalid USD reference")
}

var reviewed = map[string]map[string]struct {
	asset    string
	decimals int
}{
	"Ethereum": {"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48": {"USDC", 6}, "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2": {"ETH", 18}, "0x2260fac5e5542a773aa44fbcfedf7c193bc2c599": {"BTC", 8}},
	"Arbitrum": {"0xaf88d065e77c8cc2239327c5edb3a432268e5831": {"USDC", 6}, "0x82af49447d8a07e3bd95bd0d56f35241523fbab1": {"ETH", 18}, "0x2f2a2543b76a4166549f7aab2e75bef0aefc5b0f": {"BTC", 8}},
}

func (p *Provider) yields(ctx context.Context) ([]market.LPPool, error) {
	var d struct {
		Data []struct {
			Pool    string   `json:"pool"`
			Chain   string   `json:"chain"`
			Project string   `json:"project"`
			Symbol  string   `json:"symbol"`
			TVL     float64  `json:"tvlUsd"`
			APY     *float64 `json:"apyBase"`
			Meta    string   `json:"poolMeta"`
			Tokens  []string `json:"underlyingTokens"`
		} `json:"data"`
	}
	if err := p.request(ctx, p.YieldsURL+"/pools", nil, &d); err != nil {
		return nil, err
	}
	pools := []market.LPPool{}
	for _, r := range d.Data {
		tokens := reviewed[r.Chain]
		if r.Project != "uniswap-v3" || len(r.Tokens) != 2 || r.APY == nil || !market.Finite(*r.APY) || *r.APY < 0 || r.TVL < 100000 || !market.Finite(r.TVL) {
			continue
		}
		a, b := strings.ToLower(r.Tokens[0]), strings.ToLower(r.Tokens[1])
		ta, oka := tokens[a]
		tb, okb := tokens[b]
		if !oka || !okb || (ta.asset != "USDC" && tb.asset != "USDC") || ta.asset == tb.asset {
			continue
		}
		fee, err := strconv.ParseFloat(strings.TrimSuffix(r.Meta, "%"), 64)
		if err != nil {
			continue
		}
		tier := int(math.Round(fee * 10000))
		if tier != 100 && tier != 500 && tier != 3000 && tier != 10000 {
			continue
		}
		asset := ta.asset
		if asset == "USDC" {
			asset = tb.asset
		}
		if a > b {
			a, b = b, a
		}
		pools = append(pools, market.LPPool{ID: r.Pool, Network: r.Chain, Asset: asset, Symbol: r.Symbol, Token0: a, Token1: b, Fee: tier, TVLUSD: r.TVL, HistoricalAPY: *r.APY, YieldAt: time.Now().UnixMilli(), TickSpacing: map[int]int{100: 1, 500: 10, 3000: 60, 10000: 200}[tier]})
	}
	sort.Slice(pools, func(i, j int) bool { return pools[i].TVLUSD > pools[j].TVLUSD })
	out := []market.LPPool{}
	counts := map[string]int{}
	for _, r := range pools {
		if counts[r.Network] < 6 {
			out = append(out, r)
			counts[r.Network]++
		}
	}
	return out, nil
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}
type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func rpc(id int, method string, params any) rpcRequest {
	return rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
}
func hexInt(s string) (*big.Int, bool) {
	return new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
}
func (p *Provider) prices(ctx context.Context, network string, pools []market.LPPool) ([]market.LPPool, error) {
	var block struct {
		Result struct {
			Number    string `json:"number"`
			Timestamp string `json:"timestamp"`
		} `json:"result"`
		Error any `json:"error"`
	}
	url := p.RPC[network]
	var chain struct {
		Result string `json:"result"`
	}
	if err := p.request(ctx, url, rpc(2, "eth_chainId", []any{}), &chain); err != nil {
		return nil, err
	}
	expected := map[string]string{"Ethereum": "0x1", "Arbitrum": "0xa4b1"}[network]
	if expected == "" || chain.Result != expected {
		return nil, errors.New("unexpected RPC chain")
	}
	if err := p.request(ctx, url, rpc(1, "eth_getBlockByNumber", []any{"latest", false}), &block); err != nil {
		return nil, err
	}
	stamp, ok := hexInt(block.Result.Timestamp)
	if !ok || block.Error != nil || block.Result.Number == "" {
		return nil, errors.New("invalid RPC block")
	}
	at := stamp.Int64() * 1000
	if time.Now().UnixMilli()-at > 60000 || at > time.Now().UnixMilli()+2000 {
		return nil, errors.New("stale RPC block")
	}
	calls := []rpcRequest{}
	for n, r := range pools {
		data := "0x1698ee82" + fmt.Sprintf("%064s", strings.TrimPrefix(r.Token0, "0x")) + fmt.Sprintf("%064s", strings.TrimPrefix(r.Token1, "0x")) + fmt.Sprintf("%064x", r.Fee)
		data = strings.ReplaceAll(data, " ", "0")
		calls = append(calls, rpc(n+1, "eth_call", []any{map[string]string{"to": factory, "data": data}, block.Result.Number}))
	}
	if len(calls) == 0 {
		return []market.LPPool{}, nil
	}
	var results []rpcResponse
	if err := p.request(ctx, url, calls, &results); err != nil {
		return nil, err
	}
	addresses := map[int]string{}
	for _, r := range results {
		var s string
		if r.Error == nil && r.ID >= 1 && r.ID <= len(pools) && json.Unmarshal(r.Result, &s) == nil && len(s) == 66 && strings.HasPrefix(s, "0x") {
			v, valid := hexInt(s)
			if valid && v.Sign() > 0 && v.BitLen() <= 160 {
				addresses[r.ID] = "0x" + s[len(s)-40:]
			}
		}
	}
	calls = nil
	for id, address := range addresses {
		calls = append(calls, rpc(id, "eth_call", []any{map[string]string{"to": address, "data": "0x3850c7bd"}, block.Result.Number}))
	}
	if len(calls) == 0 {
		return nil, errors.New("no reviewed pools on chain")
	}
	results = nil
	if err := p.request(ctx, url, calls, &results); err != nil {
		return nil, err
	}
	out := []market.LPPool{}
	for _, r := range results {
		var s string
		if r.Error != nil || r.ID < 1 || r.ID > len(pools) || json.Unmarshal(r.Result, &s) != nil || len(s) < 130 {
			continue
		}
		root, ok := hexInt(s[2:66])
		if !ok || root.Sign() <= 0 {
			continue
		}
		tick, ok := hexInt(s[66:130])
		if !ok {
			continue
		}
		if tick.Bit(255) == 1 {
			tick.Sub(tick, new(big.Int).Lsh(big.NewInt(1), 256))
		}
		ratio, _ := new(big.Float).Quo(new(big.Float).SetInt(new(big.Int).Mul(root, root)), new(big.Float).SetInt(new(big.Int).Lsh(big.NewInt(1), 192))).Float64()
		pool := pools[r.ID-1]
		t0, t1 := reviewed[network][pool.Token0], reviewed[network][pool.Token1]
		price := ratio * math.Pow10(t0.decimals-t1.decimals)
		tickPrice := math.Pow(1.0001, float64(tick.Int64())) * math.Pow10(t0.decimals-t1.decimals)
		if t0.asset == "USDC" {
			price = 1 / price
			tickPrice = 1 / tickPrice
		}
		if !market.Finite(price) || price <= 0 {
			continue
		}
		pool.PriceUSDC = price
		pool.TickPriceUSDC = tickPrice
		pool.Address = addresses[r.ID]
		pool.ObservedAt = at
		pool.Tick = int(tick.Int64())
		out = append(out, pool)
	}
	if len(out) == 0 {
		return nil, errors.New("pool prices unavailable")
	}
	return out, nil
}
func (p *Provider) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			for _, asset := range []string{"USDT", "USDC"} {
				q, err := p.usd(ctx, asset)
				p.mu.Lock()
				if err == nil {
					if asset == "USDT" {
						p.data.USDTUSD = q
					} else {
						p.data.USDCUSD = q
					}
				}
				p.mu.Unlock()
				p.status("Kraken "+asset+"/USD", err, 1)
			}
			if !wait(ctx, 10*time.Second) {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		var catalog []market.LPPool
		var last time.Time
		for ctx.Err() == nil {
			if time.Since(last) > 5*time.Minute || len(catalog) == 0 {
				list, err := p.yields(ctx)
				if err == nil {
					catalog = list
					last = time.Now()
				}
				p.status("DefiLlama Uniswap v3", err, len(list))
			}
			out := []market.LPPool{}
			for _, network := range []string{"Ethereum", "Arbitrum"} {
				list := []market.LPPool{}
				for _, r := range catalog {
					if r.Network == network {
						list = append(list, r)
					}
				}
				rows, err := p.prices(ctx, network, list)
				p.status("Uniswap "+network, err, len(rows))
				if err == nil {
					out = append(out, rows...)
				}
			}
			p.mu.Lock()
			p.data.Pools = out
			p.mu.Unlock()
			if !wait(ctx, 20*time.Second) {
				return
			}
		}
	}()
	<-ctx.Done()
	wg.Wait()
}
func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
