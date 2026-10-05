package analytics

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type exchangeClient struct {
	exchange    string
	credentials Credentials
	client      *http.Client
}

func newExchange(exchange string, c Credentials) *exchangeClient {
	return &exchangeClient{exchange, c, &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func signature(secret, payload string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}
func (x *exchangeClient) get(ctx context.Context, host, path string, q url.Values, private bool, out any) error {
	if q == nil {
		q = url.Values{}
	}
	// Fixed exchange origins only: user input cannot direct credential-bearing requests.
	base := "https://api.bybit.com"
	if x.exchange == "binance" {
		base = "https://fapi.binance.com"
		if host == "spot" {
			base = "https://api.binance.com"
		}
	}
	stamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if private && x.exchange == "binance" {
		q.Set("timestamp", stamp)
		q.Set("recvWindow", "10000")
		q.Set("signature", signature(x.credentials.Secret, q.Encode()))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	if private {
		if x.exchange == "bybit" {
			req.Header.Set("X-BAPI-API-KEY", x.credentials.Key)
			req.Header.Set("X-BAPI-TIMESTAMP", stamp)
			req.Header.Set("X-BAPI-RECV-WINDOW", "10000")
			req.Header.Set("X-BAPI-SIGN", signature(x.credentials.Secret, stamp+x.credentials.Key+"10000"+q.Encode()))
		} else {
			req.Header.Set("X-MBX-APIKEY", x.credentials.Key)
		}
	}
	// Bound throughput per connection and allow cancellation during throttling.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(120 * time.Millisecond):
	}
	res, err := x.client.Do(req)
	if err != nil {
		return errors.New("Exchange is unavailable. Try again later.")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("Exchange rejected request (HTTP %d). Check key permissions, IP allowlist and region.", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if x.exchange == "bybit" {
		var envelope struct {
			Code   int             `json:"retCode"`
			Result json.RawMessage `json:"result"`
		}
		if err = json.Unmarshal(b, &envelope); err != nil {
			return err
		}
		if envelope.Code != 0 {
			return fmt.Errorf("Bybit error %d. Check permissions, IP allowlist and account type.", envelope.Code)
		}
		return json.Unmarshal(envelope.Result, out)
	}
	return json.Unmarshal(b, out)
}
func (x *exchangeClient) validate(ctx context.Context) error {
	if x.exchange == "bybit" {
		var r struct {
			ReadOnly *int `json:"readOnly"`
		}
		if err := x.get(ctx, "", "/v5/user/query-api", nil, true, &r); err != nil {
			return err
		}
		if r.ReadOnly == nil || *r.ReadOnly != 1 {
			return errors.New("Only read-only API keys are accepted. Disable trading and withdrawal permissions.")
		}
		return nil
	}
	var flags map[string]any
	if err := x.get(ctx, "spot", "/sapi/v1/account/apiRestrictions", nil, true, &flags); err != nil {
		return err
	}
	if flags["enableReading"] != true {
		return errors.New("Enable read access for this API key.")
	}
	for _, key := range []string{"enableWithdrawals", "enableInternalTransfer", "permitsUniversalTransfer", "enableMargin", "enableFutures", "enableVanillaOptions", "enableFixApiTrade", "enableSpotAndMarginTrading", "enablePortfolioMarginTrading"} {
		if flags[key] == true {
			return errors.New("Only read-only API keys are accepted. Disable trading, transfer and withdrawal permissions.")
		}
	}
	return nil
}

type sourceEvent struct {
	ID, Kind, Symbol, Currency, Amount string
	At                                 int64
	Normalized                         any
	Raw                                any
}
type syncResult struct {
	Events   []sourceEvent
	Snapshot Snapshot
	Warnings []string
}
type record map[string]json.RawMessage

func str(r record, k string) string {
	var s string
	if json.Unmarshal(r[k], &s) == nil {
		return s
	}
	return string(r[k])
}
func num(r record, k string) float64  { v, _ := strconv.ParseFloat(str(r, k), 64); return v }
func boolean(r record, k string) bool { return string(r[k]) == "true" }
func optional(r record, k string) *float64 {
	if str(r, k) == "" || str(r, k) == "null" {
		return nil
	}
	v := num(r, k)
	return &v
}
func records(r record, k string) []record {
	var out []record
	_ = json.Unmarshal(r[k], &out)
	return out
}
func decimal(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
func stable(currency string) bool {
	return currency == "USDT" || currency == "USDC" || currency == "USD"
}
func ledgerEvent(c Connection, r record, id, category, symbol, currency, amount string, at int64) sourceEvent {
	v, _ := strconv.ParseFloat(amount, 64)
	return sourceEvent{ID: id, Kind: "ledger", Symbol: symbol, Currency: currency, Amount: amount, At: at, Normalized: Ledger{ID: id, ConnectionID: c.ID, Exchange: c.Exchange, At: at, Category: category, Symbol: symbol, Currency: currency, Amount: v, AmountExact: amount}, Raw: r}
}
func (x *exchangeClient) bybitPages(ctx context.Context, path string, q url.Values) ([]record, error) {
	out := []record{}
	seen := map[string]bool{}
	for page := 0; page < 1000; page++ {
		var r record
		if err := x.get(ctx, "", path, q, true, &r); err != nil {
			return nil, err
		}
		out = append(out, records(r, "list")...)
		cursor := str(r, "nextPageCursor")
		if cursor == "" {
			return out, nil
		}
		if seen[cursor] {
			return nil, errors.New("Exchange returned a repeated pagination cursor.")
		}
		seen[cursor] = true
		q.Set("cursor", cursor)
	}
	return nil, errors.New("History page limit reached; coverage was not advanced.")
}
func (x *exchangeClient) sync(ctx context.Context, c Connection, from, to time.Time) (syncResult, error) {
	if x.exchange == "bybit" {
		return x.syncBybit(ctx, c, from, to)
	}
	return x.syncBinance(ctx, c, from, to)
}
func (x *exchangeClient) syncBybit(ctx context.Context, c Connection, from, to time.Time) (syncResult, error) {
	result := syncResult{Events: []sourceEvent{}, Warnings: []string{"Trade analytics cover USDT linear contracts. Other wallet currencies remain visible in the source ledger; their historical USD conversion is unavailable.", "Historical leverage, stop-loss and take-profit are unavailable unless recorded at entry."}}
	for start := from; start.Before(to); {
		end := start.Add(7*24*time.Hour - time.Millisecond)
		if end.After(to) {
			end = to
		}
		q := url.Values{"startTime": {strconv.FormatInt(start.UnixMilli(), 10)}, "endTime": {strconv.FormatInt(end.UnixMilli(), 10)}, "limit": {"100"}, "category": {"linear"}}
		rows, err := x.bybitPages(ctx, "/v5/execution/list", q)
		if err != nil {
			return result, err
		}
		for _, r := range rows {
			// Forced closures are executions too. Dropping them leaves historical
			// positions open and corrupts the reconstructed PnL and holding time.
			switch str(r, "execType") {
			case "Trade", "AdlTrade", "BustTrade", "Delivery", "BlockTrade", "FutureSpread":
			default:
				continue
			}
			if !strings.HasSuffix(str(r, "symbol"), "USDT") {
				continue
			}
			feeCurrency := str(r, "feeCurrency")
			if feeCurrency == "" {
				feeCurrency = "USDT"
			}
			f := Fill{ID: str(r, "execId"), ConnectionID: c.ID, Exchange: c.Exchange, Symbol: str(r, "symbol"), Market: "linear", Side: strings.ToUpper(str(r, "side")), At: int64(num(r, "execTime")), Price: num(r, "execPrice"), Quantity: num(r, "execQty"), Fee: num(r, "execFee"), FeeCurrency: feeCurrency, FeeKnown: stable(feeCurrency), Maker: boolean(r, "isMaker"), ClosedQuantity: optional(r, "closedSize")}
			// closedSize identifies the inventory lane even in Bybit hedge mode.
			f.PositionSide = "LONG"
			if (f.Side == "SELL" && (f.ClosedQuantity == nil || *f.ClosedQuantity == 0)) || (f.Side == "BUY" && f.ClosedQuantity != nil && *f.ClosedQuantity > 0) {
				f.PositionSide = "SHORT"
			}
			for _, part := range splitBybitReversal(f) {
				result.Events = append(result.Events, sourceEvent{ID: part.ID, Kind: "fill", At: part.At, Symbol: part.Symbol, Currency: feeCurrency, Amount: "0", Normalized: part, Raw: r})
			}
		}
		q.Del("cursor")
		q.Del("category")
		q.Set("accountType", "UNIFIED")
		q.Set("limit", "50")
		logs, err := x.bybitPages(ctx, "/v5/account/transaction-log", q)
		if err != nil {
			return result, err
		}
		for _, r := range logs {
			at := int64(num(r, "transactionTime"))
			id := str(r, "id")
			currency := str(r, "currency")
			symbol := str(r, "symbol")
			typ := str(r, "type")
			category := "other"
			switch typ {
			case "TRADE", "SETTLEMENT", "LIQUIDATION", "ADL", "DELIVERY":
				category = "trading"
				if str(r, "category") == "spot" {
					category = "asset_exchange"
				}
			case "TRANSFER_IN", "TRANSFER_OUT":
				category = "transfer"
			case "DEPOSIT":
				category = "deposit"
			case "WITHDRAW":
				category = "withdrawal"
			case "INTEREST":
				category = "interest"
			case "BONUS", "CASHBACK", "AIRDROP", "EARN":
				category = "rewards"
			}
			for _, part := range []struct {
				k, cat   string
				negative bool
			}{{"cashFlow", category, false}, {"fee", "fees", true}, {"funding", "funding", false}} {
				amount := str(r, part.k)
				if amount == "" || num(r, part.k) == 0 {
					continue
				}
				if part.negative {
					if strings.HasPrefix(amount, "-") {
						amount = strings.TrimPrefix(amount, "-")
					} else {
						amount = "-" + amount
					}
				}
				result.Events = append(result.Events, ledgerEvent(c, r, id+":"+part.k, part.cat, symbol, currency, amount, at))
			}
		}
		start = end.Add(time.Millisecond)
	}
	var wallet record
	if err := x.get(ctx, "", "/v5/account/wallet-balance", url.Values{"accountType": {"UNIFIED"}}, true, &wallet); err != nil {
		return result, err
	}
	accounts := records(wallet, "list")
	if len(accounts) != 1 {
		return result, errors.New("Unified account balance unavailable.")
	}
	w := accounts[0]
	s := Snapshot{ConnectionID: c.ID, At: time.Now().UnixMilli(), Equity: num(w, "totalEquity"), Wallet: num(w, "totalWalletBalance"), Unrealized: num(w, "totalPerpUPL"), Margin: num(w, "totalInitialMargin"), Assets: []Asset{}, Positions: []Position{}}
	for _, r := range records(w, "coin") {
		s.Assets = append(s.Assets, Asset{str(r, "coin"), num(r, "usdValue")})
	}
	positions, err := x.bybitPages(ctx, "/v5/position/list", url.Values{"category": {"linear"}, "settleCoin": {"USDT"}, "limit": {"200"}})
	if err != nil {
		return result, err
	}
	for _, r := range positions {
		if num(r, "size") == 0 {
			continue
		}
		side := "LONG"
		if str(r, "side") == "Sell" {
			side = "SHORT"
		}
		s.Positions = append(s.Positions, Position{Symbol: str(r, "symbol"), Side: side, Quantity: num(r, "size"), Entry: num(r, "avgPrice"), Mark: num(r, "markPrice"), Notional: num(r, "positionValue"), Unrealized: num(r, "unrealisedPnl"), Leverage: optional(r, "leverage"), StopLoss: optional(r, "stopLoss"), TakeProfit: optional(r, "takeProfit"), Liquidation: optional(r, "liqPrice")})
	}
	var ticker record
	if x.get(ctx, "", "/v5/market/tickers", url.Values{"category": {"linear"}, "symbol": {"BTCUSDT"}}, false, &ticker) == nil {
		rows := records(ticker, "list")
		if len(rows) > 0 {
			v := num(rows[0], "lastPrice")
			s.BTCPrice = &v
		}
	}
	result.Snapshot = s
	return result, nil
}
func (x *exchangeClient) syncBinance(ctx context.Context, c Connection, from, to time.Time) (syncResult, error) {
	result := syncResult{Events: []sourceEvent{}, Warnings: []string{"Coverage: Binance USD-M futures. Spot, options and coin-margined accounts are not included.", "Historical leverage, stop-loss and take-profit are not returned by the trade-history API."}}
	symbols := map[string]bool{}
	for start := from; start.Before(to); {
		end := start.Add(7*24*time.Hour - time.Millisecond)
		if end.After(to) {
			end = to
		}
		for page := 1; page <= 1000; page++ {
			var rows []record
			q := url.Values{"startTime": {strconv.FormatInt(start.UnixMilli(), 10)}, "endTime": {strconv.FormatInt(end.UnixMilli(), 10)}, "limit": {"1000"}, "page": {strconv.Itoa(page)}}
			if err := x.get(ctx, "", "/fapi/v1/income", q, true, &rows); err != nil {
				return result, err
			}
			for _, r := range rows {
				typ := str(r, "incomeType")
				symbol := str(r, "symbol")
				if symbol != "" {
					symbols[symbol] = true
				}
				cat := "other"
				switch typ {
				case "REALIZED_PNL":
					cat = "trading"
				case "COMMISSION":
					cat = "fees"
				case "FUNDING_FEE":
					cat = "funding"
				case "TRANSFER", "INTERNAL_TRANSFER":
					cat = "transfer"
				case "WELCOME_BONUS", "COMMISSION_REBATE", "API_REBATE", "REFERRAL_KICKBACK":
					cat = "rewards"
				case "INSURANCE_CLEAR":
					cat = "other"
				}
				result.Events = append(result.Events, ledgerEvent(c, r, typ+":"+str(r, "tranId")+":"+str(r, "asset"), cat, symbol, str(r, "asset"), str(r, "income"), int64(num(r, "time"))))
			}
			if len(rows) < 1000 {
				break
			}
			if page == 1000 {
				return result, errors.New("Income history page limit reached.")
			}
		}
		start = end.Add(time.Millisecond)
	}
	var w record
	if err := x.get(ctx, "", "/fapi/v3/account", nil, true, &w); err != nil {
		return result, err
	}
	s := Snapshot{ConnectionID: c.ID, At: time.Now().UnixMilli(), Equity: num(w, "totalMarginBalance"), Wallet: num(w, "totalWalletBalance"), Unrealized: num(w, "totalUnrealizedProfit"), Margin: num(w, "totalInitialMargin"), Assets: []Asset{}, Positions: []Position{}}
	for _, r := range records(w, "assets") {
		if stable(str(r, "asset")) {
			s.Assets = append(s.Assets, Asset{str(r, "asset"), num(r, "marginBalance")})
		} else if num(r, "walletBalance") != 0 {
			result.Warnings = append(result.Warnings, "Non-stable collateral requires currency valuation; USD reconciliation is incomplete.")
		}
	}
	var positions []record
	if err := x.get(ctx, "", "/fapi/v3/positionRisk", nil, true, &positions); err != nil {
		return result, err
	}
	for _, r := range positions {
		amount := num(r, "positionAmt")
		if amount == 0 {
			continue
		}
		symbols[str(r, "symbol")] = true
		side := "LONG"
		if amount < 0 {
			side = "SHORT"
			amount = -amount
		}
		notional := num(r, "notional")
		if notional < 0 {
			notional = -notional
		}
		s.Positions = append(s.Positions, Position{Symbol: str(r, "symbol"), Side: side, Quantity: amount, Entry: num(r, "entryPrice"), Mark: num(r, "markPrice"), Notional: notional, Unrealized: num(r, "unRealizedProfit"), Liquidation: optional(r, "liquidationPrice")})
	}
	for symbol := range symbols {
		if !strings.HasSuffix(symbol, "USDT") {
			result.Warnings = append(result.Warnings, "Non-USDT contracts are retained in the ledger but excluded from trade reconstruction.")
			continue
		}
		for start := from; start.Before(to); {
			end := start.Add(7*24*time.Hour - time.Millisecond)
			if end.After(to) {
				end = to
			}
			q := url.Values{"symbol": {symbol}, "startTime": {strconv.FormatInt(start.UnixMilli(), 10)}, "endTime": {strconv.FormatInt(end.UnixMilli(), 10)}, "limit": {"1000"}}
			for page := 0; page < 1000; page++ {
				var rows []record
				if err := x.get(ctx, "", "/fapi/v1/userTrades", q, true, &rows); err != nil {
					return result, err
				}
				for _, r := range rows {
					f := Fill{ID: symbol + ":" + str(r, "id"), ConnectionID: c.ID, Exchange: c.Exchange, Symbol: symbol, Market: "linear", Side: str(r, "side"), PositionSide: str(r, "positionSide"), At: int64(num(r, "time")), Price: num(r, "price"), Quantity: num(r, "qty"), Fee: num(r, "commission"), FeeCurrency: str(r, "commissionAsset"), FeeKnown: stable(str(r, "commissionAsset")), Maker: boolean(r, "maker")}
					if f.PositionSide == "BOTH" && num(r, "realizedPnl") != 0 {
						v := f.Quantity
						f.ClosedQuantity = &v
					}
					result.Events = append(result.Events, sourceEvent{ID: f.ID, Kind: "fill", At: f.At, Symbol: symbol, Currency: f.FeeCurrency, Amount: "0", Normalized: f, Raw: r})
				}
				if len(rows) < 1000 {
					break
				}
				// fromId and start/endTime cannot be combined. Paginate by time with
				// overlap; fail closed when >1000 fills share the boundary millisecond.
				last := int64(num(rows[len(rows)-1], "time"))
				prev, _ := strconv.ParseInt(q.Get("startTime"), 10, 64)
				if last <= prev {
					return result, errors.New("Too many executions at one timestamp; import coverage is incomplete.")
				}
				q.Set("startTime", strconv.FormatInt(last, 10))
				if page == 999 {
					return result, errors.New("Trade history page limit reached.")
				}
			}
			start = end.Add(time.Millisecond)
		}
	}
	var ticker record
	if x.get(ctx, "", "/fapi/v1/ticker/price", url.Values{"symbol": {"BTCUSDT"}}, false, &ticker) == nil {
		v := num(ticker, "price")
		s.BTCPrice = &v
	}
	result.Snapshot = s
	return result, nil
}

func (x *exchangeClient) candles(ctx context.Context, symbol string, from, to int64) ([]Candle, error) {
	out := []Candle{}
	for start := from / 60000 * 60000; start <= to; {
		end := start + 999*60000
		if end > to {
			end = to
		}
		q := url.Values{"symbol": {symbol}, "limit": {"1000"}}
		var rows [][]json.RawMessage
		if x.exchange == "bybit" {
			q.Set("category", "linear")
			q.Set("interval", "1")
			q.Set("start", strconv.FormatInt(start, 10))
			q.Set("end", strconv.FormatInt(end, 10))
			var data struct {
				List [][]json.RawMessage `json:"list"`
			}
			if err := x.get(ctx, "", "/v5/market/mark-price-kline", q, false, &data); err != nil {
				return nil, err
			}
			rows = data.List
		} else {
			q.Set("interval", "1m")
			q.Set("startTime", strconv.FormatInt(start, 10))
			q.Set("endTime", strconv.FormatInt(end, 10))
			if err := x.get(ctx, "", "/fapi/v1/markPriceKlines", q, false, &rows); err != nil {
				return nil, err
			}
		}
		for _, r := range rows {
			if len(r) < 5 {
				continue
			}
			v := make([]float64, 5)
			for i := range v {
				v[i] = num(record{"v": r[i]}, "v")
			}
			out = append(out, Candle{Time: int64(v[0]) / 1000, Open: v[1], High: v[2], Low: v[3], Close: v[4]})
		}
		if len(out) > 30000 {
			return nil, errors.New("Trade exceeds the 30,000 minute chart limit.")
		}
		start = end + 60000
	}
	return out, nil
}
