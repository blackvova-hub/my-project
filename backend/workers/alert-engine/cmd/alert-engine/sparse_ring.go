package main

import (
	"math"
	"sync"
)

func ringIndex(minute int64, size int) int {
	index := minute % int64(size)
	if index < 0 {
		index += int64(size)
	}
	return int(index)
}

type sparseSeries struct {
	minuteKeys []int64
	values     []float64
	occupied   []uint64
	valid      []uint64
	lastMinute int64
	hasLast    bool
}

func newSparseSeries(capacity int) *sparseSeries {
	series := &sparseSeries{}
	series.Resize(capacity)
	return series
}

func (s *sparseSeries) Capacity() int {
	if s == nil {
		return 0
	}
	return len(s.minuteKeys)
}

// Put stores a minute only when it can still belong to the retained window.
// Redis Streams may redeliver an old pending message after newer data has
// already wrapped the ring. Rejecting that replay prevents it from replacing
// a newer slot which happens to have the same modulo index.
func (s *sparseSeries) Put(minute int64, value float64, valid bool) bool {
	if s == nil || len(s.minuteKeys) == 0 {
		return false
	}
	if s.hasLast && minute < s.lastMinute-int64(len(s.minuteKeys))+1 {
		return false
	}
	if s.hasLast && minute > s.lastMinute {
		s.evictBefore(minute - int64(len(s.minuteKeys)) + 1)
	}
	index := ringIndex(minute, len(s.minuteKeys))
	if sparseBit(s.occupied, index) && s.minuteKeys[index] > minute {
		return false
	}
	s.minuteKeys[index] = minute
	s.values[index] = value
	setSparseBit(s.occupied, index, true)
	setSparseBit(s.valid, index, valid && !math.IsNaN(value) && !math.IsInf(value, 0))
	if !s.hasLast || minute > s.lastMinute {
		s.lastMinute = minute
		s.hasLast = true
	}
	return true
}

// evictBefore removes occupied slots that fall outside the retained window
// after the series advances. A stream can legitimately jump by multiple
// minutes (for example after a restart or a period without candles), so
// overwriting only the new minute's modulo slot is not sufficient.
func (s *sparseSeries) evictBefore(oldest int64) {
	if s == nil || !s.hasLast || len(s.minuteKeys) == 0 {
		return
	}
	previousOldest := s.lastMinute - int64(len(s.minuteKeys)) + 1
	advance := oldest - previousOldest
	if advance <= 0 {
		return
	}
	if advance >= int64(len(s.minuteKeys)) {
		clear(s.occupied)
		clear(s.valid)
		return
	}
	for minute := previousOldest; minute < oldest; minute++ {
		index := ringIndex(minute, len(s.minuteKeys))
		if sparseBit(s.occupied, index) && s.minuteKeys[index] == minute {
			setSparseBit(s.occupied, index, false)
			setSparseBit(s.valid, index, false)
		}
	}
}

func (s *sparseSeries) Get(minute int64) (float64, bool) {
	if s == nil || len(s.minuteKeys) == 0 {
		return 0, false
	}
	index := ringIndex(minute, len(s.minuteKeys))
	if !sparseBit(s.occupied, index) || s.minuteKeys[index] != minute || !sparseBit(s.valid, index) {
		return 0, false
	}
	return s.values[index], true
}

func (s *sparseSeries) Resize(capacity int) {
	if s == nil {
		return
	}
	if capacity < 1 {
		capacity = 1
	}
	if capacity > compactHistoryRetentionMinutes {
		capacity = compactHistoryRetentionMinutes
	}
	if capacity == len(s.minuteKeys) {
		return
	}
	next := &sparseSeries{
		minuteKeys: make([]int64, capacity),
		values:     make([]float64, capacity),
		occupied:   make([]uint64, sparseWordCount(capacity)),
		valid:      make([]uint64, sparseWordCount(capacity)),
		lastMinute: s.lastMinute,
		hasLast:    s.hasLast,
	}
	if s.hasLast {
		oldest := s.lastMinute - int64(capacity) + 1
		for index, minute := range s.minuteKeys {
			if !sparseBit(s.occupied, index) || minute < oldest || minute > s.lastMinute {
				continue
			}
			next.Put(minute, s.values[index], sparseBit(s.valid, index))
		}
	}
	*s = *next
}

func sparseWordCount(capacity int) int {
	return (capacity + 63) / 64
}

func sparseBit(words []uint64, index int) bool {
	if index < 0 || index/64 >= len(words) {
		return false
	}
	return words[index/64]&(uint64(1)<<uint(index%64)) != 0
}

func setSparseBit(words []uint64, index int, value bool) {
	if index < 0 || index/64 >= len(words) {
		return
	}
	mask := uint64(1) << uint(index%64)
	if value {
		words[index/64] |= mask
	} else {
		words[index/64] &^= mask
	}
}

type sparseHistory struct {
	mu     sync.RWMutex
	series map[string]*sparseSeries
}

type sparseHistoryStats struct {
	Series       int
	ValueSlots   int
	KeySlots     int
	ValidityBits int
	BackingBytes int64
	MaxDepth     int
}

func newSparseHistory(plan historyPlan) *sparseHistory {
	history := &sparseHistory{series: make(map[string]*sparseSeries)}
	history.Reconfigure(plan)
	return history
}

func (h *sparseHistory) Reconfigure(plan historyPlan) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.series == nil {
		h.series = make(map[string]*sparseSeries)
	}
	for metric := range h.series {
		if capacity := plan[metric]; !isHistoryMetric(metric) || capacity <= 0 {
			delete(h.series, metric)
		}
	}
	for metric, capacity := range plan {
		if !isHistoryMetric(metric) || capacity <= 0 {
			continue
		}
		series := h.series[metric]
		if series == nil {
			h.series[metric] = newSparseSeries(capacity)
			continue
		}
		series.Resize(capacity)
	}
}

func (h *sparseHistory) Put(minute int64, event CandleEvent) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for metric, series := range h.series {
		value, valid := sparseEventMetric(event, metric)
		series.Put(minute, value, valid)
	}
}

func (h *sparseHistory) Get(minute int64, metric string) (float64, bool) {
	if h == nil {
		return 0, false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.series[metric].Get(minute)
}

// get lets production evaluators share one read-only history contract with
// the sparse store. Keeping the contract metric-oriented avoids exposing ring
// layout details to Important Events.
func (h *sparseHistory) get(minute int64, metric string) (float64, bool) {
	return h.Get(minute, metric)
}

func (h *sparseHistory) Capacity(metric string) int {
	if h == nil {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.series[metric].Capacity()
}

func (h *sparseHistory) SeriesCount() int {
	if h == nil {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.series)
}

func (h *sparseHistory) Stats() sparseHistoryStats {
	if h == nil {
		return sparseHistoryStats{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	stats := sparseHistoryStats{Series: len(h.series)}
	for _, series := range h.series {
		capacity := series.Capacity()
		if capacity > stats.MaxDepth {
			stats.MaxDepth = capacity
		}
		stats.ValueSlots += capacity
		stats.KeySlots += capacity
		stats.ValidityBits += capacity * 2
		stats.BackingBytes += int64(capacity*16 + len(series.occupied)*8 + len(series.valid)*8)
	}
	return stats
}

func sparseEventMetric(event CandleEvent, metric string) (float64, bool) {
	if metric == "openInterestTimestamp" {
		value := float64(event.OpenInterestTimestamp)
		valid := event.OpenInterestTimestamp > 0 && !hasInvalidMetric(event, metric)
		return value, valid && !math.IsNaN(value) && !math.IsInf(value, 0)
	}
	value, valid := eventMetric(event, metric)
	return value, valid && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func eventMetric(event CandleEvent, name string) (float64, bool) {
	if metric, ok := event.Metrics[name]; ok {
		return metric.Value, metric.Valid
	}
	return compactCandleMetricValue(event, name), !hasInvalidMetric(event, name)
}

func compactCandleMetricValue(event CandleEvent, name string) float64 {
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
