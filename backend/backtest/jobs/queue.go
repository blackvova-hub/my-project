package jobs

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"strings"
	"time"
)

const Stream = "backtest:jobs:v1"
const Group = "backtest-workers-v1"

type Queue struct {
	Store Store
	Redis *redis.Client
}

func (q Queue) Init(ctx context.Context) error {
	err := q.Redis.XGroupCreateMkStream(ctx, Stream, Group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

// PostgreSQL queued rows are a durable enqueue outbox. Redis downtime or a
// crash between XADD and commit can at worst duplicate a delivery, never lose it.
func (q Queue) Dispatch(ctx context.Context) error {
	_, err := q.Store.Pool.Exec(ctx, `UPDATE backtest_jobs SET status=CASE WHEN attempts>=3 THEN 'failed' ELSE 'queued' END,phase=CASE WHEN attempts>=3 THEN 'failed' ELSE 'queued' END,error=CASE WHEN attempts>=3 THEN 'Расчёт прерывался несколько раз. Запустите тест повторно.' ELSE '' END,finished_at=CASE WHEN attempts>=3 THEN now() ELSE NULL END,lease_token=NULL,enqueued_at=NULL,updated_at=now(),progress=0 WHERE status='running' AND heartbeat_at<now()-interval '90 seconds'`)
	if err != nil {
		return err
	}
	tx, err := q.Store.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id::text FROM backtest_jobs WHERE status='queued' AND (enqueued_at IS NULL OR enqueued_at<now()-interval '5 minutes') ORDER BY created_at LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err = q.Redis.XAdd(ctx, &redis.XAddArgs{Stream: Stream, Values: map[string]any{"jobId": id}}).Err(); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE backtest_jobs SET enqueued_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (q Queue) Next(ctx context.Context, consumer string) (redis.XMessage, error) {
	// Reclaim pending deliveries left by a crashed consumer. SQL leases fence the
	// computation independently, so reclaiming a live delivery cannot run it twice.
	messages, _, err := q.Redis.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: Stream, Group: Group, Consumer: consumer, MinIdle: 2 * time.Minute, Start: "0-0", Count: 1}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return redis.XMessage{}, err
	}
	if len(messages) > 0 {
		return messages[0], nil
	}
	streams, err := q.Redis.XReadGroup(ctx, &redis.XReadGroupArgs{Group: Group, Consumer: consumer, Streams: []string{Stream, ">"}, Count: 1, Block: time.Second}).Result()
	if err != nil {
		return redis.XMessage{}, err
	}
	if len(streams) == 0 || len(streams[0].Messages) == 0 {
		return redis.XMessage{}, redis.Nil
	}
	return streams[0].Messages[0], nil
}
func (q Queue) Ack(ctx context.Context, id string) error {
	if err := q.Redis.XAck(ctx, Stream, Group, id).Err(); err != nil {
		return err
	}
	return q.Redis.XDel(ctx, Stream, id).Err()
}
func IsUnclaimable(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
