package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"net/url"
	"os"
	bt "shortlong/backtest"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Bybit struct {
	ArchiveURL   string
	ArchiveHTTP  *http.Client
	BaseURL      string
	HTTP         *http.Client
	Redis        *redis.Client
	RateInterval time.Duration
}

func NewBybit(base string, rdb *redis.Client) *Bybit {
	if base == "" {
		base = "https://api.bybit.com"
	}
	rps, err := strconv.Atoi(os.Getenv("MARKET_HISTORY_RPS"))
	if err != nil || rps < 1 || rps > 30 {
		rps = 20
	}
	return &Bybit{BaseURL: strings.TrimRight(base, "/"), HTTP: httpClient(20 * time.Second), ArchiveHTTP: httpClient(5 * time.Minute), Redis: rdb, RateInterval: time.Second / time.Duration(rps)}
}
func wait(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (b *Bybit) limit(ctx context.Context) error {
	if b.Redis == nil {
		return nil
	}
	for {
		cooldown, err := b.Redis.PTTL(ctx, "backtest:bybit:cooldown").Result()
		if err != nil {
			return err
		}
		if cooldown > 0 {
			if err = wait(ctx, cooldown); err != nil {
				return err
			}
			continue
		}
		interval := b.RateInterval
		if interval <= 0 {
			interval = 180 * time.Millisecond
		}
		ok, err := b.Redis.SetNX(ctx, "backtest:bybit:rate", 1, interval).Result()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if err = wait(ctx, 10*time.Millisecond); err != nil {
			return err
		}
	}
}
func (b *Bybit) get(ctx context.Context, path string, params url.Values, out any) error {
	for attempt := 0; attempt < 4; attempt++ {
		if err := b.limit(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.BaseURL+path+"?"+params.Encode(), nil)
		if err != nil {
			return err
		}
		response, err := b.HTTP.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt == 3 {
				return errors.New("Bybit недоступен. Повторите тест позже")
			}
			if err = wait(ctx, time.Duration(1<<attempt)*time.Second); err != nil {
				return err
			}
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20))
		response.Body.Close()
		if response.StatusCode == http.StatusForbidden && b.Redis != nil {
			_ = b.Redis.Set(ctx, "backtest:bybit:cooldown", 1, 10*time.Minute).Err()
		}
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			if err = wait(ctx, time.Duration(1<<attempt)*time.Second); err != nil {
				return err
			}
			continue
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("Bybit вернул HTTP %d. История недоступна", response.StatusCode)
		}
		if readErr != nil {
			return readErr
		}
		var envelope struct {
			Code   int             `json:"retCode"`
			Result json.RawMessage `json:"result"`
		}
		if err = json.Unmarshal(raw, &envelope); err != nil {
			return errors.New("Некорректный ответ Bybit")
		}
		if envelope.Code == 10006 {
			if err = wait(ctx, time.Duration(1<<attempt)*time.Second); err != nil {
				return err
			}
			continue
		}
		if envelope.Code != 0 {
			return fmt.Errorf("Bybit не смог обработать запрос (код %d)", envelope.Code)
		}
		return json.Unmarshal(envelope.Result, out)
	}
	return errors.New("Лимит запросов Bybit. Повторите тест позже")
}

func (b *Bybit) Symbols(ctx context.Context, market string) ([]string, error) {
	if market != "spot" && market != "linear" {
		return nil, errors.New("Некорректный рынок")
	}
	key := "backtest:symbols:" + market
	if b.Redis != nil {
		if raw, err := b.Redis.Get(ctx, key).Bytes(); err == nil {
			var cached []string
			if json.Unmarshal(raw, &cached) == nil && len(cached) > 0 {
				return cached, nil
			}
		}
	}
	instruments, err := b.Instruments(ctx, market)
	if err != nil {
		return nil, err
	}
	symbols := make([]string, 0, len(instruments))
	for _, i := range instruments {
		symbols = append(symbols, i.Symbol)
	}
	if b.Redis != nil {
		raw, _ := json.Marshal(symbols)
		_ = b.Redis.Set(ctx, key, raw, 10*time.Minute).Err()
	}
	return symbols, nil
}

type Instrument struct {
	Symbol     string
	LaunchTime int64 // Spot does not publish this field; zero means unknown.
}

func (b *Bybit) Instruments(ctx context.Context, market string) ([]Instrument, error) {
	if market != "spot" && market != "linear" {
		return nil, errors.New("Некорректный рынок")
	}
	instruments := []Instrument{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 20; page++ {
		params := url.Values{"category": {market}}
		if market == "linear" {
			params.Set("limit", "1000")
			params.Set("cursor", cursor)
		}
		var result struct {
			List []struct {
				Symbol       string `json:"symbol"`
				QuoteCoin    string `json:"quoteCoin"`
				Status       string `json:"status"`
				ContractType string `json:"contractType"`
				LaunchTime   string `json:"launchTime"`
			} `json:"list"`
			Cursor string `json:"nextPageCursor"`
		}
		if err := b.get(ctx, "/v5/market/instruments-info", params, &result); err != nil {
			return nil, err
		}
		for _, i := range result.List {
			if i.Status == "Trading" && i.QuoteCoin == "USDT" && bt.ValidSymbol(i.Symbol) && (market == "spot" || i.ContractType == "LinearPerpetual") && !seen[i.Symbol] {
				launch, _ := strconv.ParseInt(i.LaunchTime, 10, 64)
				instruments = append(instruments, Instrument{Symbol: i.Symbol, LaunchTime: launch})
				seen[i.Symbol] = true
			}
		}
		if market == "spot" || result.Cursor == "" {
			sort.Slice(instruments, func(i, j int) bool { return instruments[i].Symbol < instruments[j].Symbol })
			if len(instruments) == 0 {
				return nil, errors.New("Bybit вернул пустой список инструментов")
			}
			return instruments, nil
		}
		if result.Cursor == cursor {
			return nil, errors.New("Bybit повторил страницу каталога")
		}
		cursor = result.Cursor
	}
	return nil, errors.New("Каталог Bybit превысил допустимый размер")
}

func (b *Bybit) Candles(ctx context.Context, market, symbol, tf string, from, to int64) ([]bt.Candle, error) {
	step := bt.Interval(tf)
	if tf == "1m" {
		step = time.Minute
	}
	if step == 0 || !bt.ValidSymbol(symbol) || (market != "spot" && market != "linear") {
		return nil, errors.New("Некорректный запрос истории")
	}
	params := url.Values{"category": {market}, "symbol": {symbol}, "interval": {strconv.Itoa(int(step / time.Minute))}, "start": {strconv.FormatInt(from, 10)}, "end": {strconv.FormatInt(to-1, 10)}, "limit": {"1000"}}
	var result struct {
		List [][]string `json:"list"`
	}
	if err := b.get(ctx, "/v5/market/kline", params, &result); err != nil {
		return nil, err
	}
	candles := make([]bt.Candle, 0, len(result.List))
	seen := map[int64]bool{}
	for _, row := range result.List {
		if len(row) < 6 {
			return nil, errors.New("Неполная свеча Bybit")
		}
		ts, err := strconv.ParseInt(row[0], 10, 64)
		if err != nil {
			return nil, err
		}
		if ts < from || ts >= to || ts+step.Milliseconds() > time.Now().UnixMilli() {
			continue
		}
		if ts%step.Milliseconds() != 0 {
			return nil, errors.New("Bybit вернул свечу вне границы таймфрейма")
		}
		if seen[ts] {
			return nil, errors.New("Bybit вернул повторяющуюся свечу")
		}
		seen[ts] = true
		nums := make([]float64, 5)
		for i := range nums {
			nums[i], err = strconv.ParseFloat(row[i+1], 64)
			if err != nil {
				return nil, errors.New("Некорректная цена Bybit")
			}
		}
		candles = append(candles, bt.Candle{Time: ts, Open: nums[0], High: nums[1], Low: nums[2], Close: nums[3], Volume: nums[4]})
	}
	sort.Slice(candles, func(i, j int) bool { return candles[i].Time < candles[j].Time })
	return candles, nil
}
