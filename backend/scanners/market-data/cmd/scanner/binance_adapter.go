package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
)

type BinanceAdapter struct {
	cfg          Config
	http         *http.Client
	limiter      *rate.Limiter
	exchange     Exchange
	marketType   MarketType
	rateMu       sync.Mutex
	blockedUntil time.Time
	tickerMu     sync.Mutex
	tickers      map[string]binanceTicker
	tickersUntil time.Time
}

type binanceTicker struct {
	MarkPrice       string `json:"markPrice"`
	IndexPrice      string `json:"indexPrice"`
	LastFundingRate string `json:"lastFundingRate"`
	Symbol          string `json:"symbol"`
}

type binanceForceOrderEvent struct {
	Event     string `json:"e"`
	EventTime int64  `json:"E"`
	Order     struct {
		Symbol       string `json:"s"`
		Side         string `json:"S"`
		Quantity     string `json:"q"`
		Filled       string `json:"z"`
		AveragePrice string `json:"ap"`
		Price        string `json:"p"`
		Timestamp    int64  `json:"T"`
	} `json:"o"`
}

type BinanceRateLimitError struct {
	Status int
	Until  time.Time
	Body   string
}

func (e BinanceRateLimitError) Error() string {
	return fmt.Sprintf("binance rate limited http %d until %s: %s", e.Status, e.Until.UTC().Format(time.RFC3339), e.Body)
}

func isBinanceRateLimitError(err error) bool {
	var rateErr BinanceRateLimitError
	return errors.As(err, &rateErr)
}

var binanceBanUntilRe = regexp.MustCompile(`(?i)banned until\s+(\d{10,13})`)

func NewBinanceAdapter(cfg Config) *BinanceAdapter {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   8 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConns:        200,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 8 * time.Second,
	}
	return &BinanceAdapter{
		cfg: cfg,
		http: &http.Client{
			Timeout:   cfg.HTTPTimeout,
			Transport: transport,
		},
		limiter:    rate.NewLimiter(rate.Limit(cfg.RPS), cfg.Burst),
		exchange:   ExchangeBinance,
		marketType: normalizeMarketType(cfg.MarketType),
		tickers:    make(map[string]binanceTicker),
	}
}

func (a *BinanceAdapter) Exchange() Exchange     { return a.exchange }
func (a *BinanceAdapter) MarketType() MarketType { return a.marketType }
func (a *BinanceAdapter) Instrument(symbol string) InstrumentKey {
	return NewInstrumentKey(a.exchange, a.marketType, symbol)
}

func (a *BinanceAdapter) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	if a.marketType != MarketTypePerpetual {
		return fmt.Errorf("binance adapter currently supports marketType=%s only", MarketTypePerpetual)
	}
	if until := a.rateLimitDeadline(); until.After(time.Now()) {
		return BinanceRateLimitError{Status: http.StatusTooManyRequests, Until: until, Body: "local cooldown"}
	}
	if err := a.limiter.Wait(ctx); err != nil {
		return err
	}
	if until := a.rateLimitDeadline(); until.After(time.Now()) {
		return BinanceRateLimitError{Status: http.StatusTooManyRequests, Until: until, Body: "local cooldown"}
	}
	u := strings.TrimRight(a.cfg.BaseURL, "/") + path
	if encoded := q.Encode(); encoded != "" {
		u += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "shortlong-scanner/2.0")
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusTeapot {
			until := binanceRetryDeadline(resp.StatusCode, resp.Header.Get("Retry-After"), string(body))
			a.setRateLimitDeadline(until)
			return BinanceRateLimitError{Status: resp.StatusCode, Until: until, Body: strings.TrimSpace(string(body))}
		}
		return HTTPStatusError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return decodeBoundedJSON(resp.Body, out)
}

func (a *BinanceAdapter) rateLimitDeadline() time.Time {
	a.rateMu.Lock()
	defer a.rateMu.Unlock()
	return a.blockedUntil
}

func (a *BinanceAdapter) setRateLimitDeadline(until time.Time) {
	a.rateMu.Lock()
	if until.After(a.blockedUntil) {
		a.blockedUntil = until
	}
	a.rateMu.Unlock()
}

func binanceRetryDeadline(status int, retryAfter, body string) time.Time {
	now := time.Now().UTC()
	until := now.Add(time.Minute)
	if status == http.StatusTeapot {
		until = now.Add(5 * time.Minute)
	}
	if raw := strings.TrimSpace(retryAfter); raw != "" {
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if seconds > 10_000_000_000 {
				until = time.UnixMilli(seconds).UTC()
			} else if seconds > 0 {
				until = now.Add(time.Duration(seconds) * time.Second)
			}
		} else if parsed, err := http.ParseTime(raw); err == nil {
			until = parsed.UTC()
		}
	}
	if match := binanceBanUntilRe.FindStringSubmatch(body); len(match) == 2 {
		if raw, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			parsed := time.Unix(raw, 0).UTC()
			if raw > 10_000_000_000 {
				parsed = time.UnixMilli(raw).UTC()
			}
			if parsed.After(until) {
				until = parsed
			}
		}
	}
	return until
}

func (a *BinanceAdapter) FetchKline1m(ctx context.Context, symbol string, targetMinuteMs int64) (int64, float64, float64, float64, float64, float64, float64, error) {
	q := url.Values{}
	q.Set("symbol", normalizeSymbol(symbol))
	q.Set("interval", "1m")
	q.Set("limit", "1")
	if targetMinuteMs > 0 {
		q.Set("startTime", strconv.FormatInt(targetMinuteMs, 10))
		q.Set("endTime", strconv.FormatInt(targetMinuteMs+ringMinuteMs-1, 10))
	}
	var rows [][]json.RawMessage
	if err := a.getJSON(ctx, "/fapi/v1/klines", q, &rows); err != nil {
		return 0, 0, 0, 0, 0, 0, 0, err
	}
	if len(rows) == 0 || len(rows[0]) < 8 {
		return 0, 0, 0, 0, 0, 0, 0, errors.New("binance kline empty")
	}
	start := rawInt64(rows[0][0])
	if targetMinuteMs > 0 && start != targetMinuteMs {
		return 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("binance kline timestamp mismatch: got=%d want=%d", start, targetMinuteMs)
	}
	values := make([]float64, 5)
	for index := range values {
		value, parseErr := rawFloatStrict(rows[0][index+1])
		if parseErr != nil || value < 0 || (index < 4 && value <= 0) {
			return 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("binance kline field %d invalid", index+1)
		}
		values[index] = value
	}
	quoteVolumeUsd, err := rawFloatStrict(rows[0][7])
	if err != nil || quoteVolumeUsd < 0 {
		return 0, 0, 0, 0, 0, 0, 0, errors.New("binance kline quote volume invalid")
	}
	return start, values[0], values[1], values[2], values[3], values[4], quoteVolumeUsd, nil
}

type binanceAggTrade struct {
	ID           int64  `json:"a"`
	Price        string `json:"p"`
	Quantity     string `json:"q"`
	FirstTradeID int64  `json:"f"`
	LastTradeID  int64  `json:"l"`
	Timestamp    int64  `json:"T"`
	BuyerMaker   bool   `json:"m"`
}

func (a *BinanceAdapter) FetchDeltaForMinute(ctx context.Context, symbol string, minuteStartMs int64, tradeLimit int) (TradeStats, error) {
	q := url.Values{}
	q.Set("symbol", normalizeSymbol(symbol))
	q.Set("startTime", strconv.FormatInt(minuteStartMs, 10))
	q.Set("endTime", strconv.FormatInt(minuteStartMs+ringMinuteMs-1, 10))
	q.Set("limit", strconv.Itoa(tradeLimit))
	var rows []binanceAggTrade
	if err := a.getJSON(ctx, "/fapi/v1/aggTrades", q, &rows); err != nil {
		return TradeStats{}, err
	}
	stats := TradeStats{Complete: len(rows) < tradeLimit, Source: "rest"}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Timestamp < rows[j].Timestamp })
	for _, row := range rows {
		price, priceErr := parseFiniteFloat(row.Price)
		size, sizeErr := parseFiniteFloat(row.Quantity)
		if priceErr != nil || sizeErr != nil || price <= 0 || size <= 0 || row.Timestamp < minuteStartMs || row.Timestamp >= minuteStartMs+ringMinuteMs {
			return TradeStats{}, errors.New("binance aggregate trade contains invalid price, size, or timestamp")
		}
		count := row.LastTradeID - row.FirstTradeID + 1
		if count < 1 {
			count = 1
		}
		side := "Buy"
		if row.BuyerMaker {
			side = "Sell"
		}
		stats.addTrade(row.Timestamp, price, size, side, float64(count))
	}
	return stats, nil
}

func binancePeriod(period string) string {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "5min", "5m":
		return "5m"
	case "15min", "15m":
		return "15m"
	case "30min", "30m":
		return "30m"
	case "1h", "60min":
		return "1h"
	case "2h", "4h", "6h", "12h", "1d":
		return strings.ToLower(strings.TrimSpace(period))
	default:
		return "5m"
	}
}

func (a *BinanceAdapter) FetchOpenInterest(ctx context.Context, symbol string, targetMinuteMs int64) (float64, int64, error) {
	q := url.Values{}
	q.Set("symbol", normalizeSymbol(symbol))
	q.Set("period", binancePeriod(a.cfg.OIInterval))
	q.Set("limit", "1")
	if targetMinuteMs > 0 {
		q.Set("endTime", strconv.FormatInt(targetMinuteMs+ringMinuteMs-1, 10))
	}
	var rows []struct {
		OpenInterest string `json:"sumOpenInterest"`
		Timestamp    int64  `json:"timestamp"`
	}
	if err := a.getJSON(ctx, "/futures/data/openInterestHist", q, &rows); err != nil {
		return 0, 0, err
	}
	if len(rows) == 0 {
		return 0, 0, errors.New("binance open interest empty")
	}
	value, err := parseFiniteFloat(rows[len(rows)-1].OpenInterest)
	if err != nil || value < 0 {
		return 0, 0, errors.New("binance open interest invalid")
	}
	timestamp := rows[len(rows)-1].Timestamp
	if timestamp <= 0 {
		return 0, 0, errors.New("binance open interest timestamp invalid")
	}
	return value, timestamp, nil
}

func (a *BinanceAdapter) FetchAccountRatio(ctx context.Context, symbol string, period string, targetMinuteMs int64) (float64, float64, error) {
	q := url.Values{}
	q.Set("symbol", normalizeSymbol(symbol))
	q.Set("period", binancePeriod(period))
	q.Set("limit", "1")
	if targetMinuteMs > 0 {
		q.Set("endTime", strconv.FormatInt(targetMinuteMs+ringMinuteMs-1, 10))
	}
	var rows []struct {
		Long  string `json:"longAccount"`
		Short string `json:"shortAccount"`
	}
	if err := a.getJSON(ctx, "/futures/data/globalLongShortAccountRatio", q, &rows); err != nil {
		return 0, 0, err
	}
	if len(rows) == 0 {
		return 0, 0, errors.New("binance account ratio empty")
	}
	longRatio, err := parseFiniteFloat(rows[len(rows)-1].Long)
	if err != nil || longRatio < 0 {
		return 0, 0, errors.New("binance long account ratio invalid")
	}
	shortRatio, err := parseFiniteFloat(rows[len(rows)-1].Short)
	if err != nil || shortRatio < 0 {
		return 0, 0, errors.New("binance short account ratio invalid")
	}
	return longRatio, shortRatio, nil
}

func (a *BinanceAdapter) FetchOrderbook(ctx context.Context, symbol string, limit int) (float64, float64, float64, float64, error) {
	q := url.Values{}
	q.Set("symbol", normalizeSymbol(symbol))
	if limit <= 0 {
		limit = 5
	}
	q.Set("limit", strconv.Itoa(limit))
	var payload struct {
		Bids [][]string `json:"bids"`
		Asks [][]string `json:"asks"`
	}
	if err := a.getJSON(ctx, "/fapi/v1/depth", q, &payload); err != nil {
		return 0, 0, 0, 0, err
	}
	return aggregateOrderbook(payload.Bids, payload.Asks)
}

func (a *BinanceAdapter) FetchTickers(ctx context.Context, symbol string) (float64, float64, float64, error) {
	symbol = normalizeSymbol(symbol)
	a.tickerMu.Lock()
	defer a.tickerMu.Unlock()
	if time.Now().After(a.tickersUntil) {
		var payload []binanceTicker
		if err := a.getJSON(ctx, "/fapi/v1/premiumIndex", nil, &payload); err != nil {
			return 0, 0, 0, err
		}
		next := make(map[string]binanceTicker, len(payload))
		for _, ticker := range payload {
			if key := normalizeSymbol(ticker.Symbol); key != "" {
				next[key] = ticker
			}
		}
		a.tickers = next
		a.tickersUntil = time.Now().Add(30 * time.Second)
	}
	payload, ok := a.tickers[symbol]
	if !ok {
		return 0, 0, 0, errors.New("binance premium index symbol missing")
	}
	mark, err := parseFiniteFloat(payload.MarkPrice)
	if err != nil || mark <= 0 {
		return 0, 0, 0, errors.New("binance mark price invalid")
	}
	index, err := parseFiniteFloat(payload.IndexPrice)
	if err != nil || index <= 0 {
		return 0, 0, 0, errors.New("binance index price invalid")
	}
	funding, err := parseFiniteFloat(payload.LastFundingRate)
	if err != nil {
		return 0, 0, 0, errors.New("binance funding rate invalid")
	}
	return mark, index, funding, nil
}

func (a *BinanceAdapter) fetchPriceKline(ctx context.Context, path, parameter, symbol string, targetMinuteMs int64) (float64, error) {
	q := url.Values{}
	q.Set(parameter, normalizeSymbol(symbol))
	q.Set("interval", "1m")
	q.Set("limit", "1")
	if targetMinuteMs > 0 {
		q.Set("startTime", strconv.FormatInt(targetMinuteMs, 10))
		q.Set("endTime", strconv.FormatInt(targetMinuteMs+ringMinuteMs-1, 10))
	}
	var rows [][]json.RawMessage
	if err := a.getJSON(ctx, path, q, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 || len(rows[0]) < 5 {
		return 0, errors.New("binance price kline empty")
	}
	value, err := rawFloatStrict(rows[0][4])
	if err != nil || value <= 0 {
		return 0, errors.New("binance price kline close invalid")
	}
	return value, nil
}

func (a *BinanceAdapter) FetchMarkPrice(ctx context.Context, symbol string, targetMinuteMs int64) (float64, error) {
	return a.fetchPriceKline(ctx, "/fapi/v1/markPriceKlines", "symbol", symbol, targetMinuteMs)
}

func (a *BinanceAdapter) FetchIndexPrice(ctx context.Context, symbol string, targetMinuteMs int64) (float64, error) {
	return a.fetchPriceKline(ctx, "/fapi/v1/indexPriceKlines", "pair", symbol, targetMinuteMs)
}

func (a *BinanceAdapter) FetchFundingRate(ctx context.Context, symbol string) (float64, error) {
	q := url.Values{}
	q.Set("symbol", normalizeSymbol(symbol))
	q.Set("limit", "1")
	var rows []struct {
		FundingRate string `json:"fundingRate"`
	}
	if err := a.getJSON(ctx, "/fapi/v1/fundingRate", q, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, errors.New("binance funding rate empty")
	}
	value, err := parseFiniteFloat(rows[len(rows)-1].FundingRate)
	if err != nil {
		return 0, errors.New("binance funding rate invalid")
	}
	return value, nil
}

func (a *BinanceAdapter) FetchAllSymbolsFromAPI(ctx context.Context) ([]string, error) {
	var payload struct {
		Symbols []struct {
			Symbol       string `json:"symbol"`
			Status       string `json:"status"`
			ContractType string `json:"contractType"`
			QuoteAsset   string `json:"quoteAsset"`
		} `json:"symbols"`
	}
	if err := a.getJSON(ctx, "/fapi/v1/exchangeInfo", nil, &payload); err != nil {
		return nil, err
	}
	if len(payload.Symbols) > maxScannerCatalogSymbols {
		return nil, errors.New("binance exchangeInfo catalog exceeds safe symbol limit")
	}
	out := make([]string, 0, len(payload.Symbols))
	for _, item := range payload.Symbols {
		if !strings.EqualFold(item.Status, "TRADING") || !strings.EqualFold(item.ContractType, "PERPETUAL") || !strings.EqualFold(item.QuoteAsset, "USDT") {
			continue
		}
		out = append(out, normalizeSymbol(item.Symbol))
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, errors.New("binance exchangeInfo returned no USDT perpetual symbols")
	}
	return out, nil
}

func (a *BinanceAdapter) RunStreams(ctx context.Context, symbols []string, trades *TradeStore, liquidations *LiquidationStore) error {
	if len(symbols) == 0 {
		return nil
	}
	if trades == nil || liquidations == nil {
		return errors.New("binance stream stores are required")
	}

	// Binance permits at most 1024 streams per connection. Each symbol keeps
	// its original aggregate-trade and force-order subscriptions.
	const symbolsPerConnection = 80
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	connections := (len(symbols) + symbolsPerConnection - 1) / symbolsPerConnection
	errs := make(chan error, connections)
	ready := make(chan struct{}, connections)
	defer trades.SetConnected(false)
	defer liquidations.SetConnected(false)
	for start := 0; start < len(symbols); start += symbolsPerConnection {
		end := start + symbolsPerConnection
		if end > len(symbols) {
			end = len(symbols)
		}
		batch := append([]string(nil), symbols[start:end]...)
		go func() {
			errs <- a.runStreamConnection(streamCtx, batch, trades, liquidations, ready)
		}()
	}
	for connected := 0; connected < connections; connected++ {
		select {
		case <-ready:
		case err := <-errs:
			cancel()
			return err
		case <-ctx.Done():
			cancel()
			return ctx.Err()
		}
	}
	trades.SetConnected(true)
	liquidations.SetConnected(true)
	err := <-errs
	cancel()
	if err == nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (a *BinanceAdapter) runStreamConnection(ctx context.Context, symbols []string, trades *TradeStore, liquidations *LiquidationStore, ready chan<- struct{}) error {
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS12},
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.Dial(a.cfg.WSURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetReadLimit(8 << 20)
	const readTimeout = 60 * time.Second
	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(readTimeout))
	})
	closeDone := make(chan struct{})
	defer close(closeDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-closeDone:
		}
	}()
	var writeMu sync.Mutex
	writeJSON := func(value any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(value)
	}

	requestID := int64(1)
	pendingSubscriptions := make(map[int64]struct{})
	subscribedSymbols := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		subscribedSymbols[normalizeSymbol(symbol)] = struct{}{}
	}
	const batchSize = 40
	for start := 0; start < len(symbols); start += batchSize {
		end := start + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}
		params := make([]string, 0, (end-start)*2)
		for _, symbol := range symbols[start:end] {
			streamSymbol := strings.ToLower(normalizeSymbol(symbol))
			params = append(params, streamSymbol+"@aggTrade", streamSymbol+"@forceOrder")
		}
		if err := writeJSON(map[string]any{"method": "SUBSCRIBE", "params": params, "id": requestID}); err != nil {
			return err
		}
		pendingSubscriptions[requestID] = struct{}{}
		requestID++
	}
	subscriptionDeadline := time.Now().Add(15 * time.Second)
	_ = conn.SetReadDeadline(subscriptionDeadline)
	readySent := false

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-ticker.C:
				writeMu.Lock()
				err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
				writeMu.Unlock()
				if err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if readySent {
			_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		} else {
			_ = conn.SetReadDeadline(subscriptionDeadline)
		}
		var envelope struct {
			Stream    string          `json:"stream"`
			Data      json.RawMessage `json:"data"`
			Event     string          `json:"e"`
			EventTime int64           `json:"E"`
			ID        int64           `json:"id"`
			Result    json.RawMessage `json:"result"`
			Code      *int            `json:"code"`
			Message   string          `json:"msg"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}
		if envelope.ID != 0 && (len(envelope.Result) > 0 || envelope.Code != nil || envelope.Message != "") {
			if _, pending := pendingSubscriptions[envelope.ID]; !pending {
				continue
			}
			if envelope.Code != nil {
				return fmt.Errorf("binance websocket subscription failed url=%s id=%d code=%d message=%s", a.cfg.WSURL, envelope.ID, *envelope.Code, envelope.Message)
			}
			delete(pendingSubscriptions, envelope.ID)
			if len(pendingSubscriptions) == 0 && !readySent {
				select {
				case ready <- struct{}{}:
					readySent = true
					_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			continue
		}
		payload := raw
		if len(envelope.Data) > 0 {
			payload = envelope.Data
			var eventOnly struct {
				Event     string `json:"e"`
				EventTime int64  `json:"E"`
			}
			_ = json.Unmarshal(payload, &eventOnly)
			envelope.Event = eventOnly.Event
		}
		switch envelope.Event {
		case "aggTrade":
			var event struct {
				Symbol       string `json:"s"`
				Price        string `json:"p"`
				Quantity     string `json:"q"`
				Timestamp    int64  `json:"T"`
				BuyerMaker   bool   `json:"m"`
				FirstTradeID int64  `json:"f"`
				LastTradeID  int64  `json:"l"`
			}
			if json.Unmarshal(payload, &event) != nil {
				continue
			}
			price, priceErr := parseFiniteFloat(event.Price)
			size, sizeErr := parseFiniteFloat(event.Quantity)
			count := float64(event.LastTradeID - event.FirstTradeID + 1)
			if priceErr != nil || sizeErr != nil || price <= 0 || size <= 0 || event.Timestamp <= 0 || count <= 0 || event.Symbol == "" {
				continue
			}
			side := "Buy"
			if event.BuyerMaker {
				side = "Sell"
			}
			trades.Add(a.Instrument(event.Symbol), event.Timestamp, price, size, side, count)
		case "forceOrder":
			if a.cfg.LiqDebug {
				fmt.Printf("BINANCE_FORCE_RAW stream=%s payload=%s\n", envelope.Stream, string(payload))
			}
			events := parseBinanceForceOrderEvents(payload)
			if a.cfg.LiqDebug {
				fmt.Printf("BINANCE_FORCE_PARSED count=%d\n", len(events))
			}
			for _, event := range events {
				accepted := a.storeBinanceLiquidation(event, subscribedSymbols, liquidations)
				if a.cfg.LiqDebug {
					fmt.Printf(
						"BINANCE_FORCE_STORE accepted=%v symbol=%s side=%s z=%s ap=%s T=%d\n",
						accepted,
						event.Order.Symbol,
						event.Order.Side,
						event.Order.Filled,
						event.Order.AveragePrice,
						event.Order.Timestamp,
					)
				}
			}
		}
	}
}

func parseBinanceForceOrderEvents(payload []byte) []binanceForceOrderEvent {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return nil
	}
	if payload[0] == '[' {
		var events []binanceForceOrderEvent
		if err := json.Unmarshal(payload, &events); err != nil {
			fmt.Printf(
				"BINANCE_FORCE_PARSE_ERROR array=true error=%v payload=%q\n",
				err,
				string(payload),
			)
			return nil
		}
		return events
	}

	var event binanceForceOrderEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		fmt.Printf(
			"BINANCE_FORCE_PARSE_ERROR array=false error=%v payload=%q\n",
			err,
			string(payload),
		)
		return nil
	}

	// The outer envelope switch has already identified this as forceOrder.
	return []binanceForceOrderEvent{event}
}

func (a *BinanceAdapter) storeBinanceLiquidation(event binanceForceOrderEvent, allowedSymbols map[string]struct{}, liquidations *LiquidationStore) bool {
	symbol := normalizeSymbol(event.Order.Symbol)
	if _, allowed := allowedSymbols[symbol]; !allowed {
		return false
	}
	price, _ := parseFiniteFloat(event.Order.AveragePrice)
	if price <= 0 {
		price, _ = parseFiniteFloat(event.Order.Price)
	}
	size, _ := parseFiniteFloat(event.Order.Filled)
	if size <= 0 {
		size, _ = parseFiniteFloat(event.Order.Quantity)
	}
	canonicalSide, ok := binanceLiquidationCanonicalSide(event.Order.Side)
	if !ok || symbol == "" || event.Order.Timestamp <= 0 || price <= 0 || size <= 0 {
		return false
	}
	liquidations.Add(a.Instrument(symbol), event.Order.Timestamp, price*size, canonicalSide)
	return true
}

func binanceLiquidationCanonicalSide(orderSide string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(orderSide)) {
	case "SELL":
		return "Buy", true // forced sell closes a long position
	case "BUY":
		return "Sell", true // forced buy closes a short position
	default:
		return "", false
	}
}

func aggregateOrderbook(bids, asks [][]string) (bestBid, bestAsk, bidDepth, askDepth float64, err error) {
	for index, row := range bids {
		if len(row) < 2 {
			continue
		}
		price, priceErr := parseFiniteFloat(row[0])
		size, sizeErr := parseFiniteFloat(row[1])
		if priceErr != nil || sizeErr != nil || price <= 0 || size < 0 {
			return 0, 0, 0, 0, errors.New("binance orderbook bid invalid")
		}
		bidDepth += size
		if index == 0 || price > bestBid {
			bestBid = price
		}
	}
	for index, row := range asks {
		if len(row) < 2 {
			continue
		}
		price, priceErr := parseFiniteFloat(row[0])
		size, sizeErr := parseFiniteFloat(row[1])
		if priceErr != nil || sizeErr != nil || price <= 0 || size < 0 {
			return 0, 0, 0, 0, errors.New("binance orderbook ask invalid")
		}
		askDepth += size
		if index == 0 || bestAsk == 0 || price < bestAsk {
			bestAsk = price
		}
	}
	if bestBid <= 0 || bestAsk <= 0 || math.IsNaN(bestBid) || math.IsNaN(bestAsk) {
		return 0, 0, 0, 0, errors.New("orderbook empty")
	}
	return bestBid, bestAsk, bidDepth, askDepth, nil
}

func rawFloatStrict(raw json.RawMessage) (float64, error) {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return parseFiniteFloat(text)
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("invalid numeric value")
	}
	return value, nil
}

func parseFiniteFloat(text string) (float64, error) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("invalid numeric value")
	}
	return value, nil
}

func rawInt64(raw json.RawMessage) int64 {
	var value int64
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var text string
	_ = json.Unmarshal(raw, &text)
	value, _ = strconv.ParseInt(text, 10, 64)
	return value
}
