package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"net/url"
	bt "shortlong/backtest"
	"shortlong/backtest/engine"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Binance struct {
	SpotURL, FuturesURL string
	HTTP                *http.Client
	Redis               *redis.Client
}

func NewBinance(spot, futures string, rdb *redis.Client) *Binance {
	if spot == "" {
		spot = "https://api.binance.com"
	}
	if futures == "" {
		futures = "https://fapi.binance.com"
	}
	return &Binance{strings.TrimRight(spot, "/"), strings.TrimRight(futures, "/"), httpClient(30 * time.Second), rdb}
}

func (b *Binance) get(ctx context.Context, market, path string, p url.Values, out any) error {
	base := b.SpotURL
	if market == "linear" {
		base = b.FuturesURL
	} else if market != "spot" {
		return fmt.Errorf("invalid market")
	}
	key := "archive:binance:" + market
	for attempt := 0; attempt < 4; attempt++ {
		if b.Redis != nil {
			for {
				cooldown, err := b.Redis.PTTL(ctx, key+":cooldown").Result()
				if err != nil {
					return err
				}
				if cooldown > 0 {
					if err = wait(ctx, cooldown); err != nil {
						return err
					}
					continue
				}
				// At most 2 requests/s per API. Leaves room for the existing live scanners.
				ok, err := b.Redis.SetNX(ctx, key+":rate", 1, 500*time.Millisecond).Result()
				if err != nil {
					return err
				}
				if ok {
					break
				}
				if err = wait(ctx, 50*time.Millisecond); err != nil {
					return err
				}
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+p.Encode(), nil)
		if err != nil {
			return err
		}
		res, err := b.HTTP.Do(req)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, (32<<20)+1))
		res.Body.Close()
		if res.StatusCode == 429 || res.StatusCode == 418 {
			seconds, _ := strconv.Atoi(res.Header.Get("Retry-After"))
			if seconds < 60 {
				seconds = 60
			}
			if res.StatusCode == 418 && seconds < 120 {
				seconds = 120
			}
			if b.Redis != nil {
				if err = b.Redis.Set(ctx, key+":cooldown", 1, time.Duration(seconds)*time.Second).Err(); err != nil {
					return err
				}
			}
			return fmt.Errorf("Binance rate limit HTTP %d; cooldown %ds", res.StatusCode, seconds)
		}
		if res.StatusCode >= 500 {
			if err = wait(ctx, time.Duration(1<<attempt)*time.Second); err != nil {
				return err
			}
			continue
		}
		if res.StatusCode != 200 {
			return fmt.Errorf("Binance HTTP %d", res.StatusCode)
		}
		if readErr != nil {
			return readErr
		}
		if len(raw) > 32<<20 {
			return fmt.Errorf("Binance response exceeds 32 MiB")
		}
		return json.Unmarshal(raw, out)
	}
	return fmt.Errorf("Binance retries exhausted")
}

func (b *Binance) Instruments(ctx context.Context, market string) ([]Instrument, error) {
	path := "/api/v3/exchangeInfo"
	if market == "linear" {
		path = "/fapi/v1/exchangeInfo"
	}
	var r struct {
		Symbols []struct {
			Symbol, Status, QuoteAsset, ContractType string
			OnboardDate                              int64
		}
	}
	params := url.Values{}
	if market == "spot" {
		params.Set("symbolStatus", "TRADING")
		params.Set("showPermissionSets", "false")
	}
	if err := b.get(ctx, market, path, params, &r); err != nil {
		return nil, err
	}
	out := []Instrument{}
	seen := map[string]bool{}
	for _, s := range r.Symbols {
		if s.Status != "TRADING" || s.QuoteAsset != "USDT" || !bt.ValidSymbol(s.Symbol) || seen[s.Symbol] || (market == "linear" && s.ContractType != "PERPETUAL") {
			continue
		}
		seen[s.Symbol] = true
		out = append(out, Instrument{Symbol: s.Symbol, LaunchTime: s.OnboardDate})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	if len(out) == 0 {
		return nil, fmt.Errorf("empty Binance catalog")
	}
	return out, nil
}

func (b *Binance) Candles(ctx context.Context, market, symbol, tf string, from, to int64) ([]bt.Candle, error) {
	step := candleStep(tf)
	if !bt.ValidSymbol(symbol) || step == 0 || from < 0 || to <= from || from%step != 0 || to%step != 0 || to-from > 1000*step {
		return nil, fmt.Errorf("invalid Binance candle range")
	}
	path := "/api/v3/klines"
	if market == "linear" {
		path = "/fapi/v1/klines"
	}
	p := url.Values{"symbol": {symbol}, "interval": {tf}, "startTime": {strconv.FormatInt(from, 10)}, "endTime": {strconv.FormatInt(to-1, 10)}, "limit": {"1000"}}
	var rows [][]json.RawMessage
	if err := b.get(ctx, market, path, p, &rows); err != nil {
		return nil, err
	}
	out := []bt.Candle{}
	seen := map[int64]bool{}
	for _, r := range rows {
		if len(r) < 6 {
			return nil, fmt.Errorf("short Binance candle")
		}
		var c bt.Candle
		if err := json.Unmarshal(r[0], &c.Time); err != nil {
			return nil, err
		}
		if c.Time < from || c.Time >= to || c.Time+step > time.Now().UnixMilli() {
			continue
		}
		if seen[c.Time] {
			return nil, fmt.Errorf("duplicate Binance timestamp")
		}
		seen[c.Time] = true
		for i, target := range []*float64{&c.Open, &c.High, &c.Low, &c.Close, &c.Volume} {
			var s string
			if err := json.Unmarshal(r[i+1], &s); err != nil {
				return nil, err
			}
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, err
			}
			*target = v
		}
		if err := engine.ValidateCandles([]bt.Candle{c}, c.Time, c.Time+step, step); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out, nil
}
