package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type trimmingBootstrapSource struct {
	messages   []redis.XMessage
	pageCalls  int
	groupCalls int
}

func (s *trimmingBootstrapSource) CaptureBounds(context.Context, string) (compactStreamBounds, error) {
	return compactStreamBounds{FirstID: s.messages[0].ID, UpperID: s.messages[len(s.messages)-1].ID}, nil
}

func (s *trimmingBootstrapSource) RangePage(context.Context, string, string, string, int64) (compactStreamPage, error) {
	s.pageCalls++
	if s.pageCalls == 1 {
		return compactStreamPage{FirstID: s.messages[0].ID, Messages: s.messages[:1]}, nil
	}
	// Simulates XTRIM moving the retained head beyond the last applied cursor
	// before the next atomic page read.
	return compactStreamPage{FirstID: s.messages[1].ID, Messages: s.messages[1:]}, nil
}

func (*trimmingBootstrapSource) LookupSkipDigests(context.Context, string, string, []redis.XMessage) (map[string]string, error) {
	return map[string]string{}, nil
}

func (s *trimmingBootstrapSource) SetGroupStart(context.Context, string, string, string) error {
	s.groupCalls++
	return nil
}

func TestCompactBootstrapFailsClosedWhenRetentionMovesPastCursor(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	first := compactBootstrapValidMessage(t, now.Add(-time.Hour), 0)
	second := compactBootstrapValidMessage(t, now, 0)
	source := &trimmingBootstrapSource{messages: []redis.XMessage{first, second}}
	history := newSnapshotTestHistory()
	_, err := BootstrapCompactStreams(context.Background(), source, &fakeCompactBootstrapOwner{}, history, []string{snapshotTestStream}, "group", now)
	if !errors.Is(err, errCompactStreamRewound) {
		t.Fatalf("retention gap error=%v", err)
	}
	if source.groupCalls != 0 {
		t.Fatal("consumer group moved across an unproven retention gap")
	}
	if _, exists := history.StreamHighWater(snapshotTestStream); exists {
		t.Fatal("checkpoint advanced across an unproven retention gap")
	}
}

func TestCompactBootstrapDurableLedgerIsAuthoritativeBeforeDecode(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	message := compactBootstrapValidMessage(t, now.Add(-time.Hour), 0)
	stream, group := snapshotTestStream, "group"
	ledger, err := streamMessageSkipLedgerRecord(message, stream, group, streamFailureUnifiedConflict)
	if err != nil {
		t.Fatal(err)
	}
	source := &fakeCompactBootstrapSkipSource{message: message, ledger: map[string]string{ledger.Key: ledger.Value}}
	history := newSnapshotTestHistory()
	if _, err := BootstrapCompactStreams(context.Background(), source, &fakeCompactBootstrapOwner{}, history, []string{stream}, group, now); err != nil {
		t.Fatal(err)
	}
	if stats := history.Stats(); stats.Instruments != 0 {
		t.Fatalf("durably skipped decodable message mutated history: %+v", stats)
	}
	water, exists := history.StreamHighWater(stream)
	if !exists || water.SkipDigest != ledger.SkipDigest || water.Skipped != 1 {
		t.Fatalf("checkpoint=%+v exists=%v", water, exists)
	}
}

func compactBootstrapValidMessage(t *testing.T, at time.Time, sequence int) redis.XMessage {
	t.Helper()
	minute := at.UTC().Truncate(time.Minute).UnixMilli()
	catalog, version := compactCatalog([]string{"BTCUSDT"})
	wire := compactWireBatch{
		TransportVersion: compactTransportVersion, CanonicalVersion: compactCanonicalVersion,
		Kind: "c", Exchange: "bybit", MarketType: "perpetual", Minute: minute,
		ShardTotal: 1, CatalogVersion: version, Catalog: catalog, Coverage: true,
		LayoutVersion: compactMetricLayoutV1, LayoutHash: compactMetricLayoutHash,
		CandleRows: []compactWireCandleRow{{Symbol: "BTCUSDT", Values: make([]float64, len(compactMetricLayout))}},
	}
	wire.BatchID = compactBatchID(wire)
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	return redis.XMessage{ID: fmt.Sprintf("%d-%d", minute, sequence), Values: map[string]any{"json": string(raw)}}
}
