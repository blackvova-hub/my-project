package service

import (
	"context"
	"github.com/redis/go-redis/v9"
	"shortlong/arbitrage/internal/collector"
	"shortlong/arbitrage/internal/engine"
	"shortlong/arbitrage/internal/market"
	"time"
)

func (s *Service) queueDepth(ctx context.Context, rows []engine.Opportunity) {
	if s.Redis == nil {
		return
	}
	op, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	pipe := s.Redis.Pipeline()
	seen := map[string]bool{}
	now := float64(time.Now().UnixMilli())
	for _, o := range rows[:min(25, len(rows))] {
		for _, leg := range []engine.Leg{o.Buy, o.Sell} {
			i := leg.Instrument
			if !market.KnownExchange(i.Exchange) || seen[i.ID] {
				continue
			}
			seen[i.ID] = true
			key := collector.Prefix + "depth-demand:" + i.Exchange
			pipe.ZAdd(op, key, redis.Z{Member: i.ID, Score: now})
			pipe.Expire(op, key, time.Minute)
		}
	}
	_, _ = pipe.Exec(op)
}
