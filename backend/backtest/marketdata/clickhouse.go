package marketdata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	bt "shortlong/backtest"
	"strconv"
	"strings"
	"time"
)

type ClickHouse struct {
	URL, User, Password string
	HTTP                *http.Client
	MinuteArchive       bool
}

func NewClickHouse(endpoint, user, password string) *ClickHouse {
	return &ClickHouse{URL: endpoint, User: user, Password: password, HTTP: httpClient(60 * time.Second), MinuteArchive: os.Getenv("CANDLE_ARCHIVE_MODE") == "1m"}
}
func (c *ClickHouse) query(ctx context.Context, sql string, params url.Values, body []byte) ([]byte, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("Некорректный адрес хранилища свечей")
	}
	q := u.Query()
	q.Set("query", sql)
	q.Set("wait_end_of_query", "1")
	q.Set("output_format_json_quote_64bit_integers", "0")
	for k, v := range params {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.User, c.Password)
	response, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Хранилище свечей недоступно: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Хранилище свечей вернуло HTTP %d", response.StatusCode)
	}
	return raw, nil
}
func (c *ClickHouse) Init(ctx context.Context) error {
	if c.MinuteArchive {
		return c.InitMinutes(ctx)
	}
	_, err := c.query(ctx, `CREATE TABLE IF NOT EXISTS shortlong_backtest.candles (
 market LowCardinality(String), symbol LowCardinality(String), timeframe LowCardinality(String),
 time Int64, open Float64, high Float64, low Float64, close Float64, volume Float64,
 version UInt64 DEFAULT toUnixTimestamp64Milli(now64(3))
 ) ENGINE=ReplacingMergeTree(version)
 PARTITION BY toYYYYMM(toDateTime(intDiv(time,1000)))
 ORDER BY (market,symbol,timeframe,time)`, nil, nil)
	return err
}
func (c *ClickHouse) Ping(ctx context.Context) error {
	_, err := c.query(ctx, "SELECT 1", nil, nil)
	return err
}
func (c *ClickHouse) Read(ctx context.Context, market, symbol, tf string, from, to int64) ([]bt.Candle, error) {
	if c.MinuteArchive {
		return c.ReadMinutes(ctx, "bybit", market, symbol, tf, from, to)
	}
	params := url.Values{"param_market": {market}, "param_symbol": {symbol}, "param_tf": {tf}, "param_from": {strconv.FormatInt(from, 10)}, "param_to": {strconv.FormatInt(to, 10)}}
	raw, err := c.query(ctx, `SELECT time,open,high,low,close,volume FROM shortlong_backtest.candles FINAL WHERE market={market:String} AND symbol={symbol:String} AND timeframe={tf:String} AND time>={from:Int64} AND time<{to:Int64} ORDER BY time FORMAT JSONEachRow`, params, nil)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	out := []bt.Candle{}
	for {
		var candle bt.Candle
		err = decoder.Decode(&candle)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("Повреждённый ответ хранилища свечей")
		}
		out = append(out, candle)
	}
	return out, nil
}
func (c *ClickHouse) Insert(ctx context.Context, market, symbol, tf string, candles []bt.Candle) error {
	if c.MinuteArchive {
		if tf != "1m" {
			return fmt.Errorf("minute archive only accepts 1m source candles")
		}
		return c.InsertMinutes(ctx, "bybit", market, symbol, candles)
	}
	if len(candles) == 0 {
		return nil
	}
	var data strings.Builder
	encoder := json.NewEncoder(&data)
	for _, v := range candles {
		row := struct {
			Market    string `json:"market"`
			Symbol    string `json:"symbol"`
			Timeframe string `json:"timeframe"`
			bt.Candle
		}{market, symbol, tf, v}
		if err := encoder.Encode(row); err != nil {
			return err
		}
	}
	_, err := c.query(ctx, `INSERT INTO shortlong_backtest.candles (market,symbol,timeframe,time,open,high,low,close,volume) FORMAT JSONEachRow`, nil, []byte(data.String()))
	return err
}
