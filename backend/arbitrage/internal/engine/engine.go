package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"shortlong/arbitrage/internal/market"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Query struct {
	Simple        bool     `json:"simple"`
	Search        string   `json:"search,omitempty"`
	Offset        int      `json:"offset,omitempty"`
	Routes        []string `json:"routes,omitempty"`
	Strategy      string   `json:"strategy"`
	Amount        float64  `json:"amount"`
	MinSpread     float64  `json:"minSpread"`
	MaxAgeMS      int64    `json:"maxAgeMs"`
	HorizonHours  int      `json:"horizonHours"`
	ExitSpread    float64  `json:"exitSpread"`
	FeeSpot       *float64 `json:"feeSpot,omitempty"`
	FeePerpetual  *float64 `json:"feePerpetual,omitempty"`
	Exchanges     []string `json:"exchanges"`
	Assets        []string `json:"assets"`
	InterExchange bool     `json:"interExchange"`
	Sort          string   `json:"sort"`
	Limit         int      `json:"limit"`
}

func Defaults() Query {
	return Query{Simple: true, Strategy: "spot-perpetual", Amount: 1000, MinSpread: 0, MaxAgeMS: 10000, HorizonHours: 24, Exchanges: append([]string{}, market.Exchanges...), InterExchange: true, Sort: "spread", Limit: 100}
}
func (q Query) Validate() error {
	if len(q.Routes) > 200 {
		return errors.New("Слишком много связок")
	}
	for _, id := range q.Routes {
		if len(id) != 24 {
			return errors.New("Некорректная связка")
		}
		if _, err := hex.DecodeString(id); err != nil {
			return errors.New("Некорректная связка")
		}
	}
	if q.Offset < 0 || q.Offset > 1000000 || len(q.Search) > 30 || (q.Search != "" && strings.ContainsAny(q.Search, " /:?&")) {
		return errors.New("Некорректный поиск")
	}
	if q.Strategy != "spot-perpetual" && q.Strategy != "perpetual-perpetual" && q.Strategy != "spot-spot" {
		return errors.New("Некорректный режим")
	}
	if !market.Finite(q.Amount) || q.Amount < 10 || q.Amount > 1000000 {
		return errors.New("Размер позиции: от 10 до 1 000 000 USDT")
	}
	if !market.Finite(q.MinSpread) || q.MinSpread < -10 || q.MinSpread > 100 || !market.Finite(q.ExitSpread) || math.Abs(q.ExitSpread) > 50 {
		return errors.New("Некорректный спред")
	}
	if q.MaxAgeMS < 1000 || q.MaxAgeMS > 30000 || q.HorizonHours < 1 || q.HorizonHours > 168 || q.Limit < 1 || q.Limit > 300 {
		return errors.New("Некорректные ограничения")
	}
	if len(q.Exchanges) == 0 || len(q.Exchanges) > 6 || len(q.Assets) > 100 {
		return errors.New("Выберите биржи")
	}
	seen := map[string]bool{}
	for _, e := range q.Exchanges {
		if !market.KnownExchange(e) || seen[e] {
			return errors.New("Некорректный список бирж")
		}
		seen[e] = true
	}
	for _, fee := range []*float64{q.FeeSpot, q.FeePerpetual} {
		if fee != nil && (!market.Finite(*fee) || *fee < 0 || *fee > 2) {
			return errors.New("Комиссия: от 0 до 2%")
		}
	}
	if q.Sort != "spread" && q.Sort != "net" && q.Sort != "funding" && q.Sort != "duration" {
		return errors.New("Некорректная сортировка")
	}
	for _, a := range q.Assets {
		if len(a) < 1 || len(a) > 30 || strings.ContainsAny(a, " /:?&") {
			return errors.New("Некорректный актив")
		}
	}
	return nil
}

type Leg struct {
	Instrument market.Instrument `json:"instrument"`
	Price      float64           `json:"price"`
	Notional   float64           `json:"notional"`
	Fee        float64           `json:"fee"`
	Funding    *market.Funding   `json:"funding,omitempty"`
	BookAt     int64             `json:"bookAt"`
	Bids       []market.Level    `json:"bids,omitempty"`
	Asks       []market.Level    `json:"asks,omitempty"`
}
type Opportunity struct {
	HorizonHours     int      `json:"horizonHours"`
	ExitSpreadPct    float64  `json:"exitSpreadPct"`
	ID               string   `json:"id"`
	Asset            string   `json:"asset"`
	Strategy         string   `json:"strategy"`
	Buy              Leg      `json:"buy"`
	Sell             Leg      `json:"sell"`
	Quantity         float64  `json:"quantity"`
	EntrySpreadPct   float64  `json:"entrySpreadPct"`
	QuoteSpreadPct   float64  `json:"quoteSpreadPct"`
	EntryFees        float64  `json:"entryFees"`
	NetEntryPct      float64  `json:"netEntryPct"`
	ExitFeesEstimate float64  `json:"exitFeesEstimate"`
	FundingEstimate  *float64 `json:"fundingEstimate"`
	FundingEvents    int      `json:"fundingEvents"`
	ScenarioPnL      *float64 `json:"scenarioPnl"`
	SpotCyclePnL     *float64 `json:"spotCyclePnl"`
	DepthUSDT        float64  `json:"depthUsdt"`
	AgeMS            int64    `json:"ageMs"`
	Since            int64    `json:"since"`
	CalculatedAt     int64    `json:"calculatedAt"`
}

func RouteID(buy, sell string) string {
	h := sha256.Sum256([]byte(buy + "|" + sell))
	return hex.EncodeToString(h[:12])
}
func fee(i market.Instrument, q Query) float64 {
	v := q.FeePerpetual
	if i.Market == "spot" {
		v = q.FeeSpot
	}
	if v != nil {
		return *v / 100
	}
	return i.TakerFee
}

// Common quantity uses decimal integer steps and LCM; there is no residual unhedged leg.
func commonQuantity(q, a, b float64) float64 {
	scale := 1e12
	if !market.Finite(q) || !market.Finite(a) || !market.Finite(b) || a <= 0 || b <= 0 {
		return 0
	}
	// Large meme-token contract lots need a smaller integer scale. Keep all
	// significant step digits; reject a step that the safe scale cannot represent.
	for math.Max(a, b)*scale > 1e15 && scale > 1 {
		scale /= 10
	}
	if math.Max(a, b)*scale > 1e15 {
		return 0
	}
	x, y := int64(math.Round(a*scale)), int64(math.Round(b*scale))
	if x == 0 || y == 0 || math.Abs(float64(x)/scale-a) > a*1e-10 || math.Abs(float64(y)/scale-b) > b*1e-10 {
		return 0
	}
	gcd := func(a, b int64) int64 {
		for b != 0 {
			a, b = b, a%b
		}
		return a
	}
	g := gcd(x, y)
	if x/g > math.MaxInt64/y {
		return 0
	}
	step := float64(x/g*y) / scale
	return math.Floor(q/step+1e-9) * step
}
func funding(f *market.Funding, now time.Time, hours int) (float64, int, bool) {
	if f == nil || !market.Finite(f.Rate) || math.Abs(f.Rate) > 0.1 || f.IntervalMS < 3600000 || f.IntervalMS > 24*3600000 || now.UnixMilli()-f.ObservedAt > 120000 || f.ObservedAt > now.UnixMilli()+2000 || f.NextAt <= now.UnixMilli() {
		return 0, 0, false
	}
	end := now.Add(time.Duration(hours) * time.Hour).UnixMilli()
	if f.NextAt > end {
		return 0, 0, true
	}
	n := int((end-f.NextAt)/f.IntervalMS) + 1
	return f.Rate * float64(n), n, true
}
func Calculate(b, s market.Book, q Query, now time.Time) (Opportunity, bool) {
	if b.Instrument.Identity() != s.Instrument.Identity() || b.Instrument.ID == s.Instrument.ID || (q.InterExchange && b.Instrument.Exchange == s.Instrument.Exchange) || !b.Fresh(now, time.Duration(q.MaxAgeMS)*time.Millisecond) || !s.Fresh(now, time.Duration(q.MaxAgeMS)*time.Millisecond) {
		return Opportunity{}, false
	}
	if q.Strategy == "spot-perpetual" && (b.Instrument.Market != "spot" || s.Instrument.Market != "perpetual") || q.Strategy == "spot-spot" && (b.Instrument.Market != "spot" || s.Instrument.Market != "spot") || q.Strategy == "perpetual-perpetual" && (b.Instrument.Market != "perpetual" || s.Instrument.Market != "perpetual") {
		return Opportunity{}, false
	}
	if q.Simple {
		// Show prices that can actually be matched at the best bid/ask, with
		// the same rounded base quantity on both venues. No hidden budget.
		bp, sp := b.Asks[0].Price, s.Bids[0].Price
		qty := commonQuantity(math.Min(b.Asks[0].Quantity, s.Bids[0].Quantity), b.Instrument.Step, s.Instrument.Step)
		if sp <= bp || qty <= 0 || qty < b.Instrument.MinQuantity || qty < s.Instrument.MinQuantity || qty*bp < b.Instrument.MinNotional || qty*sp < s.Instrument.MinNotional {
			return Opportunity{}, false
		}
		spread := (sp/bp - 1) * 100
		o := Opportunity{ID: RouteID(b.Instrument.ID, s.Instrument.ID), Asset: b.Instrument.Asset, Strategy: q.Strategy, Quantity: qty, EntrySpreadPct: spread, QuoteSpreadPct: spread, DepthUSDT: qty * bp, AgeMS: now.UnixMilli() - min(b.QuoteAt(), s.QuoteAt()), CalculatedAt: now.UnixMilli()}
		o.Buy = Leg{Instrument: b.Instrument, Price: bp, Notional: qty * bp, BookAt: b.QuoteAt(), Bids: b.Bids, Asks: b.Asks}
		o.Sell = Leg{Instrument: s.Instrument, Price: sp, Notional: qty * sp, BookAt: s.QuoteAt(), Bids: s.Bids, Asks: s.Asks}
		return o, true
	}
	qty := commonQuantity(q.Amount/b.Asks[0].Price, b.Instrument.Step, s.Instrument.Step)
	bp, ok := market.VWAP(b.Asks, qty)
	if !ok {
		return Opportunity{}, false
	}
	// VWAP raises the buy price: round down again so amount remains a budget.
	if bp*qty > q.Amount+1e-7 {
		qty = commonQuantity(q.Amount/bp, b.Instrument.Step, s.Instrument.Step)
		bp, ok = market.VWAP(b.Asks, qty)
		if !ok {
			return Opportunity{}, false
		}
	}
	sp, ok := market.VWAP(s.Bids, qty)
	if !ok || qty < b.Instrument.MinQuantity || qty < s.Instrument.MinQuantity || qty*bp < b.Instrument.MinNotional || qty*sp < s.Instrument.MinNotional {
		return Opportunity{}, false
	}
	spread := (sp/bp - 1) * 100
	if spread < q.MinSpread {
		return Opportunity{}, false
	}
	bf, sf := fee(b.Instrument, q), fee(s.Instrument, q)
	entryFees := qty * (bp*bf + sp*sf)
	basis := qty * (sp - bp)
	o := Opportunity{ID: RouteID(b.Instrument.ID, s.Instrument.ID), Asset: b.Instrument.Asset, Strategy: q.Strategy, Quantity: qty, EntrySpreadPct: spread, QuoteSpreadPct: (s.Bids[0].Price/b.Asks[0].Price - 1) * 100, EntryFees: entryFees, NetEntryPct: (basis - entryFees) / (qty * bp) * 100, DepthUSDT: math.Min(market.Depth(b.Asks), market.Depth(s.Bids)), AgeMS: now.UnixMilli() - min(b.ReceivedAt, s.ReceivedAt), CalculatedAt: now.UnixMilli()}
	o.Buy = Leg{b.Instrument, bp, qty * bp, qty * bp * bf, b.Funding, b.QuoteAt(), b.Bids, b.Asks}
	o.HorizonHours = q.HorizonHours
	o.ExitSpreadPct = q.ExitSpread
	o.Sell = Leg{s.Instrument, sp, qty * sp, qty * sp * sf, s.Funding, s.QuoteAt(), s.Bids, s.Asks}
	o.AgeMS = now.UnixMilli() - min(b.QuoteAt(), s.QuoteAt())
	if q.Strategy == "spot-spot" {
		pnl := basis - entryFees
		o.SpotCyclePnL = &pnl
		return o, true
	}
	// Exit fees assume unchanged buy-leg price; exit spread is explicit user scenario.
	o.ExitFeesEstimate = qty * bp * (bf + (1+q.ExitSpread/100)*sf)
	buyFund, bn, bok := 0.0, 0, true
	if b.Instrument.Market == "perpetual" {
		buyFund, bn, bok = funding(b.Funding, now, q.HorizonHours)
	}
	sellFund, sn, sok := funding(s.Funding, now, q.HorizonHours)
	if bok && sok {
		f := qty * (sp*sellFund - bp*buyFund)
		o.FundingEstimate = &f
		o.FundingEvents = bn + sn
		pnl := basis - qty*bp*q.ExitSpread/100 - entryFees - o.ExitFeesEstimate + f
		o.ScenarioPnL = &pnl
	}
	return o, true
}
func Scan(books []market.Book, q Query, now time.Time) []Opportunity {
	exchangeSet := map[string]bool{}
	for _, e := range q.Exchanges {
		exchangeSet[e] = true
	}
	assets := map[string]bool{}
	for _, a := range q.Assets {
		assets[strings.ToUpper(a)] = true
	}
	groups := map[string][]market.Book{}
	for _, b := range books {
		if exchangeSet[b.Instrument.Exchange] && (len(assets) == 0 || assets[b.Instrument.Asset]) && strings.Contains(b.Instrument.Asset, strings.ToUpper(q.Search)) && b.Fresh(now, time.Duration(q.MaxAgeMS)*time.Millisecond) {
			groups[b.Instrument.Identity()] = append(groups[b.Instrument.Identity()], b)
		}
	}
	out := make([]Opportunity, 0)
	routes := map[string]bool{}
	for _, id := range q.Routes {
		routes[id] = true
	}
	for _, group := range groups {
		for _, b := range group {
			for _, s := range group {
				if len(routes) > 0 && !routes[RouteID(b.Instrument.ID, s.Instrument.ID)] {
					continue
				}
				if o, ok := Calculate(b, s, q, now); ok {
					out = append(out, o)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if q.Sort == "net" && a.NetEntryPct != b.NetEntryPct {
			return a.NetEntryPct > b.NetEntryPct
		}
		if q.Sort == "funding" {
			af, bf := -math.MaxFloat64, -math.MaxFloat64
			if a.FundingEstimate != nil {
				af = *a.FundingEstimate
			}
			if b.FundingEstimate != nil {
				bf = *b.FundingEstimate
			}
			if af != bf {
				return af > bf
			}
		}
		if a.EntrySpreadPct == b.EntrySpreadPct {
			return a.ID < b.ID
		}
		return a.EntrySpreadPct > b.EntrySpreadPct
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out
}
func Parse(raw map[string][]string) (Query, error) {
	q := Defaults()
	get := func(k string) string {
		v := raw[k]
		if len(v) == 0 {
			return ""
		}
		return v[0]
	}
	var err error
	for key, target := range map[string]*float64{"amount": &q.Amount, "minSpread": &q.MinSpread, "exitSpread": &q.ExitSpread} {
		if v := get(key); v != "" {
			*target, err = strconv.ParseFloat(v, 64)
			if err != nil {
				return q, errors.New("Некорректное число: " + key)
			}
		}
	}
	for key, target := range map[string]*int{"horizonHours": &q.HorizonHours, "limit": &q.Limit, "offset": &q.Offset} {
		if v := get(key); v != "" {
			*target, err = strconv.Atoi(v)
			if err != nil {
				return q, errors.New("Некорректное число: " + key)
			}
		}
	}
	if v := get("maxAgeMs"); v != "" {
		q.MaxAgeMS, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return q, errors.New("Некорректный возраст данных")
		}
	}
	if v := get("strategy"); v != "" {
		q.Strategy = v
	}
	q.Search = strings.ToUpper(strings.TrimSpace(get("search")))
	if v := get("routes"); v != "" {
		q.Routes = strings.Split(v, ",")
	}
	if v := get("simple"); v != "" {
		q.Simple, err = strconv.ParseBool(v)
		if err != nil {
			return q, errors.New("Некорректный режим расчёта")
		}
	}
	if v := get("sort"); v != "" {
		q.Sort = v
	}
	if v := get("exchanges"); v != "" {
		q.Exchanges = strings.Split(v, ",")
	}
	if v := get("assets"); v != "" {
		q.Assets = strings.FieldsFunc(strings.ToUpper(v), func(r rune) bool { return r == ',' || r == ' ' })
	}
	if v := get("interExchange"); v != "" {
		q.InterExchange, err = strconv.ParseBool(v)
		if err != nil {
			return q, errors.New("Некорректное направление")
		}
	}
	for k, p := range map[string]**float64{"feeSpot": &q.FeeSpot, "feePerpetual": &q.FeePerpetual} {
		if v := get(k); v != "" {
			n, e := strconv.ParseFloat(v, 64)
			if e != nil {
				return q, e
			}
			*p = &n
		}
	}
	return q, q.Validate()
}
