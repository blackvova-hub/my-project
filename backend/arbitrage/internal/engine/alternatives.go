package engine

import (
	"math"
	"shortlong/arbitrage/internal/market"
	"sort"
	"strings"
	"time"
)

func usdFresh(q market.USDQuote, now time.Time) bool {
	return market.Finite(q.Bid) && market.Finite(q.Ask) && q.Bid > 0 && q.Ask >= q.Bid && now.UnixMilli()-q.ObservedAt <= 30000 && q.ObservedAt <= now.UnixMilli()+2000
}
func ScanAlternatives(books []market.Book, q Query, a market.AlternativeData, now time.Time) []Opportunity {
	out := []Opportunity{}
	if !usdFresh(a.USDTUSD, now) {
		return out
	}
	venues := map[string]bool{}
	routes := map[string]bool{}
	assets := map[string]bool{}
	for _, e := range q.Exchanges {
		venues[e] = true
	}
	for _, id := range q.Routes {
		routes[id] = true
	}
	for _, asset := range q.Assets {
		assets[asset] = true
	}
	add := func(o Opportunity, ok bool) {
		if ok && o.DepthUSDT+1e-9 >= q.MinAvailable && (len(routes) == 0 || routes[o.ID]) {
			out = append(out, o)
		}
	}
	for _, b := range books {
		if !venues[b.Instrument.Exchange] || !strings.Contains(b.Instrument.Asset, strings.ToUpper(q.Search)) || (len(assets) > 0 && !assets[b.Instrument.Asset]) || !b.Fresh(now, time.Duration(q.MaxAgeMS)*time.Millisecond) {
			continue
		}
		if q.Strategy == "redemption" && b.Instrument.Market == "spot" {
			add(redemption(b, q, a, now))
		}
		if q.Strategy == "hedged-lp" && b.Instrument.Market == "perpetual" && usdFresh(a.USDCUSD, now) {
			for _, pool := range a.Pools {
				if (q.Network == "" || q.Network == "all" || pool.Network == q.Network) && pool.Asset == b.Instrument.Identity() {
					add(hedgedLP(pool, b, q, a, now))
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Metrics.ValuePct == out[j].Metrics.ValuePct {
			return out[i].ID < out[j].ID
		}
		return out[i].Metrics.ValuePct > out[j].Metrics.ValuePct
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out
}
func redemption(b market.Book, q Query, a market.AlternativeData, now time.Time) (Opportunity, bool) {
	issuer, url, condition := "", "", ""
	switch b.Instrument.Identity() {
	case "USDC":
		issuer = "Circle"
		url = "https://www.circle.com/circle-mint"
		condition = "Погашение требует доступного Circle Mint/применимого права выкупа, разрешённой сети и банковского маршрута. Доступ, комиссия и срок не подтверждены."
	case "USDE":
		issuer = "Ethena"
		url = "https://docs.ethena.fi/technical-design/minting-usde"
		condition = "Погашение USDe требует whitelisted KYC/KYB-контрагента и актуального redemption quote. $1 — номинальный ориентир, а не подтверждённая выплата."
	default:
		return Opportunity{}, false
	}
	qty, covered := BudgetQuantity(b.Asks, q.Amount)
	qty = commonQuantity(qty, b.Instrument.Step, b.Instrument.Step)
	if qty <= 0 {
		return Opportunity{}, false
	}
	bp, ok := market.VWAP(b.Asks, qty)
	if !ok {
		return Opportunity{}, false
	}
	cost := qty * bp * fee(b.Instrument, q)
	cashUSDT := qty / a.USDTUSD.Ask
	gross := cashUSDT - qty*bp
	discount := (1 - bp*a.USDTUSD.Ask) * 100
	status := "redemption-conditional"
	if !covered {
		status = "depth-insufficient"
	}
	full := covered
	if qty < b.Instrument.MinQuantity || qty*bp < b.Instrument.MinNotional {
		full = false
		status = "below-minimum"
	}
	i := market.Instrument{ID: "redemption:" + b.Instrument.Asset, Exchange: issuer, Market: "redemption", Native: b.Instrument.Asset + " / USD", Asset: b.Instrument.Asset, TradeURL: url, AssetIdentity: b.Instrument.Identity()}
	ex := &Execution{RequestedUSDT: q.Amount, FilledUSDT: qty * bp, CapacityUSDT: market.Depth(b.Asks), FillPct: math.Min(100, qty*bp/q.Amount*100), Full: full, Status: status, BuyLevels: usedLevels(b.Asks, qty), BuySlippagePct: (bp/b.Asks[0].Price - 1) * 100, DepthAt: b.QuoteAt()}
	return Opportunity{ID: RouteID("redemption:"+b.Instrument.ID, i.ID), Asset: b.Instrument.Asset, Strategy: "redemption", Buy: leg(b, bp, qty), Sell: Leg{Instrument: i, Price: 1 / a.USDTUSD.Ask, Notional: cashUSDT, BookAt: a.USDTUSD.ObservedAt}, Quantity: qty, Execution: ex, EntrySpreadPct: discount, DepthUSDT: ex.CapacityUSDT, CalculatedAt: now.UnixMilli(), Metrics: &Metrics{Kind: "redemption", ValuePct: discount, CashUSDT: gross - cost, GrossUSDT: gross, CostsUSDT: cost, ObservedAt: a.USDTUSD.ObservedAt, ReferenceUSD: 1, DiscountPct: discount, Eligibility: "unverified", Condition: condition + " Расходы на погашение/сеть/банк не включены; USD-якорь — Kraken USDT/USD ask.", URL: url}}, true
}

// Normalized v3 position inventory. Bounds are rounded outward to actual fee-tier ticks.
func LPInventory(pool market.LPPool, capitalUSDC, rangePct float64) (qty, stable, lower, upper float64, ok bool) {
	p := pool.PriceUSDC
	if !market.Finite(p) || p <= 0 || !market.Finite(capitalUSDC) || capitalUSDC <= 0 || !market.Finite(rangePct) || rangePct <= 0 || rangePct > 80 || pool.TickSpacing <= 0 {
		return
	}
	// USDC token0 on Ethereum; the native tick then increases as the base-asset price falls.
	base0 := pool.Token0 != "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48" && pool.Token0 != "0xaf88d065e77c8cc2239327c5edb3a432268e5831"
	lo, hi := math.Log(1-rangePct/100)/math.Log(1.0001), math.Log(1+rangePct/100)/math.Log(1.0001)
	if !base0 {
		lo, hi = -hi, -lo
	}
	spacing := float64(pool.TickSpacing)
	lt := math.Floor((float64(pool.Tick)+lo)/spacing) * spacing
	ut := math.Ceil((float64(pool.Tick)+hi)/spacing) * spacing
	if lt < -887272 || ut > 887272 {
		return
	}
	anchor := pool.TickPriceUSDC
	if !market.Finite(anchor) || anchor <= 0 {
		return
	}
	lower = anchor * math.Pow(1.0001, lt-float64(pool.Tick))
	upper = anchor * math.Pow(1.0001, ut-float64(pool.Tick))
	if !base0 {
		lower, upper = anchor*math.Pow(1.0001, float64(pool.Tick)-ut), anchor*math.Pow(1.0001, float64(pool.Tick)-lt)
	}
	root := math.Sqrt(p)
	x := 1/root - 1/math.Sqrt(upper)
	y := root - math.Sqrt(lower)
	liquidity := capitalUSDC / (x*p + y)
	qty, stable = x*liquidity, y*liquidity
	ok = market.Finite(qty) && market.Finite(stable) && qty > 0 && stable > 0
	return
}
func hedgedLP(pool market.LPPool, hedge market.Book, q Query, a market.AlternativeData, now time.Time) (Opportunity, bool) {
	if now.UnixMilli()-pool.ObservedAt > 60000 || pool.ObservedAt > now.UnixMilli()+2000 || now.UnixMilli()-pool.YieldAt > 10*60000 || pool.YieldAt > now.UnixMilli()+2000 || pool.Address == "" || pool.TVLUSD <= 0 || !market.Finite(pool.HistoricalAPY) {
		return Opportunity{}, false
	}
	usdt := (a.USDTUSD.Bid + a.USDTUSD.Ask) / 2
	usdc := (a.USDCUSD.Bid + a.USDCUSD.Ask) / 2
	rangePct := q.LPRangePct
	if rangePct == 0 {
		rangePct = 10
	}
	exposure, _, lower, upper, ok := LPInventory(pool, q.Amount*usdt/usdc, rangePct)
	if !ok {
		return Opportunity{}, false
	}
	desired := commonQuantity(exposure, hedge.Instrument.Step, hedge.Instrument.Step)
	qty := commonQuantity(math.Min(desired, totalQuantity(hedge.Bids)), hedge.Instrument.Step, hedge.Instrument.Step)
	if qty <= 0 {
		return Opportunity{}, false
	}
	sp, ok := market.VWAP(hedge.Bids, qty)
	if !ok {
		return Opportunity{}, false
	}
	rate, events, known := funding(hedge.Funding, now, q.HorizonHours)
	if !known {
		return Opportunity{}, false
	}
	status := "lp-conditional"
	full := qty+1e-10 >= desired
	if !full {
		status = "depth-insufficient"
	}
	if qty < hedge.Instrument.MinQuantity || qty*sp < hedge.Instrument.MinNotional {
		full = false
		status = "below-minimum"
	}
	if q.Amount*usdt > pool.TVLUSD*.01 {
		full = false
		status = "pool-size-limit"
	}
	i := market.Instrument{ID: "lp:" + pool.ID, Exchange: "uniswap", Market: "lp", Native: pool.Symbol, Asset: pool.Asset, AssetIdentity: pool.Asset, TradeURL: "https://app.uniswap.org/explore/pools/" + strings.ToLower(pool.Network) + "/" + pool.Address}
	cash := qty * sp * rate
	costs := 2 * qty * sp * fee(hedge.Instrument, q)
	capacity := q.Amount * totalQuantity(hedge.Bids) / exposure
	capacity = math.Min(capacity, pool.TVLUSD*.01/usdt)
	ex := &Execution{RequestedUSDT: q.Amount, FilledUSDT: q.Amount * math.Min(1, qty/desired), CapacityUSDT: capacity, FillPct: math.Min(100, qty/desired*100), Full: full, Status: status, SellLevels: usedLevels(hedge.Bids, qty), DepthAt: hedge.QuoteAt()}
	return Opportunity{ID: RouteID(i.ID, hedge.Instrument.ID), Asset: pool.Asset, Strategy: "hedged-lp", DepthUSDT: capacity, Buy: Leg{Instrument: i, Price: pool.PriceUSDC * usdc / usdt, Notional: q.Amount, BookAt: pool.ObservedAt}, Sell: leg(hedge, sp, qty), Quantity: qty, Execution: ex, CalculatedAt: now.UnixMilli(), Metrics: &Metrics{Kind: "hedged-lp", ValuePct: pool.HistoricalAPY, HistoricalAPY: pool.HistoricalAPY, FundingUSDT: cash, CostsUSDT: costs, HorizonHours: q.HorizonHours, FundingEvents: events, NextAt: hedge.Funding.NextAt, Network: pool.Network, Pool: pool.ID, PoolAddress: pool.Address, TVLUSD: pool.TVLUSD, LPRangePct: rangePct, PriceLower: lower, PriceUpper: upper, HedgeQuantity: qty, ResidualQuantity: exposure - qty, ObservedAt: pool.ObservedAt, URL: i.TradeURL, Condition: "APY — историческая комиссия всего пула, не доход вашей позиции. Хедж — текущая delta v3; нужны отдельная маржа и перебалансировка. Газ, IL/LVR и basis risk не включены."}}, true
}
