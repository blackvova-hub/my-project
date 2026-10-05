package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type priceShockWindow struct {
	Minutes       int
	ChangePercent float64
	Threshold     float64
	VolumeUSD     float64
	MedianVolume  float64
	VolumeRatio   float64
}

var majorAltAssets = map[string]struct{}{
	"SOL": {}, "XRP": {}, "BNB": {}, "ADA": {}, "DOGE": {}, "TRX": {},
	"AVAX": {}, "LINK": {}, "TON": {}, "DOT": {}, "LTC": {}, "BCH": {},
	"SUI": {}, "NEAR": {}, "APT": {}, "ICP": {}, "UNI": {}, "AAVE": {},
	"XLM": {}, "HBAR": {}, "ATOM": {}, "ETC": {}, "FIL": {}, "ARB": {}, "OP": {},
}

func (e *Engine) buildSixEventEvaluation(_ context.Context, _ *pgxpool.Pool, ce CandleEvent, r metricHistory, venueHistories map[string]metricHistory, mk int64) importantEvaluation {
	evaluation := importantEvaluation{
		Events: make([]importantEvent, 0, 3),
		EvaluatedFamilies: map[string]bool{
			"price_shock":            true,
			"open_interest_shock":    true,
			"large_aggressive_trade": true,
		},
	}
	if event, ok := e.detectPriceShock(ce, r, venueHistories, mk); ok {
		evaluation.Events = append(evaluation.Events, event)
	}
	if event, ok := detectOpenInterestShock(ce, r, mk); ok {
		evaluation.Events = append(evaluation.Events, event)
	}
	if event, ok := detectLargeAggressiveTrade(ce, r, mk); ok {
		evaluation.Events = append(evaluation.Events, event)
	}
	return evaluation
}

func priceShockThreshold(symbol string, minutes int) float64 {
	asset := baseAsset(symbol)
	if asset == "BTC" || asset == "ETH" {
		switch minutes {
		case 5:
			return 3
		case 10:
			return 5
		default:
			return 7
		}
	}
	if _, ok := majorAltAssets[asset]; ok {
		switch minutes {
		case 5:
			return 6
		case 10:
			return 9
		default:
			return 12
		}
	}
	switch minutes {
	case 5:
		return 10
	case 10:
		return 15
	default:
		return 20
	}
}

func (e *Engine) detectPriceShock(ce CandleEvent, r metricHistory, venueHistories map[string]metricHistory, mk int64) (importantEvent, bool) {
	var best priceShockWindow
	for _, window := range []int{5, 10, 15} {
		change, priceOK := exactPriceChange(r, mk, window)
		volume, volumeOK := exactQuoteVolumeWindow(r, mk, window)
		if !priceOK || !volumeOK {
			continue
		}
		samples := historicalWindowSamples(r, mk, window, importantHistoryMinutes, func(end int64) (float64, bool) {
			return exactQuoteVolumeWindow(r, end, window)
		})
		stats, baselineOK := robustBaseline(samples, volume)
		if !baselineOK || stats.Median <= 0 {
			continue
		}
		threshold := priceShockThreshold(ce.Symbol, window)
		ratio := volume / stats.Median
		if math.Abs(change) < threshold || volume < 500_000 || ratio < 2 {
			continue
		}
		current := priceShockWindow{Minutes: window, ChangePercent: change, Threshold: threshold, VolumeUSD: volume, MedianVolume: stats.Median, VolumeRatio: ratio}
		if best.Minutes == 0 || math.Abs(change)/threshold > math.Abs(best.ChangePercent)/best.Threshold {
			best = current
		}
	}
	if best.Minutes == 0 {
		return importantEvent{}, false
	}
	direction := "up"
	if best.ChangePercent < 0 {
		direction = "down"
	}
	venues := sameDirectionPriceVenues(ce, venueHistories, mk, best.Minutes, best.Threshold, direction)
	event := newImportantEvent(ce, "price_shock", "price_shock", direction, best.Minutes)
	if len(venues) >= 2 {
		event.Exchange = "multiple"
	}
	event.SourceKind = "market"
	event.Confidence = 2
	if hasVenue(venues, "binance") && hasVenue(venues, "bybit") {
		event.Confidence = 3
	}
	event.ChangePercent = floatPtr(best.ChangePercent)
	event.AmountUSD = floatPtr(best.VolumeUSD)
	event.BaselineValue = floatPtr(best.MedianVolume)
	event.BaselineRatio = floatPtr(best.VolumeRatio)
	event.Title = fmt.Sprintf("%s: ценовой шок %+.2f%% за %d минут", baseAsset(ce.Symbol), best.ChangePercent, best.Minutes)
	event.Details = fmt.Sprintf("Изменение цены %+.2f%%; торговый объём %s (%.2fx к медиане); подтверждающие площадки: %s.", best.ChangePercent, formatUSD(best.VolumeUSD), best.VolumeRatio, strings.Join(venues, ", "))
	event.Metadata = map[string]any{
		"factorNames":               []string{"price", "volume"},
		"change5m":                  metricPriceChange(r, mk, 5),
		"change10m":                 metricPriceChange(r, mk, 10),
		"change15m":                 metricPriceChange(r, mk, 15),
		"thresholdPercent":          best.Threshold,
		"volumeUsd":                 best.VolumeUSD,
		"medianVolumeUsd":           best.MedianVolume,
		"volumeRatio":               best.VolumeRatio,
		"confirmed_venues":          venues,
		"venue_count":               len(venues),
		"requiredObservations":      1,
		"confirmationWindowMinutes": 5,
		"clusterWindowMinutes":      30,
	}
	return event, true
}

func hasVenue(venues []string, target string) bool {
	for _, venue := range venues {
		if strings.EqualFold(venue, target) {
			return true
		}
	}
	return false
}

func metricPriceChange(r metricHistory, mk int64, window int) any {
	value, ok := exactPriceChange(r, mk, window)
	if !ok {
		return nil
	}
	return value
}

func sameDirectionPriceVenues(_ CandleEvent, histories map[string]metricHistory, mk int64, window int, threshold float64, direction string) []string {
	venues := make([]string, 0, 4)
	for venue, candidate := range histories {
		if candidate == nil {
			continue
		}
		move, ok := exactPriceChange(candidate, mk, window)
		if !ok || math.Abs(move) < threshold {
			continue
		}
		if (direction == "up" && move > 0) || (direction == "down" && move < 0) {
			venues = append(venues, venue)
		}
	}
	sort.Strings(venues)
	return uniqueStrings(venues)
}

func detectOpenInterestShock(ce CandleEvent, r metricHistory, mk int64) (importantEvent, bool) {
	const window = 5
	nowOI, nowOK := r.get(mk, "openInterest")
	oldOI, oldOK := r.get(mk-window, "openInterest")
	nowOITS, nowTSOK := r.get(mk, "openInterestTimestamp")
	oldOITS, oldTSOK := r.get(mk-window, "openInterestTimestamp")
	previousOITS, previousTSOK := r.get(mk-1, "openInterestTimestamp")
	mark, markOK := r.get(mk, "markPrice")
	volume, volumeOK := exactQuoteVolumeWindow(r, mk, window)
	if !nowOK || !oldOK || !nowTSOK || !oldTSOK || !markOK || !volumeOK ||
		oldOI <= 0 || mark <= 0 || volume <= 0 || nowOITS <= oldOITS ||
		(previousTSOK && previousOITS == nowOITS) {
		return importantEvent{}, false
	}
	change := (nowOI/oldOI - 1) * 100
	deltaUSD := math.Abs(nowOI-oldOI) * mark
	volumeRatio := deltaUSD / volume
	samples := historicalWindowSamples(r, mk, window, importantHistoryMinutes, func(end int64) (float64, bool) {
		return exactQuoteVolumeWindow(r, end, window)
	})
	stats, baselineOK := robustBaseline(samples, volume)
	if !baselineOK || stats.Median <= 0 {
		return importantEvent{}, false
	}
	baselineRatio := volume / stats.Median
	if math.Abs(change) < 15 || deltaUSD < 1_000_000 || volumeRatio < .5 || baselineRatio < 2 {
		return importantEvent{}, false
	}
	direction := "increase"
	if change < 0 {
		direction = "decrease"
	}
	event := newImportantEvent(ce, "open_interest_shock", "open_interest_shock", direction, window)
	event.SourceKind = "market"
	event.Confidence = 2
	event.AmountUSD = floatPtr(deltaUSD)
	event.ChangePercent = floatPtr(change)
	event.BaselineValue = floatPtr(stats.Median)
	event.BaselineRatio = floatPtr(baselineRatio)
	event.Title = fmt.Sprintf("%s: скачок открытого интереса %+.2f%%", baseAsset(ce.Symbol), change)
	event.Details = fmt.Sprintf("Изменение OI: %s; отношение изменения OI к объёму: %.2f; торговый объём: %.2fx к медиане.", formatUSD(deltaUSD), volumeRatio, baselineRatio)
	event.Metadata = map[string]any{
		"factorNames": []string{"open_interest", "volume"},
		"oiNow":       nowOI, "oiAgo": oldOI, "markPrice": mark,
		"oiSourceTimestamp": int64(nowOITS), "oiPreviousSourceTimestamp": int64(oldOITS),
		"deltaOiUsd": deltaUSD, "oiVolumeRatio": volumeRatio,
		"volumeUsd": volume, "medianVolumeUsd": stats.Median, "volumeRatio": baselineRatio,
		"requiredObservations": 1, "confirmationWindowMinutes": 5, "clusterWindowMinutes": 30,
	}
	return event, true
}

func detectLargeAggressiveTrade(ce CandleEvent, r metricHistory, mk int64) (importantEvent, bool) {
	largest, largestOK := r.get(mk, "largestTradeUsd")
	median, medianOK := r.get(mk, "medianTradeUsd")
	cluster, clusterOK := r.get(mk, "tradeClusterUsd")
	clusterSide, clusterSideOK := r.get(mk, "tradeClusterSide")
	clusterCount, countOK := r.get(mk, "tradeClusterCount")
	impact, impactOK := r.get(mk, "tradePriceImpactPct")
	volume5m, volumeOK := exactQuoteVolumeWindow(r, mk, 5)
	if !largestOK || !medianOK || !volumeOK || largest <= 0 || median <= 0 || volume5m <= 0 {
		return importantEvent{}, false
	}
	threshold := math.Max(1_000_000, math.Max(volume5m*.05, median*20))
	clusterHasDirection := clusterOK && clusterSideOK && clusterSide != 0
	clusterMeetsThreshold := clusterHasDirection && cluster >= threshold
	clusterConfirmed := clusterMeetsThreshold && countOK && clusterCount >= 2
	impactConfirmed := clusterMeetsThreshold && impactOK && math.Abs(impact) >= .3 &&
		((clusterSide > 0 && impact > 0) || (clusterSide < 0 && impact < 0))
	if !clusterConfirmed && !impactConfirmed {
		return importantEvent{}, false
	}
	amount, side := cluster, clusterSide
	direction, noun := "buy", "покупок"
	if side < 0 {
		direction, noun = "sell", "продаж"
	}
	event := newImportantEvent(ce, "large_aggressive_trade", "large_aggressive_trade", direction, 1)
	event.SourceKind = "market"
	event.Confidence = 2
	event.AmountUSD = floatPtr(amount)
	event.Title = fmt.Sprintf("%s: кластер агрессивных %s на %s", baseAsset(ce.Symbol), noun, formatUSD(amount))
	event.Details = fmt.Sprintf("Порог %s; объём за 5 минут %s; медианный размер публичной сделки %s.", formatUSD(threshold), formatUSD(volume5m), formatUSD(median))
	event.Metadata = map[string]any{
		"factorNames": []string{"aggressive_trade"},
		"tradeUsd":    largest, "medianTradeUsd": median, "thresholdUsd": threshold,
		"clusterUsd": cluster, "clusterCount": clusterCount,
		"priceImpactPercent": impact, "aggregationKind": "public_trade_cluster",
		"identityClaim":        "no_trader_identity",
		"requiredObservations": 1, "confirmationWindowMinutes": 1, "clusterWindowMinutes": 10,
	}
	return event, true
}

func exactQuoteVolumeWindow(r metricHistory, endMinute int64, window int) (float64, bool) {
	return sumExactWindow(r, endMinute, window, "quoteVolumeUsd")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
