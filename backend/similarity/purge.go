package similarity

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"log"
	"regexp"
	"shortlong/backtest/marketdata"
)

// Explicit maintenance command. Never runs on startup or as part of a search.
// Deletes derived similarity data only; source candles and asset metadata stay intact.
func PurgeDerived(ctx context.Context, s Store, q *Qdrant, ch *marketdata.ClickHouse, cache *redis.Client) error {
	var active int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM similarity_jobs WHERE status IN ('queued','running')`).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return fmt.Errorf("cancel active similarity jobs before purging")
	}
	var collections struct {
		Result struct {
			Collections []struct {
				Name string `json:"name"`
			} `json:"collections"`
		} `json:"result"`
	}
	if err := q.call(ctx, "GET", "/collections", nil, &collections); err != nil {
		return err
	}
	owned := regexp.MustCompile(`^similarity_ohlcv_v[0-9]+_[0-9a-f]{12}_[0-9]+$`)
	o := Outcomes{ch}
	before, err := o.query(ctx, `SELECT count() FROM shortlong_backtest.candles`, nil, nil)
	if err != nil {
		return err
	}
	for _, c := range collections.Result.Collections {
		if !owned.MatchString(c.Name) {
			continue
		}
		if err = q.call(ctx, "DELETE", "/collections/"+c.Name, nil, nil); err != nil {
			return err
		}
		log.Printf("deleted derived collection %s", c.Name)
	}
	if _, err = o.query(ctx, `DROP TABLE IF EXISTS shortlong_backtest.similarity_outcomes_v1 SYNC`, nil, nil); err != nil {
		return err
	}
	if _, err = s.DB.Exec(ctx, `TRUNCATE similarity_jobs,similarity_repairs,similarity_streams RESTART IDENTITY`); err != nil {
		return err
	}
	var cursor uint64
	for {
		keys, next, err := cache.Scan(ctx, cursor, "similarity:search:*", 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err = cache.Unlink(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	after, err := o.query(ctx, `SELECT count() FROM shortlong_backtest.candles`, nil, nil)
	if err != nil {
		return err
	}
	log.Printf("purge complete: source candle rows before=%s after=%s", before, after)
	return nil
}
