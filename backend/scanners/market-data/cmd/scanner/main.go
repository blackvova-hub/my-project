package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
)

const (
	ringMinuteMs                = int64(60_000)
	defaultBinanceFuturesWSURL  = "wss://fstream.binance.com/market/stream"
	maxScannerJSONResponseBytes = 16 << 20
	maxScannerCatalogSymbols    = 20_000
	maxScannerCatalogPages      = 100
)

var errBinanceTradeMinuteUnavailable = errors.New("binance websocket trade minute unavailable")

type Config struct {
	// Sharding
	ShardTotal int
	ShardIndex int

	// Canonical venue identity. BaseURL/WSURL are resolved for the selected adapter.
	Exchange   string // bybit|binance|okx|bitget|gateio
	MarketType string // perpetual|spot
	BaseURL    string
	Category   string // Bybit API category (linear|spot|inverse)
	WSURL      string
	// WS keepalive
	WSPingIntervalSeconds int // 20
	WSReadTimeoutSeconds  int
	LiqLogIntervalSeconds int // 0 = off
	LiqDebug              bool

	// Symbols input
	SymbolsMode string // api|file
	SymbolsFile string // only if SymbolsMode=file

	// Symbols API load
	SymbolsLoadTimeoutSeconds int
	SymbolsLoadRetries        int

	// Scheduling
	AlignToMinute bool

	// Closed-candle scheduling.
	RunTimeoutSeconds        int // default 55
	CandleSettleDelaySeconds int // wait after each minute boundary before scanning the closed candle

	// Concurrency + throttling
	Workers int
	RPS     float64
	Burst   int

	// recent-trade limit
	TradeLimit int

	// open interest intervalTime (Bybit min 5min)
	OIInterval           string
	FundingIntervalHours float64

	// orderbook
	OrderbookEnabled bool

	// Publisher
	PublisherType         string // stdout|redis
	RedisAddr             string
	RedisPassword         string
	RedisDB               int
	CompactRedisStream    string
	CompactRetentionHours int

	// HTTP
	HTTPTimeout time.Duration
}

type Publisher interface {
	Publish(ctx context.Context, eventJSON []byte) error
	Close() error
}

type StdoutPublisher struct{}

func (p *StdoutPublisher) Publish(ctx context.Context, eventJSON []byte) error {
	_ = ctx
	fmt.Println(string(eventJSON))
	return nil
}
func (p *StdoutPublisher) Close() error { return nil }

type RedisStreamPublisher struct {
	rdb           *redis.Client
	stream        string
	maxLen        int64
	retention     time.Duration
	closeClient   bool
	publishErrors atomic.Uint64
}

func (p *RedisStreamPublisher) Publish(ctx context.Context, eventJSON []byte) error {
	// Trim before XADD. With noeviction, an over-budget Redis rejects XADD;
	// a post-add-only trim can then never free expired transport entries.
	if p.retention > 0 {
		if err := p.trimExpired(ctx); err != nil {
			total := p.publishErrors.Add(1)
			log.Printf("redis time retention error stream=%s total=%d err=%v", p.stream, total, err)
			return err
		}
	}
	args := &redis.XAddArgs{
		Stream: p.stream,
		Values: map[string]any{"json": string(eventJSON)},
	}
	if p.maxLen > 0 {
		args.MaxLen = p.maxLen
		// Approx trim (lightweight) if supported by your go-redis version
		args.Approx = true
	}
	_, err := p.rdb.XAdd(ctx, args).Result()
	if err != nil {
		total := p.publishErrors.Add(1)
		log.Printf("redis publish error stream=%s total=%d err=%v", p.stream, total, err)
		return err
	}
	return nil
}

func (p *RedisStreamPublisher) trimExpired(ctx context.Context) error {
	if p == nil || p.rdb == nil || p.retention <= 0 || strings.TrimSpace(p.stream) == "" {
		return errors.New("invalid Redis stream retention publisher")
	}
	serverTime, err := p.rdb.Time(ctx).Result()
	if err != nil {
		return fmt.Errorf("read Redis server time: %w", err)
	}
	minID, err := retentionMinID(fmt.Sprintf("%d-0", serverTime.UTC().UnixMilli()), p.retention)
	if err != nil {
		return err
	}
	if err := p.rdb.XTrimMinIDApprox(ctx, p.stream, minID, 0).Err(); err != nil {
		return fmt.Errorf("trim stream %s before publish to minID %s: %w", p.stream, minID, err)
	}
	return nil
}

func retentionMinID(serverStreamID string, retention time.Duration) (string, error) {
	if retention <= 0 {
		return "", errors.New("stream retention must be positive")
	}
	separator := strings.IndexByte(serverStreamID, '-')
	if separator <= 0 {
		return "", fmt.Errorf("redis returned invalid stream id %q", serverStreamID)
	}
	serverMillis, err := strconv.ParseInt(serverStreamID[:separator], 10, 64)
	if err != nil {
		return "", fmt.Errorf("parse redis stream id %q: %w", serverStreamID, err)
	}
	return fmt.Sprintf("%d-0", serverMillis-retention.Milliseconds()), nil
}
func (p *RedisStreamPublisher) Close() error {
	if p.closeClient {
		return p.rdb.Close()
	}
	return nil
}

// ---- Bybit client ----

type BybitClient struct {
	cfg     Config
	http    *http.Client
	limiter *rate.Limiter
}

func NewBybitClient(cfg Config) *BybitClient {
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
	return &BybitClient{
		cfg: cfg,
		http: &http.Client{
			Timeout:   cfg.HTTPTimeout,
			Transport: transport,
		},
		limiter: rate.NewLimiter(rate.Limit(cfg.RPS), cfg.Burst),
	}
}

func (c *BybitClient) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	u := c.cfg.BaseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return HTTPStatusError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	return decodeBoundedJSON(resp.Body, out)
}

func decodeBoundedJSON(reader io.Reader, out any) error {
	if reader == nil || out == nil {
		return errors.New("invalid JSON decode target")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxScannerJSONResponseBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxScannerJSONResponseBytes {
		return fmt.Errorf("JSON response exceeds %d bytes", maxScannerJSONResponseBytes)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return err
	}
	return nil
}

type HTTPStatusError struct {
	Status int
	Body   string
}

func (e HTTPStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("http %d", e.Status)
	}
	return fmt.Sprintf("http %d: %s", e.Status, e.Body)
}

// ----- API types -----

type klineResp struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Symbol   string `json:"symbol"`
		Category string `json:"category"`
		// list: [startTime, open, high, low, close, volume, turnover]
		List [][]string `json:"list"`
	} `json:"result"`
}

type orderbookResp struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Symbol string     `json:"symbol"`
		Bids   [][]string `json:"b"`
		Asks   [][]string `json:"a"`
	} `json:"result"`
}

type tickersResp struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Category string `json:"category"`
		List     []struct {
			Symbol      string `json:"symbol"`
			MarkPrice   string `json:"markPrice"`
			IndexPrice  string `json:"indexPrice"`
			FundingRate string `json:"fundingRate"`
		} `json:"list"`
	} `json:"result"`
}

type markPriceKlineResp struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Symbol   string `json:"symbol"`
		Category string `json:"category"`
		// list: [startTime, open, high, low, close]
		List [][]string `json:"list"`
	} `json:"result"`
}

type indexPriceKlineResp struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Symbol   string `json:"symbol"`
		Category string `json:"category"`
		// list: [startTime, open, high, low, close]
		List [][]string `json:"list"`
	} `json:"result"`
}

type fundingHistoryResp struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Symbol   string `json:"symbol"`
		Category string `json:"category"`
		List     []struct {
			FundingRate          string `json:"fundingRate"`
			FundingRateTimestamp string `json:"fundingRateTimestamp"`
		} `json:"list"`
	} `json:"result"`
}

type recentTradeResp struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Category string `json:"category"`
		List     []struct {
			Symbol string `json:"symbol"`
			Price  string `json:"price"`
			Size   string `json:"size"`
			Side   string `json:"side"` // Buy | Sell
			Time   string `json:"time"` // unix ms as string
		} `json:"list"`
	} `json:"result"`
}

type openInterestResp struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Symbol   string `json:"symbol"`
		Category string `json:"category"`
		List     []struct {
			OpenInterest string `json:"openInterest"`
			Timestamp    string `json:"timestamp"` // unix ms as string
		} `json:"list"`
	} `json:"result"`
}

type wsMessage struct {
	Topic   string          `json:"topic"`
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data"`
	Op      string          `json:"op"`
	ReqID   string          `json:"req_id"`
	Success *bool           `json:"success"`
	RetMsg  string          `json:"ret_msg"`
}

var errBybitSubscription = errors.New("bybit subscription failed")

type bybitSubscriptionAcks struct {
	pending map[string]struct{}
}

func newBybitSubscriptionAcks(ids []string) (*bybitSubscriptionAcks, error) {
	pending := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("%w: empty request id", errBybitSubscription)
		}
		if _, exists := pending[id]; exists {
			return nil, fmt.Errorf("%w: duplicate request id %q", errBybitSubscription, id)
		}
		pending[id] = struct{}{}
	}
	if len(pending) == 0 {
		return nil, fmt.Errorf("%w: no subscription requests", errBybitSubscription)
	}
	return &bybitSubscriptionAcks{pending: pending}, nil
}

func (a *bybitSubscriptionAcks) observe(message wsMessage) (bool, error) {
	if a == nil {
		return false, fmt.Errorf("%w: missing ACK tracker", errBybitSubscription)
	}
	id := strings.TrimSpace(message.ReqID)
	if id == "" {
		return false, fmt.Errorf("%w: ACK has no req_id", errBybitSubscription)
	}
	if _, expected := a.pending[id]; !expected {
		return false, fmt.Errorf("%w: unknown or duplicate ACK %q", errBybitSubscription, id)
	}
	if message.Success == nil || !*message.Success {
		return false, fmt.Errorf("%w: req_id=%s ret_msg=%s", errBybitSubscription, id, strings.TrimSpace(message.RetMsg))
	}
	delete(a.pending, id)
	return len(a.pending) == 0, nil
}

type liquidationItem struct {
	T      int64  `json:"T"`
	Symbol string `json:"s"`
	Side   string `json:"S"`
	Size   string `json:"v"`
	Price  string `json:"p"`
}

type instrumentsInfoResp struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Category       string `json:"category"`
		NextPageCursor string `json:"nextPageCursor"`
		List           []struct {
			Symbol       string `json:"symbol"`
			Status       string `json:"status"` // e.g. "Trading"
			ContractType string `json:"contractType"`
		} `json:"list"`
	} `json:"result"`
}

func (c *BybitClient) FetchKline1m(ctx context.Context, symbol string, targetMinuteMs int64) (minuteStartMs int64, open float64, high float64, low float64, close float64, volume float64, quoteVolumeUsd float64, err error) {
	symbol = normalizeSymbol(symbol)
	q := url.Values{}
	q.Set("category", c.cfg.Category)
	q.Set("symbol", symbol)
	q.Set("interval", "1")
	q.Set("limit", "1")
	if targetMinuteMs > 0 {
		q.Set("start", strconv.FormatInt(targetMinuteMs, 10))
		q.Set("end", strconv.FormatInt(targetMinuteMs+ringMinuteMs-1, 10))
	}

	var r klineResp
	if err := c.getJSON(ctx, "/v5/market/kline", q, &r); err != nil {
		return 0, 0, 0, 0, 0, 0, 0, err
	}
	if r.RetCode != 0 {
		return 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("kline retCode=%d retMsg=%s", r.RetCode, r.RetMsg)
	}
	if len(r.Result.List) == 0 || len(r.Result.List[0]) < 7 {
		return 0, 0, 0, 0, 0, 0, 0, errors.New("kline empty")
	}

	startMs, err := strconv.ParseInt(r.Result.List[0][0], 10, 64)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("kline start time: %w", err)
	}
	if targetMinuteMs > 0 && startMs != targetMinuteMs {
		return 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("kline timestamp mismatch: got=%d want=%d", startMs, targetMinuteMs)
	}
	values := []*float64{&open, &high, &low, &close, &volume, &quoteVolumeUsd}
	for index, target := range values {
		parsed, parseErr := strconv.ParseFloat(r.Result.List[0][index+1], 64)
		if parseErr != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, 0, 0, 0, 0, 0, 0, fmt.Errorf("kline field %d invalid", index+1)
		}
		*target = parsed
	}
	if open <= 0 || high <= 0 || low <= 0 || close <= 0 || volume < 0 || quoteVolumeUsd < 0 {
		return 0, 0, 0, 0, 0, 0, 0, errors.New("kline contains invalid values")
	}
	return startMs, open, high, low, close, volume, quoteVolumeUsd, nil
}

type accountRatioResp struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List []struct {
			Symbol    string `json:"symbol"`
			BuyRatio  string `json:"buyRatio"`
			SellRatio string `json:"sellRatio"`
			Timestamp string `json:"timestamp"`
		} `json:"list"`
	} `json:"result"`
}

func (c *BybitClient) FetchDeltaForMinute(ctx context.Context, symbol string, minuteStartMs int64, tradeLimit int) (TradeStats, error) {
	symbol = normalizeSymbol(symbol)
	q := url.Values{}
	q.Set("category", c.cfg.Category)
	q.Set("symbol", symbol)
	q.Set("limit", strconv.Itoa(tradeLimit))

	var r recentTradeResp
	if err := c.getJSON(ctx, "/v5/market/recent-trade", q, &r); err != nil {
		return TradeStats{}, err
	}
	if r.RetCode != 0 {
		return TradeStats{}, fmt.Errorf("recent-trade retCode=%d retMsg=%s", r.RetCode, r.RetMsg)
	}

	minuteEndMs := minuteStartMs + ringMinuteMs
	oldestTS := int64(math.MaxInt64)
	type parsedTrade struct {
		ts    int64
		price float64
		size  float64
		side  string
	}
	parsed := make([]parsedTrade, 0, len(r.Result.List))
	for _, t := range r.Result.List {
		ts, _ := strconv.ParseInt(t.Time, 10, 64)
		if ts > 0 && ts < oldestTS {
			oldestTS = ts
		}
		if ts < minuteStartMs || ts >= minuteEndMs {
			continue
		}
		sz, sizeErr := strconv.ParseFloat(t.Size, 64)
		price, priceErr := strconv.ParseFloat(t.Price, 64)
		if sizeErr != nil || priceErr != nil || sz <= 0 || price <= 0 {
			return TradeStats{}, errors.New("recent trade contains invalid price or size")
		}
		if !strings.EqualFold(t.Side, "Buy") && !strings.EqualFold(t.Side, "Sell") {
			return TradeStats{}, errors.New("recent trade contains invalid side")
		}
		parsed = append(parsed, parsedTrade{ts: ts, price: price, size: sz, side: t.Side})
	}
	sort.SliceStable(parsed, func(i, j int) bool { return parsed[i].ts < parsed[j].ts })
	complete := isTradeWindowComplete(len(r.Result.List), tradeLimit, oldestTS, minuteStartMs)
	stats := TradeStats{Complete: complete, Source: "rest"}
	for _, trade := range parsed {
		stats.addTrade(trade.ts, trade.price, trade.size, trade.side, 1)
	}
	return stats, nil
}

func isTradeWindowComplete(resultCount, requestedLimit int, oldestTradeMs, minuteStartMs int64) bool {
	if resultCount < requestedLimit {
		return true
	}
	return oldestTradeMs > 0 && oldestTradeMs < minuteStartMs
}

func (c *BybitClient) FetchOpenInterest(ctx context.Context, symbol string, targetMinuteMs int64) (float64, int64, error) {
	symbol = normalizeSymbol(symbol)
	q := url.Values{}
	q.Set("category", c.cfg.Category)
	q.Set("symbol", symbol)
	q.Set("intervalTime", c.cfg.OIInterval) // e.g. 5min
	q.Set("limit", "1")
	if targetMinuteMs > 0 {
		q.Set("endTime", strconv.FormatInt(targetMinuteMs+ringMinuteMs-1, 10))
	}

	var r openInterestResp
	if err := c.getJSON(ctx, "/v5/market/open-interest", q, &r); err != nil {
		return 0, 0, err
	}
	if r.RetCode != 0 {
		return 0, 0, fmt.Errorf("open-interest retCode=%d retMsg=%s", r.RetCode, r.RetMsg)
	}
	if len(r.Result.List) == 0 {
		return 0, 0, errors.New("open-interest empty")
	}
	oi, err := strconv.ParseFloat(r.Result.List[0].OpenInterest, 64)
	if err != nil || oi < 0 {
		return 0, 0, errors.New("open-interest invalid")
	}
	timestamp, err := strconv.ParseInt(r.Result.List[0].Timestamp, 10, 64)
	if err != nil || timestamp <= 0 {
		return 0, 0, errors.New("open-interest timestamp invalid")
	}
	return oi, timestamp, nil
}

func (c *BybitClient) FetchAccountRatio(ctx context.Context, symbol string, period string, targetMinuteMs int64) (buyRatio float64, sellRatio float64, err error) {
	symbol = normalizeSymbol(symbol)
	q := url.Values{}
	q.Set("category", c.cfg.Category)
	q.Set("symbol", symbol)
	q.Set("period", period)
	q.Set("limit", "1")
	if targetMinuteMs > 0 {
		q.Set("endTime", strconv.FormatInt(targetMinuteMs+ringMinuteMs-1, 10))
	}

	var r accountRatioResp
	if err := c.getJSON(ctx, "/v5/market/account-ratio", q, &r); err != nil {
		return 0, 0, err
	}
	if r.RetCode != 0 {
		return 0, 0, fmt.Errorf("account-ratio retCode=%d retMsg=%s", r.RetCode, r.RetMsg)
	}
	if len(r.Result.List) == 0 {
		return 0, 0, errors.New("account-ratio empty")
	}
	buyRatio, err = strconv.ParseFloat(r.Result.List[0].BuyRatio, 64)
	if err != nil {
		return 0, 0, errors.New("buy ratio invalid")
	}
	sellRatio, err = strconv.ParseFloat(r.Result.List[0].SellRatio, 64)
	if err != nil || buyRatio < 0 || sellRatio < 0 {
		return 0, 0, errors.New("account ratio invalid")
	}
	return buyRatio, sellRatio, nil
}

func (c *BybitClient) FetchOrderbook(ctx context.Context, symbol string, limit int) (bestBid float64, bestAsk float64, bidDepth float64, askDepth float64, err error) {
	symbol = normalizeSymbol(symbol)
	q := url.Values{}
	q.Set("category", c.cfg.Category)
	q.Set("symbol", symbol)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}

	var r orderbookResp
	if err := c.getJSON(ctx, "/v5/market/orderbook", q, &r); err != nil {
		return 0, 0, 0, 0, err
	}
	if r.RetCode != 0 {
		return 0, 0, 0, 0, fmt.Errorf("orderbook retCode=%d retMsg=%s", r.RetCode, r.RetMsg)
	}
	for i, row := range r.Result.Bids {
		if len(row) < 2 {
			continue
		}
		price, _ := strconv.ParseFloat(row[0], 64)
		size, _ := strconv.ParseFloat(row[1], 64)
		bidDepth += size
		if i == 0 || price > bestBid {
			bestBid = price
		}
	}
	for i, row := range r.Result.Asks {
		if len(row) < 2 {
			continue
		}
		price, _ := strconv.ParseFloat(row[0], 64)
		size, _ := strconv.ParseFloat(row[1], 64)
		askDepth += size
		if i == 0 || bestAsk == 0 || price < bestAsk {
			bestAsk = price
		}
	}
	if bestBid <= 0 || bestAsk <= 0 {
		return 0, 0, 0, 0, errors.New("orderbook empty")
	}
	return bestBid, bestAsk, bidDepth, askDepth, nil
}

func (c *BybitClient) FetchTickers(ctx context.Context, symbol string) (markPrice float64, indexPrice float64, fundingRate float64, err error) {
	symbol = normalizeSymbol(symbol)
	q := url.Values{}
	q.Set("category", c.cfg.Category)
	q.Set("symbol", symbol)

	var r tickersResp
	if err := c.getJSON(ctx, "/v5/market/tickers", q, &r); err != nil {
		return 0, 0, 0, err
	}
	if r.RetCode != 0 || len(r.Result.List) == 0 {
		return 0, 0, 0, fmt.Errorf("tickers retCode=%d retMsg=%s", r.RetCode, r.RetMsg)
	}
	markPrice, err = strconv.ParseFloat(r.Result.List[0].MarkPrice, 64)
	if err != nil || markPrice <= 0 {
		return 0, 0, 0, errors.New("mark price invalid")
	}
	indexPrice, err = strconv.ParseFloat(r.Result.List[0].IndexPrice, 64)
	if err != nil || indexPrice <= 0 {
		return 0, 0, 0, errors.New("index price invalid")
	}
	fundingRate, err = strconv.ParseFloat(r.Result.List[0].FundingRate, 64)
	if err != nil {
		return 0, 0, 0, errors.New("funding rate invalid")
	}
	return markPrice, indexPrice, fundingRate, nil
}

func (c *BybitClient) FetchMarkPrice(ctx context.Context, symbol string, targetMinuteMs int64) (float64, error) {
	symbol = normalizeSymbol(symbol)
	q := url.Values{}
	q.Set("category", c.cfg.Category)
	q.Set("symbol", symbol)
	q.Set("interval", "1")
	q.Set("limit", "1")
	if targetMinuteMs > 0 {
		q.Set("start", strconv.FormatInt(targetMinuteMs, 10))
		q.Set("end", strconv.FormatInt(targetMinuteMs+ringMinuteMs-1, 10))
	}

	var r markPriceKlineResp
	if err := c.getJSON(ctx, "/v5/market/mark-price-kline", q, &r); err != nil {
		return 0, err
	}
	if r.RetCode != 0 || len(r.Result.List) == 0 || len(r.Result.List[0]) < 5 {
		return 0, fmt.Errorf("mark-price-kline retCode=%d retMsg=%s", r.RetCode, r.RetMsg)
	}
	price, err := strconv.ParseFloat(r.Result.List[0][4], 64)
	if err != nil || price <= 0 {
		return 0, errors.New("mark price invalid")
	}
	return price, nil
}

func (c *BybitClient) FetchIndexPrice(ctx context.Context, symbol string, targetMinuteMs int64) (float64, error) {
	symbol = normalizeSymbol(symbol)
	q := url.Values{}
	q.Set("category", c.cfg.Category)
	q.Set("symbol", symbol)
	q.Set("interval", "1")
	q.Set("limit", "1")
	if targetMinuteMs > 0 {
		q.Set("start", strconv.FormatInt(targetMinuteMs, 10))
		q.Set("end", strconv.FormatInt(targetMinuteMs+ringMinuteMs-1, 10))
	}

	var r indexPriceKlineResp
	if err := c.getJSON(ctx, "/v5/market/index-price-kline", q, &r); err != nil {
		return 0, err
	}
	if r.RetCode != 0 || len(r.Result.List) == 0 || len(r.Result.List[0]) < 5 {
		return 0, fmt.Errorf("index-price-kline retCode=%d retMsg=%s", r.RetCode, r.RetMsg)
	}
	price, err := strconv.ParseFloat(r.Result.List[0][4], 64)
	if err != nil || price <= 0 {
		return 0, errors.New("index price invalid")
	}
	return price, nil
}

func (c *BybitClient) FetchFundingRate(ctx context.Context, symbol string) (float64, error) {
	symbol = normalizeSymbol(symbol)
	q := url.Values{}
	q.Set("category", c.cfg.Category)
	q.Set("symbol", symbol)
	q.Set("limit", "1")

	var r fundingHistoryResp
	if err := c.getJSON(ctx, "/v5/market/funding/history", q, &r); err != nil {
		return 0, err
	}
	if r.RetCode != 0 || len(r.Result.List) == 0 {
		return 0, fmt.Errorf("funding-history retCode=%d retMsg=%s", r.RetCode, r.RetMsg)
	}
	fr, err := strconv.ParseFloat(r.Result.List[0].FundingRate, 64)
	if err != nil {
		return 0, errors.New("funding rate invalid")
	}
	return fr, nil
}

// Fetch all instruments for cfg.Category (for futures: set BYBIT_CATEGORY=linear)
// Returns only "Trading" symbols, sorted.
func (c *BybitClient) FetchAllSymbolsFromAPI(ctx context.Context) ([]string, error) {
	// Pagination via cursor; limit chosen high to reduce calls.
	limit := 1000
	cursor := ""

	seen := make(map[string]struct{})
	seenCursors := make(map[string]struct{})
	pages := 0
	for {
		pages++
		if pages > maxScannerCatalogPages {
			return nil, errors.New("instruments-info pagination exceeded safe page limit")
		}
		q := url.Values{}
		q.Set("category", c.cfg.Category)
		q.Set("limit", strconv.Itoa(limit))
		if cursor != "" {
			q.Set("cursor", cursor)
		}

		var r instrumentsInfoResp
		if err := c.getJSON(ctx, "/v5/market/instruments-info", q, &r); err != nil {
			return nil, err
		}
		if r.RetCode != 0 {
			return nil, fmt.Errorf("instruments-info retCode=%d retMsg=%s", r.RetCode, r.RetMsg)
		}

		for _, it := range r.Result.List {
			sym := strings.ToUpper(strings.TrimSpace(it.Symbol))
			if sym == "" {
				continue
			}
			// The linear category contains both perpetuals and dated futures.
			// Scanner requests must never mix those instrument types.
			isPerpetual := strings.EqualFold(strings.TrimSpace(it.ContractType), "LinearPerpetual") ||
				strings.EqualFold(strings.TrimSpace(it.ContractType), "InversePerpetual")
			if strings.EqualFold(strings.TrimSpace(it.Status), "Trading") && isPerpetual {
				seen[sym] = struct{}{}
				if len(seen) > maxScannerCatalogSymbols {
					return nil, errors.New("instruments-info catalog exceeds safe symbol limit")
				}
			}
		}

		cursor = strings.TrimSpace(r.Result.NextPageCursor)
		if cursor == "" {
			break
		}
		if _, duplicate := seenCursors[cursor]; duplicate {
			return nil, errors.New("instruments-info repeated pagination cursor")
		}
		seenCursors[cursor] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, errors.New("instruments-info returned 0 trading symbols (check BYBIT_CATEGORY)")
	}
	return out, nil
}

// ---- CVD state (per symbol) ----

type CVDStore struct {
	mu  sync.Mutex
	cvd map[string]float64
}

func NewCVDStore() *CVDStore {
	return &CVDStore{cvd: make(map[string]float64)}
}

func (s *CVDStore) Add(key InstrumentKey, delta float64) float64 {
	instrumentKey := key.String()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cvd[instrumentKey] += delta
	return s.cvd[instrumentKey]
}

// ---- Account ratio cache (per symbol) ----

type RatioStore struct {
	mu     sync.Mutex
	ratios map[string]RatioPair
}

type RatioPair struct {
	Long      float64
	Short     float64
	UpdatedAt time.Time
}

func NewRatioStore() *RatioStore {
	return &RatioStore{ratios: make(map[string]RatioPair)}
}

func (s *RatioStore) Set(key InstrumentKey, longRatio, shortRatio float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ratios[key.String()] = RatioPair{Long: longRatio, Short: shortRatio, UpdatedAt: time.Now().UTC()}
}

func (s *RatioStore) Get(key InstrumentKey) (RatioPair, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	instrumentKey := key.String()
	pair, ok := s.ratios[instrumentKey]
	if ok && time.Since(pair.UpdatedAt) > 15*time.Minute {
		delete(s.ratios, instrumentKey)
		return RatioPair{}, false
	}
	return pair, ok
}

// ---- Liquidations store (USD sum per symbol per minute) ----

type LiquidationStore struct {
	mu            sync.Mutex
	sums          map[string]map[int64]*LiquidationBucket
	connected     bool
	connectedAtMs int64
	coverage      []streamCoverageInterval
}

type streamCoverageInterval struct {
	startMs int64
	endMs   int64
}

const maxStreamCoverageIntervals = 64

type LiquidationBucket struct {
	Total     float64
	Long      float64
	Short     float64
	Count     float64
	Largest   float64
	Timestamp int64
}

func NewLiquidationStore() *LiquidationStore {
	return &LiquidationStore{sums: make(map[string]map[int64]*LiquidationBucket)}
}

func (s *LiquidationStore) SetConnected(connected bool) {
	s.mu.Lock()
	now := time.Now().UTC().UnixMilli()
	if connected && !s.connected {
		s.connectedAtMs = time.Now().UTC().UnixMilli()
	} else if !connected && s.connected && s.connectedAtMs > 0 {
		s.coverage = appendCoverageInterval(s.coverage, streamCoverageInterval{startMs: s.connectedAtMs, endMs: now})
	}
	s.connected = connected
	s.mu.Unlock()
}

func (s *LiquidationStore) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

func (s *LiquidationStore) CoversMinute(minuteStartMs int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return streamCoveredMinute(s.connected, s.connectedAtMs, s.coverage, minuteStartMs)
}

func appendCoverageInterval(intervals []streamCoverageInterval, next streamCoverageInterval) []streamCoverageInterval {
	if next.startMs <= 0 || next.endMs < next.startMs {
		return intervals
	}
	intervals = append(intervals, next)
	if len(intervals) > maxStreamCoverageIntervals {
		intervals = append([]streamCoverageInterval(nil), intervals[len(intervals)-maxStreamCoverageIntervals:]...)
	}
	return intervals
}

func streamCoveredMinute(connected bool, connectedAtMs int64, intervals []streamCoverageInterval, minuteStartMs int64) bool {
	if minuteStartMs <= 0 {
		return false
	}
	if connected && connectedAtMs > 0 && connectedAtMs <= minuteStartMs {
		return true
	}
	minuteEndMs := minuteStartMs + ringMinuteMs
	for index := len(intervals) - 1; index >= 0; index-- {
		interval := intervals[index]
		if interval.startMs <= minuteStartMs && interval.endMs >= minuteEndMs {
			return true
		}
		if interval.endMs < minuteStartMs {
			break
		}
	}
	return false
}

func (s *LiquidationStore) Add(key InstrumentKey, tsMs int64, usd float64, side string) {
	if tsMs <= 0 || usd <= 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return
	}
	if key.Symbol == "" {
		return
	}
	if !isShortLiquidation(side) && !isLongLiquidation(side) {
		return
	}
	instrumentKey := key.String()
	minKey := minuteKey(tsMs)
	s.mu.Lock()
	defer s.mu.Unlock()
	byMin := s.sums[instrumentKey]
	if byMin == nil {
		byMin = make(map[int64]*LiquidationBucket)
		s.sums[instrumentKey] = byMin
	}
	bucket := byMin[minKey]
	if bucket == nil {
		bucket = &LiquidationBucket{}
		byMin[minKey] = bucket
	}
	bucket.Total += usd
	bucket.Count++
	if usd > bucket.Largest {
		bucket.Largest = usd
	}
	if tsMs > bucket.Timestamp {
		bucket.Timestamp = tsMs
	}
	if isShortLiquidation(side) {
		bucket.Short += usd
	} else if isLongLiquidation(side) {
		bucket.Long += usd
	}
}

func (s *LiquidationStore) Get(key InstrumentKey, minStartMs int64) LiquidationBucket {
	minKey := minuteKey(minStartMs)
	s.mu.Lock()
	defer s.mu.Unlock()
	if byMin := s.sums[key.String()]; byMin != nil {
		if bucket := byMin[minKey]; bucket != nil {
			return *bucket
		}
	}
	return LiquidationBucket{}
}

func (s *LiquidationStore) Cleanup(olderThanMinKey int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sym, byMin := range s.sums {
		for k := range byMin {
			if k < olderThanMinKey {
				delete(byMin, k)
			}
		}
		if len(byMin) == 0 {
			delete(s.sums, sym)
		}
	}
}

func isShortLiquidation(side string) bool {
	return strings.EqualFold(side, "Sell")
}

func isLongLiquidation(side string) bool {
	return strings.EqualFold(side, "Buy")
}

func parseEventTimeMs(item liquidationItem) (int64, bool) {
	if item.T <= 0 {
		return 0, false
	}
	ts := item.T
	if ts > 0 && ts < 1_000_000_000_000 {
		ts *= 1000
	}
	return ts, true
}

// ---- Scan stats ----

type ScanStats struct {
	mu sync.Mutex

	TotalSymbols int
	Published    int
	PublishErr   int

	KlineOK  int
	KlineErr int

	DeltaOK          int
	DeltaErr         int
	DeltaUnavailable int

	OIOK  int
	OIErr int

	OrderbookOK  int
	OrderbookErr int

	TickersOK  int
	TickersErr int

	MarkOK  int
	MarkErr int

	IndexOK  int
	IndexErr int

	FundingOK  int
	FundingErr int

	LiqWithData int
	LiqEmpty    int

	ErrClient    int
	ErrServer    int
	ErrTransport int
	ErrOther     int
}

type ScanStatsSnapshot struct {
	TotalSymbols int
	Published    int
	PublishErr   int

	KlineOK  int
	KlineErr int

	DeltaOK          int
	DeltaErr         int
	DeltaUnavailable int

	OIOK  int
	OIErr int

	OrderbookOK  int
	OrderbookErr int

	TickersOK  int
	TickersErr int

	MarkOK  int
	MarkErr int

	IndexOK  int
	IndexErr int

	FundingOK  int
	FundingErr int

	LiqWithData int
	LiqEmpty    int

	ErrClient    int
	ErrServer    int
	ErrTransport int
	ErrOther     int
}

func (s *ScanStats) addPublished() {
	s.mu.Lock()
	s.Published++
	s.mu.Unlock()
}

func (s *ScanStats) addPublishErr(err error) {
	s.mu.Lock()
	s.PublishErr++
	s.mu.Unlock()
	s.addError(err)
}

func (s *ScanStats) addKlineOK() {
	s.mu.Lock()
	s.KlineOK++
	s.mu.Unlock()
}

func (s *ScanStats) addKlineErr(err error) {
	s.mu.Lock()
	s.KlineErr++
	s.mu.Unlock()
	s.addError(err)
}

func (s *ScanStats) addDeltaOK() {
	s.mu.Lock()
	s.DeltaOK++
	s.mu.Unlock()
}

func (s *ScanStats) addDeltaErr(err error) {
	s.mu.Lock()
	s.DeltaErr++
	s.mu.Unlock()
	s.addError(err)
}

func (s *ScanStats) addDeltaUnavailable() {
	s.mu.Lock()
	s.DeltaUnavailable++
	s.mu.Unlock()
}

func (s *ScanStats) addOIOK() {
	s.mu.Lock()
	s.OIOK++
	s.mu.Unlock()
}

func (s *ScanStats) addOIErr(err error) {
	s.mu.Lock()
	s.OIErr++
	s.mu.Unlock()
	s.addError(err)
}

func (s *ScanStats) addOrderbookOK() {
	s.mu.Lock()
	s.OrderbookOK++
	s.mu.Unlock()
}

func (s *ScanStats) addOrderbookErr(err error) {
	s.mu.Lock()
	s.OrderbookErr++
	s.mu.Unlock()
	s.addError(err)
}

func (s *ScanStats) addTickersOK() {
	s.mu.Lock()
	s.TickersOK++
	s.mu.Unlock()
}

func (s *ScanStats) addTickersErr(err error) {
	s.mu.Lock()
	s.TickersErr++
	s.mu.Unlock()
	s.addError(err)
}

func (s *ScanStats) addMarkOK() {
	s.mu.Lock()
	s.MarkOK++
	s.mu.Unlock()
}

func (s *ScanStats) addMarkErr(err error) {
	s.mu.Lock()
	s.MarkErr++
	s.mu.Unlock()
	s.addError(err)
}

func (s *ScanStats) addIndexOK() {
	s.mu.Lock()
	s.IndexOK++
	s.mu.Unlock()
}

func (s *ScanStats) addIndexErr(err error) {
	s.mu.Lock()
	s.IndexErr++
	s.mu.Unlock()
	s.addError(err)
}

func (s *ScanStats) addFundingOK() {
	s.mu.Lock()
	s.FundingOK++
	s.mu.Unlock()
}

func (s *ScanStats) addFundingErr(err error) {
	s.mu.Lock()
	s.FundingErr++
	s.mu.Unlock()
	s.addError(err)
}

func (s *ScanStats) addLiq(hasData bool) {
	s.mu.Lock()
	if hasData {
		s.LiqWithData++
	} else {
		s.LiqEmpty++
	}
	s.mu.Unlock()
}

func (s *ScanStats) addError(err error) {
	if err == nil {
		return
	}
	var httpErr HTTPStatusError
	if errors.As(err, &httpErr) {
		s.mu.Lock()
		if httpErr.Status >= 500 {
			s.ErrServer++
		} else if httpErr.Status >= 400 {
			s.ErrClient++
		} else {
			s.ErrOther++
		}
		s.mu.Unlock()
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		s.mu.Lock()
		s.ErrTransport++
		s.mu.Unlock()
		return
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		s.mu.Lock()
		s.ErrTransport++
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	s.ErrOther++
	s.mu.Unlock()
}

func (s *ScanStats) snapshot() ScanStatsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ScanStatsSnapshot{
		TotalSymbols:     s.TotalSymbols,
		Published:        s.Published,
		PublishErr:       s.PublishErr,
		KlineOK:          s.KlineOK,
		KlineErr:         s.KlineErr,
		DeltaOK:          s.DeltaOK,
		DeltaErr:         s.DeltaErr,
		DeltaUnavailable: s.DeltaUnavailable,
		OIOK:             s.OIOK,
		OIErr:            s.OIErr,
		OrderbookOK:      s.OrderbookOK,
		OrderbookErr:     s.OrderbookErr,
		TickersOK:        s.TickersOK,
		TickersErr:       s.TickersErr,
		MarkOK:           s.MarkOK,
		MarkErr:          s.MarkErr,
		IndexOK:          s.IndexOK,
		IndexErr:         s.IndexErr,
		FundingOK:        s.FundingOK,
		FundingErr:       s.FundingErr,
		LiqWithData:      s.LiqWithData,
		LiqEmpty:         s.LiqEmpty,
		ErrClient:        s.ErrClient,
		ErrServer:        s.ErrServer,
		ErrTransport:     s.ErrTransport,
		ErrOther:         s.ErrOther,
	}
}

// ---- Helpers ----

func round6(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*1e6) / 1e6
}

func minuteKey(tsMs int64) int64 { return tsMs / ringMinuteMs }

func normalizeSymbol(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	if s == "" {
		return ""
	}
	if strings.HasSuffix(s, "PERP") {
		base := strings.TrimSpace(strings.TrimSuffix(s, "PERP"))
		if base != "" {
			return base + "USDT"
		}
	}
	return s
}

func normalizeSymbolList(symbols []string) []string {
	seen := make(map[string]struct{}, len(symbols))
	out := make([]string, 0, len(symbols))
	for _, raw := range symbols {
		s := normalizeSymbol(raw)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func loadSymbolsFromFile(file string) ([]string, error) {
	filePath, err := safeLocalPath(".", file)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(b), "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, strings.ToUpper(l))
	}
	sort.Strings(out)
	return out, nil
}

func safeLocalPath(baseDir, name string) (string, error) {
	if name == "" {
		return "", errors.New("symbols file is empty")
	}
	if filepath.Base(name) != name {
		return "", fmt.Errorf("invalid symbols file: %s", name)
	}
	cleanBase, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", err
	}
	cleanPath, err := filepath.Abs(filepath.Join(cleanBase, name))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(cleanPath, cleanBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid symbols file: %s", name)
	}
	return cleanPath, nil
}

// Stable content-based sharding keeps every common symbol on the same shard
// even when the two servers fetched catalogs with a different insertion.
func shardSymbols(all []string, shardIndex, shardTotal int) ([]string, error) {
	if shardTotal <= 0 {
		return nil, errors.New("SHARD_TOTAL must be > 0")
	}
	if shardIndex < 0 || shardIndex >= shardTotal {
		return nil, fmt.Errorf("SHARD_INDEX must be in [0..%d]", shardTotal-1)
	}
	out := make([]string, 0, len(all)/shardTotal+1)
	for _, s := range all {
		if stableSymbolShard(s, shardTotal) == shardIndex {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out, nil
}

func stableSymbolShard(symbol string, shardTotal int) int {
	if shardTotal <= 1 {
		return 0
	}
	sum := sha256.Sum256([]byte(normalizeSymbol(symbol)))
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(shardTotal))
}

func loadConfig() Config {
	exchange := normalizeExchange(envStr("EXCHANGE", string(ExchangeBybit)))
	marketType := normalizeMarketType(envStr("MARKET_TYPE", string(MarketTypePerpetual)))
	baseURL := envStr("BYBIT_BASE_URL", "https://api.bybit.com")
	bybitCategory := "linear"
	bybitWSURL := "wss://stream.bybit.com/v5/public/linear"
	if marketType == MarketTypeSpot {
		bybitCategory = "spot"
		bybitWSURL = "wss://stream.bybit.com/v5/public/spot"
	}
	wsURL := envStr("BYBIT_WS_PUBLIC", bybitWSURL)
	category := envStr("BYBIT_CATEGORY", bybitCategory)
	if exchange == ExchangeBinance {
		baseURL = envStr("BINANCE_FUTURES_BASE_URL", "https://fapi.binance.com")
		wsURL = envStr("BINANCE_FUTURES_WS_URL", defaultBinanceFuturesWSURL)
		category = "linear"
	} else if exchange == ExchangeOKX {
		baseURL = envStr("OKX_BASE_URL", "https://www.okx.com")
		wsURL = envStr("OKX_PUBLIC_WS_URL", "wss://ws.okx.com:8443/ws/v5/public")
		category = "linear"
	} else if exchange == ExchangeBitget {
		baseURL = envStr("BITGET_BASE_URL", "https://api.bitget.com")
		wsURL = envStr("BITGET_PUBLIC_WS_URL", "wss://ws.bitget.com/v3/ws/public")
		category = "linear"
	} else if exchange == ExchangeGateIO {
		baseURL = envStr("GATEIO_BASE_URL", "https://api.gateio.ws")
		wsURL = envStr("GATEIO_FUTURES_WS_URL", "wss://fx-ws.gateio.ws/v4/ws/usdt")
		category = "linear"
	}
	cfg := Config{
		ShardTotal:            envInt("SHARD_TOTAL", 6),
		ShardIndex:            envInt("SHARD_INDEX", 0),
		Exchange:              string(exchange),
		MarketType:            string(marketType),
		BaseURL:               baseURL,
		Category:              category,
		WSURL:                 wsURL,
		WSPingIntervalSeconds: envInt("BYBIT_WS_PING_INTERVAL_SECONDS", 20),
		WSReadTimeoutSeconds:  envInt("BYBIT_WS_READ_TIMEOUT_SECONDS", 60),
		LiqLogIntervalSeconds: envInt("LIQ_LOG_INTERVAL_SECONDS", 60),
		LiqDebug:              envBool("LIQ_DEBUG", false),

		SymbolsMode:               strings.ToLower(envStr("SYMBOLS_MODE", "api")), // api|file
		SymbolsFile:               envStr("SYMBOLS_FILE", "symbols.txt"),
		SymbolsLoadTimeoutSeconds: envInt("SYMBOLS_LOAD_TIMEOUT_SECONDS", 35),
		SymbolsLoadRetries:        envInt("SYMBOLS_LOAD_RETRIES", 3),

		AlignToMinute: envBool("ALIGN_TO_MINUTE", true),

		// New defaults as requested
		RunTimeoutSeconds:        envInt("RUN_TIMEOUT_SECONDS", 55),
		CandleSettleDelaySeconds: envInt("CANDLE_SETTLE_DELAY_SECONDS", 2),

		Workers:    envInt("WORKERS", 16),
		RPS:        envFloat("RPS", 10), // more realistic default for 100 symbols/min
		Burst:      envInt("BURST", 20),
		TradeLimit: envInt("TRADE_LIMIT", 1000),

		OIInterval:           envStr("OI_INTERVAL", "5min"),
		FundingIntervalHours: envFloat("FUNDING_INTERVAL_HOURS", 8),
		OrderbookEnabled:     envBool("ORDERBOOK_ENABLED", false),

		PublisherType:         envStr("PUBLISHER", "stdout"), // stdout|redis
		RedisAddr:             envStr("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword:         envStr("REDIS_PASSWORD", ""),
		RedisDB:               envInt("REDIS_DB", 0),
		CompactRedisStream:    envStr("COMPACT_REDIS_STREAM", defaultCompactStream(exchange, marketType)),
		CompactRetentionHours: envInt("COMPACT_RETENTION_HOURS", 26),

		HTTPTimeout: time.Duration(envInt("HTTP_TIMEOUT_SECONDS", 8)) * time.Second,
	}

	if cfg.Workers <= 0 {
		cfg.Workers = 8
	}
	if cfg.RPS < 1 {
		cfg.RPS = 1
	}
	if cfg.Burst <= 0 {
		cfg.Burst = 10
	}
	if cfg.TradeLimit <= 0 {
		cfg.TradeLimit = 1000
	}
	if cfg.TradeLimit > 1000 {
		cfg.TradeLimit = 1000
	}
	if cfg.RunTimeoutSeconds <= 0 {
		cfg.RunTimeoutSeconds = 55
	}
	if cfg.CandleSettleDelaySeconds < 0 {
		cfg.CandleSettleDelaySeconds = 2
	}
	if cfg.CandleSettleDelaySeconds > 30 {
		cfg.CandleSettleDelaySeconds = 30
	}
	maxRunSeconds := 59 - cfg.CandleSettleDelaySeconds
	if maxRunSeconds < 1 {
		maxRunSeconds = 1
	}
	if cfg.RunTimeoutSeconds > maxRunSeconds {
		cfg.RunTimeoutSeconds = maxRunSeconds
	}
	if cfg.WSPingIntervalSeconds <= 0 {
		cfg.WSPingIntervalSeconds = 20
	}
	if cfg.WSReadTimeoutSeconds <= 0 {
		cfg.WSReadTimeoutSeconds = 60
	}
	if cfg.LiqLogIntervalSeconds < 0 {
		cfg.LiqLogIntervalSeconds = 0
	}

	if cfg.SymbolsMode != "api" && cfg.SymbolsMode != "file" {
		cfg.SymbolsMode = "api"
	}
	if cfg.SymbolsLoadTimeoutSeconds <= 0 {
		cfg.SymbolsLoadTimeoutSeconds = 35
	}
	if cfg.SymbolsLoadRetries <= 0 {
		cfg.SymbolsLoadRetries = 1
	}
	if cfg.CompactRetentionHours < 26 {
		cfg.CompactRetentionHours = 26
	}

	return cfg
}

func defaultCompactStream(exchange Exchange, marketType MarketType) string {
	prefix := "candles"
	if isLiquidationOnlyExchange(exchange) {
		prefix = "liquidations"
	}
	return fmt.Sprintf("%s:v3:%s:%s", prefix, exchange, marketType)
}

func envStr(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}
func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}
func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "y" || v == "on"
}
func envFloat(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func makePublisher(ctx context.Context, cfg Config) (Publisher, error) {
	switch strings.ToLower(cfg.PublisherType) {
	case "stdout":
		return &StdoutPublisher{}, nil
	case "redis":
		if strings.TrimSpace(cfg.CompactRedisStream) == "" {
			return nil, errors.New("COMPACT_REDIS_STREAM is required")
		}
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
			PoolSize: 50,
		})
		if err := rdb.Ping(ctx).Err(); err != nil {
			return nil, err
		}
		return &RedisStreamPublisher{rdb: rdb, stream: cfg.CompactRedisStream, retention: time.Duration(cfg.CompactRetentionHours) * time.Hour, closeClient: true}, nil
	default:
		return nil, fmt.Errorf("unknown PUBLISHER=%s (use stdout|redis)", cfg.PublisherType)
	}
}

// ---- Bybit public trades + liquidations WS adapter ----

func (a *BybitAdapter) RunStreams(ctx context.Context, symbols []string, trades *TradeStore, store *LiquidationStore) error {
	return connectAndConsumeBybitStreams(ctx, a, trades, store, symbols)
}

func connectAndConsumeBybitStreams(ctx context.Context, adapter *BybitAdapter, trades *TradeStore, store *LiquidationStore, symbols []string) error {
	cfg := adapter.cfg
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS12},
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(cfg.WSURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	var writeMu sync.Mutex
	safeWrite := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(v)
	}

	conn.SetReadLimit(4 << 20)
	readTimeout := time.Duration(cfg.WSReadTimeoutSeconds) * time.Second
	const subscriptionAckTimeout = 15 * time.Second
	subscriptionDeadline := time.Now().Add(subscriptionAckTimeout)
	var subscriptionReady atomic.Bool
	_ = conn.SetReadDeadline(subscriptionDeadline)
	conn.SetPongHandler(func(string) error {
		if !subscriptionReady.Load() {
			return conn.SetReadDeadline(subscriptionDeadline)
		}
		return conn.SetReadDeadline(time.Now().Add(readTimeout))
	})

	subscriptionIDs, err := subscribeBybitStreams(conn, symbols)
	if err != nil {
		return err
	}
	subscriptionAcks, err := newBybitSubscriptionAcks(subscriptionIDs)
	if err != nil {
		return err
	}
	defer store.SetConnected(false)
	defer trades.SetConnected(false)

	var msgCount atomic.Uint64
	var subAckCount atomic.Uint64
	var subErrCount atomic.Uint64
	var lastMsgUnixMs atomic.Int64
	lastMsgUnixMs.Store(time.Now().UTC().UnixMilli())
	debugLeft := 0
	if cfg.LiqDebug {
		debugLeft = 5
	}
	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()
	go func() {
		<-connCtx.Done()
		_ = conn.Close()
	}()
	if cfg.LiqLogIntervalSeconds > 0 {
		go func() {
			t := time.NewTicker(time.Duration(cfg.LiqLogIntervalSeconds) * time.Second)
			defer t.Stop()
			for {
				select {
				case <-connCtx.Done():
					return
				case <-t.C:
					lastMsgAt := time.UnixMilli(lastMsgUnixMs.Load()).UTC()
					log.Printf("liquidations ws stats msgs=%d last=%s subAck=%d subErr=%d symbols=%d",
						msgCount.Load(), lastMsgAt.Format(time.RFC3339), subAckCount.Load(), subErrCount.Load(), len(symbols))
				}
			}
		}()
	}

	go func() {
		pingInterval := time.Duration(cfg.WSPingIntervalSeconds) * time.Second
		if pingInterval <= 0 {
			pingInterval = 20 * time.Second
		}
		t := time.NewTicker(pingInterval)
		defer t.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-t.C:
				_ = safeWrite(map[string]string{"op": "ping"})
				_ = conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
			}
		}
	}()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if subscriptionReady.Load() {
			_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		} else {
			_ = conn.SetReadDeadline(subscriptionDeadline)
		}

		var m wsMessage
		if err := json.Unmarshal(msg, &m); err != nil {
			continue
		}
		if strings.EqualFold(m.Op, "ping") {
			payload := map[string]string{"op": "pong"}
			if strings.TrimSpace(m.ReqID) != "" {
				payload["req_id"] = m.ReqID
			}
			_ = safeWrite(payload)
			continue
		}
		if strings.EqualFold(m.Op, "pong") {
			continue
		}
		if strings.EqualFold(m.Op, "subscribe") {
			ready, ackErr := subscriptionAcks.observe(m)
			if ackErr != nil {
				subErrCount.Add(1)
				return ackErr
			}
			subAckCount.Add(1)
			if ready {
				subscriptionReady.Store(true)
				store.SetConnected(true)
				trades.SetConnected(true)
				_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
			}
			continue
		}
		if strings.HasPrefix(m.Topic, "publicTrade.") {
			var items []struct {
				Timestamp int64  `json:"T"`
				Symbol    string `json:"s"`
				Side      string `json:"S"`
				Size      string `json:"v"`
				Price     string `json:"p"`
			}
			if json.Unmarshal(m.Data, &items) != nil {
				continue
			}
			topicSymbol := normalizeSymbol(strings.TrimPrefix(m.Topic, "publicTrade."))
			for _, item := range items {
				symbol := normalizeSymbol(item.Symbol)
				if symbol == "" {
					symbol = topicSymbol
				}
				price, _ := strconv.ParseFloat(item.Price, 64)
				size, _ := strconv.ParseFloat(item.Size, 64)
				trades.Add(adapter.Instrument(symbol), item.Timestamp, price, size, item.Side, 1)
			}
			continue
		}
		if m.Topic == "" || !strings.HasPrefix(m.Topic, "allLiquidation.") {
			continue
		}

		items := parseLiquidationItems(m.Data)
		if len(items) == 0 {
			continue
		}
		msgCount.Add(uint64(len(items)))
		lastMsgUnixMs.Store(time.Now().UTC().UnixMilli())

		topicSymbol := normalizeSymbol(strings.TrimPrefix(m.Topic, "allLiquidation."))
		for _, it := range items {
			sym := normalizeSymbol(it.Symbol)
			if sym == "" {
				sym = topicSymbol
			}
			ts, ok := parseEventTimeMs(it)
			if !ok {
				if cfg.LiqDebug && debugLeft > 0 {
					log.Printf("liq debug invalid time topic=%s item=%+v", m.Topic, it)
					debugLeft--
				}
				continue
			}
			price, _ := strconv.ParseFloat(it.Price, 64)
			size, _ := strconv.ParseFloat(it.Size, 64)
			usd := price * size
			if cfg.LiqDebug && debugLeft > 0 {
				log.Printf("liq debug topic=%s sym=%s side=%s size=%s price=%s usd=%.6f ts=%d", m.Topic, sym, it.Side, it.Size, it.Price, usd, ts)
				debugLeft--
			}
			if usd == 0 && cfg.LiqDebug && debugLeft > 0 {
				log.Printf("liq debug zero-usd topic=%s item=%+v", m.Topic, it)
				debugLeft--
			}
			store.Add(adapter.Instrument(sym), ts, usd, it.Side)
		}
	}
}

func subscribeBybitStreams(conn *websocket.Conn, symbols []string) ([]string, error) {
	const batchSize = 20
	requestIDs := make([]string, 0, (len(symbols)+batchSize-1)/batchSize)
	for i := 0; i < len(symbols); i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}
		args := make([]string, 0, (end-i)*2)
		seen := make(map[string]struct{}, end-i)
		for _, s := range symbols[i:end] {
			s = normalizeSymbol(s)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			args = append(args, "publicTrade."+s, "allLiquidation."+s)
		}
		if len(args) == 0 {
			continue
		}
		requestID := fmt.Sprintf("subscribe-%d", len(requestIDs)+1)
		if err := conn.WriteJSON(map[string]any{"op": "subscribe", "req_id": requestID, "args": args}); err != nil {
			return nil, err
		}
		requestIDs = append(requestIDs, requestID)
	}
	if len(requestIDs) == 0 {
		return nil, fmt.Errorf("%w: no valid symbols", errBybitSubscription)
	}
	return requestIDs, nil
}

func parseLiquidationItems(raw json.RawMessage) []liquidationItem {
	if len(raw) == 0 {
		return nil
	}
	var list []liquidationItem
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	var one liquidationItem
	if err := json.Unmarshal(raw, &one); err == nil {
		return []liquidationItem{one}
	}
	return nil
}

// ---- Scan loop ----

type job struct {
	symbol string
}

type result struct {
	symbol string
	out    CandleOut
	err    error
}

func main() {
	cfg := loadConfig()
	log.Printf("scanner starting exchange=%s marketType=%s shard=%d/%d publisher=%s symbolsMode=%s",
		cfg.Exchange, cfg.MarketType, cfg.ShardIndex, cfg.ShardTotal, cfg.PublisherType, cfg.SymbolsMode)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pub, err := makePublisher(ctx, cfg)
	if err != nil {
		log.Fatalf("publisher: %v", err)
	}
	if pub != nil {
		defer pub.Close()
	}

	if isLiquidationOnlyExchange(normalizeExchange(cfg.Exchange)) {
		if err := runLiquidationOnlyScanner(ctx, cfg, pub); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatalf("liquidation-only scanner: %v", err)
		}
		return
	}

	adapter, err := newMarketAdapter(cfg)
	if err != nil {
		log.Fatalf("market adapter: %v", err)
	}
	cvd := NewCVDStore()
	trades := NewTradeStore()
	liq := NewLiquidationStore()
	ratioStore := NewRatioStore()

	// --- Load symbols (API or file) ---
	var allSyms []string
	switch cfg.SymbolsMode {
	case "file":
		allSyms, err = loadSymbolsFromFile(cfg.SymbolsFile)
		if err != nil {
			log.Fatalf("load symbols from file: %v", err)
		}
	case "api":
		// give API more time for pagination, with small retries on slow responses
		var lastErr error
		retries := cfg.SymbolsLoadRetries
		if retries < 1 {
			retries = 1
		}
		for attempt := 1; attempt <= retries; attempt++ {
			loadCtx, loadCancel := context.WithTimeout(ctx, time.Duration(cfg.SymbolsLoadTimeoutSeconds)*time.Second)
			allSyms, err = adapter.FetchAllSymbolsFromAPI(loadCtx)
			loadCancel()
			if err == nil {
				lastErr = nil
				break
			}
			lastErr = err
			if attempt < retries {
				log.Printf("warn load symbols attempt=%d/%d err=%v", attempt, retries, err)
				time.Sleep(time.Duration(2*attempt) * time.Second)
			}
		}
		if lastErr != nil {
			log.Fatalf("load symbols from api: %v", lastErr)
		}
	default:
		log.Fatalf("unknown SYMBOLS_MODE=%s", cfg.SymbolsMode)
	}

	allSyms = normalizeSymbolList(allSyms)
	if len(allSyms) == 0 || len(allSyms) > maxScannerCatalogSymbols {
		log.Fatalf("symbol catalog size %d is outside 1..%d", len(allSyms), maxScannerCatalogSymbols)
	}

	syms, err := shardSymbols(allSyms, cfg.ShardIndex, cfg.ShardTotal)
	if err != nil {
		log.Fatalf("shard symbols: %v", err)
	}

	log.Printf("symbols total=%d shardSymbols=%d", len(allSyms), len(syms))

	go runAdapterStreams(ctx, adapter, trades, liq, syms)

	runTimeout := time.Duration(cfg.RunTimeoutSeconds) * time.Second
	settleDelay := time.Duration(cfg.CandleSettleDelaySeconds) * time.Second
	nextRun := time.Now().UTC()
	if cfg.AlignToMinute {
		nextRun = nextRun.Truncate(time.Minute).Add(time.Minute).Add(settleDelay)
	}

	for {
		if !waitUntil(ctx, nextRun) {
			return
		}
		start := time.Now().UTC()
		targetMinute := start.Truncate(time.Minute).Add(-time.Minute)
		if cfg.AlignToMinute {
			targetMinute = nextRun.Truncate(time.Minute).Add(-time.Minute)
		}

		runCtx, runCancel := context.WithTimeout(ctx, runTimeout)
		err := scanOnce(runCtx, cfg, adapter, pub, cvd, trades, liq, ratioStore, syms, targetMinute.UnixMilli())
		runCancel()

		complete := time.Now().UTC()
		log.Printf("scan cycle complete targetMinute=%s duration=%s err=%v",
			targetMinute.Format(time.RFC3339), complete.Sub(start), err)

		if !cfg.AlignToMinute {
			nextRun = complete.Add(time.Minute)
			continue
		}
		nextRun = nextRun.Add(time.Minute)
		if !nextRun.After(complete) {
			skipped := int(complete.Sub(nextRun)/time.Minute) + 1
			log.Printf("scanner fell behind schedule; skipping %d stale cycle(s)", skipped)
			nextRun = nextRun.Add(time.Duration(skipped) * time.Minute)
		}
	}
}

func waitUntil(ctx context.Context, target time.Time) bool {
	delay := time.Until(target)
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func scanOnce(ctx context.Context, cfg Config, adapter MarketAdapter, pub Publisher, cvd *CVDStore, trades *TradeStore, liq *LiquidationStore, ratioStore *RatioStore, symbols []string, targetMinuteMs int64) error {
	scanCtx, cancelScan := context.WithCancel(ctx)
	defer cancelScan()
	defer cleanupScanStores(trades, liq, targetMinuteMs)
	jobs := make(chan job)
	results := make(chan result, cfg.Workers)
	stats := &ScanStats{TotalSymbols: len(symbols)}
	if adapter.Exchange() == ExchangeBinance && (trades == nil || !trades.CoversMinute(targetMinuteMs)) {
		log.Printf("binance trade websocket warming up targetMinute=%s; delta metrics will be marked unavailable for this cycle", time.UnixMilli(targetMinuteMs).UTC().Format(time.RFC3339))
	}

	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for j := range jobs {
			out, err := fetchMetricsWithRetries(scanCtx, cfg, adapter, cvd, trades, liq, ratioStore, stats, j.symbol, targetMinuteMs)
			if isBinanceRateLimitError(err) {
				cancelScan()
			}
			select {
			case results <- result{symbol: j.symbol, out: out, err: err}:
			case <-ctx.Done():
				return
			}
		}
	}

	// start workers
	wg.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		go worker()
	}

	// enqueue jobs
	go func() {
		defer close(jobs)
		for _, s := range symbols {
			select {
			case <-scanCtx.Done():
				return
			case jobs <- job{symbol: s}:
			}
		}
	}()

	// Close results only after every worker has finished sending.
	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	compactOutputs := make([]CandleOut, 0, len(symbols))

	for r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			log.Printf("warn symbol=%s: %v", r.symbol, r.err)
			continue
		}
		compactOutputs = append(compactOutputs, r.out)
	}
	if pub != nil {
		batch, err := buildCompactCandleBatch(compactOutputs, symbols, targetMinuteMs, cfg.ShardIndex, cfg.ShardTotal, cfg.Exchange, cfg.MarketType)
		if err != nil {
			log.Printf("compact candle build failed minute=%d err=%v", targetMinuteMs, err)
			if firstErr == nil {
				firstErr = err
			}
		} else if raw, marshalErr := json.Marshal(batch); marshalErr != nil {
			log.Printf("compact candle encode failed minute=%d err=%v", targetMinuteMs, marshalErr)
			if firstErr == nil {
				firstErr = marshalErr
			}
		} else if publishErr := pub.Publish(ctx, raw); publishErr != nil {
			log.Printf("compact candle publish failed minute=%d err=%v", targetMinuteMs, publishErr)
			stats.addPublishErr(publishErr)
			if firstErr == nil {
				firstErr = publishErr
			}
		} else {
			stats.addPublished()
			log.Printf("compact candle published minute=%d rows=%d catalog=%d coverage=%v bytes=%d", targetMinuteMs, len(batch.CandleRows), len(batch.Catalog), batch.Coverage, len(raw))
		}
	}
	if firstErr == nil && ctx.Err() != nil {
		firstErr = ctx.Err()
	}
	log.Printf("scan minute complete compact_rows=%d/%d", len(compactOutputs), len(symbols))
	logScanStats(stats)
	return firstErr
}

// cleanupScanStores runs exactly once after a minute scan. Keeping it out of
// fetchMetricsOnce avoids repeatedly walking every symbol bucket from each
// concurrent symbol worker.
func cleanupScanStores(trades *TradeStore, liq *LiquidationStore, targetMinuteMs int64) {
	olderThan := minuteKey(targetMinuteMs) - 120
	if trades != nil {
		trades.Cleanup(olderThan)
	}
	if liq != nil {
		liq.Cleanup(olderThan)
	}
}

func fetchMetricsWithRetries(ctx context.Context, cfg Config, adapter MarketAdapter, cvd *CVDStore, trades *TradeStore, liq *LiquidationStore, ratioStore *RatioStore, stats *ScanStats, symbol string, targetMinuteMs int64) (CandleOut, error) {
	var lastErr error
	backoff := 150 * time.Millisecond

	for attempt := 1; attempt <= 3; attempt++ {
		out, err := fetchMetricsOnce(ctx, cfg, adapter, cvd, trades, liq, ratioStore, stats, symbol, targetMinuteMs)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if isBinanceRateLimitError(err) {
			break
		}

		if ctx.Err() != nil {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
	}
	if stats != nil {
		stats.addKlineErr(lastErr)
	}
	return CandleOut{}, lastErr
}

func fetchMetricsOnce(ctx context.Context, cfg Config, adapter MarketAdapter, cvd *CVDStore, trades *TradeStore, liq *LiquidationStore, ratioStore *RatioStore, stats *ScanStats, symbol string, targetMinuteMs int64) (CandleOut, error) {
	instrument := adapter.Instrument(symbol)
	// 1) Kline: OHLCV + minuteStartMs
	minStartMs, open, high, low, close, vol, quoteVolumeUsd, err := adapter.FetchKline1m(ctx, symbol, targetMinuteMs)
	if err != nil {
		return CandleOut{}, fmt.Errorf("kline: %w", err)
	}
	if stats != nil {
		stats.addKlineOK()
	}
	invalid := make(map[string]struct{})
	invalidate := func(names ...string) {
		for _, name := range names {
			invalid[name] = struct{}{}
		}
	}

	// 2) Delta within that minute (best-effort)
	tradeStats := TradeStats{}
	if trades != nil && trades.CoversMinute(minStartMs) {
		tradeStats, _ = trades.Get(instrument, minStartMs)
		err = nil
	} else if adapter.Exchange() == ExchangeBinance {
		// A REST aggTrades recovery for every Binance symbol has a high request
		// weight and can ban the shared server IP. Keep the minute explicitly
		// invalid until the WebSocket has full coverage.
		err = errBinanceTradeMinuteUnavailable
	} else {
		tradeStats, err = adapter.FetchDeltaForMinute(ctx, symbol, minStartMs, cfg.TradeLimit)
	}
	tradeMetrics := []string{
		"delta", "cvd", "tradeLastPrice", "tradeLastSize", "tradeLastSide",
		"tradeBuyVolume", "tradeSellVolume", "tradeBuyUsd", "tradeSellUsd", "tradeCount", "largestTradeUsd",
		"medianTradeUsd", "largestTradeSide", "tradeClusterUsd", "tradeClusterCount", "tradeClusterSide", "tradePriceImpactPct",
	}
	if err != nil {
		if !errors.Is(err, errBinanceTradeMinuteUnavailable) {
			log.Printf("warn symbol=%s delta err: %v", symbol, err)
		}
		if stats != nil {
			if errors.Is(err, errBinanceTradeMinuteUnavailable) {
				stats.addDeltaUnavailable()
			} else {
				stats.addDeltaErr(err)
			}
		}
		tradeStats = TradeStats{}
		invalidate(tradeMetrics...)
	} else if !tradeStats.Complete {
		incompleteErr := fmt.Errorf("recent trades do not cover full minute (limit=%d)", cfg.TradeLimit)
		log.Printf("warn symbol=%s delta incomplete: %v", symbol, incompleteErr)
		if stats != nil {
			stats.addDeltaErr(incompleteErr)
		}
		invalidate(tradeMetrics...)
	} else if stats != nil {
		stats.addDeltaOK()
	}
	if tradeStats.Complete && tradeStats.Count == 0 {
		invalidate("tradeLastPrice", "tradeLastSize", "tradeLastSide", "medianTradeUsd", "largestTradeSide", "tradeClusterSide", "tradePriceImpactPct")
	}

	// 3) Open interest (best-effort; 5min step)
	oi, oiTimestamp, err := adapter.FetchOpenInterest(ctx, symbol, minStartMs)
	if err != nil {
		log.Printf("warn symbol=%s oi err: %v", symbol, err)
		if stats != nil {
			stats.addOIErr(err)
		}
		oi = 0
		oiTimestamp = 0
		invalidate("openInterest")
	} else if stats != nil {
		stats.addOIOK()
	}

	// 3.1) Long/short account ratio (best-effort)
	longRatio, shortRatio, ratioErr := adapter.FetchAccountRatio(ctx, symbol, cfg.OIInterval, minStartMs)
	ratioSource := "rest"
	ratioTimestamp := time.Now().UTC().UnixMilli()
	if ratioErr != nil {
		log.Printf("warn symbol=%s account ratio err: %v", symbol, ratioErr)
		cachedOK := false
		if ratioStore != nil {
			if cached, ok := ratioStore.Get(instrument); ok {
				longRatio = cached.Long
				shortRatio = cached.Short
				ratioSource = "cache"
				ratioTimestamp = cached.UpdatedAt.UnixMilli()
				cachedOK = true
			}
		}
		if !cachedOK {
			longRatio = 0
			shortRatio = 0
			invalidate("longRatio", "shortRatio")
		}
	} else if ratioStore != nil {
		ratioStore.Set(instrument, longRatio, shortRatio)
	}

	// 4) Orderbook (best-effort)
	obBid := 0.0
	obAsk := 0.0
	obBidDepth := 0.0
	obAskDepth := 0.0
	if cfg.OrderbookEnabled {
		obBid, obAsk, obBidDepth, obAskDepth, err = adapter.FetchOrderbook(ctx, symbol, 1)
		if err != nil {
			log.Printf("warn symbol=%s orderbook err: %v", symbol, err)
			if stats != nil {
				stats.addOrderbookErr(err)
			}
			obBid = 0
			obAsk = 0
			obBidDepth = 0
			obAskDepth = 0
			invalidate("orderbookBid", "orderbookAsk", "orderbookSpread", "orderbookBidDepth", "orderbookAskDepth")
		} else if stats != nil {
			stats.addOrderbookOK()
		}
	} else {
		invalidate("orderbookBid", "orderbookAsk", "orderbookSpread", "orderbookBidDepth", "orderbookAskDepth")
	}

	// 5) Tickers (best-effort)
	markPrice, indexPrice, fundingRate, err := adapter.FetchTickers(ctx, symbol)
	tickersOK := err == nil
	if err != nil {
		log.Printf("warn symbol=%s tickers err: %v", symbol, err)
		if stats != nil {
			stats.addTickersErr(err)
		}
		markPrice = 0
		indexPrice = 0
		fundingRate = 0
	} else if stats != nil {
		stats.addTickersOK()
	}
	if tickersOK {
		if markPrice <= 0 || math.IsNaN(markPrice) || math.IsInf(markPrice, 0) {
			invalidate("markPrice")
		}
		if indexPrice <= 0 || math.IsNaN(indexPrice) || math.IsInf(indexPrice, 0) {
			invalidate("indexPrice")
		}
		if math.IsNaN(fundingRate) || math.IsInf(fundingRate, 0) {
			invalidate("fundingRate")
		}
	}

	// 6) Mark price (best-effort; fallback only if tickers failed)
	if !tickersOK {
		markPrice, err = adapter.FetchMarkPrice(ctx, symbol, minStartMs)
		if err != nil {
			log.Printf("warn symbol=%s mark price err: %v", symbol, err)
			if stats != nil {
				stats.addMarkErr(err)
			}
			markPrice = 0
			invalidate("markPrice")
		} else if stats != nil {
			stats.addMarkOK()
		}
	}

	// 7) Index price (best-effort; fallback only if tickers failed)
	if !tickersOK {
		indexPrice, err = adapter.FetchIndexPrice(ctx, symbol, minStartMs)
		if err != nil {
			log.Printf("warn symbol=%s index price err: %v", symbol, err)
			if stats != nil {
				stats.addIndexErr(err)
			}
			indexPrice = 0
			invalidate("indexPrice")
		} else if stats != nil {
			stats.addIndexOK()
		}
	}

	// 8) Funding rate (best-effort; fallback only if tickers failed)
	if !tickersOK {
		fundingRate, err = adapter.FetchFundingRate(ctx, symbol)
		if err != nil {
			log.Printf("warn symbol=%s funding err: %v", symbol, err)
			if stats != nil {
				stats.addFundingErr(err)
			}
			fundingRate = 0
			invalidate("fundingRate")
		} else if stats != nil {
			stats.addFundingOK()
		}
	}

	// 9) Liquidations (best-effort; from WS cache)
	liqTotal := 0.0
	liqLong := 0.0
	liqShort := 0.0
	liqCount := 0.0
	liqLargest := 0.0
	liqTimestamp := minStartMs + ringMinuteMs - 1
	if liq != nil && liq.CoversMinute(minStartMs) {
		bucket := liq.Get(instrument, minStartMs)
		liqTotal = bucket.Total
		liqLong = bucket.Long
		liqShort = bucket.Short
		liqCount = bucket.Count
		liqLargest = bucket.Largest
		if bucket.Timestamp > 0 {
			liqTimestamp = bucket.Timestamp
		}
	} else {
		invalidate("liquidations", "liquidationsLong", "liquidationsShort", "liquidationCount", "largestLiquidation")
	}
	if stats != nil {
		stats.addLiq(liqTotal > 0)
	}

	cvdNow := 0.0
	if _, bad := invalid["cvd"]; !bad {
		cvdNow = cvd.Add(instrument, tradeStats.Delta)
	}
	observedAt := time.Now().UTC().UnixMilli()
	restSource := fmt.Sprintf("%s:rest", adapter.Exchange())
	tradeSource := fmt.Sprintf("%s:%s", adapter.Exchange(), tradeStats.Source)
	if tradeStats.Source == "" {
		tradeSource = restSource
	}
	liquidationSource := fmt.Sprintf("%s:websocket", adapter.Exchange())
	if adapter.Exchange() == ExchangeBinance {
		liquidationSource = "binance:websocket_snapshot"
	}
	metric := func(name string, value float64, timestamp int64, source string) MetricValue {
		_, isInvalid := invalid[name]
		if math.IsNaN(value) || math.IsInf(value, 0) || timestamp <= 0 || strings.TrimSpace(source) == "" {
			isInvalid = true
			invalid[name] = struct{}{}
		}
		return MetricValue{Value: round6(value), Valid: !isInvalid, Timestamp: timestamp, Source: source}
	}
	tradeTimestamp := tradeStats.Timestamp
	if tradeTimestamp <= 0 {
		tradeTimestamp = minStartMs + ringMinuteMs - 1
	}
	metrics := map[string]MetricValue{
		"open":                 metric("open", open, minStartMs, restSource),
		"high":                 metric("high", high, minStartMs, restSource),
		"low":                  metric("low", low, minStartMs, restSource),
		"close":                metric("close", close, minStartMs, restSource),
		"volume":               metric("volume", vol, minStartMs, restSource),
		"quoteVolumeUsd":       metric("quoteVolumeUsd", quoteVolumeUsd, minStartMs, restSource),
		"delta":                metric("delta", tradeStats.Delta, tradeTimestamp, tradeSource),
		"cvd":                  metric("cvd", cvdNow, tradeTimestamp, tradeSource),
		"openInterest":         metric("openInterest", oi, oiTimestamp, restSource),
		"fundingRate":          metric("fundingRate", fundingRate, observedAt, restSource),
		"markPrice":            metric("markPrice", markPrice, observedAt, restSource),
		"indexPrice":           metric("indexPrice", indexPrice, observedAt, restSource),
		"orderbookBid":         metric("orderbookBid", obBid, observedAt, restSource),
		"orderbookAsk":         metric("orderbookAsk", obAsk, observedAt, restSource),
		"orderbookSpread":      metric("orderbookSpread", obAsk-obBid, observedAt, restSource),
		"orderbookBidDepth":    metric("orderbookBidDepth", obBidDepth, observedAt, restSource),
		"orderbookAskDepth":    metric("orderbookAskDepth", obAskDepth, observedAt, restSource),
		"tradeLastPrice":       metric("tradeLastPrice", tradeStats.LastPrice, tradeTimestamp, tradeSource),
		"tradeLastSize":        metric("tradeLastSize", tradeStats.LastSize, tradeTimestamp, tradeSource),
		"tradeLastSide":        metric("tradeLastSide", tradeStats.LastSide, tradeTimestamp, tradeSource),
		"tradeBuyVolume":       metric("tradeBuyVolume", tradeStats.BuyVolume, tradeTimestamp, tradeSource),
		"tradeSellVolume":      metric("tradeSellVolume", tradeStats.SellVolume, tradeTimestamp, tradeSource),
		"tradeBuyUsd":          metric("tradeBuyUsd", tradeStats.BuyUSD, tradeTimestamp, tradeSource),
		"tradeSellUsd":         metric("tradeSellUsd", tradeStats.SellUSD, tradeTimestamp, tradeSource),
		"tradeCount":           metric("tradeCount", tradeStats.Count, tradeTimestamp, tradeSource),
		"largestTradeUsd":      metric("largestTradeUsd", tradeStats.LargestUSD, tradeTimestamp, tradeSource),
		"medianTradeUsd":       metric("medianTradeUsd", tradeStats.MedianUSD, tradeTimestamp, tradeSource),
		"largestTradeSide":     metric("largestTradeSide", tradeStats.LargestSide, tradeTimestamp, tradeSource),
		"tradeClusterUsd":      metric("tradeClusterUsd", tradeStats.ClusterUSD, tradeTimestamp, tradeSource),
		"tradeClusterCount":    metric("tradeClusterCount", tradeStats.ClusterCount, tradeTimestamp, tradeSource),
		"tradeClusterSide":     metric("tradeClusterSide", tradeStats.ClusterSide, tradeTimestamp, tradeSource),
		"tradePriceImpactPct":  metric("tradePriceImpactPct", tradeStats.PriceImpactPct, tradeTimestamp, tradeSource),
		"longRatio":            metric("longRatio", longRatio, ratioTimestamp, fmt.Sprintf("%s:%s", adapter.Exchange(), ratioSource)),
		"shortRatio":           metric("shortRatio", shortRatio, ratioTimestamp, fmt.Sprintf("%s:%s", adapter.Exchange(), ratioSource)),
		"liquidations":         metric("liquidations", liqTotal, liqTimestamp, liquidationSource),
		"liquidationsLong":     metric("liquidationsLong", liqLong, liqTimestamp, liquidationSource),
		"liquidationsShort":    metric("liquidationsShort", liqShort, liqTimestamp, liquidationSource),
		"liquidationCount":     metric("liquidationCount", liqCount, liqTimestamp, liquidationSource),
		"largestLiquidation":   metric("largestLiquidation", liqLargest, liqTimestamp, liquidationSource),
		"fundingIntervalHours": metric("fundingIntervalHours", cfg.FundingIntervalHours, observedAt, "config"),
	}
	if shortRatio > 0 {
		metrics["longShortRatio"] = metric("longShortRatio", longRatio/shortRatio, ratioTimestamp, fmt.Sprintf("%s:%s", adapter.Exchange(), ratioSource))
	} else {
		invalid["longShortRatio"] = struct{}{}
		metrics["longShortRatio"] = MetricValue{Valid: false, Timestamp: ratioTimestamp, Source: fmt.Sprintf("%s:%s", adapter.Exchange(), ratioSource)}
	}
	invalidMetrics := make([]string, 0, len(invalid))
	for name := range invalid {
		invalidMetrics = append(invalidMetrics, name)
	}
	sort.Strings(invalidMetrics)

	out := CandleOut{
		SchemaVersion:         schemaVersion,
		Instrument:            instrument,
		Exchange:              string(adapter.Exchange()),
		MarketType:            string(adapter.MarketType()),
		Symbol:                instrument.Symbol,
		TS:                    minStartMs,
		Open:                  round6(open),
		High:                  round6(high),
		Low:                   round6(low),
		Close:                 round6(close),
		Volume:                round6(vol),
		QuoteVolumeUsd:        round6(quoteVolumeUsd),
		Delta:                 round6(tradeStats.Delta),
		CVD:                   round6(cvdNow),
		OpenInterest:          round6(oi),
		OpenInterestTimestamp: oiTimestamp,
		FundingRate:           round6(fundingRate),
		MarkPrice:             round6(markPrice),
		IndexPrice:            round6(indexPrice),
		OrderbookBid:          round6(obBid),
		OrderbookAsk:          round6(obAsk),
		OrderbookSpread:       round6(obAsk - obBid),
		OrderbookBidDepth:     round6(obBidDepth),
		OrderbookAskDepth:     round6(obAskDepth),
		TradeLastPrice:        round6(tradeStats.LastPrice),
		TradeLastSize:         round6(tradeStats.LastSize),
		TradeLastSide:         round6(tradeStats.LastSide),
		TradeBuyVolume:        round6(tradeStats.BuyVolume),
		TradeSellVolume:       round6(tradeStats.SellVolume),
		TradeBuyUsd:           round6(tradeStats.BuyUSD),
		TradeSellUsd:          round6(tradeStats.SellUSD),
		TradeCount:            round6(tradeStats.Count),
		LargestTradeUsd:       round6(tradeStats.LargestUSD),
		MedianTradeUsd:        round6(tradeStats.MedianUSD),
		LargestTradeSide:      round6(tradeStats.LargestSide),
		TradeClusterUsd:       round6(tradeStats.ClusterUSD),
		TradeClusterCount:     round6(tradeStats.ClusterCount),
		TradeClusterSide:      round6(tradeStats.ClusterSide),
		TradePriceImpactPct:   round6(tradeStats.PriceImpactPct),
		LongRatio:             round6(longRatio),
		ShortRatio:            round6(shortRatio),
		LongShortRatio:        metrics["longShortRatio"].Value,
		Liquidations:          round6(liqTotal),
		LiquidationsLong:      round6(liqLong),
		LiquidationsShort:     round6(liqShort),
		LiquidationCount:      round6(liqCount),
		LargestLiquidation:    round6(liqLargest),
		FundingIntervalHours:  round6(cfg.FundingIntervalHours),
		Metrics:               metrics,
		InvalidMetrics:        invalidMetrics,
		ShardIndex:            cfg.ShardIndex,
		ShardTotal:            cfg.ShardTotal,
	}
	return out, nil
}

func logScanStats(stats *ScanStats) {
	if stats == nil {
		return
	}
	s := stats.snapshot()
	log.Printf("scan stats total=%d published=%d publish_errors=%d kline_ok=%d kline_err=%d delta_ok=%d delta_err=%d delta_unavailable=%d oi_ok=%d oi_err=%d orderbook_ok=%d orderbook_err=%d tickers_ok=%d tickers_err=%d mark_ok=%d mark_err=%d index_ok=%d index_err=%d funding_ok=%d funding_err=%d liq_with_data=%d liq_empty=%d errors[client=%d server=%d transport=%d other=%d]",
		s.TotalSymbols,
		s.Published,
		s.PublishErr,
		s.KlineOK,
		s.KlineErr,
		s.DeltaOK,
		s.DeltaErr,
		s.DeltaUnavailable,
		s.OIOK,
		s.OIErr,
		s.OrderbookOK,
		s.OrderbookErr,
		s.TickersOK,
		s.TickersErr,
		s.MarkOK,
		s.MarkErr,
		s.IndexOK,
		s.IndexErr,
		s.FundingOK,
		s.FundingErr,
		s.LiqWithData,
		s.LiqEmpty,
		s.ErrClient,
		s.ErrServer,
		s.ErrTransport,
		s.ErrOther,
	)
}
