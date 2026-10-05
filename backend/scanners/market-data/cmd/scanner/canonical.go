package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

const schemaVersion = 3

type Exchange string

const (
	ExchangeBybit   Exchange = "bybit"
	ExchangeBinance Exchange = "binance"
	ExchangeOKX     Exchange = "okx"
	ExchangeBitget  Exchange = "bitget"
	ExchangeGateIO  Exchange = "gateio"
)

type MarketType string

const (
	MarketTypeSpot      MarketType = "spot"
	MarketTypePerpetual MarketType = "perpetual"
)

type InstrumentKey struct {
	Exchange   Exchange   `json:"exchange"`
	MarketType MarketType `json:"marketType"`
	Symbol     string     `json:"symbol"`
}

func NewInstrumentKey(exchange Exchange, marketType MarketType, symbol string) InstrumentKey {
	return InstrumentKey{
		Exchange:   normalizeExchange(string(exchange)),
		MarketType: normalizeMarketType(string(marketType)),
		Symbol:     normalizeSymbol(symbol),
	}
}

func (k InstrumentKey) String() string {
	return fmt.Sprintf("%s:%s:%s", k.Exchange, k.MarketType, k.Symbol)
}

func normalizeExchange(raw string) Exchange {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case string(ExchangeBinance):
		return ExchangeBinance
	case string(ExchangeOKX):
		return ExchangeOKX
	case string(ExchangeBitget):
		return ExchangeBitget
	case "gate", "gate.io", string(ExchangeGateIO):
		return ExchangeGateIO
	case "", string(ExchangeBybit):
		return ExchangeBybit
	default:
		return Exchange(value)
	}
}

func normalizeMarketType(raw string) MarketType {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "spot":
		return MarketTypeSpot
	case "linear", "future", "futures", "perp", "perpetual", "usdt_perpetual":
		return MarketTypePerpetual
	case "":
		return MarketTypePerpetual
	default:
		return MarketType(value)
	}
}

type MetricValue struct {
	Value     float64 `json:"value"`
	Valid     bool    `json:"valid"`
	Timestamp int64   `json:"timestamp"`
	Source    string  `json:"source"`
}

// CandleOut is the exchange-neutral scanner event. The flat fields are kept
// for current consumers; Metrics is the authoritative validity/source layer.
type CandleOut struct {
	SchemaVersion         int                    `json:"schemaVersion"`
	Instrument            InstrumentKey          `json:"instrument"`
	Exchange              string                 `json:"exchange"`
	MarketType            string                 `json:"marketType"`
	Symbol                string                 `json:"symbol"`
	TS                    int64                  `json:"ts"`
	Open                  float64                `json:"open"`
	High                  float64                `json:"high"`
	Low                   float64                `json:"low"`
	Close                 float64                `json:"close"`
	Volume                float64                `json:"volume"`
	QuoteVolumeUsd        float64                `json:"quoteVolumeUsd"`
	Delta                 float64                `json:"delta"`
	CVD                   float64                `json:"cvd"`
	OpenInterest          float64                `json:"openInterest"`
	OpenInterestTimestamp int64                  `json:"openInterestTimestamp"`
	FundingRate           float64                `json:"fundingRate"`
	MarkPrice             float64                `json:"markPrice"`
	IndexPrice            float64                `json:"indexPrice"`
	OrderbookBid          float64                `json:"orderbookBid"`
	OrderbookAsk          float64                `json:"orderbookAsk"`
	OrderbookSpread       float64                `json:"orderbookSpread"`
	OrderbookBidDepth     float64                `json:"orderbookBidDepth"`
	OrderbookAskDepth     float64                `json:"orderbookAskDepth"`
	TradeLastPrice        float64                `json:"tradeLastPrice"`
	TradeLastSize         float64                `json:"tradeLastSize"`
	TradeLastSide         float64                `json:"tradeLastSide"`
	TradeBuyVolume        float64                `json:"tradeBuyVolume"`
	TradeSellVolume       float64                `json:"tradeSellVolume"`
	TradeBuyUsd           float64                `json:"tradeBuyUsd"`
	TradeSellUsd          float64                `json:"tradeSellUsd"`
	TradeCount            float64                `json:"tradeCount"`
	LargestTradeUsd       float64                `json:"largestTradeUsd"`
	MedianTradeUsd        float64                `json:"medianTradeUsd"`
	LargestTradeSide      float64                `json:"largestTradeSide"`
	TradeClusterUsd       float64                `json:"tradeClusterUsd"`
	TradeClusterCount     float64                `json:"tradeClusterCount"`
	TradeClusterSide      float64                `json:"tradeClusterSide"`
	TradePriceImpactPct   float64                `json:"tradePriceImpactPct"`
	LongRatio             float64                `json:"longRatio"`
	ShortRatio            float64                `json:"shortRatio"`
	LongShortRatio        float64                `json:"longShortRatio"`
	Liquidations          float64                `json:"liquidations"`
	LiquidationsLong      float64                `json:"liquidationsLong"`
	LiquidationsShort     float64                `json:"liquidationsShort"`
	LiquidationCount      float64                `json:"liquidationCount"`
	LargestLiquidation    float64                `json:"largestLiquidation"`
	FundingIntervalHours  float64                `json:"fundingIntervalHours"`
	Metrics               map[string]MetricValue `json:"metrics"`
	InvalidMetrics        []string               `json:"invalidMetrics,omitempty"`
	LiquidationOnly       bool                   `json:"liquidationOnly,omitempty"`
	ShardIndex            int                    `json:"shardIndex"`
	ShardTotal            int                    `json:"shardTotal"`
}

type TradeStats struct {
	Delta      float64
	BuyVolume  float64
	SellVolume float64
	BuyUSD     float64
	SellUSD    float64
	Count      float64
	LargestUSD float64
	MedianUSD  float64
	// Side values are aggressor-side observations: +1 buy, -1 sell.
	LargestSide float64
	// Cluster fields describe consecutive public feed observations of one side
	// within a bounded three-second span. They do not identify a person or,
	// for perpetual feeds, prove that the observations belong to one order.
	ClusterUSD   float64
	ClusterCount float64
	ClusterSide  float64
	// PriceImpactPct is the signed price move observed from the first to the
	// last observation in the selected cluster; it is not a causal estimate.
	PriceImpactPct    float64
	LastPrice         float64
	LastSize          float64
	LastSide          float64
	Complete          bool
	Timestamp         int64
	Source            string
	median            p2Median
	clusterStartTS    int64
	clusterLastTS     int64
	clusterStartPrice float64
	clusterLastPrice  float64
	clusterUSD        float64
	clusterCount      float64
	clusterSide       float64
}

type MarketAdapter interface {
	Exchange() Exchange
	MarketType() MarketType
	Instrument(symbol string) InstrumentKey
	FetchAllSymbolsFromAPI(ctx context.Context) ([]string, error)
	FetchKline1m(ctx context.Context, symbol string, targetMinuteMs int64) (minuteStartMs int64, open float64, high float64, low float64, close float64, volume float64, quoteVolumeUsd float64, err error)
	FetchDeltaForMinute(ctx context.Context, symbol string, minuteStartMs int64, tradeLimit int) (TradeStats, error)
	FetchOpenInterest(ctx context.Context, symbol string, targetMinuteMs int64) (value float64, sourceTimestampMs int64, err error)
	FetchAccountRatio(ctx context.Context, symbol string, period string, targetMinuteMs int64) (buyRatio float64, sellRatio float64, err error)
	FetchOrderbook(ctx context.Context, symbol string, limit int) (bestBid float64, bestAsk float64, bidDepth float64, askDepth float64, err error)
	FetchTickers(ctx context.Context, symbol string) (markPrice float64, indexPrice float64, fundingRate float64, err error)
	FetchMarkPrice(ctx context.Context, symbol string, targetMinuteMs int64) (float64, error)
	FetchIndexPrice(ctx context.Context, symbol string, targetMinuteMs int64) (float64, error)
	FetchFundingRate(ctx context.Context, symbol string) (float64, error)
	RunStreams(ctx context.Context, symbols []string, trades *TradeStore, liquidations *LiquidationStore) error
}

type TradeStore struct {
	mu            sync.Mutex
	buckets       map[string]map[int64]*TradeStats
	connected     bool
	connectedAtMs int64
	coverage      []streamCoverageInterval
}

func NewTradeStore() *TradeStore {
	return &TradeStore{buckets: make(map[string]map[int64]*TradeStats)}
}

func (s *TradeStore) SetConnected(connected bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	now := time.Now().UTC().UnixMilli()
	if connected && !s.connected {
		s.connectedAtMs = now
	} else if !connected && s.connected && s.connectedAtMs > 0 {
		s.coverage = appendCoverageInterval(s.coverage, streamCoverageInterval{startMs: s.connectedAtMs, endMs: now})
	}
	s.connected = connected
	s.mu.Unlock()
}

func (s *TradeStore) CoversMinute(minuteStartMs int64) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return streamCoveredMinute(s.connected, s.connectedAtMs, s.coverage, minuteStartMs)
}

func (s *TradeStore) Add(key InstrumentKey, tsMs int64, price, size float64, side string, count float64) {
	if s == nil || key.Symbol == "" || tsMs <= 0 || price <= 0 || size <= 0 ||
		math.IsNaN(price) || math.IsInf(price, 0) || math.IsNaN(size) || math.IsInf(size, 0) ||
		math.IsNaN(count) || math.IsInf(count, 0) {
		return
	}
	minKey := minuteKey(tsMs)
	s.mu.Lock()
	defer s.mu.Unlock()
	byMinute := s.buckets[key.String()]
	if byMinute == nil {
		byMinute = make(map[int64]*TradeStats)
		s.buckets[key.String()] = byMinute
	}
	bucket := byMinute[minKey]
	if bucket == nil {
		bucket = &TradeStats{Complete: true, Source: "websocket"}
		byMinute[minKey] = bucket
	}
	bucket.addTrade(tsMs, price, size, side, count)
}

func (s *TradeStore) Get(key InstrumentKey, minuteStartMs int64) (TradeStats, bool) {
	if s == nil {
		return TradeStats{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if byMinute := s.buckets[key.String()]; byMinute != nil {
		if bucket := byMinute[minuteKey(minuteStartMs)]; bucket != nil {
			return *bucket, true
		}
	}
	return TradeStats{Complete: true, Source: "websocket", Timestamp: minuteStartMs + ringMinuteMs - 1}, true
}

func (s *TradeStore) Cleanup(olderThanMinKey int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, byMinute := range s.buckets {
		for minKey := range byMinute {
			if minKey < olderThanMinKey {
				delete(byMinute, minKey)
			}
		}
		if len(byMinute) == 0 {
			delete(s.buckets, key)
		}
	}
}

type BybitAdapter struct {
	*BybitClient
	exchange   Exchange
	marketType MarketType
}

func NewBybitAdapter(cfg Config) *BybitAdapter {
	return &BybitAdapter{
		BybitClient: NewBybitClient(cfg),
		exchange:    ExchangeBybit,
		marketType:  normalizeMarketType(cfg.MarketType),
	}
}

func (a *BybitAdapter) Exchange() Exchange     { return a.exchange }
func (a *BybitAdapter) MarketType() MarketType { return a.marketType }
func (a *BybitAdapter) Instrument(symbol string) InstrumentKey {
	return NewInstrumentKey(a.exchange, a.marketType, symbol)
}

func newMarketAdapter(cfg Config) (MarketAdapter, error) {
	if normalizeMarketType(cfg.MarketType) != MarketTypePerpetual {
		return nil, fmt.Errorf("scanner currently supports marketType=%s only", MarketTypePerpetual)
	}
	switch normalizeExchange(cfg.Exchange) {
	case ExchangeBybit:
		return NewBybitAdapter(cfg), nil
	case ExchangeBinance:
		return NewBinanceAdapter(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported exchange %q", cfg.Exchange)
	}
}

func runAdapterStreams(ctx context.Context, adapter MarketAdapter, trades *TradeStore, liquidations *LiquidationStore, symbols []string) {
	if adapter == nil || len(symbols) == 0 {
		return
	}
	backoff := time.Second
	for ctx.Err() == nil {
		err := adapter.RunStreams(ctx, symbols, trades, liquidations)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			fmt.Printf("market streams exchange=%s marketType=%s error=%v\n", adapter.Exchange(), adapter.MarketType(), err)
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}
