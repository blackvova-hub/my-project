package backtestapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	bt "shortlong/backtest"
	"strconv"
	"time"
)

type minuteReader interface {
	ReadMinutes(context.Context, string, string, string, string, int64, int64) ([]bt.Candle, error)
}

func replayVenue(q url.Values) bool {
	return (q.Get("exchange") == "bybit" || q.Get("exchange") == "binance") && (q.Get("market") == "spot" || q.Get("market") == "linear")
}

func replayRange(q url.Values, now time.Time) (from, to, step int64, err error) {
	steps := map[string]int64{"1m": 60000, "5m": 300000, "15m": 900000, "1h": 3600000, "4h": 14400000}
	step = steps[q.Get("timeframe")]
	from, e1 := strconv.ParseInt(q.Get("from"), 10, 64)
	to, e2 := strconv.ParseInt(q.Get("to"), 10, 64)
	if !replayVenue(q) || !bt.ValidSymbol(q.Get("symbol")) || step == 0 || e1 != nil || e2 != nil || from < 0 || to <= from {
		return 0, 0, 0, fmt.Errorf("Некорректные параметры архива")
	}
	if to > now.UnixMilli() || to-from > 31*24*3600000 || (to-from)/step > 10000 || from%step != 0 || to%step != 0 {
		return 0, 0, 0, fmt.Errorf("Выберите закрытый период до 31 дня и не более 10 000 свечей; границы должны совпадать с интервалом")
	}
	return from, to, step, nil
}

func (a *API) replaySymbols(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !replayVenue(q) {
		respond(w, 422, map[string]string{"error": "Некорректная биржа или рынок"})
		return
	}
	// The primary key starts with exchange,market. Read metadata, never scan the candle archive.
	rows, err := a.store.Pool.Query(r.Context(), `SELECT symbol,first_available,last_available FROM candle_archive_series
	 WHERE exchange=$1 AND market=$2 AND first_available IS NOT NULL AND last_available IS NOT NULL ORDER BY symbol`, q.Get("exchange"), q.Get("market"))
	if err != nil {
		failure(w, err)
		return
	}
	defer rows.Close()
	type series struct {
		Symbol string `json:"symbol"`
		First  int64  `json:"first"`
		Last   int64  `json:"last"`
	}
	items := []series{}
	for rows.Next() {
		var item series
		if err := rows.Scan(&item.Symbol, &item.First, &item.Last); err != nil {
			failure(w, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, map[string]any{"symbols": items})
}

func (a *API) replayCandles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to, step, err := replayRange(q, time.Now().UTC())
	if err != nil {
		respond(w, 422, map[string]string{"error": err.Error()})
		return
	}
	if a.replaySlots != nil {
		select {
		case a.replaySlots <- struct{}{}:
			defer func() { <-a.replaySlots }()
		default:
			respond(w, 429, map[string]string{"error": "Архив занят. Повторите через несколько секунд"})
			return
		}
	}
	candles, err := a.replayArchive.ReadMinutes(r.Context(), q.Get("exchange"), q.Get("market"), q.Get("symbol"), q.Get("timeframe"), from, to)
	if err != nil {
		failure(w, err)
		return
	}
	if candles == nil {
		candles = []bt.Candle{}
	}
	respond(w, 200, map[string]any{"candles": candles, "missing": (to-from)/step - int64(len(candles)), "from": from, "to": to})
}
