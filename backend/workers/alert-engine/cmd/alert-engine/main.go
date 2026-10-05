package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	wildcardSymbol = "*" // DB symbol value meaning: apply rule to all symbols
)

var combinedLiquidationVenues = []string{"bybit", "binance", "okx", "bitget", "gateio"}

// ---- Input candle event (from scanners) ----
type CandleEvent struct {
	SchemaVersion         int                    `json:"schemaVersion"`
	Exchange              string                 `json:"exchange"`
	MarketType            string                 `json:"marketType"`
	Symbol                string                 `json:"symbol"`
	TS                    int64                  `json:"ts"` // unix ms for minute
	Open                  float64                `json:"open"`
	High                  float64                `json:"high"`
	Low                   float64                `json:"low"`
	Close                 float64                `json:"close"`
	Volume                float64                `json:"volume"`
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
	QuoteVolumeUsd        float64                `json:"quoteVolumeUsd"`
	MedianTradeUsd        float64                `json:"medianTradeUsd"`
	LargestTradeSide      float64                `json:"largestTradeSide"`
	TradeClusterUsd       float64                `json:"tradeClusterUsd"`
	TradeClusterSide      float64                `json:"tradeClusterSide"`
	TradeClusterCount     float64                `json:"tradeClusterCount"`
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
	Metrics               map[string]MetricValue `json:"metrics,omitempty"`
	InvalidMetrics        []string               `json:"invalidMetrics,omitempty"`
	LiquidationOnly       bool                   `json:"liquidationOnly,omitempty"`
	ShardIndex            int                    `json:"shardIndex"`
	ShardTotal            int                    `json:"shardTotal"`
}

type MetricValue struct {
	Value     float64 `json:"value"`
	Valid     bool    `json:"valid"`
	Timestamp int64   `json:"timestamp"`
	Source    string  `json:"source"`
}

// ---- Rule model (from Postgres) ----
type Direction string

const (
	DirUp   Direction = "up"
	DirDown Direction = "down"
	DirBoth Direction = "both"
)

type Rule struct {
	ID              int64
	UserID          int64
	Exchange        string
	MarketType      string
	Indicator       string
	Symbol          string // uppercase symbol or "*"
	WindowMinutes   int
	ThresholdPct    float64
	ThresholdAmount float64
	Direction       Direction
	CooldownSeconds int
	Enabled         bool
	UpdatedAt       time.Time
	Conditions      []Condition
	ScannerSlot     string
}

type Condition struct {
	Indicator       string
	Direction       Direction
	ThresholdPct    float64
	ThresholdAmount float64
	Negate          bool
}

type ConditionDTO struct {
	Indicator        string   `json:"indicator"`
	Direction        string   `json:"direction"`
	ThresholdPercent *float64 `json:"threshold_percent"`
	ThresholdAmount  *float64 `json:"threshold_amount"`
	Negate           bool     `json:"negate"`
}

// ---- Output alert event (to WS gateway via Redis Streams) ----
type AlertEvent struct {
	DeliveryID          int64   `json:"deliveryId,omitempty"`
	UserID              int64   `json:"userId"`
	RuleID              int64   `json:"ruleId"`
	Exchange            string  `json:"exchange"`
	MarketType          string  `json:"marketType"`
	Indicator           string  `json:"indicator"`
	Symbol              string  `json:"symbol"`
	TS                  int64   `json:"ts"`
	WindowMinutes       int     `json:"windowMinutes"`
	ThresholdPct        float64 `json:"thresholdPercent"`
	Direction           string  `json:"direction"`
	ChangePercent       float64 `json:"changePercent"`
	PriceNow            float64 `json:"priceNow"`
	PriceThen           float64 `json:"priceThen"`
	Liquidations        float64 `json:"liquidationsUsd"`
	BybitLiquidations   float64 `json:"bybitLiquidationsUsd,omitempty"`
	BinanceLiquidations float64 `json:"binanceLiquidationsUsd,omitempty"`
	OKXLiquidations     float64 `json:"okxLiquidationsUsd,omitempty"`
	BitgetLiquidations  float64 `json:"bitgetLiquidationsUsd,omitempty"`
	GateIOLiquidations  float64 `json:"gateioLiquidationsUsd,omitempty"`
	CombinedExchanges   bool    `json:"combinedExchanges,omitempty"`
	ScannerSlot         string  `json:"scannerSlot"`
}

type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	AlertsStream string

	ConsumerName string

	PGDSN               string
	RulesRefreshSeconds int
	RulesSlotFilter     string

	CooldownPrefix string
	LogLevel       string

	ReadBlockMs int

	TelegramBotToken string

	AlertsStreamMaxLenApprox  int64
	ImportantEventsEnabled    bool
	BinanceSpotBaseURL        string
	BybitBaseURL              string
	StablecoinWarningPct      float64
	StablecoinHighPct         float64
	StablecoinCriticalPct     float64
	StablecoinWarningMinutes  int
	StablecoinHighMinutes     int
	StablecoinRecoveryMinutes int
	StablecoinMinVolumeUSD    float64
	OnchainEnabled            bool
	EtherscanAPIKey           string
	EtherscanChainID          string
	OnchainBTCEnabled         bool
	BitcoinAPIURL             string
	OnchainSolanaEnabled      bool
	SolanaRPCURL              string
	OnchainTronEnabled        bool
	TronGridBaseURL           string
	TronGridAPIKey            string
	OnchainIntervalSeconds    int
	OnchainMaxAddresses       int
	OnchainMinUSD             float64

	PendingClaimEverySeconds  int
	PendingMinIdleSeconds     int
	PendingClaimCount         int
	StreamMaxDeliveries       int
	StreamDLQMaxLen           int64
	StreamDLQRawMaxBytes      int
	StreamDLQErrorMaxBytes    int
	ObservabilityEverySeconds int
	MarketStreams             []string
	MarketConsumerGroup       string
	StreamReadCount           int
	MaxInstruments            int
	SnapshotPath              string
	SnapshotEverySeconds      int
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

func envFloat(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return def
	}
	return f
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes" || v == "y" || v == "on"
}

func loadConfig() Config {
	cfg := Config{
		RedisAddr:     envStr("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: envStr("REDIS_PASSWORD", ""),
		RedisDB:       envInt("REDIS_DB", 0),

		AlertsStream: envStr("ALERTS_STREAM", "alerts:events"),

		ConsumerName: envStr("CONSUMER_NAME", hostnameOr("worker-1")),

		PGDSN:               envStr("PG_DSN", ""),
		RulesRefreshSeconds: envInt("RULES_REFRESH_SECONDS", 15),
		RulesSlotFilter:     strings.TrimSpace(envStr("RULES_SLOT_FILTER", "")),

		CooldownPrefix: envStr("COOLDOWN_PREFIX", "cooldown:"),
		LogLevel:       envStr("LOG_LEVEL", "info"),

		ReadBlockMs: envInt("READ_BLOCK_MS", 5000),

		TelegramBotToken: envStr("TELEGRAM_BOT_TOKEN", ""),

		AlertsStreamMaxLenApprox:  int64(envInt("ALERTS_STREAM_MAXLEN", 50000)),
		ImportantEventsEnabled:    envBool("IMPORTANT_EVENTS_ENABLED", false),
		BinanceSpotBaseURL:        envStr("BINANCE_SPOT_BASE_URL", "https://api.binance.com"),
		BybitBaseURL:              envStr("BYBIT_BASE_URL", "https://api.bybit.com"),
		StablecoinWarningPct:      envFloat("STABLECOIN_WARNING_PCT", 0.5),
		StablecoinHighPct:         envFloat("STABLECOIN_HIGH_PCT", 1.0),
		StablecoinCriticalPct:     envFloat("STABLECOIN_CRITICAL_PCT", 2.0),
		StablecoinWarningMinutes:  envInt("STABLECOIN_WARNING_MINUTES", 3),
		StablecoinHighMinutes:     envInt("STABLECOIN_HIGH_MINUTES", 2),
		StablecoinRecoveryMinutes: envInt("STABLECOIN_RECOVERY_MINUTES", 2),
		StablecoinMinVolumeUSD:    envFloat("STABLECOIN_MIN_VOLUME_USD", 1_000_000),
		OnchainEnabled:            envBool("ONCHAIN_MONITOR_ENABLED", envBool("ONCHAIN_ENABLED", false)),
		EtherscanAPIKey:           envStr("ETHERSCAN_API_KEY", ""),
		EtherscanChainID:          envStr("ETHERSCAN_CHAIN_ID", "1"),
		OnchainBTCEnabled:         envBool("ONCHAIN_BTC_ENABLED", false),
		BitcoinAPIURL:             envStr("BITCOIN_API_URL", "https://blockstream.info/api"),
		OnchainSolanaEnabled:      envBool("ONCHAIN_SOLANA_ENABLED", false),
		SolanaRPCURL:              envStr("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com"),
		OnchainTronEnabled:        envBool("ONCHAIN_TRON_ENABLED", false),
		TronGridBaseURL:           envStr("TRONGRID_BASE_URL", "https://api.trongrid.io"),
		TronGridAPIKey:            envStr("TRONGRID_API_KEY", ""),
		OnchainIntervalSeconds:    envInt("ONCHAIN_INTERVAL_SECONDS", 60),
		OnchainMaxAddresses:       envInt("ONCHAIN_MAX_ADDRESSES", 500),
		OnchainMinUSD:             envFloat("ONCHAIN_MIN_USD", 1_000_000),

		PendingClaimEverySeconds:  envInt("PENDING_CLAIM_EVERY_SECONDS", 10),
		PendingMinIdleSeconds:     envInt("PENDING_MIN_IDLE_SECONDS", 15),
		PendingClaimCount:         envInt("PENDING_CLAIM_COUNT", 200),
		StreamMaxDeliveries:       envInt("STREAM_MAX_DELIVERIES", defaultStreamMaxDeliveries),
		StreamDLQMaxLen:           int64(envInt("STREAM_DLQ_MAXLEN", int(defaultStreamDLQMaxLen))),
		StreamDLQRawMaxBytes:      envInt("STREAM_DLQ_RAW_MAX_BYTES", defaultStreamDLQRawMaxBytes),
		StreamDLQErrorMaxBytes:    envInt("STREAM_DLQ_ERROR_MAX_BYTES", defaultStreamDLQErrorMaxBytes),
		ObservabilityEverySeconds: envInt("OBSERVABILITY_EVERY_SECONDS", 60),
		MarketStreams: parseStreamList(envStr("MARKET_STREAMS",
			"candles:v3:bybit:perpetual,candles:v3:binance:perpetual,liquidations:v3:okx:perpetual,liquidations:v3:bitget:perpetual,liquidations:v3:gateio:perpetual")),
		StreamReadCount:      envInt("STREAM_READ_COUNT", defaultStreamReadCount),
		MaxInstruments:       envInt("SPARSE_MAX_INSTRUMENTS", 20_000),
		SnapshotPath:         envStr("SPARSE_SNAPSHOT_PATH", "/app/state/worker.snapshot"),
		SnapshotEverySeconds: envInt("SPARSE_SNAPSHOT_EVERY_SECONDS", 300),
	}
	cfg.MarketConsumerGroup = envStr("MARKET_CONSUMER_GROUP", "alert-engine")
	if cfg.StreamMaxDeliveries <= 0 {
		cfg.StreamMaxDeliveries = defaultStreamMaxDeliveries
	}
	dlqConfig := (streamDLQConfig{MaxLen: cfg.StreamDLQMaxLen, RawMaxBytes: cfg.StreamDLQRawMaxBytes, ErrorMaxBytes: cfg.StreamDLQErrorMaxBytes}).normalized()
	cfg.StreamDLQMaxLen = dlqConfig.MaxLen
	cfg.StreamDLQRawMaxBytes = dlqConfig.RawMaxBytes
	cfg.StreamDLQErrorMaxBytes = dlqConfig.ErrorMaxBytes
	if cfg.StreamReadCount <= 0 {
		cfg.StreamReadCount = defaultStreamReadCount
	}
	if cfg.MaxInstruments <= 0 {
		cfg.MaxInstruments = 20_000
	}
	if cfg.SnapshotEverySeconds <= 0 {
		cfg.SnapshotEverySeconds = 300
	}
	return cfg
}

func parseStreamList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '	' })
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool)
	for _, part := range parts {
		stream := strings.TrimSpace(part)
		if stream == "" || seen[stream] {
			continue
		}
		seen[stream] = true
		out = append(out, stream)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func hostnameOr(def string) string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return def
	}
	return h
}

// ---- Rules cache ----
type RulesCache struct {
	mu               sync.RWMutex
	bySym            map[string][]Rule
	maxWindowMinutes int
	generation       uint64
}

func NewRulesCache() *RulesCache {
	return &RulesCache{bySym: make(map[string][]Rule)}
}

func (c *RulesCache) Set(m map[string][]Rule) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bySym = m
	c.generation++
	c.maxWindowMinutes = 0
	for _, rules := range m {
		for _, rule := range rules {
			if rule.WindowMinutes > c.maxWindowMinutes {
				c.maxWindowMinutes = rule.WindowMinutes
			}
		}
	}
}

func (c *RulesCache) MaxWindowMinutes() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.maxWindowMinutes
}

func (c *RulesCache) Snapshot() ([]Rule, uint64) {
	if c == nil {
		return nil, 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	count := 0
	for _, rules := range c.bySym {
		count += len(rules)
	}
	out := make([]Rule, 0, count)
	for _, rules := range c.bySym {
		for _, rule := range rules {
			out = append(out, cloneRule(rule))
		}
	}
	return out, c.generation
}

func normalizeExchange(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "binance":
		return "binance"
	case "okx":
		return "okx"
	case "bitget":
		return "bitget"
	case "gate", "gate.io", "gateio":
		return "gateio"
	case "", "bybit":
		return "bybit"
	default:
		return ""
	}
}

func normalizeMarketType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "linear", "future", "futures", "perp", "perpetual":
		return "perpetual"
	default:
		return ""
	}
}

func instrumentCacheKey(exchange, marketType, symbol string) string {
	return normalizeExchange(exchange) + ":" + normalizeMarketType(marketType) + ":" + strings.ToUpper(strings.TrimSpace(symbol))
}

func prepareCandleEventForStream(event *CandleEvent, stream string) bool {
	if event == nil {
		return false
	}
	expectedExchange := ""
	lowerStream := strings.ToLower(strings.TrimSpace(stream))
	if strings.Contains(lowerStream, "gateio") || strings.Contains(lowerStream, "gate.io") {
		expectedExchange = "gateio"
	} else if strings.Contains(lowerStream, "bitget") {
		expectedExchange = "bitget"
	} else if strings.Contains(lowerStream, "okx") {
		expectedExchange = "okx"
	} else if strings.Contains(lowerStream, "binance") {
		expectedExchange = "binance"
	} else if strings.Contains(lowerStream, "bybit") {
		expectedExchange = "bybit"
	}
	rawExchange := strings.TrimSpace(event.Exchange)
	if rawExchange == "" {
		event.Exchange = expectedExchange
	} else {
		event.Exchange = normalizeExchange(rawExchange)
	}
	if event.Exchange == "" || (expectedExchange != "" && event.Exchange != expectedExchange) {
		return false
	}
	event.MarketType = normalizeMarketType(event.MarketType)
	return event.MarketType != ""
}

// GetForInstrument returns venue-specific exact and wildcard rules.
func (c *RulesCache) GetForInstrument(exchange, marketType, symbol string) []Rule {
	exactKey := instrumentCacheKey(exchange, marketType, symbol)
	wildcardKey := instrumentCacheKey(exchange, marketType, wildcardSymbol)
	c.mu.RLock()
	defer c.mu.RUnlock()

	exact := c.bySym[exactKey]
	wild := c.bySym[wildcardKey]
	if len(exact) == 0 && len(wild) == 0 {
		return nil
	}
	out := make([]Rule, 0, len(exact)+len(wild))
	out = append(out, exact...)
	out = append(out, wild...)
	return out
}

func (c *RulesCache) SymbolsCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.bySym)
}

func splitRuleSymbols(raw string) []string {
	value := strings.TrimSpace(raw)
	if value == "" || value == "*" || strings.EqualFold(value, "ALL") {
		return []string{wildcardSymbol}
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ' ', '	', '\n', '\r', ',', ';':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool)
	for _, part := range parts {
		s := strings.ToUpper(strings.TrimSpace(part))
		if s == "" {
			continue
		}
		if s == "*" || strings.EqualFold(s, "ALL") {
			return []string{wildcardSymbol}
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ---- DB load rules ----
func loadEnabledRulesForSlot(ctx context.Context, pool *pgxpool.Pool, slotFilter string) (map[string][]Rule, error) {
	return loadEnabledRules(ctx, pool, slotFilter, false, alertWindow24hMaxMinutes)
}

const (
	alertWindow24hMaxMinutes = 1440
)

func enabledRulesQuery(slotFilter string, allSlots bool) (string, []any) {
	base := `
SELECT a.id, a.user_id, a.exchange, a.market_type, a.indicator, a.symbol, a.window_minutes, a.threshold_percent, a.threshold_amount, a.direction, a.cooldown_seconds, a.enabled, a.updated_at, a.conditions, a.scanner_slot
FROM alerts a
`
	joins := ""
	where := "WHERE a.enabled = true"
	args := []any{}

	if allSlots {
		// One statement gives the unified engine one PostgreSQL snapshot. Three
		// sequential slot queries can miss or duplicate a rule moved mid-refresh.
		joins = "LEFT JOIN users u ON u.num_id = a.user_id"
		where += " AND a.scanner_slot IN ('SLOT_1','SLOT_2','SLOT_3')"
		where += " AND (a.scanner_slot <> 'SLOT_3' OR lower(u.plan) = 'pro')"
	} else if strings.TrimSpace(slotFilter) != "" {
		args = append(args, slotFilter)
		where += fmt.Sprintf(" AND a.scanner_slot = $%d", len(args))
		if strings.EqualFold(slotFilter, "SLOT_3") {
			joins = "JOIN users u ON u.num_id = a.user_id"
			where += " AND lower(u.plan) = 'pro'"
		}
	}
	return strings.Join([]string{base, joins, where}, "\n"), args
}

func loadEnabledRules(ctx context.Context, pool *pgxpool.Pool, slotFilter string, allSlots bool, maxWindowMinutes int) (map[string][]Rule, error) {
	if maxWindowMinutes != alertWindow24hMaxMinutes {
		return nil, fmt.Errorf("invalid rule window ceiling %d", maxWindowMinutes)
	}
	q, args := enabledRulesQuery(slotFilter, allSlots)
	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]Rule)
	for rows.Next() {
		var r Rule
		var dir string
		var thresholdPercent, thresholdAmount pgtype.Float8
		var rawConditions []byte
		if err := rows.Scan(&r.ID, &r.UserID, &r.Exchange, &r.MarketType, &r.Indicator, &r.Symbol, &r.WindowMinutes, &thresholdPercent, &thresholdAmount, &dir, &r.CooldownSeconds, &r.Enabled, &r.UpdatedAt, &rawConditions, &r.ScannerSlot); err != nil {
			return nil, err
		}
		r.Exchange = normalizeExchange(r.Exchange)
		r.MarketType = normalizeMarketType(r.MarketType)
		if r.Exchange == "" || r.MarketType == "" {
			continue
		}
		if thresholdPercent.Valid {
			r.ThresholdPct = thresholdPercent.Float64
		}
		if thresholdAmount.Valid {
			r.ThresholdAmount = thresholdAmount.Float64
		}

		r.Indicator = strings.TrimSpace(r.Indicator)
		if r.Indicator == "" {
			r.Indicator = "price"
		}

		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym == "" || sym == "ALL" || sym == "*" {
			sym = wildcardSymbol
		}
		r.Symbol = sym

		dir = strings.ToLower(strings.TrimSpace(dir))
		switch Direction(dir) {
		case DirUp, DirDown, DirBoth:
			r.Direction = Direction(dir)
		default:
			r.Direction = DirBoth
		}

		if !isValidLoadedRuleWindow(r.WindowMinutes, maxWindowMinutes) {
			continue
		}

		conds := parseRuleConditions(rawConditions)
		if len(conds) == 0 {
			conds = append(conds, Condition{
				Indicator:       r.Indicator,
				Direction:       r.Direction,
				ThresholdPct:    r.ThresholdPct,
				ThresholdAmount: r.ThresholdAmount,
			})
		}
		if !validateConditions(conds) {
			continue
		}
		r.Conditions = conds

		if r.Symbol == wildcardSymbol {
			key := instrumentCacheKey(r.Exchange, r.MarketType, wildcardSymbol)
			out[key] = append(out[key], r)
			continue
		}
		symbols := splitRuleSymbols(r.Symbol)
		if len(symbols) == 0 {
			continue
		}
		for _, s := range symbols {
			rCopy := r
			rCopy.Symbol = s
			key := instrumentCacheKey(r.Exchange, r.MarketType, s)
			out[key] = append(out[key], rCopy)
		}
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return out, nil
}

// ---- Redis stream group ----
func ensureGroup(ctx context.Context, rdb *redis.Client, stream, group string) error {
	err := rdb.XGroupCreateMkStream(ctx, stream, group, "$").Err()
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

// ---- Cooldown ----
const signalCoinCooldownTTL = 5 * time.Minute

func coinCooldownKey(prefix string, userID int64, exchange, marketType, symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	return prefix + "coin:" + strconv.FormatInt(userID, 10) + ":" + instrumentCacheKey(exchange, marketType, symbol)
}

func tryAcquireCoinCooldown(ctx context.Context, rdb *redis.Client, prefix string, userID int64, exchange, marketType, symbol string) (bool, error) {
	key := coinCooldownKey(prefix, userID, exchange, marketType, symbol)
	ok, err := rdb.SetNX(ctx, key, "1", signalCoinCooldownTTL).Result()
	return ok, err
}

func isCoinCooldownActive(ctx context.Context, rdb *redis.Client, prefix string, userID int64, exchange, marketType, symbol string) (bool, error) {
	key := coinCooldownKey(prefix, userID, exchange, marketType, symbol)
	exists, err := rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// ---- Alert publishing ----
func publishAlert(ctx context.Context, cfg Config, rdb *redis.Client, ev AlertEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	args := &redis.XAddArgs{
		Stream: cfg.AlertsStream,
		Values: map[string]any{"json": string(b)},
	}
	if cfg.AlertsStreamMaxLenApprox > 0 {
		args.MaxLen = cfg.AlertsStreamMaxLenApprox
		args.Approx = true
	}
	err = rdb.XAdd(ctx, args).Err()
	if err != nil {
		workerRedisPublishErrors.Add(1)
	}
	return err
}

// ---- DB insert signals ----
// Возвращает inserted=true, если реально вставили строку (не дубль)
func insertSignal(ctx context.Context, db *pgxpool.Pool, ev AlertEvent, evalDetails []map[string]any) (inserted bool, deliveryID int64, err error) {
	payloadMap := map[string]any{
		"exchange":               ev.Exchange,
		"marketType":             ev.MarketType,
		"indicator":              ev.Indicator,
		"direction":              ev.Direction,
		"changePercent":          ev.ChangePercent,
		"windowMinutes":          ev.WindowMinutes,
		"priceNow":               ev.PriceNow,
		"priceThen":              ev.PriceThen,
		"liquidationsUsd":        ev.Liquidations,
		"bybitLiquidationsUsd":   ev.BybitLiquidations,
		"binanceLiquidationsUsd": ev.BinanceLiquidations,
		"okxLiquidationsUsd":     ev.OKXLiquidations,
		"bitgetLiquidationsUsd":  ev.BitgetLiquidations,
		"gateioLiquidationsUsd":  ev.GateIOLiquidations,
		"combinedExchanges":      ev.CombinedExchanges,
		"symbol":                 ev.Symbol,
		"ruleId":                 ev.RuleID,
		"userId":                 ev.UserID,
		"ts":                     ev.TS,
	}
	if len(evalDetails) > 0 {
		payloadMap["eval"] = map[string]any{"details": evalDetails}
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return false, 0, err
	}
	deliveryPayload, err := json.Marshal(ev)
	if err != nil {
		return false, 0, err
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx)
	var insertedID string
	err = tx.QueryRow(ctx, `
		INSERT INTO signals (rule_id, user_id, exchange, market_type, symbol, tf, ts, payload, scanner_slot)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT DO NOTHING
		RETURNING id::text
	`, ev.RuleID, ev.UserID, ev.Exchange, ev.MarketType, ev.Symbol, "1m", ev.TS, payload, ev.ScannerSlot).Scan(&insertedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			inserted = false
			if err = tx.QueryRow(ctx, `
				SELECT id::text
				FROM signals
				WHERE rule_id=$1 AND exchange=$2 AND market_type=$3 AND symbol=$4 AND tf='1m' AND ts=$5
			`, ev.RuleID, ev.Exchange, ev.MarketType, ev.Symbol, ev.TS).Scan(&insertedID); err != nil {
				return false, 0, err
			}
		} else {
			return false, 0, err
		}
	} else {
		inserted = true
	}

	if err = tx.QueryRow(ctx, `
		INSERT INTO signal_delivery_outbox (signal_id, payload)
		VALUES ($1::uuid, $2)
		ON CONFLICT (signal_id) DO NOTHING
		RETURNING id
	`, insertedID, deliveryPayload).Scan(&deliveryID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, 0, err
		}
		if err = tx.QueryRow(ctx, `SELECT id FROM signal_delivery_outbox WHERE signal_id=$1::uuid`, insertedID).Scan(&deliveryID); err != nil {
			return false, 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, 0, err
	}
	return inserted, deliveryID, nil
}

func waitSignalOutboxDelivery(ctx context.Context, db *pgxpool.Pool, deliveryID int64, timeout time.Duration) error {
	if db == nil || deliveryID <= 0 {
		return errors.New("invalid signal outbox delivery")
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	lastError := ""
	for {
		var streamPublished, telegramProcessed, deadLettered bool
		var deliveryError, errorClass pgtype.Text
		err := db.QueryRow(waitCtx, `
			SELECT stream_published_at IS NOT NULL, telegram_processed_at IS NOT NULL,
			       dead_lettered_at IS NOT NULL, last_error, error_class
			FROM signal_delivery_outbox
			WHERE id=$1
		`, deliveryID).Scan(&streamPublished, &telegramProcessed, &deadLettered, &deliveryError, &errorClass)
		if err != nil {
			return err
		}
		if deliveryError.Valid {
			lastError = deliveryError.String
		}
		if deadLettered {
			return fmt.Errorf("signal outbox delivery %d dead-lettered class=%s: %s", deliveryID, errorClass.String, lastError)
		}
		if streamPublished && telegramProcessed {
			return nil
		}
		select {
		case <-waitCtx.Done():
			if lastError != "" {
				return fmt.Errorf("signal outbox delivery %d timed out: %s: %w", deliveryID, lastError, waitCtx.Err())
			}
			return fmt.Errorf("signal outbox delivery %d timed out: %w", deliveryID, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func pendingSignalDeliverySteps(streamPublished, telegramProcessed bool) (publishStream, processTelegram bool) {
	return !streamPublished, !telegramProcessed
}

type pendingSignalDelivery struct {
	id                int64
	payload           []byte
	streamPublished   bool
	telegramProcessed bool
	claimToken        string
	streamAttempts    int
	telegramAttempts  int
}

func flushSignalOutbox(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, cfg Config) error {
	if db == nil || rdb == nil {
		return nil
	}
	batchSize := 20
	var failures []error
	for processed := 0; processed < batchSize; processed++ {
		item, found, err := claimNextSignalDelivery(ctx, db)
		if err != nil {
			failures = append(failures, err)
			break
		}
		if !found {
			break
		}
		if err := processClaimedSignalDelivery(ctx, db, rdb, cfg, item); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func claimNextSignalDelivery(ctx context.Context, db *pgxpool.Pool) (pendingSignalDelivery, bool, error) {
	token, err := newOutboxClaimToken()
	if err != nil {
		return pendingSignalDelivery{}, false, fmt.Errorf("create signal outbox claim token: %w", err)
	}
	var item pendingSignalDelivery
	err = db.QueryRow(ctx, signalOutboxClaimSQL, token, int64(outboxClaimLease/time.Second)).Scan(
		&item.id, &item.payload, &item.streamPublished, &item.telegramProcessed,
		&item.streamAttempts, &item.telegramAttempts,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return pendingSignalDelivery{}, false, nil
	}
	if err != nil {
		return pendingSignalDelivery{}, false, err
	}
	item.claimToken = token
	return item, true, nil
}

func processClaimedSignalDelivery(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, cfg Config, item pendingSignalDelivery) error {
	var event AlertEvent
	if err := json.Unmarshal(item.payload, &event); err != nil {
		return failSignalDelivery(ctx, db, item, markOutboxPermanent(fmt.Errorf("decode signal outbox payload: %w", err)))
	}
	if err := validateSignalOutboxPayload(event); err != nil {
		return failSignalDelivery(ctx, db, item, markOutboxPermanent(err))
	}
	event.DeliveryID = item.id
	publishStream, processTelegram := pendingSignalDeliverySteps(item.streamPublished, item.telegramProcessed)
	if publishStream {
		raw, err := json.Marshal(event)
		if err != nil {
			return failSignalDelivery(ctx, db, item, markOutboxPermanent(fmt.Errorf("encode signal outbox payload: %w", err)))
		}
		_, err = publishOutboxStreamOnce(ctx, rdb, cfg.AlertsStream, fmt.Sprintf("signal:%d", item.id), string(raw), cfg.AlertsStreamMaxLenApprox, defaultOutboxPublishDedupMax, defaultOutboxPublishDedupAge, time.Now().UTC())
		if err != nil {
			workerRedisPublishErrors.Add(1)
			return failSignalDelivery(ctx, db, item, err)
		}
		if err := markSignalDeliveryStep(ctx, db, item, "stream"); err != nil {
			return err
		}
		item.streamPublished = true
		item.streamAttempts++
	}
	if processTelegram {
		if strings.TrimSpace(cfg.TelegramBotToken) == "" {
			return failSignalDelivery(ctx, db, item, errors.New("telegram bot token is not configured"))
		}
		if err := notifyTelegram(ctx, cfg.TelegramBotToken, db, rdb, event); err != nil {
			return failSignalDelivery(ctx, db, item, err)
		}
		if err := markSignalDeliveryStep(ctx, db, item, "telegram"); err != nil {
			return err
		}
		item.telegramProcessed = true
		item.telegramAttempts++
	}
	commandTag, err := db.Exec(ctx, `
		UPDATE signal_delivery_outbox
		SET attempts=attempts+1, last_error=NULL, error_class=NULL, next_attempt_at=now(),
		    claim_token=NULL, claim_expires_at=NULL
		WHERE id=$1 AND claim_token=$2
	`, item.id, item.claimToken)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("signal outbox claim lost for delivery %d", item.id)
	}
	return nil
}

func markSignalDeliveryStep(ctx context.Context, db *pgxpool.Pool, item pendingSignalDelivery, step string) error {
	query := ""
	switch step {
	case "stream":
		query = `UPDATE signal_delivery_outbox
			SET stream_published_at=now(), stream_attempts=stream_attempts+1,
			    last_error=NULL, error_class=NULL, next_attempt_at=now()
			WHERE id=$1 AND claim_token=$2`
	case "telegram":
		query = `UPDATE signal_delivery_outbox
			SET telegram_processed_at=now(), telegram_attempts=telegram_attempts+1,
			    last_error=NULL, error_class=NULL, next_attempt_at=now()
			WHERE id=$1 AND claim_token=$2`
	default:
		return fmt.Errorf("unknown signal delivery step %q", step)
	}
	commandTag, err := db.Exec(ctx, query, item.id, item.claimToken)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("signal outbox claim lost for delivery %d", item.id)
	}
	return nil
}

func failSignalDelivery(ctx context.Context, db *pgxpool.Pool, item pendingSignalDelivery, deliveryErr error) error {
	return scheduleSignalDeliveryFailure(ctx, db, item, deliveryErr)
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func notifyTelegram(ctx context.Context, token string, db rowQuerier, rdb *redis.Client, ev AlertEvent) error {
	var telegramID pgtype.Int8
	var enabled bool
	var plan pgtype.Text
	err := db.QueryRow(ctx, `
		SELECT telegram_id, telegram_enabled, plan
		FROM users
		WHERE num_id = $1 AND telegram_id IS NOT NULL
	`, ev.UserID).Scan(&telegramID, &enabled, &plan)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if !telegramID.Valid || !enabled {
		return nil
	}
	planValue := strings.ToLower(strings.TrimSpace(plan.String))
	if planValue == "" || planValue == "free" {
		return nil
	}
	if (planValue == "standard" || planValue == "standart") &&
		strings.EqualFold(strings.TrimSpace(ev.ScannerSlot), "SLOT_3") {
		return nil
	}
	allowed, count, limit, err := reserveTelegramSignal(ctx, db, rdb, ev.UserID, ev.Symbol, ev.DeliveryID)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}
	text := formatTelegramSignal(ev, count, limit)
	if err := sendTelegramMessage(token, telegramID.Int64, text); err != nil {
		if releaseErr := releaseTelegramSignalReservation(ctx, rdb, ev.UserID, ev.Symbol, ev.DeliveryID); releaseErr != nil {
			return fmt.Errorf("send Telegram: %v; release quota reservation: %w", err, releaseErr)
		}
		return err
	}
	return nil
}

func reserveTelegramSignal(ctx context.Context, db rowQuerier, rdb *redis.Client, userID int64, symbol string, deliveryID int64) (bool, int64, int, error) {
	limit, err := getTelegramSignalLimit(ctx, db, userID, symbol)
	if err != nil {
		return false, 0, limit, err
	}
	if rdb == nil {
		return true, 0, limit, nil
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	countKey := fmt.Sprintf("tg:limit:%d:%s", userID, symbol)
	reservationKey := fmt.Sprintf("tg:delivery:%d", deliveryID)
	result, err := rdb.Eval(ctx, `
		local reserved = redis.call('GET', KEYS[2])
		if reserved then
			return {1, tonumber(reserved)}
		end
		local count = tonumber(redis.call('GET', KEYS[1]) or '0')
		local max_count = tonumber(ARGV[1])
		if max_count > 0 and count >= max_count then
			return {0, count}
		end
		count = redis.call('INCR', KEYS[1])
		if count == 1 then
			redis.call('EXPIRE', KEYS[1], 86400)
		end
		redis.call('SET', KEYS[2], count, 'EX', 172800)
		return {1, count}
	`, []string{countKey, reservationKey}, limit).Result()
	if err != nil {
		return false, 0, limit, err
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return false, 0, limit, fmt.Errorf("unexpected Telegram quota result: %#v", result)
	}
	allowed, okAllowed := values[0].(int64)
	count, okCount := values[1].(int64)
	if !okAllowed || !okCount {
		return false, 0, limit, fmt.Errorf("invalid Telegram quota result: %#v", result)
	}
	return allowed == 1, count, limit, nil
}

func releaseTelegramSignalReservation(ctx context.Context, rdb *redis.Client, userID int64, symbol string, deliveryID int64) error {
	if rdb == nil {
		return nil
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	countKey := fmt.Sprintf("tg:limit:%d:%s", userID, symbol)
	reservationKey := fmt.Sprintf("tg:delivery:%d", deliveryID)
	return rdb.Eval(ctx, `
		if redis.call('DEL', KEYS[2]) == 1 then
			local count = tonumber(redis.call('GET', KEYS[1]) or '0')
			if count > 0 then
				redis.call('DECR', KEYS[1])
			end
		end
		return 1
	`, []string{countKey, reservationKey}).Err()
}

func getTelegramSignalLimit(ctx context.Context, db rowQuerier, userID int64, symbol string) (int, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return 0, nil
	}
	var limit int
	err := db.QueryRow(ctx, `
		SELECT daily_limit
		FROM telegram_signal_limits
		WHERE user_id = $1 AND symbol = $2
	`, userID, symbol).Scan(&limit)
	if err == nil {
		return limit, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	// fallback: общий лимит для всех пар
	var globalLimit int
	err = db.QueryRow(ctx, `
		SELECT daily_limit
		FROM telegram_signal_limits
		WHERE user_id = $1 AND symbol = '*'
	`, userID).Scan(&globalLimit)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return globalLimit, nil
}

func formatTelegramSignal(ev AlertEvent, count int64, limit int) string {
	when := time.UnixMilli(ev.TS).Format("02.01.2006 15:04")
	dir := strings.ToUpper(string(ev.Direction))
	slot := "Сканер"
	switch strings.ToUpper(strings.TrimSpace(ev.ScannerSlot)) {
	case "SLOT_1", "1":
		slot = "Сканер 1️⃣"
	case "SLOT_2", "2":
		slot = "Сканер 2️⃣"
	case "SLOT_3", "3":
		slot = "Сканер 3️⃣"
	}

	symbol := strings.ToUpper(strings.TrimSpace(ev.Symbol))
	escSymbol := escapeHTML(symbol)
	links := linksForSymbol(ev.Exchange, symbol)

	indicator := strings.ToLower(strings.TrimSpace(ev.Indicator))
	exchangeLabel := strings.ToUpper(ev.Exchange)
	if ev.CombinedExchanges {
		exchangeLabel = "5 бирж"
	}
	changeLine := fmt.Sprintf("Изменение: %.2f%%", ev.ChangePercent)
	if ev.CombinedExchanges {
		changeLine = fmt.Sprintf(
			"Совокупные ликвидации: %s $ (Bybit: %s $ + Binance: %s $ + OKX: %s $ + Bitget: %s $ + Gate.io: %s $)",
			formatIntWithSpaces(int64(math.Round(ev.Liquidations))),
			formatIntWithSpaces(int64(math.Round(ev.BybitLiquidations))),
			formatIntWithSpaces(int64(math.Round(ev.BinanceLiquidations))),
			formatIntWithSpaces(int64(math.Round(ev.OKXLiquidations))),
			formatIntWithSpaces(int64(math.Round(ev.BitgetLiquidations))),
			formatIntWithSpaces(int64(math.Round(ev.GateIOLiquidations))),
		)
	} else if strings.HasPrefix(indicator, "liquidations") {
		changeLine = fmt.Sprintf("Ликвидации: %s $", formatIntWithSpaces(int64(math.Round(ev.Liquidations))))
	}

	limitLine := ""
	if count > 0 || limit >= 0 {
		limitLine = fmt.Sprintf("\nСигналов за 24ч: %s", formatIntWithSpaces(count))
	}

	return fmt.Sprintf(
		"🔔 Новый сигнал\nИсточник: %s\nБиржа: %s\nСимвол: %s\nНаправление: %s\n%s\nЦена: %.6f\nВремя: %s%s\n\nБиржи: <a href=\"%s\">Bybit</a> | <a href=\"%s\">Binance</a> | <a href=\"%s\">OKX</a>\nИнструменты: <a href=\"%s\">CoinGlass</a> | <a href=\"%s\">TradingView</a>",
		slot,
		exchangeLabel,
		escSymbol,
		dir,
		changeLine,
		ev.PriceNow,
		when,
		limitLine,
		links.Bybit,
		links.Binance,
		links.OKX,
		links.CoinGlass,
		links.TradingView,
	)
}

func formatIntWithSpaces(n int64) string {
	if n == 0 {
		return "0"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return sign + s
	}
	var b strings.Builder
	b.Grow(len(s) + len(s)/3)
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return sign + b.String()
}

type symbolLinks struct {
	Bybit       string
	Binance     string
	OKX         string
	CoinGlass   string
	TradingView string
}

func linksForSymbol(exchange, symbol string) symbolLinks {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	if sym == "" {
		return symbolLinks{}
	}
	venue := "Bybit"
	tvVenue := "BYBIT"
	if normalizeExchange(exchange) == "binance" {
		venue = "Binance"
		tvVenue = "BINANCE"
	}
	return symbolLinks{
		Bybit:       fmt.Sprintf("https://www.bybit.com/en-US/trade/usdt/%s", sym),
		Binance:     fmt.Sprintf("https://www.binance.com/en/futures/%s", sym),
		OKX:         fmt.Sprintf("https://www.okx.com/trade-swap/%s-swap", strings.ToLower(sym)),
		CoinGlass:   fmt.Sprintf("https://www.coinglass.com/tv/ru/%s_%s", venue, sym),
		TradingView: fmt.Sprintf("https://www.tradingview.com/chart/?symbol=%s:%s.P", tvVenue, sym),
	}
}

func escapeHTML(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
	)
	return replacer.Replace(s)
}

func sendTelegramMessage(token string, chatID int64, text string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	url := "https://api.telegram.org/bot" + token + "/sendMessage"
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		// net/http transport errors include the full request URL. Telegram puts
		// the bot token in that URL, so never persist or log the raw error.
		return errors.New("telegram_send_transport_error")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		err := fmt.Errorf("telegram_send_status_%d", resp.StatusCode)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests {
			return markOutboxPermanent(err)
		}
		return err
	}
	return nil
}

// ---- Core processing ----
type Engine struct {
	cfg   Config
	rdb   *redis.Client
	rules *RulesCache

	outcomeScheduler *outcomeScheduler
	unifiedHistory   *sparseEngineState
	compactOwner     compactBootstrapOwner
	streamSafety     streamSafetyCounters
	streamProcessing sync.Mutex
}

func NewEngine(cfg Config, rdb *redis.Client, rules *RulesCache) *Engine {
	engine := &Engine{
		cfg: cfg, rdb: rdb, rules: rules,
	}
	engine.unifiedHistory = newSparseEngineState(rules, cfg.ImportantEventsEnabled, cfg.MaxInstruments)
	engine.outcomeScheduler = newOutcomeScheduler(defaultOutcomeSchedulerTTL, defaultOutcomeSchedulerMaxUnits)
	return engine
}

func ruleConditions(rule Rule) []Condition {
	if len(rule.Conditions) > 0 {
		return rule.Conditions
	}
	return []Condition{{
		Indicator:       rule.Indicator,
		Direction:       rule.Direction,
		ThresholdPct:    rule.ThresholdPct,
		ThresholdAmount: rule.ThresholdAmount,
	}}
}

func ruleUsesCombinedLiquidations(rule Rule) bool {
	for _, condition := range ruleConditions(rule) {
		if condition.Indicator == "liquidationsCombined" {
			return true
		}
	}
	return false
}

func sameCombinedLiquidationRuleConfig(a, b Rule) bool {
	if !ruleUsesCombinedLiquidations(a) || !ruleUsesCombinedLiquidations(b) {
		return false
	}
	if a.UserID != b.UserID || a.MarketType != b.MarketType || a.Symbol != b.Symbol ||
		a.WindowMinutes != b.WindowMinutes || a.CooldownSeconds != b.CooldownSeconds ||
		a.ScannerSlot != b.ScannerSlot {
		return false
	}
	aConditions := ruleConditions(a)
	bConditions := ruleConditions(b)
	if len(aConditions) != len(bConditions) {
		return false
	}
	for i := range aConditions {
		if aConditions[i] != bConditions[i] {
			return false
		}
	}
	return true
}

func preferCombinedLiquidationRule(candidate, current Rule) bool {
	candidateBybit := normalizeExchange(candidate.Exchange) == "bybit"
	currentBybit := normalizeExchange(current.Exchange) == "bybit"
	if candidateBybit != currentBybit {
		return candidateBybit
	}
	if !candidate.UpdatedAt.Equal(current.UpdatedAt) {
		return candidate.UpdatedAt.After(current.UpdatedAt)
	}
	return candidate.ID > current.ID
}

func dedupeCombinedLiquidationRules(rules []Rule) []Rule {
	out := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		if !ruleUsesCombinedLiquidations(rule) {
			out = append(out, rule)
			continue
		}
		duplicateIndex := -1
		for i := range out {
			if sameCombinedLiquidationRuleConfig(rule, out[i]) {
				duplicateIndex = i
				break
			}
		}
		if duplicateIndex < 0 {
			out = append(out, rule)
			continue
		}
		if preferCombinedLiquidationRule(rule, out[duplicateIndex]) {
			out[duplicateIndex] = rule
		}
	}
	return out
}

func minuteKey(tsMs int64) int64 { return tsMs / 60000 }

func hasInvalidMetric(ce CandleEvent, indicator string) bool {
	for _, name := range ce.InvalidMetrics {
		if name == indicator {
			return true
		}
	}
	return false
}

func addInvalidMetric(ce *CandleEvent, indicator string) {
	if hasInvalidMetric(*ce, indicator) {
		return
	}
	ce.InvalidMetrics = append(ce.InvalidMetrics, indicator)
}

func applyCanonicalMetric(ce *CandleEvent, name string, metric MetricValue) {
	if ce == nil {
		return
	}
	if !metric.Valid || math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) {
		addInvalidMetric(ce, name)
		return
	}
	switch name {
	case "open":
		ce.Open = metric.Value
	case "high":
		ce.High = metric.Value
	case "low":
		ce.Low = metric.Value
	case "close":
		ce.Close = metric.Value
	case "volume":
		ce.Volume = metric.Value
	case "delta":
		ce.Delta = metric.Value
	case "cvd":
		ce.CVD = metric.Value
	case "openInterest":
		ce.OpenInterest = metric.Value
	case "fundingRate":
		ce.FundingRate = metric.Value
	case "markPrice":
		ce.MarkPrice = metric.Value
	case "indexPrice":
		ce.IndexPrice = metric.Value
	case "orderbookBid":
		ce.OrderbookBid = metric.Value
	case "orderbookAsk":
		ce.OrderbookAsk = metric.Value
	case "orderbookSpread":
		ce.OrderbookSpread = metric.Value
	case "orderbookBidDepth":
		ce.OrderbookBidDepth = metric.Value
	case "orderbookAskDepth":
		ce.OrderbookAskDepth = metric.Value
	case "tradeLastPrice":
		ce.TradeLastPrice = metric.Value
	case "tradeLastSize":
		ce.TradeLastSize = metric.Value
	case "tradeLastSide":
		ce.TradeLastSide = metric.Value
	case "tradeBuyVolume":
		ce.TradeBuyVolume = metric.Value
	case "tradeSellVolume":
		ce.TradeSellVolume = metric.Value
	case "tradeBuyUsd":
		ce.TradeBuyUsd = metric.Value
	case "tradeSellUsd":
		ce.TradeSellUsd = metric.Value
	case "tradeCount":
		ce.TradeCount = metric.Value
	case "largestTradeUsd":
		ce.LargestTradeUsd = metric.Value
	case "longRatio":
		ce.LongRatio = metric.Value
	case "shortRatio":
		ce.ShortRatio = metric.Value
	case "longShortRatio":
		ce.LongShortRatio = metric.Value
	case "liquidations":
		ce.Liquidations = metric.Value
	case "liquidationsLong":
		ce.LiquidationsLong = metric.Value
	case "liquidationsShort":
		ce.LiquidationsShort = metric.Value
	case "liquidationCount":
		ce.LiquidationCount = metric.Value
	case "largestLiquidation":
		ce.LargestLiquidation = metric.Value
	case "fundingIntervalHours":
		ce.FundingIntervalHours = metric.Value
	}
}

func computePct(now, then float64) float64 {
	if then == 0 {
		return 0
	}
	return (now - then) / then * 100.0
}

func shouldTrigger(dir Direction, pct, threshold float64) bool {
	switch dir {
	case DirUp:
		return pct >= threshold
	case DirDown:
		return pct <= -threshold
	default:
		return math.Abs(pct) >= threshold
	}
}

type ConditionEval struct {
	Now           float64
	Then          float64
	Pct           float64
	Cmp           string
	Want          float64
	Got           float64
	LiqSum        float64
	BybitLiqSum   float64
	BinanceLiqSum float64
	OKXLiqSum     float64
	BitgetLiqSum  float64
	GateIOLiqSum  float64
}

type EvalResult struct {
	Available bool
	Matched   bool
	Eval      ConditionEval
	Reason    string
}

func parsePricePoints(indicator string) (string, string) {
	if indicator == "price" {
		return "close", "close"
	}
	if strings.HasPrefix(indicator, "price:") {
		parts := strings.Split(indicator, ":")
		if len(parts) == 3 {
			return parts[1], parts[2]
		}
	}
	return "close", "close"
}

func validateConditions(conds []Condition) bool {
	if len(conds) == 0 || len(conds) > 3 {
		return false
	}
	for _, c := range conds {
		if strings.TrimSpace(c.Indicator) == "" {
			return false
		}
		if strings.HasPrefix(c.Indicator, "liquidations") {
			if c.ThresholdAmount <= 0 {
				return false
			}
			continue
		}
		if c.ThresholdPct < 0 {
			return false
		}
	}
	return true
}

func parseRuleConditions(raw []byte) []Condition {
	if len(raw) == 0 {
		return nil
	}
	var dto []ConditionDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil
	}
	out := make([]Condition, 0, len(dto))
	for _, c := range dto {
		indicator := strings.TrimSpace(c.Indicator)
		if indicator == "" {
			continue
		}
		dir := strings.ToLower(strings.TrimSpace(c.Direction))
		cd := DirBoth
		switch Direction(dir) {
		case DirUp, DirDown, DirBoth:
			cd = Direction(dir)
		}
		cond := Condition{
			Indicator: indicator,
			Direction: cd,
			Negate:    c.Negate,
		}
		if strings.HasPrefix(indicator, "liquidations") {
			if c.ThresholdAmount != nil {
				cond.ThresholdAmount = *c.ThresholdAmount
			}
		} else if c.ThresholdPercent != nil {
			cond.ThresholdPct = *c.ThresholdPercent
		}
		out = append(out, cond)
	}
	return out
}

// ---- Rules refresher ----
func startRulesRefresher(ctx context.Context, pool *pgxpool.Pool, cache *RulesCache, every time.Duration, slotFilter string) {
	// initial load
	{
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		m, err := loadEnabledRulesForSlot(c, pool, slotFilter)
		cancel()
		if err != nil {
			log.Printf("rules initial load failed: %v", err)
		} else {
			cache.Set(m)
			log.Printf("rules loaded: symbols=%d slotFilter=%s", cache.SymbolsCount(), slotFilter)
		}
	}

	if every <= 0 {
		every = 15 * time.Second
	}

	t := time.NewTicker(every)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c, cancel := context.WithTimeout(ctx, 10*time.Second)
			m, err := loadEnabledRulesForSlot(c, pool, slotFilter)
			cancel()
			if err != nil {
				log.Printf("rules refresh failed: %v", err)
				continue
			}
			cache.Set(m)
			log.Printf("rules refreshed: symbols=%d slotFilter=%s", cache.SymbolsCount(), slotFilter)
		}
	}
}

func mergeRuleMaps(ruleMaps ...map[string][]Rule) map[string][]Rule {
	merged := make(map[string][]Rule)
	for _, ruleMap := range ruleMaps {
		for key, rules := range ruleMap {
			for _, rule := range rules {
				merged[key] = append(merged[key], cloneRule(rule))
			}
		}
	}
	return merged
}

func isValidLoadedRuleWindow(windowMinutes, maxWindowMinutes int) bool {
	return windowMinutes >= 1 && windowMinutes <= maxWindowMinutes
}

func loadEnabledRulesForAllSlots(ctx context.Context, pool *pgxpool.Pool, maxWindowMinutes int) (map[string][]Rule, error) {
	return loadEnabledRules(ctx, pool, "", true, maxWindowMinutes)
}

func startUnifiedRulesRefresher(ctx context.Context, pool *pgxpool.Pool, cache *RulesCache, every time.Duration, maxWindowMinutes int) {
	refresh := func(label string) {
		loadCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		rules, err := loadEnabledRulesForAllSlots(loadCtx, pool, maxWindowMinutes)
		cancel()
		if err != nil {
			log.Printf("unified rules %s failed: %v", label, err)
			return
		}
		cache.Set(rules)
		log.Printf("unified rules %s: symbols=%d slots=SLOT_1,SLOT_2,SLOT_3", label, cache.SymbolsCount())
	}
	refresh("initial load")
	if every <= 0 {
		every = 15 * time.Second
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh("refresh")
		}
	}
}

func max1(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}

func main() {
	cfg := loadConfig()
	if err := validateStartupConfig(cfg); err != nil {
		log.Fatalf("invalid worker configuration: %v", err)
	}
	log.Printf("starting sparse alert engine consumerGroup=%s consumerName=%s compact=%s alerts=%s",
		cfg.MarketConsumerGroup, cfg.ConsumerName, strings.Join(cfg.MarketStreams, ","), cfg.AlertsStream)
	if strings.TrimSpace(cfg.RulesSlotFilter) != "" {
		log.Printf("rules slot filter enabled: %s", cfg.RulesSlotFilter)
	}

	if strings.TrimSpace(cfg.PGDSN) == "" {
		log.Fatal("PG_DSN is required")
	}
	if cfg.RulesRefreshSeconds <= 0 {
		cfg.RulesRefreshSeconds = 15
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
		PoolSize: 50,
	})
	{
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := rdb.Ping(c).Err(); err != nil {
			log.Fatalf("redis ping failed: %v", err)
		}
	}
	if cfg.AlertsStreamMaxLenApprox > 0 {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		trimmed, err := rdb.Do(c, "XTRIM", cfg.AlertsStream, "MAXLEN", "~", cfg.AlertsStreamMaxLenApprox).Int64()
		cancel()
		if err != nil {
			log.Fatalf("trim alerts stream %s failed: %v", cfg.AlertsStream, err)
		}
		if trimmed > 0 {
			log.Printf("trimmed alerts stream=%s removed=%d maxLen=%d", cfg.AlertsStream, trimmed, cfg.AlertsStreamMaxLenApprox)
		}
	}
	defer func() { _ = rdb.Close() }()

	// Postgres
	poolCfg, err := pgxpool.ParseConfig(cfg.PGDSN)
	if err != nil {
		log.Fatalf("pg dsn parse: %v", err)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 1
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute
	poolCfg.HealthCheckPeriod = 30 * time.Second

	db, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		log.Fatalf("pg connect: %v", err)
	}
	defer db.Close()
	{
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := db.Ping(c); err != nil {
			log.Fatalf("pg ping failed: %v", err)
		}
	}

	rules := NewRulesCache()
	{
		loadCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		initialRules, loadErr := loadEnabledRulesForSlot(loadCtx, db, cfg.RulesSlotFilter)
		cancel()
		if loadErr != nil {
			log.Fatalf("rules initial load failed before consumer start: %v", loadErr)
		}
		rules.Set(initialRules)
		log.Printf("rules initial barrier complete: symbols=%d slotFilter=%s", rules.SymbolsCount(), cfg.RulesSlotFilter)
	}
	go startRulesRefresher(ctx, db, rules, time.Duration(cfg.RulesRefreshSeconds)*time.Second, cfg.RulesSlotFilter)

	engine := NewEngine(cfg, rdb, rules)
	var snapshotStore *unifiedSnapshotStore
	var compactOwner *pgCompactBootstrapOwner
	defer func() {
		if compactOwner != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := compactOwner.Close(closeCtx); err != nil {
				log.Printf("compact stream owner close failed: %v", err)
			}
		}
	}()
	history, store, owner, bootstrapErr := bootstrapUnifiedSnapshotRuntime(ctx, db, rdb, cfg, rules)
	if bootstrapErr != nil {
		log.Fatalf("sparse history snapshot/bootstrap failed: %v", bootstrapErr)
	}
	engine.unifiedHistory = history
	engine.compactOwner = owner
	snapshotStore = store
	compactOwner = owner
	if snapshotStore != nil {
		go func() {
			err := runOwnedUnifiedSnapshotLoop(ctx, compactOwner, snapshotStore, engine.unifiedHistory, time.Duration(cfg.SnapshotEverySeconds)*time.Second)
			if err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
				log.Printf("sparse snapshot loop stopped: %v", err)
				stop()
			}
		}()
	}
	go startOutboxDispatcher(ctx, db, rdb, cfg)
	go startWorkerObservability(ctx, engine, db, rdb, time.Duration(cfg.ObservabilityEverySeconds)*time.Second)
	if cfg.ImportantEventsEnabled {
		go startFundamentalImportantEventMonitor(ctx, db, rdb, cfg)
	}
	if cfg.ImportantEventsEnabled {
		go startImportantEventResolutionMonitor(ctx, db, rdb, cfg)
	}
	if cfg.ImportantEventsEnabled && cfg.OnchainEnabled {
		go startOnchainMonitor(ctx, db, rdb, cfg)
	}
	if err := engine.consumeCompactStreams(ctx, db); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("sparse engine stopped: %v", err)
	}
	if snapshotStore != nil {
		if err := saveFinalOwnedUnifiedSnapshot(compactOwner, snapshotStore, engine.unifiedHistory); err != nil {
			log.Printf("final sparse snapshot save skipped/failed: %v", err)
		}
	}
	log.Printf("shutdown complete")
}

func validateStartupConfig(cfg Config) error {
	if strings.TrimSpace(cfg.TelegramBotToken) == "" {
		return errors.New("TELEGRAM_BOT_TOKEN is required; refusing to mark Telegram delivery complete without sending")
	}
	slot := strings.ToUpper(strings.TrimSpace(cfg.RulesSlotFilter))
	if slot != "SLOT_1" && slot != "SLOT_2" && slot != "SLOT_3" {
		return errors.New("RULES_SLOT_FILTER must be SLOT_1, SLOT_2, or SLOT_3")
	}
	if strings.TrimSpace(cfg.MarketConsumerGroup) == "" {
		return errors.New("MARKET_CONSUMER_GROUP is required")
	}
	if len(cfg.MarketStreams) == 0 {
		return errors.New("MARKET_STREAMS must contain at least one stream")
	}
	if strings.TrimSpace(cfg.SnapshotPath) == "" {
		return errors.New("SPARSE_SNAPSHOT_PATH is required")
	}
	return nil
}
