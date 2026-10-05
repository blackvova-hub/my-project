package marketapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

const (
	marketOverviewRefreshInterval = 2 * time.Minute
	priceTrendThresholdPct        = 0.01
)

type API struct {
	httpClient *http.Client
	redis      *redis.Client
	liqCache   *liquidationCache
	mu         sync.RWMutex
	cache      marketOverviewDTO
	cacheAt    time.Time
	capHistory []float64
	volHistory []float64
	refreshing bool
	cacheFile  string
}

type sentimentPoint struct {
	TS    int64   `json:"ts"`
	Value float64 `json:"value"`
}

type sentimentDTO struct {
	Value          float64          `json:"value"`
	Classification string           `json:"classification"`
	History30      []sentimentPoint `json:"history30"`
	History90      []sentimentPoint `json:"history90"`
}

type marketCardDTO struct {
	USD                float64   `json:"usd"`
	Change24h          float64   `json:"change24h"`
	Change24hAvailable bool      `json:"change24hAvailable"`
	Series             []float64 `json:"series"`
	Source             string    `json:"source"`
	SourceUpdatedAt    string    `json:"sourceUpdatedAt"`
}

type globalMarketSnapshot struct {
	MarketCapUSD                float64
	MarketCapChange24h          float64
	MarketCapChange24hAvailable bool
	Volume24hUSD                float64
	Volume24hChange             float64
	Volume24hChangeAvailable    bool
	Source                      string
	SourceUpdatedAt             time.Time
}

type priceTrendDTO struct {
	Up           int     `json:"up"`
	Flat         int     `json:"flat"`
	Down         int     `json:"down"`
	ThresholdPct float64 `json:"thresholdPct"`
	Total        int     `json:"total"`
}

type topMoverDTO struct {
	Symbol      string  `json:"symbol"`
	ChangePct   float64 `json:"changePct"`
	Direction   string  `json:"direction"`
	LastPrice   float64 `json:"lastPrice"`
	DisplayName string  `json:"displayName"`
}

type marketSliceDTO struct {
	PriceTrend priceTrendDTO `json:"priceTrend"`
	TopMovers  []topMoverDTO `json:"topMovers"`
}

type exchangeMarketsDTO struct {
	Spot    marketSliceDTO `json:"spot"`
	Futures marketSliceDTO `json:"futures"`
}

type marketOverviewDTO struct {
	Sentiment  sentimentDTO  `json:"sentiment"`
	MarketCap  marketCardDTO `json:"marketCap"`
	Volume24h  marketCardDTO `json:"volume24h"`
	PriceTrend priceTrendDTO `json:"priceTrend"`
	TopMovers  []topMoverDTO `json:"topMovers"`
	Markets    struct {
		Spot    marketSliceDTO `json:"spot"`
		Futures marketSliceDTO `json:"futures"`
	} `json:"markets"`
	Exchanges map[string]exchangeMarketsDTO `json:"exchanges"`
	UpdatedAt string                        `json:"updatedAt"`
}

func New(redisClient *redis.Client) *API {
	a := &API{
		httpClient: &http.Client{Timeout: 7 * time.Second},
		redis:      redisClient,
		liqCache:   newLiquidationCache(),
	}
	a.cacheFile = defaultCacheFile()
	a.loadDiskCache()
	a.refreshOverviewSnapshot()
	a.startBackgroundRefresh()
	a.startLiquidationSync()
	return a
}

func (a *API) Routes(r chi.Router) {
	r.Get("/market/overview", a.handleOverview)
	r.Get("/market/derivatives", a.handleDerivatives)
}

func (a *API) handleOverview(w http.ResponseWriter, r *http.Request) {
	if cached, ok := a.getAnyCache(); ok {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "market_overview_warming_up"})
}

func (a *API) buildOverview(ctx context.Context) (marketOverviewDTO, bool) {
	var (
		sentiment      sentimentDTO
		global         globalMarketSnapshot
		spot           marketSliceDTO
		futures        marketSliceDTO
		binanceSpot    marketSliceDTO
		binanceFutures marketSliceDTO
	)

	var wg sync.WaitGroup
	wg.Add(6)
	go func() {
		defer wg.Done()
		s, err := a.fetchFearGreed(ctx)
		if err != nil {
			log.Printf("market overview: fear and greed update failed: %v", err)
			return
		}
		sentiment = s
	}()
	go func() {
		defer wg.Done()
		snapshot, err := a.fetchGlobalMarket(ctx)
		if err != nil {
			log.Printf("market overview: global market update failed: %v", err)
			return
		}
		global = snapshot
	}()
	go func() {
		defer wg.Done()
		t, m, err := a.fetchBybitDistributionAndMovers(ctx, "spot")
		if err != nil {
			log.Printf("market overview: Bybit spot update failed: %v", err)
			return
		}
		spot = marketSliceDTO{PriceTrend: t, TopMovers: m}
	}()
	go func() {
		defer wg.Done()
		t, m, err := a.fetchBybitDistributionAndMovers(ctx, "linear")
		if err != nil {
			log.Printf("market overview: Bybit futures update failed: %v", err)
			return
		}
		futures = marketSliceDTO{PriceTrend: t, TopMovers: m}
	}()
	go func() {
		defer wg.Done()
		t, m, err := a.fetchBinanceDistributionAndMovers(ctx, false)
		if err != nil {
			log.Printf("market overview: Binance spot update failed: %v", err)
			return
		}
		binanceSpot = marketSliceDTO{PriceTrend: t, TopMovers: m}
	}()
	go func() {
		defer wg.Done()
		t, m, err := a.fetchBinanceDistributionAndMovers(ctx, true)
		if err != nil {
			log.Printf("market overview: Binance futures update failed: %v", err)
			return
		}
		binanceFutures = marketSliceDTO{PriceTrend: t, TopMovers: m}
	}()
	wg.Wait()

	previous, hasPrevious := a.getAnyCache()
	if sentiment.Value <= 0 && hasPrevious {
		sentiment = previous.Sentiment
	}
	if global.MarketCapUSD <= 0 && hasPrevious {
		global.MarketCapUSD = previous.MarketCap.USD
		global.MarketCapChange24h = previous.MarketCap.Change24h
		global.MarketCapChange24hAvailable = previous.MarketCap.Change24hAvailable
	}
	if global.Volume24hUSD <= 0 && hasPrevious {
		global.Volume24hUSD = previous.Volume24h.USD
		global.Volume24hChange = previous.Volume24h.Change24h
		global.Volume24hChangeAvailable = previous.Volume24h.Change24hAvailable
	}
	if global.Source == "" && hasPrevious {
		global.Source = previous.MarketCap.Source
		if ts, err := time.Parse(time.RFC3339, previous.MarketCap.SourceUpdatedAt); err == nil {
			global.SourceUpdatedAt = ts
		}
	}
	if !hasMarketSlice(spot) && hasPrevious {
		spot = previous.Markets.Spot
		if !hasMarketSlice(spot) {
			spot = marketSliceDTO{PriceTrend: previous.PriceTrend, TopMovers: previous.TopMovers}
		}
	}
	if !hasMarketSlice(futures) && hasPrevious {
		futures = previous.Markets.Futures
	}
	if hasPrevious {
		if previousBinance, ok := previous.Exchanges["binance"]; ok {
			if !hasMarketSlice(binanceSpot) {
				binanceSpot = previousBinance.Spot
			}
			if !hasMarketSlice(binanceFutures) {
				binanceFutures = previousBinance.Futures
			}
		}
	}

	capSeriesFinal, volSeriesFinal := a.mergeSeries(
		global.MarketCapUSD,
		global.Volume24hUSD,
		global.MarketCapChange24h,
		global.MarketCapChange24hAvailable,
		global.Volume24hChange,
		global.Volume24hChangeAvailable,
	)
	sourceUpdatedAt := ""
	if !global.SourceUpdatedAt.IsZero() {
		sourceUpdatedAt = global.SourceUpdatedAt.UTC().Format(time.RFC3339)
	}

	payload := marketOverviewDTO{
		Sentiment: sentiment,
		MarketCap: marketCardDTO{
			USD:                global.MarketCapUSD,
			Change24h:          global.MarketCapChange24h,
			Change24hAvailable: global.MarketCapChange24hAvailable,
			Series:             tailValues(capSeriesFinal, 96),
			Source:             global.Source,
			SourceUpdatedAt:    sourceUpdatedAt,
		},
		Volume24h: marketCardDTO{
			USD:                global.Volume24hUSD,
			Change24h:          global.Volume24hChange,
			Change24hAvailable: global.Volume24hChangeAvailable,
			Series:             tailValues(volSeriesFinal, 96),
			Source:             global.Source,
			SourceUpdatedAt:    sourceUpdatedAt,
		},
		PriceTrend: spot.PriceTrend,
		TopMovers:  spot.TopMovers,
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	payload.Markets.Spot = spot
	payload.Markets.Futures = futures
	payload.Exchanges = map[string]exchangeMarketsDTO{
		"bybit":   {Spot: spot, Futures: futures},
		"binance": {Spot: binanceSpot, Futures: binanceFutures},
	}

	if payload.Sentiment.Value <= 0 && payload.MarketCap.USD <= 0 && payload.Volume24h.USD <= 0 &&
		!hasMarketSlice(payload.Markets.Spot) && !hasMarketSlice(payload.Markets.Futures) &&
		!hasMarketSlice(binanceSpot) && !hasMarketSlice(binanceFutures) {
		return payload, false
	}

	return payload, true
}

func (a *API) fetchFearGreed(ctx context.Context) (sentimentDTO, error) {
	var payload struct {
		Data []struct {
			Value               string `json:"value"`
			ValueClassification string `json:"value_classification"`
			Timestamp           string `json:"timestamp"`
		} `json:"data"`
	}
	if err := a.getJSON(ctx, "https://api.alternative.me/fng/?limit=90&format=json", &payload); err != nil {
		return sentimentDTO{}, err
	}
	if len(payload.Data) == 0 {
		return sentimentDTO{}, nil
	}

	points := make([]sentimentPoint, 0, len(payload.Data))
	for i := len(payload.Data) - 1; i >= 0; i-- {
		it := payload.Data[i]
		val, err := strconv.ParseFloat(strings.TrimSpace(it.Value), 64)
		if err != nil {
			continue
		}
		ts, _ := strconv.ParseInt(strings.TrimSpace(it.Timestamp), 10, 64)
		points = append(points, sentimentPoint{TS: ts, Value: val})
	}

	cur := payload.Data[0]
	currentVal, _ := strconv.ParseFloat(strings.TrimSpace(cur.Value), 64)
	if currentVal < 0 {
		currentVal = 0
	}
	if currentVal > 100 {
		currentVal = 100
	}

	result := sentimentDTO{
		Value:          currentVal,
		Classification: strings.TrimSpace(cur.ValueClassification),
		History30:      tailPoints(points, 30),
		History90:      tailPoints(points, 90),
	}
	return result, nil
}

func (a *API) fetchGlobalMarket(ctx context.Context) (globalMarketSnapshot, error) {
	snapshot, err := a.fetchCoinPaprikaGlobal(ctx)
	if err == nil {
		return snapshot, nil
	}
	log.Printf("market overview: CoinPaprika global update failed, using CoinGecko fallback: %v", err)

	snapshot, fallbackErr := a.fetchCoinGeckoGlobal(ctx)
	if fallbackErr == nil {
		return snapshot, nil
	}
	return globalMarketSnapshot{}, fmt.Errorf("CoinPaprika: %v; CoinGecko: %w", err, fallbackErr)
}

func (a *API) fetchCoinPaprikaGlobal(ctx context.Context) (globalMarketSnapshot, error) {
	var payload struct {
		MarketCapUSD       float64 `json:"market_cap_usd"`
		Volume24hUSD       float64 `json:"volume_24h_usd"`
		MarketCapChange24h float64 `json:"market_cap_change_24h"`
		Volume24hChange24h float64 `json:"volume_24h_change_24h"`
		LastUpdated        int64   `json:"last_updated"`
	}
	if err := a.getJSON(ctx, "https://api.coinpaprika.com/v1/global", &payload); err != nil {
		return globalMarketSnapshot{}, err
	}
	if payload.MarketCapUSD <= 0 || payload.Volume24hUSD <= 0 {
		return globalMarketSnapshot{}, fmt.Errorf("invalid global market values")
	}
	updatedAt := time.Now().UTC()
	if payload.LastUpdated > 0 {
		updatedAt = time.Unix(payload.LastUpdated, 0).UTC()
	}
	return globalMarketSnapshot{
		MarketCapUSD:                payload.MarketCapUSD,
		MarketCapChange24h:          payload.MarketCapChange24h,
		MarketCapChange24hAvailable: true,
		Volume24hUSD:                payload.Volume24hUSD,
		Volume24hChange:             payload.Volume24hChange24h,
		Volume24hChangeAvailable:    true,
		Source:                      "CoinPaprika",
		SourceUpdatedAt:             updatedAt,
	}, nil
}

func (a *API) fetchCoinGeckoGlobal(ctx context.Context) (globalMarketSnapshot, error) {
	var payload struct {
		Data struct {
			TotalMarketCap                 map[string]float64 `json:"total_market_cap"`
			TotalVolume                    map[string]float64 `json:"total_volume"`
			MarketCapChangePercentage24hUS float64            `json:"market_cap_change_percentage_24h_usd"`
		} `json:"data"`
	}
	if err := a.getJSON(ctx, "https://api.coingecko.com/api/v3/global", &payload); err != nil {
		return globalMarketSnapshot{}, err
	}
	marketCapUSD := payload.Data.TotalMarketCap["usd"]
	volumeUSD := payload.Data.TotalVolume["usd"]
	if marketCapUSD <= 0 || volumeUSD <= 0 {
		return globalMarketSnapshot{}, fmt.Errorf("invalid global market values")
	}
	return globalMarketSnapshot{
		MarketCapUSD:                marketCapUSD,
		MarketCapChange24h:          payload.Data.MarketCapChangePercentage24hUS,
		MarketCapChange24hAvailable: true,
		Volume24hUSD:                volumeUSD,
		Volume24hChangeAvailable:    false,
		Source:                      "CoinGecko",
		SourceUpdatedAt:             time.Now().UTC(),
	}, nil
}

func (a *API) fetchBybitDistributionAndMovers(ctx context.Context, category string) (priceTrendDTO, []topMoverDTO, error) {
	var payload struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Result  struct {
			List []struct {
				Symbol       string `json:"symbol"`
				Price24hPcnt string `json:"price24hPcnt"`
				LastPrice    string `json:"lastPrice"`
				Turnover24h  string `json:"turnover24h"`
			} `json:"list"`
		} `json:"result"`
	}
	path := fmt.Sprintf("/v5/market/tickers?category=%s", category)
	if err := a.getBybitJSON(ctx, path, &payload); err != nil {
		return priceTrendDTO{ThresholdPct: priceTrendThresholdPct}, nil, err
	}
	if payload.RetCode != 0 {
		return priceTrendDTO{ThresholdPct: priceTrendThresholdPct}, nil, fmt.Errorf("bybit retcode %d", payload.RetCode)
	}

	threshold := priceTrendThresholdPct
	up := 0
	flat := 0
	down := 0
	type tickerRow struct {
		symbol      string
		base        string
		pct         float64
		last        float64
		turnover    float64
		displayName string
	}

	bestByBase := make(map[string]tickerRow, len(payload.Result.List))

	for _, it := range payload.Result.List {
		symbol := strings.ToUpper(strings.TrimSpace(it.Symbol))
		if !strings.HasSuffix(symbol, "USDT") {
			continue
		}
		base := strings.TrimSuffix(symbol, "USDT")
		if !isMarketOverviewSymbol(symbol, base) {
			continue
		}
		turnover, _ := strconv.ParseFloat(strings.TrimSpace(it.Turnover24h), 64)
		if turnover <= 0 {
			continue
		}
		pct, err := strconv.ParseFloat(strings.TrimSpace(it.Price24hPcnt), 64)
		if err != nil {
			continue
		}
		pct = pct * 100
		last, _ := strconv.ParseFloat(strings.TrimSpace(it.LastPrice), 64)
		row := tickerRow{
			symbol:      symbol,
			base:        base,
			pct:         pct,
			last:        last,
			turnover:    turnover,
			displayName: shortSymbol(symbol),
		}
		if prev, exists := bestByBase[base]; !exists || row.turnover > prev.turnover {
			bestByBase[base] = row
		}
	}

	rows := make([]tickerRow, 0, len(bestByBase))
	for _, row := range bestByBase {
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].turnover > rows[j].turnover
	})

	movers := make([]topMoverDTO, 0, len(rows))
	for _, row := range rows {
		if row.pct > threshold {
			up++
		} else if row.pct < -threshold {
			down++
		} else {
			flat++
		}
		direction := "flat"
		if row.pct > 0 {
			direction = "up"
		} else if row.pct < 0 {
			direction = "down"
		}
		movers = append(movers, topMoverDTO{
			Symbol:      row.symbol,
			ChangePct:   row.pct,
			Direction:   direction,
			LastPrice:   row.last,
			DisplayName: row.displayName,
		})
	}

	sort.SliceStable(movers, func(i, j int) bool {
		return math.Abs(movers[i].ChangePct) > math.Abs(movers[j].ChangePct)
	})
	if len(movers) > 8 {
		movers = movers[:8]
	}

	trend := priceTrendDTO{
		Up:           up,
		Flat:         flat,
		Down:         down,
		ThresholdPct: threshold,
		Total:        up + flat + down,
	}
	return trend, movers, nil
}

func (a *API) fetchBinanceDistributionAndMovers(ctx context.Context, futures bool) (priceTrendDTO, []topMoverDTO, error) {
	var payload []struct {
		Symbol             string `json:"symbol"`
		PriceChangePercent string `json:"priceChangePercent"`
		LastPrice          string `json:"lastPrice"`
		QuoteVolume        string `json:"quoteVolume"`
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("BINANCE_SPOT_BASE_URL")), "/")
	path := "/api/v3/ticker/24hr"
	if baseURL == "" {
		baseURL = "https://api.binance.com"
	}
	if futures {
		baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("BINANCE_FUTURES_BASE_URL")), "/")
		path = "/fapi/v1/ticker/24hr"
		if baseURL == "" {
			baseURL = "https://fapi.binance.com"
		}
	}
	if err := a.getJSON(ctx, baseURL+path, &payload); err != nil {
		return priceTrendDTO{ThresholdPct: priceTrendThresholdPct}, nil, err
	}

	type tickerRow struct {
		symbol      string
		pct         float64
		last        float64
		turnover    float64
		displayName string
	}
	bestByBase := make(map[string]tickerRow, len(payload))
	for _, item := range payload {
		symbol := strings.ToUpper(strings.TrimSpace(item.Symbol))
		if !strings.HasSuffix(symbol, "USDT") {
			continue
		}
		base := strings.TrimSuffix(symbol, "USDT")
		if !isMarketOverviewSymbol(symbol, base) {
			continue
		}
		turnover, err := strconv.ParseFloat(strings.TrimSpace(item.QuoteVolume), 64)
		if err != nil || turnover <= 0 {
			continue
		}
		pct, err := strconv.ParseFloat(strings.TrimSpace(item.PriceChangePercent), 64)
		if err != nil {
			continue
		}
		last, _ := strconv.ParseFloat(strings.TrimSpace(item.LastPrice), 64)
		row := tickerRow{
			symbol:      symbol,
			pct:         pct,
			last:        last,
			turnover:    turnover,
			displayName: shortSymbol(symbol),
		}
		if previous, exists := bestByBase[base]; !exists || row.turnover > previous.turnover {
			bestByBase[base] = row
		}
	}

	rows := make([]tickerRow, 0, len(bestByBase))
	for _, row := range bestByBase {
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].turnover > rows[j].turnover })

	trend := priceTrendDTO{ThresholdPct: priceTrendThresholdPct}
	movers := make([]topMoverDTO, 0, len(rows))
	for _, row := range rows {
		switch {
		case row.pct > priceTrendThresholdPct:
			trend.Up++
		case row.pct < -priceTrendThresholdPct:
			trend.Down++
		default:
			trend.Flat++
		}
		direction := "flat"
		if row.pct > 0 {
			direction = "up"
		} else if row.pct < 0 {
			direction = "down"
		}
		movers = append(movers, topMoverDTO{
			Symbol: row.symbol, ChangePct: row.pct, Direction: direction,
			LastPrice: row.last, DisplayName: row.displayName,
		})
	}
	trend.Total = trend.Up + trend.Flat + trend.Down
	sort.SliceStable(movers, func(i, j int) bool {
		return math.Abs(movers[i].ChangePct) > math.Abs(movers[j].ChangePct)
	})
	if len(movers) > 8 {
		movers = movers[:8]
	}
	return trend, movers, nil
}

func (a *API) getBybitJSON(ctx context.Context, path string, dst any) error {
	hosts := []string{"https://api.bybit.com", "https://api.bytick.com"}
	errors := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if err := a.getJSON(ctx, host+path, dst); err == nil {
			return nil
		} else {
			errors = append(errors, host+": "+err.Error())
		}
	}
	return fmt.Errorf("all Bybit endpoints failed: %s", strings.Join(errors, "; "))
}

func isMarketOverviewSymbol(symbol, baseCoin string) bool {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	base := strings.ToUpper(strings.TrimSpace(baseCoin))
	if symbol == "" || base == "" {
		return false
	}
	if !strings.HasSuffix(symbol, "USDT") {
		return false
	}
	if strings.ContainsAny(symbol, "-_/") {
		return false
	}
	if isLeveragedTokenBase(base) {
		return false
	}
	return true
}

func isLeveragedTokenBase(base string) bool {
	if len(base) < 3 {
		return false
	}
	suffixes := []string{"2L", "2S", "3L", "3S", "4L", "4S", "5L", "5S", "UP", "DOWN", "BULL", "BEAR"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

func tailPoints(points []sentimentPoint, n int) []sentimentPoint {
	if n <= 0 || len(points) <= n {
		return points
	}
	return points[len(points)-n:]
}

func shortSymbol(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	suffixes := []string{"USDT", "USDC", "USD", "PERP"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			trimmed := strings.TrimSuffix(s, suffix)
			if trimmed != "" {
				return trimmed + "/" + suffix
			}
		}
	}
	return s
}

func (a *API) getJSON(ctx context.Context, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "shortlong-market-overview/1.0")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func (a *API) getAnyCache() (marketOverviewDTO, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.cacheAt.IsZero() {
		return marketOverviewDTO{}, false
	}
	return a.cache, true
}

func (a *API) setCache(payload marketOverviewDTO) {
	a.mu.Lock()
	a.cache = payload
	a.cacheAt = time.Now().UTC()
	a.mu.Unlock()
	a.saveDiskCache(payload)
}

func (a *API) startBackgroundRefresh() {
	go func() {
		ticker := time.NewTicker(marketOverviewRefreshInterval)
		defer ticker.Stop()

		for range ticker.C {
			a.refreshOverviewSnapshot()
		}
	}()
}

func (a *API) refreshOverviewSnapshot() {
	a.mu.Lock()
	if a.refreshing {
		a.mu.Unlock()
		return
	}
	a.refreshing = true
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.refreshing = false
		a.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	payload, ok := a.buildOverview(ctx)
	if ok {
		a.setCache(payload)
		return
	}
	log.Printf("market overview: refresh produced no usable data; keeping previous snapshot")
}

func (a *API) mergeSeries(
	mcapUSD float64,
	volUSD float64,
	mcapChange float64,
	mcapChangeAvailable bool,
	volChange float64,
	volChangeAvailable bool,
) ([]float64, []float64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.capHistory) == 0 && mcapUSD > 0 && mcapChangeAvailable {
		if previous, ok := valueBeforeChange(mcapUSD, mcapChange); ok {
			a.capHistory = append(a.capHistory, previous)
		}
	}
	if len(a.volHistory) == 0 && volUSD > 0 && volChangeAvailable {
		if previous, ok := valueBeforeChange(volUSD, volChange); ok {
			a.volHistory = append(a.volHistory, previous)
		}
	}
	if mcapUSD > 0 {
		a.capHistory = append(a.capHistory, mcapUSD)
	}
	if volUSD > 0 {
		a.volHistory = append(a.volHistory, volUSD)
	}

	a.capHistory = tailValues(a.capHistory, 720)
	a.volHistory = tailValues(a.volHistory, 720)

	return append([]float64{}, a.capHistory...), append([]float64{}, a.volHistory...)
}

func valueBeforeChange(current, changePct float64) (float64, bool) {
	denominator := 1 + changePct/100
	if current <= 0 || denominator <= 0 || math.IsNaN(denominator) || math.IsInf(denominator, 0) {
		return 0, false
	}
	previous := current / denominator
	if previous <= 0 || math.IsNaN(previous) || math.IsInf(previous, 0) {
		return 0, false
	}
	return previous, true
}

func defaultCacheFile() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "short-long", "market_overview_cache.json")
	}
	return filepath.Join(os.TempDir(), "short-long-market-overview-cache.json")
}

func (a *API) loadDiskCache() {
	if a.cacheFile == "" {
		return
	}
	f, err := os.Open(a.cacheFile)
	if err != nil {
		return
	}
	defer f.Close()

	var payload marketOverviewDTO
	if err := json.NewDecoder(f).Decode(&payload); err != nil {
		return
	}
	if payload.UpdatedAt == "" {
		return
	}
	if !hasMarketSlice(payload.Markets.Spot) && hasMarketSlice(marketSliceDTO{
		PriceTrend: payload.PriceTrend,
		TopMovers:  payload.TopMovers,
	}) {
		payload.Markets.Spot = marketSliceDTO{PriceTrend: payload.PriceTrend, TopMovers: payload.TopMovers}
	}
	if payload.Exchanges == nil {
		payload.Exchanges = make(map[string]exchangeMarketsDTO)
	}
	if _, ok := payload.Exchanges["bybit"]; !ok {
		payload.Exchanges["bybit"] = exchangeMarketsDTO{
			Spot: payload.Markets.Spot, Futures: payload.Markets.Futures,
		}
	}
	binance := payload.Exchanges["binance"]
	if payload.Sentiment.Value <= 0 && payload.MarketCap.USD <= 0 && payload.Volume24h.USD <= 0 &&
		!hasMarketSlice(payload.Markets.Spot) && !hasMarketSlice(payload.Markets.Futures) &&
		!hasMarketSlice(binance.Spot) && !hasMarketSlice(binance.Futures) {
		return
	}
	if ts, err := time.Parse(time.RFC3339, payload.UpdatedAt); err == nil {
		a.cacheAt = ts
	} else {
		a.cacheAt = time.Now().UTC()
	}
	a.cache = payload
	a.capHistory = append([]float64{}, payload.MarketCap.Series...)
	a.volHistory = append([]float64{}, payload.Volume24h.Series...)
}

func hasMarketSlice(slice marketSliceDTO) bool {
	return slice.PriceTrend.Total > 0 && len(slice.TopMovers) > 0
}

func (a *API) saveDiskCache(payload marketOverviewDTO) {
	if a.cacheFile == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(a.cacheFile), 0o755); err != nil {
		return
	}
	tmp := a.cacheFile + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	encErr := json.NewEncoder(f).Encode(payload)
	closeErr := f.Close()
	if encErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		return
	}
	_ = os.Rename(tmp, a.cacheFile)
}

func tailValues(values []float64, n int) []float64 {
	if n <= 0 || len(values) <= n {
		return values
	}
	return values[len(values)-n:]
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("market overview: JSON encoding failed: %v", err)
		status = http.StatusInternalServerError
		body = []byte(`{"error":"market_overview_encode_failed"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
