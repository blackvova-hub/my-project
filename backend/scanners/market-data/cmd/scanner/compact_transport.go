package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	compactTransportVersion = 3
	compactMetricLayoutV1   = 1
	compactMetricCount      = 41
	compactCandleKind       = "c"
	compactLiquidationKind  = "l"
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

type compactBatch struct {
	TransportVersion int                     `json:"v"`
	CanonicalVersion int                     `json:"cv"`
	Kind             string                  `json:"k"`
	Exchange         string                  `json:"e"`
	MarketType       string                  `json:"m"`
	Minute           int64                   `json:"t"`
	ShardIndex       int                     `json:"si"`
	ShardTotal       int                     `json:"sn"`
	CatalogVersion   string                  `json:"h"`
	Catalog          []string                `json:"c"`
	LayoutVersion    int                     `json:"lv,omitempty"`
	LayoutHash       string                  `json:"lh,omitempty"`
	Coverage         bool                    `json:"q"`
	Source           string                  `json:"src,omitempty"`
	BatchID          string                  `json:"id"`
	CandleRows       []compactCandleRow      `json:"r,omitempty"`
	LiquidationRows  []compactLiquidationRow `json:"lr,omitempty"`
}

type compactCandleRow struct {
	Symbol                string                      `json:"s"`
	Values                [compactMetricCount]float64 `json:"x"`
	Validity              uint64                      `json:"u"`
	OpenInterestTimestamp int64                       `json:"o,omitempty"`
}

type compactLiquidationRow struct {
	Symbol string     `json:"s"`
	Values [5]float64 `json:"x"`
}

func buildCompactCandleBatch(outputs []CandleOut, symbols []string, minute int64, shardIndex, shardTotal int, exchange, marketType string) (compactBatch, error) {
	catalog, catalogVersion := compactCatalog(symbols)
	batch := compactBatch{
		TransportVersion: compactTransportVersion, CanonicalVersion: schemaVersion, Kind: compactCandleKind,
		Exchange: string(normalizeExchange(exchange)), MarketType: string(normalizeMarketType(marketType)), Minute: minute,
		ShardIndex: shardIndex, ShardTotal: shardTotal, CatalogVersion: catalogVersion, Catalog: catalog,
		LayoutVersion: compactMetricLayoutV1, LayoutHash: compactMetricLayoutHash, Source: string(normalizeExchange(exchange)),
		CandleRows: make([]compactCandleRow, 0, len(outputs)),
	}
	if err := validateCompactHeader(batch); err != nil {
		return compactBatch{}, err
	}
	catalogSet := make(map[string]struct{}, len(catalog))
	for _, symbol := range catalog {
		catalogSet[symbol] = struct{}{}
	}
	seen := make(map[string]struct{}, len(outputs))
	for _, output := range outputs {
		symbol := normalizeSymbol(output.Symbol)
		if _, ok := catalogSet[symbol]; !ok {
			return compactBatch{}, fmt.Errorf("compact candle symbol %s is absent from catalog", symbol)
		}
		if _, duplicate := seen[symbol]; duplicate {
			return compactBatch{}, fmt.Errorf("duplicate compact candle symbol %s", symbol)
		}
		if output.TS != minute || normalizeExchange(output.Exchange) != normalizeExchange(exchange) || normalizeMarketType(output.MarketType) != normalizeMarketType(marketType) {
			return compactBatch{}, fmt.Errorf("compact candle identity mismatch for %s", symbol)
		}
		seen[symbol] = struct{}{}
		row := compactCandleRow{Symbol: symbol, OpenInterestTimestamp: output.OpenInterestTimestamp}
		for index, name := range compactMetricLayout {
			value := compactMetricValue(output, name)
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return compactBatch{}, fmt.Errorf("compact candle metric %s is non-finite for %s", name, symbol)
			}
			row.Values[index] = value
			if metric, ok := output.Metrics[name]; ok && metric.Valid {
				row.Validity |= uint64(1) << index
			}
		}
		batch.CandleRows = append(batch.CandleRows, row)
	}
	sort.Slice(batch.CandleRows, func(i, j int) bool { return batch.CandleRows[i].Symbol < batch.CandleRows[j].Symbol })
	batch.Coverage = len(batch.CandleRows) == len(catalog)
	batch.BatchID = compactBatchID(batch)
	return batch, nil
}

func buildCompactLiquidationBatch(rows []compactLiquidationRow, symbols []string, minute int64, coverage bool, source, exchange, marketType string) (compactBatch, error) {
	catalog, catalogVersion := compactCatalog(symbols)
	batch := compactBatch{
		TransportVersion: compactTransportVersion, CanonicalVersion: schemaVersion, Kind: compactLiquidationKind,
		Exchange: string(normalizeExchange(exchange)), MarketType: string(normalizeMarketType(marketType)), Minute: minute,
		ShardTotal: 1, CatalogVersion: catalogVersion, Catalog: catalog, Coverage: coverage, Source: source,
		LiquidationRows: make([]compactLiquidationRow, 0, len(rows)),
	}
	if err := validateCompactHeader(batch); err != nil {
		return compactBatch{}, err
	}
	catalogSet := make(map[string]struct{}, len(catalog))
	for _, symbol := range catalog {
		catalogSet[symbol] = struct{}{}
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		row.Symbol = normalizeSymbol(row.Symbol)
		if _, ok := catalogSet[row.Symbol]; !ok {
			return compactBatch{}, fmt.Errorf("compact liquidation symbol %s is absent from catalog", row.Symbol)
		}
		if _, duplicate := seen[row.Symbol]; duplicate {
			return compactBatch{}, fmt.Errorf("duplicate compact liquidation symbol %s", row.Symbol)
		}
		seen[row.Symbol] = struct{}{}
		nonzero := false
		for _, value := range row.Values {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				return compactBatch{}, fmt.Errorf("invalid compact liquidation value for %s", row.Symbol)
			}
			nonzero = nonzero || value != 0
		}
		if nonzero {
			batch.LiquidationRows = append(batch.LiquidationRows, row)
		}
	}
	sort.Slice(batch.LiquidationRows, func(i, j int) bool { return batch.LiquidationRows[i].Symbol < batch.LiquidationRows[j].Symbol })
	batch.BatchID = compactBatchID(batch)
	return batch, nil
}

func validateCompactHeader(batch compactBatch) error {
	if batch.TransportVersion != compactTransportVersion || batch.CanonicalVersion != schemaVersion {
		return errors.New("unsupported compact transport or canonical version")
	}
	if batch.Minute <= 0 || batch.Exchange == "" || batch.MarketType == "" || len(batch.Catalog) == 0 || batch.CatalogVersion == "" {
		return errors.New("incomplete compact batch header")
	}
	if batch.ShardTotal < 1 || batch.ShardIndex < 0 || batch.ShardIndex >= batch.ShardTotal {
		return errors.New("invalid compact shard identity")
	}
	return nil
}

func compactCatalog(symbols []string) ([]string, string) {
	set := make(map[string]struct{}, len(symbols))
	for _, raw := range symbols {
		if symbol := normalizeSymbol(raw); symbol != "" {
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

func compactBatchID(batch compactBatch) string {
	return fmt.Sprintf("%s:%s:%s:%d:%d:%d:%s:%d:%s", batch.Kind, batch.Exchange, batch.MarketType, batch.Minute, batch.ShardIndex, batch.ShardTotal, batch.CatalogVersion, batch.LayoutVersion, batch.LayoutHash)
}

func expandCompactCandleBatch(batch compactBatch) ([]CandleOut, error) {
	if err := validateCompactHeader(batch); err != nil {
		return nil, err
	}
	if batch.Kind != compactCandleKind || batch.LayoutVersion != compactMetricLayoutV1 || batch.LayoutHash != compactMetricLayoutHash {
		return nil, errors.New("unsupported compact candle kind or layout")
	}
	out := make([]CandleOut, 0, len(batch.CandleRows))
	seen := make(map[string]struct{}, len(batch.CandleRows))
	for _, row := range batch.CandleRows {
		if _, duplicate := seen[row.Symbol]; duplicate {
			return nil, fmt.Errorf("duplicate compact candle row %s", row.Symbol)
		}
		seen[row.Symbol] = struct{}{}
		event := CandleOut{SchemaVersion: batch.CanonicalVersion, Instrument: NewInstrumentKey(normalizeExchange(batch.Exchange), normalizeMarketType(batch.MarketType), row.Symbol), Exchange: batch.Exchange, MarketType: batch.MarketType, Symbol: row.Symbol, TS: batch.Minute, OpenInterestTimestamp: row.OpenInterestTimestamp, Metrics: make(map[string]MetricValue, len(compactMetricLayout)), ShardIndex: batch.ShardIndex, ShardTotal: batch.ShardTotal}
		for index, name := range compactMetricLayout {
			valid := row.Validity&(uint64(1)<<index) != 0
			value := row.Values[index]
			setCompactMetricValue(&event, name, value)
			event.Metrics[name] = MetricValue{Value: value, Valid: valid}
			if !valid {
				event.InvalidMetrics = append(event.InvalidMetrics, name)
			}
		}
		out = append(out, event)
	}
	return out, nil
}

func expandCompactLiquidationBatch(batch compactBatch) ([]CandleOut, error) {
	if err := validateCompactHeader(batch); err != nil {
		return nil, err
	}
	if batch.Kind != compactLiquidationKind {
		return nil, errors.New("unsupported compact liquidation kind")
	}
	rows := make(map[string][5]float64, len(batch.LiquidationRows))
	for _, row := range batch.LiquidationRows {
		if _, duplicate := rows[row.Symbol]; duplicate {
			return nil, fmt.Errorf("duplicate compact liquidation row %s", row.Symbol)
		}
		rows[row.Symbol] = row.Values
	}
	names := [...]string{"liquidations", "liquidationsLong", "liquidationsShort", "liquidationCount", "largestLiquidation"}
	out := make([]CandleOut, 0, len(batch.Catalog))
	for _, symbol := range batch.Catalog {
		values := rows[symbol]
		event := CandleOut{SchemaVersion: batch.CanonicalVersion, Instrument: NewInstrumentKey(normalizeExchange(batch.Exchange), normalizeMarketType(batch.MarketType), symbol), Exchange: batch.Exchange, MarketType: batch.MarketType, Symbol: symbol, TS: batch.Minute, LiquidationOnly: true, Metrics: make(map[string]MetricValue, len(names)), ShardIndex: batch.ShardIndex, ShardTotal: batch.ShardTotal}
		for index, name := range names {
			setCompactMetricValue(&event, name, values[index])
			event.Metrics[name] = MetricValue{Value: values[index], Valid: batch.Coverage, Timestamp: batch.Minute + ringMinuteMs - 1, Source: batch.Source}
			if !batch.Coverage {
				event.InvalidMetrics = append(event.InvalidMetrics, name)
			}
		}
		out = append(out, event)
	}
	return out, nil
}

func compactMetricValue(event CandleOut, name string) float64 {
	switch name {
	case "open":
		return event.Open
	case "high":
		return event.High
	case "low":
		return event.Low
	case "close":
		return event.Close
	case "volume":
		return event.Volume
	case "quoteVolumeUsd":
		return event.QuoteVolumeUsd
	case "delta":
		return event.Delta
	case "cvd":
		return event.CVD
	case "openInterest":
		return event.OpenInterest
	case "fundingRate":
		return event.FundingRate
	case "markPrice":
		return event.MarkPrice
	case "indexPrice":
		return event.IndexPrice
	case "orderbookBid":
		return event.OrderbookBid
	case "orderbookAsk":
		return event.OrderbookAsk
	case "orderbookSpread":
		return event.OrderbookSpread
	case "orderbookBidDepth":
		return event.OrderbookBidDepth
	case "orderbookAskDepth":
		return event.OrderbookAskDepth
	case "tradeLastPrice":
		return event.TradeLastPrice
	case "tradeLastSize":
		return event.TradeLastSize
	case "tradeLastSide":
		return event.TradeLastSide
	case "tradeBuyVolume":
		return event.TradeBuyVolume
	case "tradeSellVolume":
		return event.TradeSellVolume
	case "tradeBuyUsd":
		return event.TradeBuyUsd
	case "tradeSellUsd":
		return event.TradeSellUsd
	case "tradeCount":
		return event.TradeCount
	case "largestTradeUsd":
		return event.LargestTradeUsd
	case "medianTradeUsd":
		return event.MedianTradeUsd
	case "largestTradeSide":
		return event.LargestTradeSide
	case "tradeClusterUsd":
		return event.TradeClusterUsd
	case "tradeClusterCount":
		return event.TradeClusterCount
	case "tradeClusterSide":
		return event.TradeClusterSide
	case "tradePriceImpactPct":
		return event.TradePriceImpactPct
	case "longRatio":
		return event.LongRatio
	case "shortRatio":
		return event.ShortRatio
	case "longShortRatio":
		return event.LongShortRatio
	case "liquidations":
		return event.Liquidations
	case "liquidationsLong":
		return event.LiquidationsLong
	case "liquidationsShort":
		return event.LiquidationsShort
	case "liquidationCount":
		return event.LiquidationCount
	case "largestLiquidation":
		return event.LargestLiquidation
	case "fundingIntervalHours":
		return event.FundingIntervalHours
	default:
		return 0
	}
}

func setCompactMetricValue(event *CandleOut, name string, value float64) {
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
