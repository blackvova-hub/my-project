package main

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeCompactBootstrapSkipSource struct {
	stream     string
	message    redis.XMessage
	ledger     map[string]string
	groupMoves int
}

func (s *fakeCompactBootstrapSkipSource) CaptureBounds(context.Context, string) (compactStreamBounds, error) {
	return compactStreamBounds{FirstID: s.message.ID, UpperID: s.message.ID}, nil
}

func (s *fakeCompactBootstrapSkipSource) RangePage(_ context.Context, _ string, startExclusive, _ string, _ int64) (compactStreamPage, error) {
	if startExclusive == s.message.ID {
		return compactStreamPage{FirstID: s.message.ID}, nil
	}
	return compactStreamPage{FirstID: s.message.ID, Messages: []redis.XMessage{s.message}}, nil
}

func (s *fakeCompactBootstrapSkipSource) SetGroupStart(context.Context, string, string, string) error {
	s.groupMoves++
	return nil
}

func (s *fakeCompactBootstrapSkipSource) GetCompactSkipLedger(_ context.Context, key string) (string, error) {
	value, exists := s.ledger[key]
	if !exists {
		return "", redis.Nil
	}
	return value, nil
}

func (s *fakeCompactBootstrapSkipSource) LookupSkipDigests(ctx context.Context, stream, group string, messages []redis.XMessage) (map[string]string, error) {
	result := make(map[string]string)
	for _, message := range messages {
		digest, skipped, err := resolveBootstrapCompactSkip(ctx, s, stream, group, message)
		if err != nil {
			return nil, err
		}
		if skipped {
			result[message.ID] = digest
		}
	}
	return result, nil
}

func TestCompactBootstrapAdvancesAcrossExactDurableSkip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	stream, group := "candles:v3:bybit:perpetual", "compact-group"
	message := redis.XMessage{ID: streamIDAt(now.Add(-27 * time.Hour)), Values: map[string]any{"json": "not-json"}}
	ledger, err := streamMessageSkipLedgerRecord(message, stream, group, streamFailureMalformedJSON)
	if err != nil {
		t.Fatal(err)
	}
	source := &fakeCompactBootstrapSkipSource{stream: stream, message: message, ledger: map[string]string{ledger.Key: ledger.Value}}
	history := newSnapshotTestHistory()
	report, err := BootstrapCompactStreams(context.Background(), source, &fakeCompactBootstrapOwner{}, history, []string{stream}, group, now)
	if err != nil {
		t.Fatal(err)
	}
	if source.groupMoves != 1 || report.Streams[stream].Replayed != 1 {
		t.Fatalf("group moves=%d report=%+v", source.groupMoves, report.Streams[stream])
	}
	checkpoint, exists := history.StreamHighWater(stream)
	if !exists || checkpoint.MessageID != message.ID || checkpoint.SkipDigest != ledger.SkipDigest || checkpoint.Skipped != 1 {
		t.Fatalf("checkpoint=%+v exists=%v", checkpoint, exists)
	}
}

func TestCompactBootstrapFailsClosedWithoutExactSkipLedger(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	stream, group := "candles:v3:bybit:perpetual", "compact-group"
	message := redis.XMessage{ID: streamIDAt(now.Add(-27 * time.Hour)), Values: map[string]any{"json": "not-json"}}
	source := &fakeCompactBootstrapSkipSource{stream: stream, message: message, ledger: map[string]string{}}
	history := newSnapshotTestHistory()
	if _, err := BootstrapCompactStreams(context.Background(), source, &fakeCompactBootstrapOwner{}, history, []string{stream}, group, now); err == nil {
		t.Fatal("poison message without exact durable ledger was skipped")
	}
	if source.groupMoves != 0 {
		t.Fatal("consumer group moved after fail-closed poison rejection")
	}
	if _, exists := history.StreamHighWater(stream); exists {
		t.Fatal("checkpoint advanced after fail-closed poison rejection")
	}
}

func streamIDAt(at time.Time) string {
	return strconv.FormatInt(at.UnixMilli(), 10) + "-0"
}
