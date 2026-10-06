package exchanges

import (
	"context"
	"errors"
	"net/url"
	"shortlong/arbitrage/internal/market"
	"strconv"
	"time"
)

// Public partial books: 50 levels; no private endpoints or order placement.
func (c *Client) Depth(ctx context.Context, i market.Instrument) (market.Book, error) {
	base := c.BaseSpot
	path := ""
	symbol := url.QueryEscape(i.Native)
	future := i.Market == "perpetual"
	if future {
		base = c.BasePerpetual
	}
	switch c.Exchange {
	case "binance":
		path = "/api/v3/depth?symbol=" + symbol + "&limit=50"
		if future {
			path = "/fapi/v1/depth?symbol=" + symbol + "&limit=50"
		}
	case "bybit":
		category := "spot"
		if future {
			category = "linear"
		}
		path = "/v5/market/orderbook?category=" + category + "&symbol=" + symbol + "&limit=50"
	case "okx":
		path = "/api/v5/market/books?instId=" + symbol + "&sz=50"
	case "bitget":
		path = "/api/v2/spot/market/orderbook?symbol=" + symbol + "&type=step0&limit=50"
		if future {
			path = "/api/v2/mix/market/merge-depth?symbol=" + symbol + "&productType=USDT-FUTURES&precision=scale0&limit=50"
		}
	case "gateio":
		path = "/api/v4/spot/order_book?currency_pair=" + symbol + "&limit=50&with_id=true"
		if future {
			path = "/api/v4/futures/usdt/order_book?contract=" + symbol + "&limit=50&with_id=true"
		}
	case "kucoin":
		path = "/api/v1/market/orderbook/level2_100?symbol=" + symbol
		if future {
			path = "/api/v1/level2/depth100?symbol=" + symbol
		}
	default:
		return market.Book{}, errors.New("unknown depth exchange")
	}
	v, err := c.request(ctx, "GET", base, path)
	if err != nil {
		return market.Book{}, err
	}
	x := rec(v)
	stamp := func(value any) int64 {
		n, err := strconv.ParseInt(text(value), 10, 64)
		if err != nil {
			return 0
		}
		for n > 100000000000000 {
			n /= 1000
		}
		return n
	}
	ts := stamp(x["E"])
	switch c.Exchange {
	case "bybit":
		x = nested(x, "result")
		ts = stamp(x["cts"])
		if ts == 0 {
			ts = stamp(x["ts"])
		}
	case "okx":
		rows := arr(x["data"])
		if len(rows) == 0 {
			return market.Book{}, errors.New("empty depth")
		}
		x = rec(rows[0])
		ts = stamp(x["ts"])
	case "bitget":
		x = nested(x, "data")
		ts = stamp(x["ts"])
	case "kucoin":
		x = nested(x, "data")
		ts = stamp(x["time"])
		if ts == 0 {
			ts = stamp(x["timestamp"])
		}
	case "gateio":
		ts = int64(number(x, "current") * 1000)
	}
	bids, asks := x["bids"], x["asks"]
	if c.Exchange == "bybit" {
		bids, asks = x["b"], x["a"]
	}
	levels := func(v any) []market.Level {
		out := []market.Level{}
		for _, row := range arr(v) {
			p, q := 0.0, 0.0
			if a := arr(row); len(a) >= 2 {
				p, q = num(a[0]), num(a[1])
			} else {
				r := rec(row)
				p, q = number(r, "p"), number(r, "s")
			}
			if p > 0 && q > 0 {
				out = append(out, market.Level{Price: p / i.PriceScale, Quantity: q * i.QuantityScale})
			}
		}
		return out[:min(50, len(out))]
	}
	for ts > 100000000000000 {
		ts /= 1000
	}
	b := market.Book{Instrument: i, Bids: levels(bids), Asks: levels(asks), SourceAt: ts, ReceivedAt: time.Now().UnixMilli(), Valid: true, DepthSource: "rest-50"}
	market.SortLevels(b.Bids, true)
	market.SortLevels(b.Asks, false)
	if err := b.Validate(); err != nil {
		return market.Book{}, err
	}
	return b, nil
}
