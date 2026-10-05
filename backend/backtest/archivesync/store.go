package archivesync

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	bt "shortlong/backtest"
	"shortlong/backtest/marketdata"
	"time"
)

const Step int64 = 60000
const Batch int64 = 1000 * Step

type Store struct{ Pool *pgxpool.Pool }
type Task struct {
	Exchange, Market, Symbol, Kind, Token string
	From, To                              int64
}

func ClosedEnd(now time.Time) int64 {
	return now.Add(-10 * time.Second).UTC().Truncate(time.Minute).UnixMilli()
}

func (s Store) Register(ctx context.Context, exchange, market string, instruments []marketdata.Instrument, from, to int64) error {
	if len(instruments) == 0 {
		return fmt.Errorf("refusing empty catalog")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE candle_archive_series SET active=false WHERE exchange=$1 AND market=$2`, exchange, market); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, i := range instruments {
		start := max(from, (i.LaunchTime+Step-1)/Step*Step)
		if start >= to {
			continue
		}
		batch.Queue(`INSERT INTO candle_archive_series(exchange,market,symbol,history_from,history_to,backfill_to,live_to)
   VALUES($1,$2,$3,$4,$5,$5,$5) ON CONFLICT(exchange,market,symbol) DO UPDATE SET active=true,
   history_from=LEAST(candle_archive_series.history_from,EXCLUDED.history_from)`, exchange, market, i.Symbol, start, to)
	}
	if err = tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) Claim(ctx context.Context, exchange, market string, end int64) (*Task, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	t := &Task{Exchange: exchange, Market: market}
	var start, back, live int64
	err = tx.QueryRow(ctx, `SELECT symbol,history_from,backfill_to,live_to FROM candle_archive_series s
 WHERE exchange=$1 AND market=$2 AND active AND lease_until<now() AND retry_at<=now() AND
 (live_to<$3 OR backfill_to>history_from OR EXISTS(SELECT 1 FROM candle_archive_gaps g
 WHERE (g.exchange,g.market,g.symbol)=(s.exchange,s.market,s.symbol) AND g.retry_at<=now()))
 ORDER BY CASE WHEN live_to<$3 THEN 0 WHEN backfill_to>history_from THEN 1 ELSE 2 END,updated_at,symbol
 LIMIT 1 FOR UPDATE SKIP LOCKED`, exchange, market, end).Scan(&t.Symbol, &start, &back, &live)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	switch {
	case live < end:
		t.Kind = "live"
		t.From = live
		t.To = min(end, live+Batch)
	case back > start:
		t.Kind = "backfill"
		t.From = max(start, back-Batch)
		t.To = back
	default:
		t.Kind = "repair"
		err = tx.QueryRow(ctx, `SELECT range_from,range_to FROM candle_archive_gaps WHERE exchange=$1 AND market=$2 AND symbol=$3 AND retry_at<=now() ORDER BY retry_at LIMIT 1`, exchange, market, t.Symbol).Scan(&t.From, &t.To)
		if err != nil {
			return nil, err
		}
	}
	err = tx.QueryRow(ctx, `UPDATE candle_archive_series SET lease_token=gen_random_uuid(),lease_until=now()+interval '3 minutes' WHERE exchange=$1 AND market=$2 AND symbol=$3 RETURNING lease_token::text`, exchange, market, t.Symbol).Scan(&t.Token)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return t, nil
}

func (s Store) Finish(ctx context.Context, t Task, candles []bt.Candle) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var first, last int64
	if len(candles) > 0 {
		first = candles[0].Time
		last = candles[len(candles)-1].Time
	}
	tag, err := tx.Exec(ctx, `UPDATE candle_archive_series SET
 backfill_to=CASE WHEN $5='backfill' THEN $6 ELSE backfill_to END,
 live_to=CASE WHEN $5='live' THEN $7 ELSE live_to END,
 first_available=LEAST(first_available,NULLIF($8::bigint,0)),last_available=GREATEST(last_available,NULLIF($9::bigint,0)),
 written_rows=written_rows+$10,lease_token=NULL,lease_until='-infinity',retry_at='-infinity',failures=0,last_error='',updated_at=now()
 WHERE exchange=$1 AND market=$2 AND symbol=$3 AND lease_token=$4::uuid`, t.Exchange, t.Market, t.Symbol, t.Token, t.Kind, t.From, t.To, first, last, len(candles))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("archive lease lost")
	}
	missing := int((t.To-t.From)/Step) - len(candles)
	if missing > 0 {
		_, err = tx.Exec(ctx, `INSERT INTO candle_archive_gaps(exchange,market,symbol,range_from,range_to,missing_candles) VALUES($1,$2,$3,$4,$5,$6)
  ON CONFLICT(exchange,market,symbol,range_from,range_to) DO UPDATE SET missing_candles=EXCLUDED.missing_candles,retry_at=now()+interval '1 day'`, t.Exchange, t.Market, t.Symbol, t.From, t.To, missing)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM candle_archive_gaps WHERE exchange=$1 AND market=$2 AND symbol=$3 AND range_from=$4 AND range_to=$5`, t.Exchange, t.Market, t.Symbol, t.From, t.To)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) Release(ctx context.Context, t Task, cause error) error {
	message := ""
	delay := 0
	if cause != nil {
		message = cause.Error()
		delay = 60
	}
	_, err := s.Pool.Exec(ctx, `UPDATE candle_archive_series SET lease_token=NULL,lease_until='-infinity',
 last_error=$5,failures=CASE WHEN $5='' THEN failures ELSE failures+1 END,retry_at=now()+make_interval(secs=>$6),updated_at=now()
 WHERE exchange=$1 AND market=$2 AND symbol=$3 AND lease_token=$4::uuid`, t.Exchange, t.Market, t.Symbol, t.Token, message, delay)
	return err
}

type Status struct {
	Exchange  string `json:"exchange"`
	Market    string `json:"market"`
	Timeframe string `json:"timeframe"`
	Series    int    `json:"series"`
	Scanned   int    `json:"historyScanned"`
	Pending   int64  `json:"historyBarsToCheck"`
	Written   int64  `json:"processedRows"`
	Errors    int    `json:"errors"`
	Lag       int64  `json:"maxLiveLagSeconds"`
	Missing   int64  `json:"sourceMissingBars"`
}

func (s Store) Status(ctx context.Context) ([]Status, error) {
	rows, err := s.Pool.Query(ctx, `SELECT exchange,market,count(*),count(*) FILTER(WHERE backfill_to=history_from),
 COALESCE(sum((backfill_to-history_from)/60000),0),sum(written_rows),count(*) FILTER(WHERE last_error<>''),
 GREATEST(0,($1-min(live_to))/1000),
 COALESCE((SELECT sum(missing_candles) FROM candle_archive_gaps g WHERE g.exchange=s.exchange AND g.market=s.market),0)
 FROM candle_archive_series s WHERE active GROUP BY exchange,market ORDER BY exchange,market`, ClosedEnd(time.Now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Status{}
	for rows.Next() {
		st := Status{Timeframe: "1m"}
		if err = rows.Scan(&st.Exchange, &st.Market, &st.Series, &st.Scanned, &st.Pending, &st.Written, &st.Errors, &st.Lag, &st.Missing); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
