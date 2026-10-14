package macroapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"
)

func (a *API) refreshMarkets(ctx context.Context) {
	items := []MarketItem{}
	var firstErr error
	for _, asset := range []struct{ symbol, name string }{{"BTC", "Bitcoin"}, {"ETH", "Ethereum"}} {
		item, err := a.bybitMarket(ctx, asset.symbol, asset.name)
		if err != nil {
			item, err = a.binanceMarket(ctx, asset.symbol, asset.name)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			item = MarketItem{Symbol: asset.symbol, Name: asset.name, AssetClass: "crypto", Unit: "USDT", Source: "Bybit / Binance", SourceURL: "https://bybit-exchange.github.io/docs/v5/market/tickers", Points: []Point{}, Status: "unavailable", Note: "Биржевые источники недоступны. Цена не подставляется."}
			a.mu.RLock()
			for _, prior := range a.state.Markets {
				if prior.Symbol == asset.symbol {
					item = prior
					item.Status = "stale"
					item.Note = "Источник недоступен; сохранена последняя полученная котировка"
				}
			}
			a.mu.RUnlock()
		}
		if item.Status == "fresh" && item.ObservedAt != nil {
			if at, e := time.Parse(time.RFC3339, *item.ObservedAt); e != nil || a.now().Sub(at) > 2*marketInterval {
				item.Status = "stale"
				item.Note += " Время источника выходит за допустимую задержку котировки."
			}
		}
		items = append(items, item)
	}
	a.mu.Lock()
	a.state.Markets = items
	a.mu.Unlock()
	a.health("crypto-markets", firstErr)
}

func (a *API) bybitMarket(ctx context.Context, symbol, name string) (MarketItem, error) {
	data, err := a.get(ctx, "https://api.bybit.com/v5/market/tickers?category=spot&symbol="+symbol+"USDT")
	if err != nil {
		return MarketItem{}, err
	}
	var raw struct {
		Code   int   `json:"retCode"`
		Time   int64 `json:"time"`
		Result struct {
			List []struct {
				Symbol string `json:"symbol"`
				Price  string `json:"lastPrice"`
				Change string `json:"price24hPcnt"`
			} `json:"list"`
		} `json:"result"`
	}
	if json.Unmarshal(data, &raw) != nil || raw.Code != 0 || len(raw.Result.List) != 1 {
		return MarketItem{}, errors.New("Bybit не вернул котировку")
	}
	quote := raw.Result.List[0]
	price, e1 := strconv.ParseFloat(quote.Price, 64)
	change, e2 := strconv.ParseFloat(quote.Change, 64)
	if e1 != nil || e2 != nil || price <= 0 || !finite(price) || !finite(change) || quote.Symbol != symbol+"USDT" {
		return MarketItem{}, errors.New("Bybit вернул неверную котировку")
	}
	at := time.UnixMilli(raw.Time)
	if raw.Time <= 0 {
		return MarketItem{}, errors.New("Bybit не указал время котировки")
	}
	item := MarketItem{Symbol: symbol, Name: name, AssetClass: "crypto", Price: ptr(price), ChangePercent: ptr(change * 100), Unit: "USDT", Source: "Bybit · spot", SourceURL: "https://www.bybit.com/en/trade/spot/" + symbol + "/USDT", ObservedAt: ptr(at.UTC().Format(time.RFC3339)), Points: []Point{}, Status: "fresh", Note: "Цена spot в USDT; изменение за 24 часа. Мини-график — дневные закрытия; последняя свеча может быть незакрытой."}
	data, err = a.get(ctx, "https://api.bybit.com/v5/market/kline?category=spot&symbol="+symbol+"USDT&interval=D&limit=30")
	if err == nil {
		var candles struct {
			Code   int `json:"retCode"`
			Result struct {
				List [][]string `json:"list"`
			} `json:"result"`
		}
		if json.Unmarshal(data, &candles) == nil && candles.Code == 0 {
			for _, c := range candles.Result.List {
				if len(c) < 5 {
					continue
				}
				ts, e1 := strconv.ParseInt(c[0], 10, 64)
				close, e2 := strconv.ParseFloat(c[4], 64)
				if e1 == nil && e2 == nil && finite(close) && close > 0 {
					item.Points = append(item.Points, Point{time.UnixMilli(ts).UTC().Format("2006-01-02"), close})
				}
			}
		}
	}
	item.Points = normalizePoints(item.Points)
	if len(item.Points) == 0 {
		item.Note = "Цена spot в USDT; изменение за 24 часа. Источник не предоставил историю мини-графика."
	}
	return item, nil
}

func (a *API) binanceMarket(ctx context.Context, symbol, name string) (MarketItem, error) {
	data, err := a.get(ctx, "https://api.binance.com/api/v3/ticker/24hr?symbol="+symbol+"USDT")
	if err != nil {
		return MarketItem{}, err
	}
	var quote struct {
		Symbol    string `json:"symbol"`
		Price     string `json:"lastPrice"`
		Change    string `json:"priceChangePercent"`
		CloseTime int64  `json:"closeTime"`
	}
	if json.Unmarshal(data, &quote) != nil {
		return MarketItem{}, errors.New("Binance вернул неверный JSON")
	}
	price, e1 := strconv.ParseFloat(quote.Price, 64)
	change, e2 := strconv.ParseFloat(quote.Change, 64)
	if e1 != nil || e2 != nil || price <= 0 || !finite(price) || !finite(change) || quote.Symbol != symbol+"USDT" || quote.CloseTime <= 0 {
		return MarketItem{}, errors.New("Binance не вернул корректную котировку")
	}
	item := MarketItem{Symbol: symbol, Name: name, AssetClass: "crypto", Price: ptr(price), ChangePercent: ptr(change), Unit: "USDT", Source: "Binance · spot", SourceURL: "https://www.binance.com/en/trade/" + symbol + "_USDT", ObservedAt: ptr(time.UnixMilli(quote.CloseTime).UTC().Format(time.RFC3339)), Points: []Point{}, Status: "fresh", Note: "Цена spot в USDT; изменение за 24 часа. Мини-график — дневные закрытия; последняя свеча может быть незакрытой."}
	data, err = a.get(ctx, "https://api.binance.com/api/v3/klines?symbol="+symbol+"USDT&interval=1d&limit=30")
	if err == nil {
		var rows [][]json.RawMessage
		if json.Unmarshal(data, &rows) == nil {
			for _, r := range rows {
				if len(r) < 5 {
					continue
				}
				var ts int64
				var value string
				if json.Unmarshal(r[0], &ts) != nil || json.Unmarshal(r[4], &value) != nil {
					continue
				}
				v, e := strconv.ParseFloat(value, 64)
				if e == nil && finite(v) && v > 0 {
					item.Points = append(item.Points, Point{time.UnixMilli(ts).UTC().Format("2006-01-02"), v})
				}
			}
		}
	}
	item.Points = normalizePoints(item.Points)
	if len(item.Points) == 0 {
		item.Note = "Цена spot в USDT; изменение за 24 часа. История мини-графика недоступна."
	}
	return item, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
