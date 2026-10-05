package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	compactTransportVersion = 3
	compactCanonicalVersion = 3
	compactMetricLayoutV1   = 1
	compactMaxPayloadBytes  = 8 << 20
	compactMaxCatalogSize   = 20_000
)

var compactMetricLayout = [...]string{
	"open", "high", "low", "close", "volume", "quoteVolumeUsd", "delta", "cvd",
	"openInterest", "fundingRate", "markPrice", "indexPrice", "orderbookBid", "orderbookAsk",
	"orderbookSpread", "orderbookBidDepth", "orderbookAskDepth", "tradeLastPrice", "tradeLastSize",
	"tradeLastSide", "tradeBuyVolume", "tradeSellVolume", "tradeBuyUsd", "tradeSellUsd", "tradeCount",
	"largestTradeUsd", "medianTradeUsd", "largestTradeSide", "tradeClusterUsd", "tradeClusterCount",
	"tradeClusterSide", "tradePriceImpactPct", "longRatio", "shortRatio", "longShortRatio", "liquidations",
	"liquidationsLong", "liquidationsShort", "liquidationCount", "largestLiquidation", "fundingIntervalHours",
}

var compactMetricLayoutHash = compactStringListHash(compactMetricLayout[:])

type compactWireBatch struct {
	TransportVersion int                         `json:"v"`
	CanonicalVersion int                         `json:"cv"`
	Kind             string                      `json:"k"`
	Exchange         string                      `json:"e"`
	MarketType       string                      `json:"m"`
	Minute           int64                       `json:"t"`
	ShardIndex       int                         `json:"si"`
	ShardTotal       int                         `json:"sn"`
	CatalogVersion   string                      `json:"h"`
	Catalog          []string                    `json:"c"`
	LayoutVersion    int                         `json:"lv,omitempty"`
	LayoutHash       string                      `json:"lh,omitempty"`
	Coverage         bool                        `json:"q"`
	Source           string                      `json:"src,omitempty"`
	BatchID          string                      `json:"id"`
	CandleRows       []compactWireCandleRow      `json:"r,omitempty"`
	LiquidationRows  []compactWireLiquidationRow `json:"lr,omitempty"`
}

type compactWireCandleRow struct {
	Symbol                string    `json:"s"`
	Values                []float64 `json:"x"`
	Validity              uint64    `json:"u"`
	OpenInterestTimestamp int64     `json:"o,omitempty"`
}

type compactWireLiquidationRow struct {
	Symbol string    `json:"s"`
	Values []float64 `json:"x"`
}

type compactDecodedBatch struct {
	BatchID        string
	Kind           string
	Exchange       string
	MarketType     string
	Minute         int64
	ShardIndex     int
	ShardTotal     int
	Coverage       bool
	CatalogVersion string
	Catalog        []string
	Events         []CandleEvent
}

func decodeCompactBatch(raw []byte) (compactDecodedBatch, error) {
	if len(raw) == 0 || len(raw) > compactMaxPayloadBytes {
		return compactDecodedBatch{}, fmt.Errorf("compact payload size %d is outside 1..%d", len(raw), compactMaxPayloadBytes)
	}
	var batch compactWireBatch
	if err := json.Unmarshal(raw, &batch); err != nil {
		return compactDecodedBatch{}, err
	}
	if err := validateCompactBatch(batch); err != nil {
		return compactDecodedBatch{}, err
	}
	decoded := compactDecodedBatch{
		BatchID:        batch.BatchID,
		Kind:           batch.Kind,
		Exchange:       normalizeExchange(batch.Exchange),
		MarketType:     normalizeMarketType(batch.MarketType),
		Minute:         batch.Minute,
		ShardIndex:     batch.ShardIndex,
		ShardTotal:     batch.ShardTotal,
		Coverage:       batch.Coverage,
		CatalogVersion: batch.CatalogVersion,
		Catalog:        append([]string(nil), batch.Catalog...),
	}
	switch batch.Kind {
	case "c":
		events, err := decodeCompactCandleRows(batch)
		if err != nil {
			return compactDecodedBatch{}, err
		}
		decoded.Events = events
	case "l":
		events, err := decodeCompactLiquidationRows(batch)
		if err != nil {
			return compactDecodedBatch{}, err
		}
		decoded.Events = events
	default:
		return compactDecodedBatch{}, fmt.Errorf("unsupported compact kind %q", batch.Kind)
	}
	return decoded, nil
}

func validateCompactBatch(batch compactWireBatch) error {
	if batch.TransportVersion != compactTransportVersion || batch.CanonicalVersion != compactCanonicalVersion {
		return errors.New("unsupported compact transport or canonical version")
	}
	if batch.Minute <= 0 || batch.Minute%60_000 != 0 {
		return errors.New("compact minute is missing or unaligned")
	}
	exchange := strings.TrimSpace(batch.Exchange)
	marketType := strings.TrimSpace(batch.MarketType)
	if exchange == "" || marketType == "" || exchange != normalizeExchange(exchange) || marketType != normalizeMarketType(marketType) {
		return errors.New("compact exchange and market type must be explicit canonical values")
	}
	if batch.ShardTotal < 1 || batch.ShardIndex < 0 || batch.ShardIndex >= batch.ShardTotal {
		return errors.New("invalid compact shard identity")
	}
	if len(batch.Catalog) == 0 || len(batch.Catalog) > compactMaxCatalogSize {
		return errors.New("invalid compact catalog size")
	}
	normalized, version := compactCatalog(batch.Catalog)
	if len(normalized) != len(batch.Catalog) || version != batch.CatalogVersion {
		return errors.New("compact catalog version or uniqueness mismatch")
	}
	for index := range normalized {
		if normalized[index] != batch.Catalog[index] {
			return errors.New("compact catalog must be normalized and sorted")
		}
		if stableCompactSymbolShard(normalized[index], batch.ShardTotal) != batch.ShardIndex {
			return errors.New("compact catalog symbol is routed to the wrong shard")
		}
	}
	if compactBatchID(batch) != batch.BatchID {
		return errors.New("compact batch id mismatch")
	}
	return nil
}

func stableCompactSymbolShard(symbol string, shardTotal int) int {
	if shardTotal <= 1 {
		return 0
	}
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(symbol))))
	return int(binary.BigEndian.Uint64(sum[:8]) % uint64(shardTotal))
}

func decodeCompactCandleRows(batch compactWireBatch) ([]CandleEvent, error) {
	if batch.LayoutVersion != compactMetricLayoutV1 || batch.LayoutHash != compactMetricLayoutHash || len(batch.LiquidationRows) != 0 || len(batch.CandleRows) > len(batch.Catalog) {
		return nil, errors.New("invalid compact candle layout")
	}
	if batch.Coverage != (len(batch.CandleRows) == len(batch.Catalog)) {
		return nil, errors.New("compact candle coverage does not match row count")
	}
	catalog := make(map[string]struct{}, len(batch.Catalog))
	for _, symbol := range batch.Catalog {
		catalog[symbol] = struct{}{}
	}
	seen := make(map[string]struct{}, len(batch.CandleRows))
	out := make([]CandleEvent, 0, len(batch.CandleRows))
	validBits := (uint64(1) << len(compactMetricLayout)) - 1
	for _, row := range batch.CandleRows {
		if row.Symbol != strings.ToUpper(strings.TrimSpace(row.Symbol)) || len(row.Symbol) > 64 {
			return nil, fmt.Errorf("invalid compact candle symbol %q", row.Symbol)
		}
		if _, ok := catalog[row.Symbol]; !ok {
			return nil, fmt.Errorf("compact candle symbol %s is absent from catalog", row.Symbol)
		}
		if _, duplicate := seen[row.Symbol]; duplicate {
			return nil, fmt.Errorf("duplicate compact candle symbol %s", row.Symbol)
		}
		if len(row.Values) != len(compactMetricLayout) || row.Validity&^validBits != 0 {
			return nil, fmt.Errorf("invalid compact candle row layout for %s", row.Symbol)
		}
		seen[row.Symbol] = struct{}{}
		event := CandleEvent{SchemaVersion: batch.CanonicalVersion, Exchange: normalizeExchange(batch.Exchange), MarketType: normalizeMarketType(batch.MarketType), Symbol: row.Symbol, TS: batch.Minute, OpenInterestTimestamp: row.OpenInterestTimestamp, Metrics: make(map[string]MetricValue, len(compactMetricLayout)), ShardIndex: batch.ShardIndex, ShardTotal: batch.ShardTotal}
		for index, name := range compactMetricLayout {
			value := row.Values[index]
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("non-finite compact metric %s for %s", name, row.Symbol)
			}
			valid := row.Validity&(uint64(1)<<index) != 0
			setCompactCandleMetric(&event, name, value)
			event.Metrics[name] = MetricValue{Value: value, Valid: valid, Timestamp: batch.Minute, Source: batch.Source}
			if !valid {
				event.InvalidMetrics = append(event.InvalidMetrics, name)
			}
		}
		if event.Metrics["openInterest"].Valid && event.OpenInterestTimestamp <= 0 {
			return nil, fmt.Errorf("valid open interest lacks timestamp for %s", row.Symbol)
		}
		out = append(out, event)
	}
	return out, nil
}

func decodeCompactLiquidationRows(batch compactWireBatch) ([]CandleEvent, error) {
	if batch.LayoutVersion != 0 || len(batch.CandleRows) != 0 || len(batch.LiquidationRows) > len(batch.Catalog) || strings.TrimSpace(batch.Source) == "" {
		return nil, errors.New("invalid compact liquidation layout")
	}
	catalog := make(map[string]struct{}, len(batch.Catalog))
	for _, symbol := range batch.Catalog {
		catalog[symbol] = struct{}{}
	}
	rows := make(map[string][]float64, len(batch.LiquidationRows))
	for _, row := range batch.LiquidationRows {
		if _, ok := catalog[row.Symbol]; !ok {
			return nil, fmt.Errorf("compact liquidation symbol %s is absent from catalog", row.Symbol)
		}
		if _, duplicate := rows[row.Symbol]; duplicate {
			return nil, fmt.Errorf("duplicate compact liquidation symbol %s", row.Symbol)
		}
		if len(row.Values) != 5 {
			return nil, fmt.Errorf("invalid compact liquidation row width for %s", row.Symbol)
		}
		nonzero := false
		for _, value := range row.Values {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				return nil, fmt.Errorf("invalid compact liquidation value for %s", row.Symbol)
			}
			nonzero = nonzero || value != 0
		}
		if !nonzero {
			return nil, fmt.Errorf("zero compact liquidation row must be omitted for %s", row.Symbol)
		}
		rows[row.Symbol] = row.Values
	}
	names := [...]string{"liquidations", "liquidationsLong", "liquidationsShort", "liquidationCount", "largestLiquidation"}
	out := make([]CandleEvent, 0, len(batch.Catalog))
	for _, symbol := range batch.Catalog {
		values := rows[symbol]
		if values == nil {
			values = []float64{0, 0, 0, 0, 0}
		}
		event := CandleEvent{SchemaVersion: batch.CanonicalVersion, Exchange: normalizeExchange(batch.Exchange), MarketType: normalizeMarketType(batch.MarketType), Symbol: symbol, TS: batch.Minute, LiquidationOnly: true, Metrics: make(map[string]MetricValue, len(names)), ShardIndex: batch.ShardIndex, ShardTotal: batch.ShardTotal}
		for index, name := range names {
			setCompactCandleMetric(&event, name, values[index])
			event.Metrics[name] = MetricValue{Value: values[index], Valid: batch.Coverage, Timestamp: batch.Minute + 60_000 - 1, Source: batch.Source}
			if !batch.Coverage {
				event.InvalidMetrics = append(event.InvalidMetrics, name)
			}
		}
		out = append(out, event)
	}
	return out, nil
}

func compactCatalog(symbols []string) ([]string, string) {
	set := make(map[string]struct{}, len(symbols))
	for _, raw := range symbols {
		if symbol := strings.ToUpper(strings.TrimSpace(raw)); symbol != "" {
			set[symbol] = struct{}{}
		}
	}
	catalog := make([]string, 0, len(set))
	for symbol := range set {
		catalog = append(catalog, symbol)
	}
	sort.Strings(catalog)
	return catalog, compactStringListHash(catalog)
}

func compactStringListHash(values []string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\n")))
	return hex.EncodeToString(sum[:16])
}

func compactBatchID(batch compactWireBatch) string {
	return fmt.Sprintf("%s:%s:%s:%d:%d:%d:%s:%d:%s", batch.Kind, normalizeExchange(batch.Exchange), normalizeMarketType(batch.MarketType), batch.Minute, batch.ShardIndex, batch.ShardTotal, batch.CatalogVersion, batch.LayoutVersion, batch.LayoutHash)
}

func setCompactCandleMetric(event *CandleEvent, name string, value float64) {
	switch name {
	case "open":
		event.Open = value
	case "high":
		event.High = value
	case "low":
		event.Low = value
	case "close":
		event.Close = value
	case "volume":
		event.Volume = value
	case "quoteVolumeUsd":
		event.QuoteVolumeUsd = value
	case "delta":
		event.Delta = value
	case "cvd":
		event.CVD = value
	case "openInterest":
		event.OpenInterest = value
	case "fundingRate":
		event.FundingRate = value
	case "markPrice":
		event.MarkPrice = value
	case "indexPrice":
		event.IndexPrice = value
	case "orderbookBid":
		event.OrderbookBid = value
	case "orderbookAsk":
		event.OrderbookAsk = value
	case "orderbookSpread":
		event.OrderbookSpread = value
	case "orderbookBidDepth":
		event.OrderbookBidDepth = value
	case "orderbookAskDepth":
		event.OrderbookAskDepth = value
	case "tradeLastPrice":
		event.TradeLastPrice = value
	case "tradeLastSize":
		event.TradeLastSize = value
	case "tradeLastSide":
		event.TradeLastSide = value
	case "tradeBuyVolume":
		event.TradeBuyVolume = value
	case "tradeSellVolume":
		event.TradeSellVolume = value
	case "tradeBuyUsd":
		event.TradeBuyUsd = value
	case "tradeSellUsd":
		event.TradeSellUsd = value
	case "tradeCount":
		event.TradeCount = value
	case "largestTradeUsd":
		event.LargestTradeUsd = value
	case "medianTradeUsd":
		event.MedianTradeUsd = value
	case "largestTradeSide":
		event.LargestTradeSide = value
	case "tradeClusterUsd":
		event.TradeClusterUsd = value
	case "tradeClusterCount":
		event.TradeClusterCount = value
	case "tradeClusterSide":
		event.TradeClusterSide = value
	case "tradePriceImpactPct":
		event.TradePriceImpactPct = value
	case "longRatio":
		event.LongRatio = value
	case "shortRatio":
		event.ShortRatio = value
	case "longShortRatio":
		event.LongShortRatio = value
	case "liquidations":
		event.Liquidations = value
	case "liquidationsLong":
		event.LiquidationsLong = value
	case "liquidationsShort":
		event.LiquidationsShort = value
	case "liquidationCount":
		event.LiquidationCount = value
	case "largestLiquidation":
		event.LargestLiquidation = value
	case "fundingIntervalHours":
		event.FundingIntervalHours = value
	}
}
