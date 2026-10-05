package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	bt "shortlong/backtest"
	"time"
)

var ErrActive = errors.New("У вас уже есть запущенный тест")
var ErrCapacity = errors.New("Очередь заполнена. Попробуйте позже")
var ErrLeaseLost = errors.New("backtest lease lost or cancelled")

type Store struct{ Pool *pgxpool.Pool }
type Job struct {
	ID         string     `json:"id"`
	Request    bt.Request `json:"request"`
	Status     string     `json:"status"`
	Phase      string     `json:"phase"`
	Progress   int        `json:"progress"`
	Error      string     `json:"error,omitempty"`
	Result     *bt.Result `json:"result,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

const columns = `id::text, request, status, phase, progress, error, result, created_at, finished_at`

func scan(row pgx.Row) (Job, error) {
	var j Job
	var request, result []byte
	err := row.Scan(&j.ID, &request, &j.Status, &j.Phase, &j.Progress, &j.Error, &result, &j.CreatedAt, &j.FinishedAt)
	if err != nil {
		return j, err
	}
	if err = json.Unmarshal(request, &j.Request); err != nil {
		return j, err
	}
	if len(result) > 0 && string(result) != "null" {
		err = json.Unmarshal(result, &j.Result)
	}
	return j, err
}

func (s Store) Create(ctx context.Context, user, key string, r bt.Request) (string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	// Short global admission lock makes the capacity bound exact, even across APIs.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(92834711)`); err != nil {
		return "", err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT id::text FROM backtest_jobs WHERE user_id=$1 AND request_key=$2`, user, key).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM backtest_jobs WHERE status IN ('queued','running')`).Scan(&count); err != nil {
		return "", err
	}
	if count >= 1000 {
		return "", ErrCapacity
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO backtest_jobs(user_id,request_key,request) VALUES($1,$2,$3) RETURNING id::text`, user, key, raw).Scan(&id)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return "", ErrActive
		}
		return "", err
	}
	return id, tx.Commit(ctx)
}
func (s Store) Get(ctx context.Context, user, id string) (Job, error) {
	return scan(s.Pool.QueryRow(ctx, `SELECT `+columns+` FROM backtest_jobs WHERE id=$1 AND user_id=$2`, id, user))
}
func (s Store) List(ctx context.Context, user string) ([]Job, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id::text,request,status,phase,progress,error,NULL::jsonb,created_at,finished_at FROM backtest_jobs WHERE user_id=$1 ORDER BY created_at DESC LIMIT 30`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
func (s Store) Cancel(ctx context.Context, user, id string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE backtest_jobs SET status='cancelled',phase='cancelled',finished_at=now(),updated_at=now(),lease_token=NULL WHERE id=$1 AND user_id=$2 AND status IN ('queued','running')`, id, user)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		_, err = s.Get(ctx, user, id)
	}
	return err
}
func (s Store) Trades(ctx context.Context, user, id string, offset, limit int) ([]bt.Trade, error) {
	rows, err := s.Pool.Query(ctx, `SELECT t.trade FROM backtest_trades t JOIN backtest_jobs j ON j.id=t.job_id WHERE t.job_id=$1 AND j.user_id=$2 ORDER BY t.ordinal LIMIT $3 OFFSET $4`, id, user, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	trades := []bt.Trade{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var t bt.Trade
		if err = json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		trades = append(trades, t)
	}
	return trades, rows.Err()
}
func (s Store) Claim(ctx context.Context, id string) (bt.Request, string, error) {
	var raw []byte
	var token string
	err := s.Pool.QueryRow(ctx, `UPDATE backtest_jobs SET status='running',phase='loading',progress=1,attempts=attempts+1,lease_token=gen_random_uuid(),heartbeat_at=now(),updated_at=now() WHERE id=$1 AND status='queued' RETURNING request,lease_token::text`, id).Scan(&raw, &token)
	var r bt.Request
	if err != nil {
		return r, "", err
	}
	err = json.Unmarshal(raw, &r)
	return r, token, err
}
func (s Store) Heartbeat(ctx context.Context, id, token, phase string, progress int) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE backtest_jobs SET heartbeat_at=now(),updated_at=now(),phase=$3,progress=$4 WHERE id=$1 AND lease_token=$2 AND status='running'`, id, token, phase, progress)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}
func (s Store) Fail(ctx context.Context, id, token, message string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE backtest_jobs SET status='failed',phase='failed',error=$3,finished_at=now(),updated_at=now(),lease_token=NULL WHERE id=$1 AND lease_token=$2 AND status='running'`, id, token, message)
	return err
}
func (s Store) Complete(ctx context.Context, id, token string, result *bt.Result) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	summary := *result
	summary.Trades = nil
	raw, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE backtest_jobs SET status='completed',phase='completed',progress=100,result=$3,finished_at=now(),updated_at=now(),lease_token=NULL WHERE id=$1 AND lease_token=$2 AND status='running'`, id, token, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	rows := make([][]any, len(result.Trades))
	for i, t := range result.Trades {
		raw, err := json.Marshal(t)
		if err != nil {
			return err
		}
		rows[i] = []any{id, i, raw}
	}
	if len(rows) > 0 {
		if _, err = tx.CopyFrom(ctx, pgx.Identifier{"backtest_trades"}, []string{"job_id", "ordinal", "trade"}, pgx.CopyFromRows(rows)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
