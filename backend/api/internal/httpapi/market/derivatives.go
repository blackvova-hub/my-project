package marketapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	liquidationWindow   = 24 * time.Hour
	liquidationPageSize = 100
)

var liquidationStreams = []string{
	"candles:v3:bybit:perpetual",
	"candles:v3:binance:perpetual",
	"liquidations:v3:okx:perpetual",
	"liquidations:v3:bitget:perpetual",
	"liquidations:v3:gateio:perpetual",
}

type liquidationCache struct {
	mu      sync.RWMutex
	buckets map[string]map[string]map[int64]float64
}

type liquidationVenueDTO struct {
	Exchange string  `json:"exchange"`
	USD      float64 `json:"usd"`
}

type derivativesDTO struct {
	Symbol       string `json:"symbol"`
	Source       string `json:"source"`
	AsOf         string `json:"asOf"`
	OpenInterest struct {
		USD       float64  `json:"usd"`
		Change1h  *float64 `json:"change1h"`
		Change4h  *float64 `json:"change4h"`
		Change24h *float64 `json:"change24h"`
	} `json:"openInterest"`
	Funding struct {
		Rate            *float64 `json:"rate"`
		NextFundingTime int64    `json:"nextFundingTime"`
	} `json:"funding"`
	Liquidations struct {
		WindowHours int                   `json:"windowHours"`
		TotalUSD    float64               `json:"totalUsd"`
		ByExchange  []liquidationVenueDTO `json:"byExchange"`
	} `json:"liquidations"`
}

type bybitTicker struct {
	OpenInterest      string `json:"openInterest"`
	OpenInterestValue string `json:"openInterestValue"`
	FundingRate       string `json:"fundingRate"`
	NextFundingTime   string `json:"nextFundingTime"`
}

type openInterestPoint struct {
	OpenInterest string `json:"openInterest"`
	Timestamp    string `json:"timestamp"`
}

type binancePremiumIndex struct {
	MarkPrice       string `json:"markPrice"`
	LastFundingRate string `json:"lastFundingRate"`
	NextFundingTime int64  `json:"nextFundingTime"`
}

type binanceOpenInterest struct {
	OpenInterest string `json:"openInterest"`
}

type binanceOpenInterestPoint struct {
	SumOpenInterest string `json:"sumOpenInterest"`
	Timestamp       int64  `json:"timestamp"`
}

type compactMarketBatch struct {
	Kind            string                  `json:"k"`
	Exchange        string                  `json:"e"`
	Minute          int64                   `json:"t"`
	CandleRows      []compactMarketRow      `json:"r"`
	LiquidationRows []compactLiquidationRow `json:"lr"`
}

type compactMarketRow struct {
	Symbol string    `json:"s"`
	Values []float64 `json:"x"`
}

type compactLiquidationRow struct {
	Symbol string    `json:"s"`
	Values []float64 `json:"x"`
}

func newLiquidationCache() *liquidationCache {
	return &liquidationCache{buckets: make(map[string]map[string]map[int64]float64)}
}

func (c *liquidationCache) set(symbol, exchange string, minute int64, usd float64) {
	if c == nil || symbol == "" || exchange == "" || minute <= 0 || usd <= 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	byExchange := c.buckets[symbol]
	if byExchange == nil {
		byExchange = make(map[string]map[int64]float64)
		c.buckets[symbol] = byExchange
	}
	byMinute := byExchange[exchange]
	if byMinute == nil {
		byMinute = make(map[int64]float64)
		byExchange[exchange] = byMinute
	}
	byMinute[minute] = usd
}

func (c *liquidationCache) totals(symbol string, now time.Time) (float64, []liquidationVenueDTO) {
	if c == nil {
		return 0, []liquidationVenueDTO{}
	}
	cutoff := now.Add(-liquidationWindow).UnixMilli()
	c.mu.Lock()
	defer c.mu.Unlock()
	byExchange := c.buckets[symbol]
	venues := make([]liquidationVenueDTO, 0, len(byExchange))
	total := 0.0
	for exchange, byMinute := range byExchange {
		exchangeTotal := 0.0
		for minute, value := range byMinute {
			if minute < cutoff {
				delete(byMinute, minute)
				continue
			}
			exchangeTotal += value
		}
		if len(byMinute) == 0 {
			delete(byExchange, exchange)
		}
		if exchangeTotal > 0 {
			venues = append(venues, liquidationVenueDTO{Exchange: exchange, USD: exchangeTotal})
			total += exchangeTotal
		}
	}
	if len(byExchange) == 0 {
		delete(c.buckets, symbol)
	}
	sort.Slice(venues, func(i, j int) bool { return venues[i].USD > venues[j].USD })
	return total, venues
}

func (a *API) handleDerivatives(w http.ResponseWriter, r *http.Request) {
	symbol, ok := normalizeDerivativeSymbol(r.URL.Query().Get("symbol"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_symbol"})
		return
	}
	exchange, ok := normalizeDerivativeExchange(r.URL.Query().Get("exchange"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_exchange"})
		return
	}

	now := time.Now().UTC()
	result := derivativesDTO{Symbol: symbol, Source: derivativeExchangeLabel(exchange), AsOf: now.Format(time.RFC3339)}
	result.Liquidations.WindowHours = 24
	result.Liquidations.TotalUSD, result.Liquidations.ByExchange = a.liqCache.totals(symbol, now)
	if exchange == "binance" {
		a.populateBinanceDerivatives(r.Context(), &result, symbol, now)
	} else {
		a.populateBybitDerivatives(r.Context(), &result, symbol, now)
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *API) populateBybitDerivatives(ctx context.Context, result *derivativesDTO, symbol string, now time.Time) {

	var ticker bybitTicker
	var history []openInterestPoint
	var tickerErr, historyErr error
	var marketWG sync.WaitGroup
	marketWG.Add(2)
	go func() {
		defer marketWG.Done()
		ticker, tickerErr = a.fetchBybitTicker(ctx, symbol)
	}()
	go func() {
		defer marketWG.Done()
		history, historyErr = a.fetchBybitOpenInterest(ctx, symbol)
	}()
	marketWG.Wait()
	if tickerErr == nil {
		result.OpenInterest.USD, _ = strconv.ParseFloat(strings.TrimSpace(ticker.OpenInterestValue), 64)
		currentOI, _ := strconv.ParseFloat(strings.TrimSpace(ticker.OpenInterest), 64)
		if historyErr == nil && currentOI > 0 {
			result.OpenInterest.Change1h = openInterestChange(currentOI, history, now.Add(-time.Hour))
			result.OpenInterest.Change4h = openInterestChange(currentOI, history, now.Add(-4*time.Hour))
			result.OpenInterest.Change24h = openInterestChange(currentOI, history, now.Add(-24*time.Hour))
		}
		if fundingRate, err := strconv.ParseFloat(strings.TrimSpace(ticker.FundingRate), 64); err == nil && !math.IsNaN(fundingRate) && !math.IsInf(fundingRate, 0) {
			result.Funding.Rate = &fundingRate
		}
		result.Funding.NextFundingTime, _ = strconv.ParseInt(strings.TrimSpace(ticker.NextFundingTime), 10, 64)
	}
}

func (a *API) populateBinanceDerivatives(ctx context.Context, result *derivativesDTO, symbol string, now time.Time) {
	var premium binancePremiumIndex
	var current binanceOpenInterest
	var history []binanceOpenInterestPoint
	var premiumErr, currentErr, historyErr error
	var marketWG sync.WaitGroup
	marketWG.Add(3)
	go func() {
		defer marketWG.Done()
		premium, premiumErr = a.fetchBinancePremiumIndex(ctx, symbol)
	}()
	go func() {
		defer marketWG.Done()
		current, currentErr = a.fetchBinanceOpenInterest(ctx, symbol)
	}()
	go func() {
		defer marketWG.Done()
		history, historyErr = a.fetchBinanceOpenInterestHistory(ctx, symbol)
	}()
	marketWG.Wait()

	markPrice := parsePositiveFloat(premium.MarkPrice)
	currentOI := parsePositiveFloat(current.OpenInterest)
	if currentErr == nil && currentOI > 0 {
		result.OpenInterest.USD = currentOI * markPrice
		if historyErr == nil {
			points := make([]openInterestPoint, 0, len(history))
			for _, point := range history {
				points = append(points, openInterestPoint{
					OpenInterest: point.SumOpenInterest,
					Timestamp:    strconv.FormatInt(point.Timestamp, 10),
				})
			}
			result.OpenInterest.Change1h = openInterestChange(currentOI, points, now.Add(-time.Hour))
			result.OpenInterest.Change4h = openInterestChange(currentOI, points, now.Add(-4*time.Hour))
			result.OpenInterest.Change24h = openInterestChange(currentOI, points, now.Add(-24*time.Hour))
		}
	}
	if premiumErr == nil {
		if fundingRate, err := strconv.ParseFloat(strings.TrimSpace(premium.LastFundingRate), 64); err == nil && !math.IsNaN(fundingRate) && !math.IsInf(fundingRate, 0) {
			result.Funding.Rate = &fundingRate
		}
		result.Funding.NextFundingTime = premium.NextFundingTime
	}
}

func normalizeDerivativeExchange(raw string) (string, bool) {
	exchange := strings.ToLower(strings.TrimSpace(raw))
	if exchange == "" {
		return "bybit", true
	}
	if exchange != "bybit" && exchange != "binance" {
		return "", false
	}
	return exchange, true
}

func derivativeExchangeLabel(exchange string) string {
	if exchange == "binance" {
		return "Binance"
	}
	return "Bybit"
}

func parsePositiveFloat(raw string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func normalizeDerivativeSymbol(raw string) (string, bool) {
	symbol := strings.ToUpper(strings.TrimSpace(raw))
	if len(symbol) < 4 || len(symbol) > 32 {
		return "", false
	}
	for _, char := range symbol {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return "", false
		}
	}
	return symbol, true
}

func (a *API) fetchBybitTicker(ctx context.Context, symbol string) (bybitTicker, error) {
	var payload struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []bybitTicker `json:"list"`
		} `json:"result"`
	}
	path := "/v5/market/tickers?category=linear&symbol=" + symbol
	if err := a.getBybitJSON(ctx, path, &payload); err != nil {
		return bybitTicker{}, err
	}
	if payload.RetCode != 0 || len(payload.Result.List) == 0 {
		return bybitTicker{}, fmt.Errorf("bybit ticker unavailable: %s", payload.RetMsg)
	}
	return payload.Result.List[0], nil
}

func (a *API) fetchBybitOpenInterest(ctx context.Context, symbol string) ([]openInterestPoint, error) {
	var payload struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []openInterestPoint `json:"list"`
		} `json:"result"`
	}
	path := "/v5/market/open-interest?category=linear&intervalTime=1h&limit=30&symbol=" + symbol
	if err := a.getBybitJSON(ctx, path, &payload); err != nil {
		return nil, err
	}
	if payload.RetCode != 0 {
		return nil, fmt.Errorf("bybit open interest unavailable: %s", payload.RetMsg)
	}
	return payload.Result.List, nil
}

func (a *API) fetchBinancePremiumIndex(ctx context.Context, symbol string) (binancePremiumIndex, error) {
	var payload binancePremiumIndex
	path := "/fapi/v1/premiumIndex?symbol=" + symbol
	if err := a.getJSON(ctx, binanceFuturesBaseURL()+path, &payload); err != nil {
		return binancePremiumIndex{}, err
	}
	if parsePositiveFloat(payload.MarkPrice) == 0 {
		return binancePremiumIndex{}, fmt.Errorf("binance premium index unavailable")
	}
	return payload, nil
}

func (a *API) fetchBinanceOpenInterest(ctx context.Context, symbol string) (binanceOpenInterest, error) {
	var payload binanceOpenInterest
	path := "/fapi/v1/openInterest?symbol=" + symbol
	if err := a.getJSON(ctx, binanceFuturesBaseURL()+path, &payload); err != nil {
		return binanceOpenInterest{}, err
	}
	if parsePositiveFloat(payload.OpenInterest) == 0 {
		return binanceOpenInterest{}, fmt.Errorf("binance open interest unavailable")
	}
	return payload, nil
}

func (a *API) fetchBinanceOpenInterestHistory(ctx context.Context, symbol string) ([]binanceOpenInterestPoint, error) {
	var payload []binanceOpenInterestPoint
	path := "/futures/data/openInterestHist?period=1h&limit=30&symbol=" + symbol
	if err := a.getJSON(ctx, binanceFuturesBaseURL()+path, &payload); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("binance open interest history unavailable")
	}
	return payload, nil
}

func binanceFuturesBaseURL() string {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("BINANCE_FUTURES_BASE_URL")), "/")
	if baseURL == "" {
		return "https://fapi.binance.com"
	}
	return baseURL
}

func openInterestChange(current float64, points []openInterestPoint, target time.Time) *float64 {
	var selectedValue float64
	var selectedTime int64
	targetMillis := target.UnixMilli()
	for _, point := range points {
		timestamp, err := strconv.ParseInt(strings.TrimSpace(point.Timestamp), 10, 64)
		if err != nil || timestamp > targetMillis || timestamp <= selectedTime {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(point.OpenInterest), 64)
		if err != nil || value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		selectedTime = timestamp
		selectedValue = value
	}
	if selectedValue <= 0 {
		return nil
	}
	change := (current/selectedValue - 1) * 100
	return &change
}

func (a *API) startLiquidationSync() {
	if a == nil || a.redis == nil || a.liqCache == nil {
		return
	}
	go func() {
		cursors := make(map[string]string, len(liquidationStreams))
		cutoff := time.Now().UTC().Add(-liquidationWindow).UnixMilli()
		for _, stream := range liquidationStreams {
			cursors[stream] = fmt.Sprintf("%d-0", cutoff)
		}
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			err := a.syncLiquidationStreams(ctx, cursors)
			cancel()
			if err != nil && a.redis != nil {
				log.Printf("market derivatives: liquidation Redis sync failed: %v", err)
			}
			time.Sleep(10 * time.Second)
		}
	}()
}

func (a *API) syncLiquidationStreams(ctx context.Context, cursors map[string]string) error {
	for _, stream := range liquidationStreams {
		for {
			messages, err := a.redis.XRangeN(ctx, stream, "("+cursors[stream], "+", liquidationPageSize).Result()
			if err != nil && err != redis.Nil {
				return err
			}
			if len(messages) == 0 {
				break
			}
			for _, message := range messages {
				cursors[stream] = message.ID
				raw, ok := message.Values["json"].(string)
				if !ok || raw == "" {
					continue
				}
				var batch compactMarketBatch
				if err := json.Unmarshal([]byte(raw), &batch); err != nil {
					continue
				}
				a.observeLiquidationBatch(batch)
			}
			if len(messages) < liquidationPageSize {
				break
			}
		}
	}
	return nil
}

func (a *API) observeLiquidationBatch(batch compactMarketBatch) {
	exchange := strings.ToLower(strings.TrimSpace(batch.Exchange))
	if batch.Minute <= 0 || exchange == "" {
		return
	}
	if batch.Kind == "l" {
		for _, row := range batch.LiquidationRows {
			if len(row.Values) > 0 {
				a.liqCache.set(strings.ToUpper(row.Symbol), exchange, batch.Minute, row.Values[0])
			}
		}
		return
	}
	if batch.Kind == "c" {
		const liquidationMetricIndex = 35
		for _, row := range batch.CandleRows {
			if len(row.Values) > liquidationMetricIndex {
				a.liqCache.set(strings.ToUpper(row.Symbol), exchange, batch.Minute, row.Values[liquidationMetricIndex])
			}
		}
	}
}
