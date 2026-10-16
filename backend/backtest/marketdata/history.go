package marketdata

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	bt "shortlong/backtest"
	"shortlong/backtest/engine"
	"time"
)

type History struct {
	DB      *pgxpool.Pool
	Storage *ClickHouse
	Bybit   *Bybit
}

// SQL session locks serialize cache filling across every worker replica. A
// waiting request rechecks storage after acquiring the lock, avoiding 500 downloads.
func (h *History) Load(ctx context.Context, market, symbol, tf string, from, to int64, progress func(int)) ([]bt.Candle, error) {
	step := bt.Interval(tf).Milliseconds()
	if step == 0 || from < 0 || to <= from || from%step != 0 || to%step != 0 {
		return nil, fmt.Errorf("Некорректный диапазон истории")
	}
	if h.Storage.MinuteArchive {
		cached, err := h.Storage.Read(ctx, market, symbol, tf, from, to)
		if err != nil {
			return nil, err
		}
		if engine.ValidateCandles(cached, from, to, step) == nil {
			return cached, nil
		}
		base, err := h.fill(ctx, market, symbol, "1m", from, to, progress, false, true)
		if err != nil {
			return nil, err
		}
		return aggregateFrom(base, tf, from, to, 60000)
	}
	if tf == "1m" {
		// The legacy series-retention registry only accepts larger timeframes.
		// Raw minutes, like minute-archive mode, do not register a 5m series.
		return h.load(ctx, market, symbol, tf, from, to, progress, false)
	}
	if tf != "5m" {
		if bt.Interval(tf) == 0 {
			return nil, fmt.Errorf("Некорректный таймфрейм")
		}
		cached, err := h.Storage.Read(ctx, market, symbol, tf, from, to)
		if err != nil {
			return nil, err
		}
		if engine.ValidateCandles(cached, from, to, bt.Interval(tf).Milliseconds()) == nil {
			return cached, nil
		}
		base, err := h.load(ctx, market, symbol, "5m", from, to, progress, true)
		if err != nil {
			return nil, err
		}
		return Aggregate(base, tf, from, to)
	}
	return h.load(ctx, market, symbol, tf, from, to, progress, true)
}

// LoadAvailable accepts an unavailable prefix, such as time before listing, but
// requires every source candle from the first actual row through to. Incomplete
// leading timeframe buckets are discarded rather than presented as full bars.
// Load remains strict for callers that require the whole requested range.
func (h *History) LoadAvailable(ctx context.Context, market, symbol, tf string, from, to int64, progress func(int)) ([]bt.Candle, error) {
	step := bt.Interval(tf).Milliseconds()
	if step == 0 || from < 0 || to <= from || from%step != 0 || to%step != 0 {
		return nil, fmt.Errorf("%s: некорректный диапазон истории", symbol)
	}
	if h.Storage.MinuteArchive || tf != "5m" {
		cached, err := h.Storage.Read(ctx, market, symbol, tf, from, to)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", symbol, err)
		}
		if engine.ValidateCandles(cached, from, to, step) == nil {
			return cached, nil
		}
	}
	baseTF, baseStep, touch := "5m", int64(300000), true
	if h.Storage.MinuteArchive {
		baseTF, baseStep, touch = "1m", MinuteStep, false
	} else if tf == "1m" {
		baseTF, baseStep, touch = "1m", MinuteStep, false
	}
	base, err := h.fill(ctx, market, symbol, baseTF, from, to, progress, touch, false)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", symbol, err)
	}
	return availableTail(base, symbol, tf, from, to, baseStep)
}

func availableTail(base []bt.Candle, symbol, tf string, from, to, baseStep int64) ([]bt.Candle, error) {
	if len(base) == 0 {
		return []bt.Candle{}, nil
	}
	step := bt.Interval(tf).Milliseconds()
	first := base[0].Time
	if first < from || first >= to || first%baseStep != 0 {
		return nil, fmt.Errorf("%s: некорректная граница истории %s UTC", symbol, time.UnixMilli(first).UTC().Format("02.01.2006 15:04"))
	}
	expected := first
	for _, candle := range base {
		if candle.Time != expected {
			return nil, missingCandle(symbol, expected)
		}
		if err := engine.ValidateCandles([]bt.Candle{candle}, expected, expected+baseStep, baseStep); err != nil {
			return nil, fmt.Errorf("%s: %s UTC: %w", symbol, time.UnixMilli(expected).UTC().Format("02.01.2006 15:04"), err)
		}
		expected += baseStep
	}
	if expected != to {
		return nil, missingCandle(symbol, expected)
	}
	start := first
	if remainder := start % step; remainder != 0 {
		start += step - remainder
	}
	if start >= to {
		return []bt.Candle{}, nil
	}
	base = base[int((start-first)/baseStep):]
	return aggregateFrom(base, tf, start, to, baseStep)
}

func missingCandle(symbol string, ts int64) error {
	return fmt.Errorf("%s: пропуск свечи %s UTC. Пропуски после начала истории не допускаются", symbol, time.UnixMilli(ts).UTC().Format("02.01.2006 15:04"))
}

func (h *History) load(ctx context.Context, market, symbol, tf string, from, to int64, progress func(int), touch bool) ([]bt.Candle, error) {
	return h.fill(ctx, market, symbol, tf, from, to, progress, touch, true)
}

// SyncRange archives real provider rows, including sparse or pre-listing ranges.
// The caller records missing coverage; strict consumers still reject every gap.
func (h *History) SyncRange(ctx context.Context, market, symbol string, from, to int64) ([]bt.Candle, error) {
	if from < 0 || to <= from || from%300000 != 0 || to%300000 != 0 || to-from > 1000*300000 {
		return nil, fmt.Errorf("Invalid archive range")
	}
	return h.fill(ctx, market, symbol, "5m", from, to, nil, false, false)
}

func (h *History) fill(ctx context.Context, market, symbol, tf string, from, to int64, progress func(int), touch, strict bool) ([]bt.Candle, error) {
	conn, err := h.DB.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	key := "backtest:candles:" + market + ":" + symbol + ":" + tf
	for {
		var acquired bool
		if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&acquired); err != nil {
			return nil, err
		}
		if acquired {
			break
		}
		if err = wait(ctx, time.Second); err != nil {
			return nil, err
		}
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := conn.Exec(releaseCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key); e != nil {
			_ = conn.Conn().Close(releaseCtx)
		}
	}()
	if touch {
		if _, err = conn.Exec(ctx, `INSERT INTO backtest_candle_series(market,symbol,timeframe) VALUES($1,$2,$3) ON CONFLICT(market,symbol,timeframe) DO UPDATE SET last_used_at=now()`, market, symbol, tf); err != nil {
			return nil, err
		}
	}
	cached, err := h.Storage.Read(ctx, market, symbol, tf, from, to)
	if err != nil {
		return nil, err
	}
	step := candleStep(tf)
	byTime := make(map[int64]bt.Candle, len(cached))
	for _, c := range cached {
		byTime[c.Time] = c
	}
	for start := from; start < to; {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if progress != nil {
			progress(int((start - from) * 100 / (to - from)))
		}
		if _, ok := byTime[start]; ok {
			start += step
			continue
		}
		end := start + step
		for end < to && end < start+1000*step {
			if _, ok := byTime[end]; ok {
				break
			}
			end += step
		}
		fresh, e := h.Bybit.Candles(ctx, market, symbol, tf, start, end)
		if e != nil {
			return nil, e
		}
		for _, candle := range fresh {
			if e = engine.ValidateCandles([]bt.Candle{candle}, candle.Time, candle.Time+step, step); e != nil {
				return nil, fmt.Errorf("%s: %w", symbol, e)
			}
		}
		if e = h.Storage.Insert(ctx, market, symbol, tf, fresh); e != nil {
			return nil, e
		}
		for _, c := range fresh {
			byTime[c.Time] = c
		}
		start = end
		start = end
	}
	candles := make([]bt.Candle, 0, len(byTime))
	for ts := from; ts < to; ts += step {
		if candle, ok := byTime[ts]; ok {
			candles = append(candles, candle)
		}
	}
	if strict {
		if err = engine.ValidateCandles(candles, from, to, step); err != nil {
			return nil, err
		}
	}
	if _, err = conn.Exec(ctx, `UPDATE backtest_candle_series SET refreshed_at=now() WHERE market=$1 AND symbol=$2 AND timeframe=$3`, market, symbol, tf); err != nil {
		return nil, err
	}
	if progress != nil {
		progress(100)
	}
	return candles, nil
}
