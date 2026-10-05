package marketdata

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (b *Bybit) OpenInterest(ctx context.Context, symbol string, from, to int64) ([]MetricPoint, error) {
	params := url.Values{"category": {"linear"}, "symbol": {symbol}, "intervalTime": {"1h"}, "startTime": {strconv.FormatInt(from, 10)}, "endTime": {strconv.FormatInt(to-1, 10)}, "limit": {"200"}}
	out := []MetricPoint{}
	seen := map[string]bool{}
	for {
		var response struct {
			List []struct {
				OpenInterest string `json:"openInterest"`
				Timestamp    string `json:"timestamp"`
			} `json:"list"`
			Cursor string `json:"nextPageCursor"`
		}
		if err := b.get(ctx, "/v5/market/open-interest", params, &response); err != nil {
			return nil, err
		}
		earliest := to
		for _, r := range response.List {
			ts, e1 := strconv.ParseInt(r.Timestamp, 10, 64)
			value, e2 := strconv.ParseFloat(r.OpenInterest, 64)
			if e1 != nil || e2 != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || ts%3600000 != 0 {
				return nil, errors.New("Некорректная история открытого интереса Bybit")
			}
			if ts < earliest {
				earliest = ts
			}
			if ts >= from && ts < to {
				out = append(out, MetricPoint{Time: ts, Name: "openInterest", Value: value})
			}
		}
		if response.Cursor == "" || len(response.List) == 0 || earliest <= from {
			break
		}
		if seen[response.Cursor] {
			return nil, errors.New("Bybit повторил страницу открытого интереса")
		}
		seen[response.Cursor] = true
		params.Set("cursor", response.Cursor)
	}
	return out, nil
}

// TradeDay streams the official public archive and keeps only 48 hourly totals
// in memory. Compressed/raw trades are never retained on disk. No background job.
func (b *Bybit) TradeDay(ctx context.Context, market, symbol string, day int64) ([]MetricPoint, error) {
	if (market != "spot" && market != "linear") || day%86400000 != 0 {
		return nil, errors.New("Некорректный диапазон архива сделок")
	}
	date := time.UnixMilli(day).UTC().Format("2006-01-02")
	base := b.ArchiveURL
	if base == "" {
		base = "https://public.bybit.com"
	}
	path := "/trading/" + symbol + "/" + symbol + date + ".csv.gz"
	if market == "spot" {
		path = "/spot/" + symbol + "/" + symbol + "_" + date + ".csv.gz"
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := b.limit(ctx); err != nil {
			return nil, err
		}
		fetch, cancel := context.WithTimeout(ctx, 5*time.Minute)
		req, err := http.NewRequestWithContext(fetch, http.MethodGet, strings.TrimRight(base, "/")+path, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		client := b.ArchiveHTTP
		if client == nil {
			client = httpClient(5 * time.Minute)
		}
		response, err := client.Do(req)
		if err == nil {
			if response.StatusCode == http.StatusOK {
				gz, e := gzip.NewReader(response.Body)
				if e == nil {
					var result []MetricPoint
					result, e = parseTradeDay(fetch, gz, market, symbol, day)
					gz.Close()
					response.Body.Close()
					cancel()
					if e == nil {
						return result, nil
					}
					err = e
				} else {
					response.Body.Close()
					cancel()
					err = e
				}
			} else {
				response.Body.Close()
				cancel()
				if response.StatusCode == http.StatusNotFound {
					return nil, fmt.Errorf("Архив сделок Bybit за %s не опубликован для %s. Выберите другой период для CVD/дельты", date, symbol)
				}
				if response.StatusCode != 429 && response.StatusCode < 500 {
					return nil, fmt.Errorf("Архив сделок Bybit вернул HTTP %d", response.StatusCode)
				}
				err = fmt.Errorf("Архив сделок Bybit временно недоступен (HTTP %d)", response.StatusCode)
			}
		} else {
			cancel()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt == 2 {
			return nil, fmt.Errorf("Не удалось загрузить сделки за %s: %w", date, err)
		}
		if err = wait(ctx, time.Duration(1<<attempt)*time.Second); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("Архив сделок недоступен")
}
func parseTradeDay(ctx context.Context, source io.Reader, market, symbol string, day int64) ([]MetricPoint, error) {
	// Bound decoded input; a truncated file is rejected, never cached as complete.
	limited := &io.LimitedReader{R: source, N: 16 << 30}
	reader := csv.NewReader(limited)
	reader.ReuseRecord = true
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	columns := map[string]int{}
	for i, k := range header {
		columns[k] = i
	}
	size := "size"
	if market == "spot" {
		size = "volume"
	}
	for _, key := range []string{"timestamp", "side", "price", size} {
		if _, ok := columns[key]; !ok {
			return nil, fmt.Errorf("В архиве сделок нет колонки %s", key)
		}
	}
	totals := [24][2]float64{}
	count := 0
	for {
		if count%4096 == 0 {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
		}
		row, e := reader.Read()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, e
		}
		stamp, e1 := strconv.ParseFloat(row[columns["timestamp"]], 64)
		price, e2 := strconv.ParseFloat(row[columns["price"]], 64)
		qty, e3 := strconv.ParseFloat(row[columns[size]], 64)
		if e1 != nil || e2 != nil || e3 != nil || math.IsNaN(stamp) || math.IsInf(stamp, 0) || math.IsNaN(price) || math.IsInf(price, 0) || math.IsNaN(qty) || math.IsInf(qty, 0) || price <= 0 || qty < 0 {
			return nil, errors.New("Повреждённая запись в архиве сделок")
		}
		if market == "linear" {
			stamp *= 1000
		}
		ts := int64(math.Floor(stamp))
		if ts < day || ts >= day+86400000 {
			return nil, errors.New("Сделка находится вне даты архива")
		}
		if column, ok := columns["symbol"]; ok && row[column] != symbol {
			return nil, errors.New("Неверный символ в архиве сделок")
		}
		side := strings.ToLower(row[columns["side"]])
		index := 0
		if side == "sell" {
			index = 1
		} else if side != "buy" {
			return nil, errors.New("Неизвестная сторона сделки")
		}
		totals[(ts-day)/3600000][index] += price * qty
		count++
	}
	if limited.N == 0 {
		return nil, errors.New("Архив сделок превысил допустимый размер")
	}
	if count == 0 {
		return nil, errors.New("Пустой архив сделок Bybit")
	}
	out := make([]MetricPoint, 0, 48)
	for hour, values := range totals {
		for i, name := range []string{"tradeBuyVolume", "tradeSellVolume"} {
			out = append(out, MetricPoint{Time: day + int64(hour)*3600000, Name: name, Value: values[i]})
		}
	}
	return out, nil
}
