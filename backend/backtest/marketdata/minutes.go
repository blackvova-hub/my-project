package marketdata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	bt "shortlong/backtest"
	"shortlong/backtest/engine"
	"strconv"
	"strings"
	"time"
)

const MinuteStep int64 = 60000

func candleStep(tf string) int64 {
	if tf == "1m" {
		return MinuteStep
	}
	return bt.Interval(tf).Milliseconds()
}

// A separate versioned schema keeps existing 5m installations recoverable.
// Minute-mode deployments store raw OHLCV only here; larger bars are read-time aggregates.
func (c *ClickHouse) InitMinutes(ctx context.Context) error {
	_, err := c.query(ctx, `CREATE TABLE IF NOT EXISTS shortlong_backtest.candles_1m (
 exchange LowCardinality(String), market LowCardinality(String), symbol LowCardinality(String),
 time Int64 CODEC(Delta, ZSTD(1)),
 open Float64 CODEC(Gorilla, ZSTD(1)), high Float64 CODEC(Gorilla, ZSTD(1)),
 low Float64 CODEC(Gorilla, ZSTD(1)), close Float64 CODEC(Gorilla, ZSTD(1)),
 volume Float64 CODEC(Gorilla, ZSTD(1)),
 version UInt64 DEFAULT toUnixTimestamp64Milli(now64(3)) CODEC(Delta, ZSTD(1))
 ) ENGINE=ReplacingMergeTree(version)
 PARTITION BY toYYYYMM(toDateTime(intDiv(time,1000)))
 ORDER BY (exchange,market,symbol,time)`, nil, nil)
	return err
}

func validVenue(exchange, market string) bool {
	return (exchange == "bybit" || exchange == "binance") && (market == "spot" || market == "linear")
}

func (c *ClickHouse) ReadMinutes(ctx context.Context, exchange, market, symbol, tf string, from, to int64) ([]bt.Candle, error) {
	step := candleStep(tf)
	if !validVenue(exchange, market) || !bt.ValidSymbol(symbol) || step < MinuteStep || from < 0 || to <= from || from%step != 0 || to%step != 0 {
		return nil, fmt.Errorf("invalid minute archive query")
	}
	p := url.Values{"param_exchange": {exchange}, "param_market": {market}, "param_symbol": {symbol}, "param_from": {strconv.FormatInt(from, 10)}, "param_to": {strconv.FormatInt(to, 10)}, "param_step": {strconv.FormatInt(step, 10)}}
	// FINAL removes retry duplicates before aggregation. Missing minutes never become flat bars.
	raw, err := c.query(ctx, `SELECT intDiv(time,{step:Int64})*{step:Int64} AS bucket,
 argMin(open,time) AS open,max(high) AS high,min(low) AS low,argMax(close,time) AS close,sum(volume) AS volume
 FROM shortlong_backtest.candles_1m FINAL
 WHERE exchange={exchange:String} AND market={market:String} AND symbol={symbol:String}
 AND time>={from:Int64} AND time<{to:Int64}
 GROUP BY bucket HAVING count()=intDiv({step:Int64},60000) ORDER BY bucket FORMAT JSONEachRow`, p, nil)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	out := []bt.Candle{}
	for {
		var row struct {
			Bucket int64 `json:"bucket"`
			bt.Candle
		}
		err = dec.Decode(&row)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		row.Candle.Time = row.Bucket
		out = append(out, row.Candle)
	}
	return out, nil
}

func (c *ClickHouse) InsertMinutes(ctx context.Context, exchange, market, symbol string, candles []bt.Candle) error {
	if !validVenue(exchange, market) || !bt.ValidSymbol(symbol) {
		return fmt.Errorf("invalid minute archive series")
	}
	if len(candles) == 0 {
		return nil
	}
	var data strings.Builder
	enc := json.NewEncoder(&data)
	closed := time.Now().UTC().Truncate(time.Minute).UnixMilli()
	for _, v := range candles {
		if v.Time >= closed {
			return fmt.Errorf("refusing open/future minute")
		}
		if err := engine.ValidateCandles([]bt.Candle{v}, v.Time, v.Time+MinuteStep, MinuteStep); err != nil {
			return err
		}
		row := struct {
			Exchange string `json:"exchange"`
			Market   string `json:"market"`
			Symbol   string `json:"symbol"`
			bt.Candle
		}{exchange, market, symbol, v}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	// Acknowledge only after durable flush; concurrent batches share server-side buffers.
	_, err := c.query(ctx, `INSERT INTO shortlong_backtest.candles_1m (exchange,market,symbol,time,open,high,low,close,volume)
 SETTINGS async_insert=1,wait_for_async_insert=1,async_insert_busy_timeout_ms=1000 FORMAT JSONEachRow`, nil, []byte(data.String()))
	return err
}
