package main

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestCompactStreamBatchCheckpointAndCapacityDLQIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)

	t.Run("full reply registration and duplicate advance contiguous checkpoint", func(t *testing.T) {
		stream, group, consumer := "codex:test:compact-live:"+suffix+":batch", "group-"+suffix+"-batch", "consumer"
		defer client.Del(ctx, stream, streamDLQName(stream))
		raw := compactLivePayload(t, time.Now().UTC().Truncate(time.Minute), []string{"BTCUSDT"})
		ids := addCompactPendingBatch(t, ctx, client, stream, group, consumer, raw, raw)
		history := newSnapshotTestHistory()
		if err := history.SetStreamBootstrapCheckpoint(stream, compactStreamHighWater{MessageID: "0-0", ObservedAt: time.Now().UTC(), ReadyAt: time.Now().UTC(), Bootstrapped: true}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		engine := compactLiveTestEngine(client, group, history, 10)
		if err := engine.processCompactStreamBatch(ctx, nil, stream, readPendingForConsumer(t, ctx, client, stream, group, consumer), false, consumer, newCompactBatchDeduper(time.Minute, 100)); err != nil {
			t.Fatal(err)
		}
		pending, err := client.XPending(ctx, stream, group).Result()
		if err != nil || pending.Count != 0 {
			t.Fatalf("pending=%+v err=%v", pending, err)
		}
		water, exists := history.StreamHighWater(stream)
		if !exists || water.MessageID != ids[1] {
			t.Fatalf("checkpoint=%+v exists=%v", water, exists)
		}
	})

	t.Run("capacity stays pending then exhausts to durable skip", func(t *testing.T) {
		stream, group, consumer := "codex:test:compact-live:"+suffix+":capacity", "group-"+suffix+"-capacity", "consumer"
		defer client.Del(ctx, stream, streamDLQName(stream))
		raw := compactLivePayload(t, time.Now().UTC().Truncate(time.Minute), []string{"BTCUSDT", "ETHUSDT"})
		ids := addCompactPendingBatch(t, ctx, client, stream, group, consumer, raw)
		cache := NewRulesCache()
		cache.Set(map[string][]Rule{"bybit:perpetual:*": {{ID: 1, Exchange: "bybit", MarketType: "perpetual", Symbol: "*", WindowMinutes: 1, Conditions: []Condition{{Indicator: "price"}}}}})
		history := newSparseEngineState(cache, false, 1)
		now := time.Now().UTC()
		if err := history.SetStreamBootstrapCheckpoint(stream, compactStreamHighWater{MessageID: "0-0", ObservedAt: now, ReadyAt: now, Bootstrapped: true}, now); err != nil {
			t.Fatal(err)
		}
		engine := compactLiveTestEngine(client, group, history, 1)
		messages := readPendingForConsumer(t, ctx, client, stream, group, consumer)
		deduper := newCompactBatchDeduper(time.Minute, 100)
		if err := engine.processCompactStreamBatch(ctx, nil, stream, messages, false, consumer, deduper); err != nil {
			t.Fatal(err)
		}
		pending, _ := client.XPending(ctx, stream, group).Result()
		if pending.Count != 1 {
			t.Fatalf("capacity message was not retained pending: %+v", pending)
		}
		if err := engine.processCompactStreamBatch(ctx, nil, stream, messages, true, consumer, deduper); err != nil {
			t.Fatal(err)
		}
		pending, _ = client.XPending(ctx, stream, group).Result()
		if pending.Count != 0 {
			t.Fatalf("exhausted capacity message remains pending: %+v", pending)
		}
		water, exists := history.StreamHighWater(stream)
		if !exists || water.MessageID != ids[0] || water.SkipDigest == "" || water.Skipped != 1 {
			t.Fatalf("capacity skip checkpoint=%+v exists=%v", water, exists)
		}
	})

	t.Run("dependency change remains pending and is never recorded as a skip", func(t *testing.T) {
		stream, group, consumer := "codex:test:compact-live:"+suffix+":rules", "group-"+suffix+"-rules", "consumer"
		defer client.Del(ctx, stream, streamDLQName(stream))
		raw := compactLivePayload(t, time.Now().UTC().Truncate(time.Minute), []string{"BTCUSDT"})
		addCompactPendingBatch(t, ctx, client, stream, group, consumer, raw)
		history := newSnapshotTestHistory()
		now := time.Now().UTC()
		if err := history.SetStreamBootstrapCheckpoint(stream, compactStreamHighWater{MessageID: "0-0", ObservedAt: now, ReadyAt: now, Bootstrapped: true}, now); err != nil {
			t.Fatal(err)
		}
		fingerprint := history.currentDependencyFingerprint()
		if err := history.setBootstrapDependencyFingerprint(fingerprint); err != nil {
			t.Fatal(err)
		}
		history.rules.Set(map[string][]Rule{"bybit:perpetual:*": {{
			ID: 8, Exchange: "bybit", MarketType: "perpetual", Symbol: "*", WindowMinutes: 2,
			Conditions: []Condition{{Indicator: "openInterest"}},
		}}})
		engine := compactLiveTestEngine(client, group, history, 1)
		err := engine.processCompactStreamBatch(ctx, nil, stream, readPendingForConsumer(t, ctx, client, stream, group, consumer), false, consumer, newCompactBatchDeduper(time.Minute, 100))
		if err != nil {
			t.Fatalf("process changed rules: %v", err)
		}
		pending, pendingErr := client.XPending(ctx, stream, group).Result()
		if pendingErr != nil || pending.Count != 1 {
			t.Fatalf("pending=%+v err=%v", pending, pendingErr)
		}
		if length, lengthErr := client.XLen(ctx, streamDLQName(stream)).Result(); lengthErr != nil || length != 0 {
			t.Fatalf("dlq length=%d err=%v", length, lengthErr)
		}
		water, exists := history.StreamHighWater(stream)
		if !exists || water.MessageID != "0-0" || water.SkipDigest != "" || water.Skipped != 0 {
			t.Fatalf("checkpoint=%+v exists=%v", water, exists)
		}
	})
}

func compactLiveTestEngine(client *redis.Client, group string, history *sparseEngineState, maxDeliveries int) *Engine {
	return &Engine{
		cfg: Config{MarketConsumerGroup: group,
			PendingMinIdleSeconds: 60, StreamMaxDeliveries: maxDeliveries, StreamDLQMaxLen: 10,
			StreamDLQRawMaxBytes: 4096, StreamDLQErrorMaxBytes: 1024},
		rdb: client, unifiedHistory: history, compactOwner: &fakeCompactBootstrapOwner{},
	}
}

func compactLivePayload(t *testing.T, at time.Time, symbols []string) string {
	t.Helper()
	catalog, version := compactCatalog(symbols)
	rows := make([]compactWireCandleRow, 0, len(symbols))
	for _, symbol := range symbols {
		rows = append(rows, compactWireCandleRow{Symbol: symbol, Values: make([]float64, len(compactMetricLayout))})
	}
	wire := compactWireBatch{TransportVersion: compactTransportVersion, CanonicalVersion: compactCanonicalVersion,
		Kind: "c", Exchange: "bybit", MarketType: "perpetual", Minute: at.UnixMilli(), ShardTotal: 1,
		CatalogVersion: version, Catalog: catalog, Coverage: true, LayoutVersion: compactMetricLayoutV1,
		LayoutHash: compactMetricLayoutHash, CandleRows: rows}
	wire.BatchID = compactBatchID(wire)
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func addCompactPendingBatch(t *testing.T, ctx context.Context, client *redis.Client, stream, group, consumer string, payloads ...string) []string {
	t.Helper()
	ids := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"json": payload}}).Result()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := client.XGroupCreate(ctx, stream, group, "0-0").Err(); err != nil {
		t.Fatal(err)
	}
	readPendingForConsumer(t, ctx, client, stream, group, consumer)
	return ids
}

func readPendingForConsumer(t *testing.T, ctx context.Context, client *redis.Client, stream, group, consumer string) []redis.XMessage {
	t.Helper()
	streams, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: group, Consumer: consumer, Streams: []string{stream, ">"}, Count: 100, Block: -time.Millisecond}).Result()
	if err != nil && err != redis.Nil {
		t.Fatal(err)
	}
	if len(streams) > 0 {
		return streams[0].Messages
	}
	messages, err := client.XRange(ctx, stream, "-", "+").Result()
	if err != nil {
		t.Fatal(err)
	}
	return messages
}
