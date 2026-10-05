package market

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

var Exchanges = []string{"binance", "bybit", "okx", "bitget", "gateio", "kucoin"}

// Deliberate reviewed universe. Unknown same-name tokens are not silently matched.
const DefaultAssets = "BTC,ETH,SOL,XRP,DOGE,ADA,AVAX,LINK,DOT,LTC,BCH,TRX,TON,SUI,APT,ARB,OP,NEAR,UNI,AAVE,ATOM,ETC,FIL,ICP,INJ,SEI,PEPE,SHIB,BONK,FLOKI"

type Instrument struct {
	ID                string  `json:"id"`
	Exchange          string  `json:"exchange"`
	Market            string  `json:"market"`
	Native            string  `json:"nativeSymbol"`
	Asset             string  `json:"asset"`
	PriceScale        float64 `json:"priceScale"`
	QuantityScale     float64 `json:"quantityScale"`
	Step              float64 `json:"quantityStep"`
	MinQuantity       float64 `json:"minQuantity"`
	MinNotional       float64 `json:"minNotional"`
	TakerFee          float64 `json:"takerFee"`
	FundingIntervalMS int64   `json:"fundingIntervalMs"`
	TradeURL          string  `json:"tradeUrl"`
}
type Level struct {
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
}
type Book struct {
	Instrument Instrument `json:"instrument"`
	Bids       []Level    `json:"bids"`
	Asks       []Level    `json:"asks"`
	SourceAt   int64      `json:"sourceAt"`
	ReceivedAt int64      `json:"receivedAt"`
	Sequence   int64      `json:"sequence"`
	Valid      bool       `json:"valid"`
	Funding    *Funding   `json:"funding,omitempty"`
}
type Funding struct {
	Rate       float64 `json:"rate"`
	NextAt     int64   `json:"nextAt"`
	IntervalMS int64   `json:"intervalMs"`
	ObservedAt int64   `json:"observedAt"`
}
type Status struct {
	Exchange    string `json:"exchange"`
	Market      string `json:"market"`
	State       string `json:"state"`
	Message     string `json:"message,omitempty"`
	Instruments int    `json:"instruments"`
	Books       int    `json:"books"`
	UpdatedAt   int64  `json:"updatedAt"`
	LastBookAt  int64  `json:"lastBookAt"`
}

func Finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func KnownExchange(s string) bool {
	for _, e := range Exchanges {
		if s == e {
			return true
		}
	}
	return false
}
func Asset(base string, allowed map[string]bool) (string, float64, bool) {
	base = strings.ToUpper(base)
	if base == "XBT" {
		base = "BTC"
	}
	if allowed[base] {
		return base, 1, true
	}
	// Only reviewed bundled contracts. Never strip numeric prefixes in general.
	bundles := map[string]string{"1000PEPE": "PEPE", "1000SHIB": "SHIB", "1000BONK": "BONK", "1000FLOKI": "FLOKI"}
	if a, ok := bundles[base]; ok && allowed[a] {
		return a, 1000, true
	}
	return "", 0, false
}
func AssetSet(raw string) map[string]bool {
	out := map[string]bool{}
	for _, s := range strings.Split(raw, ",") {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s != "" {
			out[s] = true
		}
	}
	return out
}
func (i Instrument) Key() string { return i.Exchange + ":" + i.Market + ":" + i.Native }

// Conservatively use the older exchange/receipt timestamp for quote ageing.
func (b Book) QuoteAt() int64 {
	if b.SourceAt > 0 {
		return min(b.SourceAt, b.ReceivedAt)
	}
	return b.ReceivedAt
}
func (b Book) Validate() error {
	i := b.Instrument
	if !Finite(i.MinQuantity) || i.MinQuantity < 0 || !Finite(i.MinNotional) || i.MinNotional < 0 {
		return errors.New("invalid minimum size")
	}
	if !KnownExchange(i.Exchange) || (i.Market != "spot" && i.Market != "perpetual") || i.ID != i.Key() || i.Asset == "" || !Finite(i.Step) || i.Step <= 0 || !Finite(i.PriceScale) || i.PriceScale <= 0 || !Finite(i.QuantityScale) || i.QuantityScale <= 0 || !Finite(i.TakerFee) || i.TakerFee < 0 || i.TakerFee > 0.05 {
		return errors.New("invalid instrument")
	}
	if !b.Valid || b.ReceivedAt <= 0 || len(b.Bids) == 0 || len(b.Asks) == 0 || len(b.Bids) > 50 || len(b.Asks) > 50 {
		return errors.New("invalid book")
	}
	for side, levels := range [][]Level{b.Bids, b.Asks} {
		for n, l := range levels {
			if !Finite(l.Price) || !Finite(l.Quantity) || l.Price <= 0 || l.Quantity <= 0 {
				return errors.New("invalid level")
			}
			if n > 0 && ((side == 0 && levels[n-1].Price <= l.Price) || (side == 1 && levels[n-1].Price >= l.Price)) {
				return errors.New("unordered book")
			}
		}
	}
	if b.Bids[0].Price >= b.Asks[0].Price {
		return errors.New("crossed book")
	}
	return nil
}
func (b Book) Fresh(now time.Time, maxAge time.Duration) bool {
	if b.Validate() != nil {
		return false
	}
	at := now.UnixMilli()
	age := at - b.ReceivedAt
	if age < -2000 || age > maxAge.Milliseconds() {
		return false
	}
	if b.SourceAt > 0 && (at-b.SourceAt > maxAge.Milliseconds() || b.SourceAt-at > 2000) {
		return false
	}
	return true
}
func VWAP(levels []Level, q float64) (float64, bool) {
	if !Finite(q) || q <= 0 {
		return 0, false
	}
	left, total := q, 0.0
	for _, l := range levels {
		take := math.Min(left, l.Quantity)
		total += take * l.Price
		left -= take
		if left <= q*1e-10 {
			return total / q, true
		}
	}
	return 0, false
}
func Depth(levels []Level) float64 {
	v := 0.0
	for _, l := range levels {
		v += l.Price * l.Quantity
	}
	return v
}
func SortLevels(levels []Level, bids bool) {
	sort.Slice(levels, func(i, j int) bool {
		if bids {
			return levels[i].Price > levels[j].Price
		}
		return levels[i].Price < levels[j].Price
	})
}
