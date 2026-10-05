package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type compactBootstrapPageSource struct {
	messages []redis.XMessage
}

func (s *compactBootstrapPageSource) CaptureBounds(context.Context, string) (compactStreamBounds, error) {
	return compactStreamBounds{FirstID: s.messages[0].ID, UpperID: s.messages[len(s.messages)-1].ID}, nil
}

func (s *compactBootstrapPageSource) RangePage(_ context.Context, _, startExclusive, endInclusive string, count int64) (compactStreamPage, error) {
	result := make([]redis.XMessage, 0, len(s.messages))
	for _, message := range s.messages {
		afterStart, _ := compareRedisStreamIDs(message.ID, startExclusive)
		beforeEnd, _ := compareRedisStreamIDs(message.ID, endInclusive)
		if afterStart > 0 && beforeEnd <= 0 {
			result = append(result, message)
			if int64(len(result)) == count {
				break
			}
		}
	}
	return compactStreamPage{FirstID: s.messages[0].ID, Messages: result}, nil
}

func (*compactBootstrapPageSource) LookupSkipDigests(context.Context, string, string, []redis.XMessage) (map[string]string, error) {
	return map[string]string{}, nil
}

func (*compactBootstrapPageSource) SetGroupStart(context.Context, string, string, string) error {
	return nil
}

func TestCompactBootstrapOwnerChecksScaleByPageNotMessage(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	minute := now.Add(-27 * time.Hour).Truncate(time.Minute).UnixMilli()
	catalog, version := compactCatalog([]string{"BTCUSDT"})
	batch := compactWireBatch{
		TransportVersion: compactTransportVersion, CanonicalVersion: compactCanonicalVersion,
		Kind: "c", Exchange: "bybit", MarketType: "perpetual", Minute: minute,
		ShardTotal: 1, CatalogVersion: version, Catalog: catalog, Coverage: true,
		LayoutVersion: compactMetricLayoutV1, LayoutHash: compactMetricLayoutHash,
		CandleRows: []compactWireCandleRow{{Symbol: "BTCUSDT", Values: make([]float64, len(compactMetricLayout))}},
	}
	batch.BatchID = compactBatchID(batch)
	raw, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	messages := make([]redis.XMessage, 100)
	for index := range messages {
		messages[index] = redis.XMessage{ID: fmt.Sprintf("%d-%d", minute, index), Values: map[string]any{"json": string(raw)}}
	}
	owner := &fakeCompactBootstrapOwner{}
	if _, err := BootstrapCompactStreams(context.Background(), &compactBootstrapPageSource{messages: messages}, owner, newSnapshotTestHistory(), []string{"candles:v3:bybit:perpetual"}, "group", now); err != nil {
		t.Fatal(err)
	}
	if owner.calls > 10 {
		t.Fatalf("owner assertions=%d for one 100-message page; expected page/barrier scale", owner.calls)
	}
}
