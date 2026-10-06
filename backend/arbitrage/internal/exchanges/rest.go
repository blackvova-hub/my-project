package exchanges

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"shortlong/arbitrage/internal/market"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Record map[string]any

type httpStatusError struct {
	exchange string
	status   int
	label    string
}

func (e *httpStatusError) Error() string { return fmt.Sprintf("%s: HTTP %d", e.exchange, e.status) }

func text(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return string(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}
func num(v any) float64 {
	n, _ := strconv.ParseFloat(text(v), 64)
	if !market.Finite(n) {
		return 0
	}
	return n
}
func publicFee(r Record, key string, fallback float64) float64 {
	v, err := strconv.ParseFloat(text(r[key]), 64)
	if err != nil || !market.Finite(v) || v < 0 || v > 0.05 {
		return fallback
	}
	return v
}
func rec(v any) Record {
	if r, ok := v.(map[string]any); ok {
		return r
	}
	return Record{}
}
func arr(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}
func str(r Record, k string) string     { return text(r[k]) }
func number(r Record, k string) float64 { return num(r[k]) }
func nested(r Record, k string) Record  { return rec(r[k]) }
func env(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

type Client struct {
	Exchange      string
	HTTP          *http.Client
	Allowed       map[string]bool
	BaseSpot      string
	BasePerpetual string
}

func New(exchange, assets string) *Client {
	bases := map[string][2]string{"binance": {"https://api.binance.com", "https://fapi.binance.com"}, "bybit": {"https://api.bybit.com", "https://api.bybit.com"}, "okx": {"https://www.okx.com", "https://www.okx.com"}, "bitget": {"https://api.bitget.com", "https://api.bitget.com"}, "gateio": {"https://api.gateio.ws", "https://api.gateio.ws"}, "kucoin": {"https://api.kucoin.com", "https://api-futures.kucoin.com"}}
	b := bases[exchange]
	prefix := strings.ToUpper(exchange)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if exchange == "gateio" {
		// Gate's HTTP/2 responses can stall on this deployment's network path.
		// Keep TLS validation and connection reuse while using its HTTP/1.1 API.
		transport.Protocols = new(http.Protocols)
		transport.Protocols.SetHTTP1(true)
		transport.ForceAttemptHTTP2 = false
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}
	}
	return &Client{exchange, &http.Client{Transport: transport, Timeout: 45 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, market.AssetSet(assets), env(prefix+"_SPOT_BASE_URL", b[0]), env(prefix+"_FUTURES_BASE_URL", b[1])}
}
func (c *Client) request(ctx context.Context, method, base, path string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShortLongArbitrage/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var detail struct {
			Label string `json:"label"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&detail)
		return nil, &httpStatusError{c.Exchange, resp.StatusCode, detail.Label}
	}
	d := json.NewDecoder(io.LimitReader(resp.Body, 32<<20))
	d.UseNumber()
	var v any
	if err = d.Decode(&v); err != nil {
		return nil, err
	}
	r := rec(v)
	if code := str(r, "code"); code != "" && code != "0" && code != "00000" && code != "200000" {
		return nil, fmt.Errorf("%s: API code %s", c.Exchange, code)
	}
	if n, ok := r["retCode"]; ok && text(n) != "0" {
		return nil, fmt.Errorf("bybit: API code %s", text(n))
	}
	return v, nil
}
func (c *Client) Catalog(ctx context.Context, kind string) ([]market.Instrument, error) {
	base := c.BaseSpot
	if kind == "perpetual" {
		base = c.BasePerpetual
	}
	var rows []any
	path := ""
	switch c.Exchange {
	case "binance":
		path = "/api/v3/exchangeInfo"
		if kind == "perpetual" {
			path = "/fapi/v1/exchangeInfo"
		}
	case "bybit":
		category := "spot"
		if kind == "perpetual" {
			category = "linear"
		}
		path = "/v5/market/instruments-info?category=" + category + "&limit=1000"
	case "okx":
		t := "SPOT"
		if kind == "perpetual" {
			t = "SWAP"
		}
		path = "/api/v5/public/instruments?instType=" + t
	case "bitget":
		path = "/api/v2/spot/public/symbols"
		if kind == "perpetual" {
			path = "/api/v2/mix/market/contracts?productType=USDT-FUTURES"
		}
	case "gateio":
		path = "/api/v4/spot/currency_pairs"
		if kind == "perpetual" {
			path = "/api/v4/futures/usdt/contracts"
		}
	case "kucoin":
		path = "/api/v2/symbols"
		if kind == "perpetual" {
			path = "/api/v1/contracts/active"
		}
	default:
		return nil, fmt.Errorf("unknown exchange %s", c.Exchange)
	}
	var value any
	var err error
	if c.Exchange == "gateio" && !market.AllAssets(c.Allowed) {
		symbols := make([]string, 0, len(c.Allowed))
		for asset := range c.Allowed {
			symbols = append(symbols, asset+"_USDT")
			if kind == "perpetual" && (asset == "PEPE" || asset == "SHIB" || asset == "BONK" || asset == "FLOKI") {
				symbols = append(symbols, "1000"+asset+"_USDT")
			}
		}
		value, err = c.gateRows(ctx, base, path, symbols)
	} else {
		value, err = c.request(ctx, "GET", base, path)
	}
	if err != nil {
		return nil, err
	}
	r := rec(value)
	switch c.Exchange {
	case "binance":
		rows = arr(r["symbols"])
	case "bybit":
		rows = arr(nested(r, "result")["list"])
		cursor := str(nested(r, "result"), "nextPageCursor")
		seenCursor := map[string]bool{}
		for cursor != "" {
			if seenCursor[cursor] || len(seenCursor) >= 20 {
				return nil, fmt.Errorf("bybit catalogue pagination is incomplete")
			}
			seenCursor[cursor] = true
			v, e := c.request(ctx, "GET", base, path+"&cursor="+url.QueryEscape(cursor))
			if e != nil {
				return nil, e
			}
			result := nested(rec(v), "result")
			rows = append(rows, arr(result["list"])...)
			cursor = str(result, "nextPageCursor")
		}
	case "gateio":
		rows = arr(value)
	default:
		rows = arr(r["data"])
	}
	out := make([]market.Instrument, 0, len(rows))
	seen := map[string]bool{}
	for _, v := range rows {
		x := rec(v)
		i := market.Instrument{Exchange: c.Exchange, Market: kind, PriceScale: 1, QuantityScale: 1, TakerFee: 0.001}
		if kind == "perpetual" {
			i.TakerFee = 0.0006
		}
		quote, asset, status := "", "", ""
		enabled := true
		switch c.Exchange {
		case "binance":
			i.Native = str(x, "symbol")
			asset = str(x, "baseAsset")
			quote = str(x, "quoteAsset")
			status = str(x, "status")
			enabled = status == "TRADING"
			if kind == "perpetual" {
				enabled = enabled && str(x, "contractType") == "PERPETUAL" && str(x, "marginAsset") == "USDT"
				i.FundingIntervalMS = 8 * 3600000
				i.TakerFee = 0.0005
			} else {
				enabled = enabled && x["isSpotTradingAllowed"] == true
			}
			for _, f := range arr(x["filters"]) {
				p := rec(f)
				switch str(p, "filterType") {
				case "LOT_SIZE":
					i.Step = number(p, "stepSize")
					i.MinQuantity = number(p, "minQty")
				case "MIN_NOTIONAL":
					i.MinNotional = math.Max(number(p, "minNotional"), number(p, "notional"))
				case "NOTIONAL":
					i.MinNotional = math.Max(i.MinNotional, number(p, "minNotional"))
				}
			}
		case "bybit":
			i.Native = str(x, "symbol")
			asset = str(x, "baseCoin")
			quote = str(x, "quoteCoin")
			enabled = str(x, "status") == "Trading"
			lot := nested(x, "lotSizeFilter")
			i.Step = number(lot, "qtyStep")
			i.MinQuantity = number(lot, "minOrderQty")
			i.MinNotional = math.Max(number(lot, "minOrderAmt"), number(lot, "minNotionalValue"))
			if kind == "spot" {
				i.Step = number(lot, "basePrecision")
			} else {
				enabled = enabled && str(x, "contractType") == "LinearPerpetual" && str(x, "settleCoin") == "USDT"
				i.FundingIntervalMS = int64(number(x, "fundingInterval") * 60000)
				i.TakerFee = 0.00055
			}
		case "okx":
			i.Native = str(x, "instId")
			asset = str(x, "baseCcy")
			quote = str(x, "quoteCcy")
			enabled = str(x, "state") == "live"
			i.Step = number(x, "lotSz")
			i.MinQuantity = number(x, "minSz")
			if kind == "perpetual" {
				parts := strings.Split(i.Native, "-")
				if len(parts) >= 3 {
					asset = parts[0]
					quote = parts[1]
				}
				enabled = enabled && str(x, "ctType") == "linear" && str(x, "settleCcy") == "USDT" && str(x, "ctValCcy") == asset
				i.QuantityScale = number(x, "ctVal")
				if m := number(x, "ctMult"); m > 0 {
					i.QuantityScale *= m
				}
			}
		case "bitget":
			i.Native = str(x, "symbol")
			asset = str(x, "baseCoin")
			quote = str(x, "quoteCoin")
			i.TakerFee = publicFee(x, "takerFeeRate", i.TakerFee)
			enabled = str(x, "status") == "online"
			i.Step = math.Pow10(-int(number(x, "quantityPrecision")))
			i.MinQuantity = number(x, "minTradeAmount")
			i.MinNotional = number(x, "minTradeUSDT")
			if kind == "perpetual" {
				enabled = str(x, "symbolStatus") == "normal" && str(x, "symbolType") == "perpetual" && str(x, "isRwa") != "YES"
				i.Step = number(x, "sizeMultiplier")
				i.MinQuantity = number(x, "minTradeNum")
				i.FundingIntervalMS = int64(number(x, "fundInterval") * 3600000)
			}
		case "gateio":
			i.Native = str(x, "id")
			asset = str(x, "base")
			quote = str(x, "quote")
			enabled = str(x, "trade_status") == "tradable"
			i.Step = math.Pow10(-int(number(x, "amount_precision")))
			i.MinQuantity = number(x, "min_base_amount")
			i.MinNotional = number(x, "min_quote_amount")
			if kind == "perpetual" {
				i.Native = str(x, "name")
				parts := strings.Split(i.Native, "_")
				if len(parts) == 2 {
					asset = parts[0]
					quote = parts[1]
				}
				enabled = x["in_delisting"] != true && x["is_pre_market"] != true && (str(x, "status") == "trading" || str(x, "status") == "")
				// Stocks/ETFs and other traditional markets may reuse coin tickers.
				classification := strings.ToLower(str(x, "contract_type"))
				if classification == "stocks" || classification == "metals" || classification == "indices" || classification == "forex" || classification == "commodities" {
					enabled = false
				}
				i.QuantityScale = number(x, "quanto_multiplier")
				i.Step = 1
				if x["enable_decimal"] == true {
					i.Step = number(x, "order_size_min")
				}
				i.MinQuantity = number(x, "order_size_min")
				i.MinNotional = 0
				i.TakerFee = publicFee(x, "taker_fee_rate", i.TakerFee)
				i.FundingIntervalMS = int64(number(x, "funding_interval") * 1000)
			}
		case "kucoin":
			i.Native = str(x, "symbol")
			asset = str(x, "baseCurrency")
			quote = str(x, "quoteCurrency")
			enabled = x["enableTrading"] == true
			i.Step = number(x, "baseIncrement")
			i.MinQuantity = number(x, "baseMinSize")
			i.MinNotional = number(x, "minFunds")
			if kind == "perpetual" {
				enabled = str(x, "status") == "Open" && x["isInverse"] != true && str(x, "settleCurrency") == "USDT" && x["expireDate"] == nil
				i.Step = number(x, "lotSize")
				i.MinQuantity = i.Step
				i.QuantityScale = number(x, "multiplier")
				i.FundingIntervalMS = int64(number(x, "currentFundingRateGranularity"))
				if i.FundingIntervalMS == 0 {
					i.FundingIntervalMS = int64(number(x, "fundingRateGranularity"))
				}
				i.TakerFee = publicFee(x, "takerFeeRate", i.TakerFee)
			}
		}
		a, scale, ok := market.Asset(asset, c.Allowed)
		if !enabled || quote != "USDT" || !ok || i.Native == "" || i.QuantityScale <= 0 || i.Step <= 0 {
			continue
		}
		i.Asset = a
		i.AssetIdentity = market.IdentityFor(c.Exchange, kind, a)
		i.PriceScale = scale
		i.QuantityScale *= scale
		i.Step *= i.QuantityScale
		i.MinQuantity *= i.QuantityScale
		i.ID = i.Key()
		i.TradeURL = tradeURL(i)
		if seen[i.ID] {
			continue
		}
		seen[i.ID] = true
		out = append(out, i)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s %s: no supported instruments", c.Exchange, kind)
	}
	return out, nil
}

// Fetch only reviewed instruments: the full Gate catalogue can exceed a megabyte.
// Individual public endpoints keep startup and funding usable on slow connections.
func (c *Client) gateRows(ctx context.Context, base, path string, symbols []string) ([]any, error) {
	sort.Strings(symbols)
	rows := make([]any, 0, len(symbols))
	for _, symbol := range symbols {
		value, err := c.request(ctx, "GET", base, path+"/"+url.PathEscape(symbol))
		if err != nil {
			var status *httpStatusError
			if errors.As(err, &status) && (status.status == http.StatusNotFound || status.status == http.StatusBadRequest && (status.label == "INVALID_CURRENCY_PAIR" || status.label == "CONTRACT_NOT_FOUND")) {
				continue
			}
			return nil, err
		}
		r := rec(value)
		if str(r, "id") != symbol && str(r, "name") != symbol {
			return nil, fmt.Errorf("gateio: unexpected instrument for %s", symbol)
		}
		rows = append(rows, value)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return rows, nil
}
func tradeURL(i market.Instrument) string {
	s := url.PathEscape(i.Native)
	switch i.Exchange {
	case "binance":
		if i.Market == "spot" {
			return "https://www.binance.com/en/trade/" + i.Asset + "_USDT?type=spot"
		}
		return "https://www.binance.com/en/futures/" + s
	case "bybit":
		if i.Market == "spot" {
			return "https://www.bybit.com/en/trade/spot/" + i.Asset + "/USDT"
		}
		return "https://www.bybit.com/trade/usdt/" + s
	case "okx":
		p := "spot"
		if i.Market == "perpetual" {
			p = "swap"
		}
		return "https://www.okx.com/trade-" + p + "/" + strings.ToLower(s)
	case "bitget":
		if i.Market == "spot" {
			return "https://www.bitget.com/spot/" + s
		}
		return "https://www.bitget.com/futures/usdt/" + s
	case "gateio":
		if i.Market == "spot" {
			return "https://www.gate.com/trade/" + s
		}
		return "https://www.gate.com/futures/USDT/" + s
	case "kucoin":
		if i.Market == "spot" {
			return "https://www.kucoin.com/trade/" + s
		}
		return "https://www.kucoin.com/futures/trade/" + s
	}
	return ""
}
func (c *Client) Funding(ctx context.Context, instruments []market.Instrument) (map[string]market.Funding, error) {
	out := map[string]market.Funding{}
	if len(instruments) == 0 {
		return out, nil
	}
	now := time.Now().UnixMilli()
	base := c.BasePerpetual
	path := ""
	switch c.Exchange {
	case "binance":
		path = "/fapi/v1/premiumIndex"
	case "bybit":
		path = "/v5/market/tickers?category=linear"
	case "bitget":
		path = "/api/v2/mix/market/current-fund-rate?productType=USDT-FUTURES"
	case "gateio":
		path = "/api/v4/futures/usdt/contracts"
	case "kucoin":
		path = "/api/v1/contracts/active"
	}
	byNative := map[string]market.Instrument{}
	for _, i := range instruments {
		byNative[i.Native] = i
	}
	if c.Exchange == "okx" {
		for _, i := range instruments {
			v, err := c.request(ctx, "GET", base, "/api/v5/public/funding-rate?instId="+url.QueryEscape(i.Native))
			if err != nil {
				return out, err
			}
			rows := arr(rec(v)["data"])
			if len(rows) == 0 {
				continue
			}
			x := rec(rows[0])
			next := int64(number(x, "fundingTime"))
			interval := int64(number(x, "nextFundingTime")) - next
			if interval <= 0 {
				continue
			}
			rate, parseErr := strconv.ParseFloat(text(x["fundingRate"]), 64)
			if parseErr == nil && market.Finite(rate) && math.Abs(rate) <= 0.1 && next > now {
				out[i.ID] = market.Funding{Rate: rate, NextAt: next, IntervalMS: interval, ObservedAt: now}
			}
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(120 * time.Millisecond):
			}
		}
		return out, nil
	}
	var v any
	var err error
	if c.Exchange == "gateio" {
		symbols := make([]string, 0, len(instruments))
		for _, i := range instruments {
			symbols = append(symbols, i.Native)
		}
		v, err = c.gateRows(ctx, base, path, symbols)
	} else {
		v, err = c.request(ctx, "GET", base, path)
	}
	if err != nil {
		return out, err
	}
	r := rec(v)
	rows := arr(v)
	switch c.Exchange {
	case "bybit":
		rows = arr(nested(r, "result")["list"])
	case "bitget", "kucoin":
		rows = arr(r["data"])
	}
	intervals := map[string]int64{}
	if c.Exchange == "binance" {
		v, e := c.request(ctx, "GET", base, "/fapi/v1/fundingInfo")
		if e != nil {
			return out, e
		}
		for _, a := range arr(v) {
			x := rec(a)
			intervals[str(x, "symbol")] = int64(number(x, "fundingIntervalHours") * 3600000)
		}
	}
	for _, row := range rows {
		x := rec(row)
		native := str(x, "symbol")
		if c.Exchange == "gateio" {
			native = str(x, "name")
		}
		i, ok := byNative[native]
		if !ok {
			continue
		}
		f := market.Funding{IntervalMS: i.FundingIntervalMS, ObservedAt: now}
		switch c.Exchange {
		case "binance":
			f.Rate = number(x, "lastFundingRate")
			f.NextAt = int64(number(x, "nextFundingTime"))
			if interval := intervals[native]; interval > 0 {
				f.IntervalMS = interval
			}
		case "bybit":
			f.Rate = number(x, "fundingRate")
			f.NextAt = int64(number(x, "nextFundingTime"))
			if h := number(x, "fundingIntervalHour"); h > 0 {
				f.IntervalMS = int64(h * 3600000)
			}
		case "bitget":
			f.Rate = number(x, "fundingRate")
			f.NextAt = int64(number(x, "nextUpdate"))
			if h := number(x, "fundingRateInterval"); h > 0 {
				f.IntervalMS = int64(h * 3600000)
			}
		case "gateio":
			f.Rate = number(x, "funding_rate")
			f.NextAt = int64(number(x, "funding_next_apply") * 1000)
			f.IntervalMS = int64(number(x, "funding_interval") * 1000)
		case "kucoin":
			f.Rate = number(x, "fundingFeeRate")
			f.NextAt = int64(number(x, "nextFundingRateDateTime"))
			if h := number(x, "currentFundingRateGranularity"); h > 0 {
				f.IntervalMS = int64(h)
			}
		}
		// Missing fields never become a fabricated zero-rate funding projection.
		rateKey := map[string]string{"binance": "lastFundingRate", "bybit": "fundingRate", "bitget": "fundingRate", "gateio": "funding_rate", "kucoin": "fundingFeeRate"}[c.Exchange]
		rate, parseErr := strconv.ParseFloat(text(x[rateKey]), 64)
		if parseErr == nil && market.Finite(rate) && math.Abs(rate) <= 0.1 && f.NextAt > now && f.IntervalMS > 0 {
			f.Rate = rate
			out[i.ID] = f
		}
	}
	return out, nil
}
