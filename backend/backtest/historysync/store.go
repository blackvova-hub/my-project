package historysync

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	bt "shortlong/backtest"
	"shortlong/backtest/marketdata"
)

const Step int64 = 300000
const Batch int64 = 1000 * Step

type Store struct{ Pool *pgxpool.Pool }
type Task struct {
	Market, Symbol, Kind, Token string
	From, To                    int64
}

// Calendar months, clamped to the destination month's last day, at UTC midnight.
func HistoryStart(now time.Time, months int) int64 {
	now = now.UTC()
	month := time.Date(now.Year(), now.Month()-time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	last := month.AddDate(0, 1, -1).Day()
	day := now.Day()
	if day > last {
		day = last
	}
	return month.AddDate(0, 0, day-1).UnixMilli()
}

// Ten seconds for the exchange to publish the most recently closed bar.
func ClosedEnd(now time.Time) int64 {
	return now.Add(-10 * time.Second).UTC().Truncate(5 * time.Minute).UnixMilli()
}

// A fixed UTC boundary survives restarts without triggering another live scan.
// Wait ten seconds after the boundary for Bybit to publish the final candle.
func RefreshEnd(now time.Time, interval time.Duration) int64 {
	return now.Add(-10 * time.Second).UTC().Truncate(interval).UnixMilli()
}

func (s Store) Register(ctx context.Context, market string, instruments []marketdata.Instrument, from, to int64) error {
	if len(instruments) == 0 {
		return fmt.Errorf("Refusing empty catalog")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE market_history_series SET active=false WHERE market=$1`, market); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, i := range instruments {
		start := from
		if launch := i.LaunchTime / Step * Step; launch > start {
			start = launch
		}
		if start >= to {
			continue
		}
		batch.Queue(`INSERT INTO market_history_series(market,symbol,history_from,history_to,backfill_to,live_to,launch_time)
   VALUES($1,$2,$3,$4,$4,$4,$5)
   ON CONFLICT(market,symbol,timeframe) DO UPDATE SET active=true,
   history_from=LEAST(market_history_series.history_from,EXCLUDED.history_from),
   launch_time=EXCLUDED.launch_time,catalog_at=now()`, market, i.Symbol, start, to, i.LaunchTime)
	}
	if err = tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// A short SQL claim transaction; the HTTP work never holds row locks.
// Expiring leases plus fencing allow safe restarts and multiple replicas.
func (s Store) Claim(ctx context.Context, end int64) (*Task, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	t := &Task{}
	var from, backfill, live int64
	err = tx.QueryRow(ctx, `SELECT market,symbol,history_from,backfill_to,live_to FROM market_history_series s
  WHERE active AND lease_until<now() AND retry_at<=now() AND
  (live_to<$1 OR backfill_to>history_from OR EXISTS(SELECT 1 FROM market_history_gaps g WHERE
   (g.market,g.symbol,g.timeframe)=(s.market,s.symbol,s.timeframe) AND g.retry_at<=now()))
  ORDER BY CASE WHEN live_to<$1 THEN 0 WHEN backfill_to>history_from THEN 1 ELSE 2 END,updated_at,market,symbol
  LIMIT 1 FOR UPDATE SKIP LOCKED`, end).Scan(&t.Market, &t.Symbol, &from, &backfill, &live)
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
	case backfill > from:
		t.Kind = "backfill"
		t.To = backfill
		t.From = max(from, backfill-Batch)
	default:
		t.Kind = "repair"
		err = tx.QueryRow(ctx, `SELECT range_from,range_to FROM market_history_gaps WHERE market=$1 AND symbol=$2 AND retry_at<=now() ORDER BY retry_at LIMIT 1`, t.Market, t.Symbol).Scan(&t.From, &t.To)
		if err != nil {
			return nil, err
		}
	}
	err = tx.QueryRow(ctx, `UPDATE market_history_series SET lease_token=gen_random_uuid(),lease_until=now()+interval '3 minutes'
  WHERE market=$1 AND symbol=$2 RETURNING lease_token::text`, t.Market, t.Symbol).Scan(&t.Token)
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
	tag, err := tx.Exec(ctx, `UPDATE market_history_series SET
  backfill_to=CASE WHEN $4='backfill' THEN $5 ELSE backfill_to END,
  live_to=CASE WHEN $4='live' THEN $6 ELSE live_to END,
  first_available=LEAST(first_available,NULLIF($7::bigint,0)),last_available=GREATEST(last_available,NULLIF($8::bigint,0)),
  lease_token=NULL,lease_until='-infinity',retry_at='-infinity',failures=0,last_error='',updated_at=now()
  WHERE market=$1 AND symbol=$2 AND lease_token=$3::uuid`, t.Market, t.Symbol, t.Token, t.Kind, t.From, t.To, first, last)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("History lease lost")
	}
	missing := int((t.To-t.From)/Step) - len(candles)
	if missing > 0 {
		_, err = tx.Exec(ctx, `INSERT INTO market_history_gaps(market,symbol,range_from,range_to,missing_candles)
   VALUES($1,$2,$3,$4,$5) ON CONFLICT(market,symbol,timeframe,range_from,range_to)
   DO UPDATE SET missing_candles=EXCLUDED.missing_candles,checked_at=now(),retry_at=now()+interval '1 day'`, t.Market, t.Symbol, t.From, t.To, missing)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM market_history_gaps WHERE market=$1 AND symbol=$2 AND range_from=$3 AND range_to=$4`, t.Market, t.Symbol, t.From, t.To)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) Fail(ctx context.Context, t Task, cause error) error {
	_, err := s.Pool.Exec(ctx, `UPDATE market_history_series SET lease_token=NULL,lease_until='-infinity',
  failures=failures+1,last_error=$4,retry_at=now()+make_interval(secs=>LEAST(600,15*power(2,LEAST(failures,6)))::int),updated_at=now()
  WHERE market=$1 AND symbol=$2 AND lease_token=$3::uuid`, t.Market, t.Symbol, t.Token, cause.Error())
	return err
}

// A deliberate shutdown is not a provider failure and needs no error backoff.
func (s Store) Release(ctx context.Context, t Task) error {
	_, err := s.Pool.Exec(ctx, `UPDATE market_history_series SET lease_token=NULL,lease_until='-infinity'
 WHERE market=$1 AND symbol=$2 AND lease_token=$3::uuid`, t.Market, t.Symbol, t.Token)
	return err
}

type Status struct {
	Market              string  `json:"market"`
	Series              int     `json:"series"`
	Scanned             int     `json:"historyScanned"`
	PendingBars         int64   `json:"historyBarsToCheck"`
	CheckedPct          float64 `json:"historyCheckedPct"`
	LiveLagSeconds      int64   `json:"maxLiveLagSeconds"`
	Errors              int     `json:"errors"`
	MissingBars         int64   `json:"sourceMissingBars"`
	RefreshHours        int     `json:"refreshHours"`
	RefreshThrough      int64   `json:"refreshThrough"`
	NextRefreshAt       int64   `json:"nextRefreshAt"`
	ScheduledLagSeconds int64   `json:"maxScheduledLagSeconds"`
}

func (s Store) Status(ctx context.Context, end int64) ([]Status, error) {
	rows, err := s.Pool.Query(ctx, `SELECT market,count(*),count(*) FILTER(WHERE backfill_to=history_from),
  sum((backfill_to-history_from)/300000),
  round(100*(1-sum(backfill_to-history_from)::numeric/NULLIF(sum(history_to-history_from),0)),2)::float8,
  GREATEST(0,($1-min(live_to))/1000),count(*) FILTER(WHERE last_error<>''),
  COALESCE((SELECT sum(missing_candles) FROM market_history_gaps g JOIN market_history_series x USING(market,symbol,timeframe) WHERE g.market=s.market AND x.active),0)
  FROM market_history_series s WHERE active GROUP BY market ORDER BY market`, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Status{}
	for rows.Next() {
		var st Status
		if err = rows.Scan(&st.Market, &st.Series, &st.Scanned, &st.PendingBars, &st.CheckedPct, &st.LiveLagSeconds, &st.Errors, &st.MissingBars); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}
