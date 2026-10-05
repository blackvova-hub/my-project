package similarity

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
)

type Store struct {
	DB     *pgxpool.Pool
	Config Config
}
type Asset struct {
	Market  string   `json:"market"`
	Symbol  string   `json:"symbol"`
	Sectors []string `json:"sectors"`
	Alt     bool     `json:"isAlt"`
	Stable  bool     `json:"isStablecoin"`
	Enabled bool     `json:"enabled"`
}

func (s Store) SyncAssets(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO similarity_assets(market,symbol,is_alt,is_stablecoin) SELECT market,symbol,symbol<>'BTCUSDT',symbol IN ('USDCUSDT','USDEUSDT','DAIUSDT','FDUSDUSDT','TUSDUSDT','USDDUSDT','USD1USDT','PYUSDUSDT','RLUSDUSDT','USDPUSDT') FROM `+historySource()+` WHERE active ON CONFLICT DO NOTHING`)
	return err
}

// Similarity currently supports Bybit; preserve its 5m analysis windows while
// reading only complete 5m aggregates from the canonical minute archive.
func historySource() string {
	if os.Getenv("CANDLE_ARCHIVE_MODE") == "1m" {
		return `(SELECT market,symbol,active,((first_available+299999)/300000)*300000 AS first_available,
		((last_available+60000)/300000)*300000-300000 AS last_available FROM candle_archive_series WHERE exchange='bybit')`
	}
	return "market_history_series"
}

// Only explicit searches extend coverage, for one market/window and allowed assets.
func (s Store) Discover(ctx context.Context, r Request, allowed []string) error {
	w, ok := s.Config.Window(r.Window)
	if !ok {
		return fmt.Errorf("unsupported window")
	}
	rows, err := s.DB.Query(ctx, `SELECT h.symbol,h.first_available,h.last_available FROM `+historySource()+` h JOIN similarity_assets a USING(market,symbol) WHERE h.active AND a.enabled AND h.market=$1 AND h.symbol=ANY($2::text[]) AND h.first_available IS NOT NULL AND h.last_available IS NOT NULL`, r.Market, allowed)
	if err != nil {
		return err
	}
	batch := &pgx.Batch{}
	step := int64(w.Stride) * Step
	for rows.Next() {
		var symbol string
		var first, last int64
		if err = rows.Scan(&symbol, &first, &last); err != nil {
			rows.Close()
			return err
		}
		from := (first + int64(2*w.Bars)*Step + step - 1) / step * step
		lastEnd := min(last+Step, r.End-int64(r.Window)*Step)
		to := lastEnd/step*step + step
		if from >= to {
			continue
		}
		batch.Queue(`INSERT INTO similarity_streams(version,market,symbol,kind,span,stride,target_from,target_to,scanned_from,scanned_to) VALUES($1,$2,$3,'features',$4,$5,$6,$7,$7,$7) ON CONFLICT(version,market,symbol,kind,span,stride) DO UPDATE SET target_from=LEAST(similarity_streams.target_from,EXCLUDED.target_from),target_to=GREATEST(similarity_streams.target_to,EXCLUDED.target_to)`, s.Config.Version, r.Market, symbol, w.Bars, w.Stride, from, to)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	return s.DB.SendBatch(ctx, batch).Close()
}
func (s Store) Pending(ctx context.Context, r Request, allowed []string) (int64, error) {
	var n int64
	err := s.DB.QueryRow(ctx, `SELECT COALESCE(sum(GREATEST(scanned_from-target_from,0)+GREATEST(target_to-scanned_to,0)),0)::bigint FROM similarity_streams WHERE version=$1 AND market=$2 AND span=$3 AND symbol=ANY($4::text[]) AND kind='features'`, s.Config.Version, r.Market, r.Window, allowed).Scan(&n)
	return n, err
}

type Task struct {
	ID                                     int64
	Token, Market, Symbol, Kind, Direction string
	Span, Stride                           int
	From, To                               int64
}

func (s Store) Claim(ctx context.Context, r Request, allowed []string) (*Task, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	t := &Task{}
	var from, to, left, right int64
	err = tx.QueryRow(ctx, `SELECT s.id,s.market,s.symbol,s.kind,s.span,s.stride,s.target_from,s.target_to,s.scanned_from,s.scanned_to FROM similarity_streams s JOIN similarity_assets a USING(market,symbol) WHERE s.version=$1 AND s.market=$2 AND s.span=$3 AND s.symbol=ANY($4::text[]) AND s.kind='features' AND a.enabled AND s.lease_until<now() AND s.retry_at<=now() AND (s.target_from<s.scanned_from OR s.target_to>s.scanned_to OR EXISTS(SELECT 1 FROM similarity_repairs r WHERE r.stream_id=s.id AND r.retry_at<=now())) ORDER BY s.updated_at,s.id LIMIT 1 FOR UPDATE OF s SKIP LOCKED`, s.Config.Version, r.Market, r.Window, allowed).Scan(&t.ID, &t.Market, &t.Symbol, &t.Kind, &t.Span, &t.Stride, &from, &to, &left, &right)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	chunk := int64(7*288) * Step
	switch {
	case right < to:
		t.Direction = "forward"
		t.From = right
		t.To = min(to, right+chunk)
	case from < left:
		t.Direction = "backward"
		t.From = max(from, left-chunk)
		t.To = left
	default:
		t.Direction = "repair"
		err = tx.QueryRow(ctx, `SELECT range_from,range_to FROM similarity_repairs WHERE stream_id=$1 AND retry_at<=now() ORDER BY retry_at LIMIT 1`, t.ID).Scan(&t.From, &t.To)
		if err != nil {
			return nil, err
		}
	}
	err = tx.QueryRow(ctx, `UPDATE similarity_streams SET lease_token=gen_random_uuid(),lease_until=now()+interval '3 minutes' WHERE id=$1 RETURNING lease_token::text`, t.ID).Scan(&t.Token)
	if err != nil {
		return nil, err
	}
	return t, tx.Commit(ctx)
}
func (s Store) Finish(ctx context.Context, t Task, missing bool) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE similarity_streams SET scanned_from=CASE WHEN $3='backward' THEN $4 ELSE scanned_from END,scanned_to=CASE WHEN $3='forward' THEN $5 ELSE scanned_to END,lease_until='-infinity',lease_token=NULL,last_error='',retry_at='-infinity',updated_at=now() WHERE id=$1 AND lease_token=$2::uuid`, t.ID, t.Token, t.Direction, t.From, t.To)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("similarity lease lost")
	}
	if missing {
		_, err = tx.Exec(ctx, `INSERT INTO similarity_repairs(stream_id,range_from,range_to) VALUES($1,$2,$3) ON CONFLICT(stream_id,range_from,range_to) DO UPDATE SET retry_at=now()+interval '1 hour'`, t.ID, t.From, t.To)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM similarity_repairs WHERE stream_id=$1 AND range_from=$2 AND range_to=$3`, t.ID, t.From, t.To)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s Store) Release(ctx context.Context, t Task, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	_, err := s.DB.Exec(ctx, `UPDATE similarity_streams SET lease_token=NULL,lease_until='-infinity',last_error=$3,retry_at=CASE WHEN $3='' THEN now() ELSE now()+interval '1 minute' END,updated_at=now() WHERE id=$1 AND lease_token=$2::uuid`, t.ID, t.Token, msg)
	return err
}
func (s Store) Assets(ctx context.Context) ([]Asset, error) {
	rows, e := s.DB.Query(ctx, `SELECT market,symbol,sectors,is_alt,is_stablecoin,enabled FROM similarity_assets ORDER BY market,symbol`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Asset{}
	for rows.Next() {
		var a Asset
		if e = rows.Scan(&a.Market, &a.Symbol, &a.Sectors, &a.Alt, &a.Stable, &a.Enabled); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s Store) Status(ctx context.Context) (json.RawMessage, error) {
	var raw []byte
	e := s.DB.QueryRow(ctx, `SELECT COALESCE(json_agg(x),'[]') FROM (SELECT market,kind,count(*) AS streams,sum(GREATEST(scanned_from-target_from,0)/300000+GREATEST(target_to-scanned_to,0)/300000) AS pending_bars,count(*) FILTER(WHERE last_error<>'') AS errors FROM similarity_streams WHERE version=$1 GROUP BY market,kind ORDER BY market,kind) x`, s.Config.Version).Scan(&raw)
	return raw, e
}
