package marketdata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	bt "shortlong/backtest"
	"strconv"
	"strings"
	"time"
)

type MetricPoint struct {
	Time  int64   `json:"time"`
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

func (c *ClickHouse) InitIndicators(ctx context.Context) error {
	_, err := c.query(ctx, `CREATE TABLE IF NOT EXISTS shortlong_backtest.indicator_history (
 market LowCardinality(String), symbol LowCardinality(String), name LowCardinality(String),
 time Int64, value Float64, version UInt64 DEFAULT toUnixTimestamp64Milli(now64(3))
 ) ENGINE=ReplacingMergeTree(version) PARTITION BY toYYYYMM(toDateTime(intDiv(time,1000)))
 ORDER BY (market,symbol,name,time)`, nil, nil)
	return err
}
func (c *ClickHouse) readMetrics(ctx context.Context, market, symbol, kind string, from, to int64) ([]MetricPoint, error) {
	names := "'openInterest'"
	if kind == "trades" {
		names = "'tradeBuyVolume','tradeSellVolume'"
	}
	params := url.Values{"param_market": {market}, "param_symbol": {symbol}, "param_from": {strconv.FormatInt(from, 10)}, "param_to": {strconv.FormatInt(to, 10)}}
	raw, err := c.query(ctx, `SELECT time,name,value FROM shortlong_backtest.indicator_history FINAL WHERE market={market:String} AND symbol={symbol:String} AND name IN (`+names+`) AND time>={from:Int64} AND time<{to:Int64} ORDER BY time,name FORMAT JSONEachRow`, params, nil)
	if err != nil {
		return nil, err
	}
	out := []MetricPoint{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var p MetricPoint
		err = dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
func (c *ClickHouse) insertMetrics(ctx context.Context, market, symbol string, points []MetricPoint) error {
	if len(points) == 0 {
		return nil
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	for _, p := range points {
		if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) || p.Value < 0 {
			return errors.New("Некорректные исторические метрики Bybit")
		}
		row := struct {
			Market string `json:"market"`
			Symbol string `json:"symbol"`
			MetricPoint
		}{market, symbol, p}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	_, err := c.query(ctx, `INSERT INTO shortlong_backtest.indicator_history (market,symbol,name,time,value) FORMAT JSONEachRow`, nil, []byte(buf.String()))
	return err
}

// Enrich loads only metrics used by this strategy, on demand. A shared session
// lock prevents two jobs from downloading the same immutable daily trade file.
func (h *History) Enrich(ctx context.Context, r bt.Request, symbol string, candles []bt.Candle, progress func(string, int)) error {
	warmups := r.MetricWarmup()
	completed := 0
	for _, kind := range []string{"oi", "trades"} {
		warm, needed := warmups[kind]
		if !needed {
			continue
		}
		from := r.From.UnixMilli() - int64(warm)*bt.Interval(r.Timeframe).Milliseconds()
		to := r.To.UnixMilli()
		report := func(phase string, percent int) {
			if progress != nil {
				progress(phase, (completed*100+percent)/len(warmups))
			}
		}
		if err := h.enrichRange(ctx, r.Market, symbol, kind, r.Timeframe, from, to, candles, report); err != nil {
			return err
		}
		completed++
	}
	return nil
}
func (h *History) enrichRange(ctx context.Context, market, symbol, kind, tf string, from, to int64, candles []bt.Candle, progress func(string, int)) error {
	const hour = int64(3600000)
	const day = 24 * hour
	start, end := from, to+hour // OI observations include the final closing boundary.
	if kind == "trades" {
		start = from / day * day
		end = (to + day - 1) / day * day
	}
	conn, err := h.DB.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	key := "backtest:metrics:" + market + ":" + symbol + ":" + kind
	for {
		var locked bool
		if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&locked); err != nil {
			return err
		}
		if locked {
			break
		}
		if err = wait(ctx, time.Second); err != nil {
			return err
		}
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(release, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key); e != nil {
			_ = conn.Conn().Close(release)
		}
	}()
	cached, err := h.Storage.readMetrics(ctx, market, symbol, kind, start, end)
	if err != nil {
		return err
	}
	byTime := map[int64]map[string]float64{}
	add := func(points []MetricPoint) {
		for _, p := range points {
			if byTime[p.Time] == nil {
				byTime[p.Time] = map[string]float64{}
			}
			byTime[p.Time][p.Name] = p.Value
		}
	}
	add(cached)
	names := []string{"openInterest"}
	chunk := 200 * hour
	if kind == "trades" {
		names = []string{"tradeBuyVolume", "tradeSellVolume"}
		chunk = day
	}
	for at := start; at < end; {
		chunkEnd := at + chunk
		if chunkEnd > end {
			chunkEnd = end
		}
		complete := true
		for ts := at; ts < chunkEnd; ts += hour {
			for _, name := range names {
				if _, ok := byTime[ts][name]; !ok {
					complete = false
				}
			}
		}
		if !complete {
			if progress != nil {
				progress(kind, int((at-start)*100/(end-start)))
			}
			var fresh []MetricPoint
			if kind == "trades" {
				fresh, err = h.Bybit.TradeDay(ctx, market, symbol, at)
			} else {
				fresh, err = h.Bybit.OpenInterest(ctx, symbol, at, chunkEnd)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", symbol, err)
			}
			if err = h.Storage.insertMetrics(ctx, market, symbol, fresh); err != nil {
				return err
			}
			add(fresh)
		}
		at = chunkEnd
	}
	step := bt.Interval(tf).Milliseconds()
	for i := range candles {
		b := &candles[i]
		if b.Time < from {
			continue
		}
		if b.Metrics == nil {
			b.Metrics = map[string]float64{}
		}
		for _, name := range names {
			value := 0.0
			if kind == "oi" {
				v, ok := byTime[b.Time+step][name]
				if !ok {
					return missingMetric(symbol, name, b.Time+step)
				}
				value = v
			} else {
				for ts := b.Time; ts < b.Time+step; ts += hour {
					v, ok := byTime[ts][name]
					if !ok {
						return missingMetric(symbol, name, ts)
					}
					value += v
				}
			}
			b.Metrics[name] = value
		}
	}
	if progress != nil {
		progress(kind, 100)
	}
	return nil
}
func missingMetric(symbol, name string, ts int64) error {
	return fmt.Errorf("%s: нет истории %s за %s UTC. Выберите период с доступной историей; пропуски не заменяются нулями", symbol, name, time.UnixMilli(ts).UTC().Format("02.01.2006 15:04"))
}
