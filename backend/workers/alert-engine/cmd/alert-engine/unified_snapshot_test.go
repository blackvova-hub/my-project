package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const snapshotTestStream = "candles:v3:bybit:perpetual"

func TestUnifiedSnapshotRoundTripPartialCoverageAndAtomicReplace(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	history, batch := snapshotTestFixture(t, now)
	path := filepath.Join(t.TempDir(), "state", "unified-v3.snapshot")
	store := newUnifiedSnapshotStore(path)
	if err := store.Save(history, now); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) <= unifiedSnapshotHeaderSize {
		t.Fatalf("snapshot size=%d", len(first))
	}
	if got := binary.LittleEndian.Uint32(first[8:12]); got != unifiedSnapshotSchemaVersion {
		t.Fatalf("schema version=%d want=%d", got, unifiedSnapshotSchemaVersion)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("snapshot mode=%o want=600", got)
		}
	}
	if err := store.Save(history, now); err != nil {
		t.Fatalf("replace snapshot: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("same state/time did not produce deterministic snapshot")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("temporary snapshot artifact remained: %+v", entries)
	}

	restored := newSnapshotTestHistory()
	if err := store.Load(restored, now); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	key := instrumentCacheKey("bybit", "perpetual", "BTCUSDT")
	if value, ok := restored.Get(key, minuteKey(batch.Minute), "close"); !ok || value != 100 {
		t.Fatalf("restored close=(%v,%v)", value, ok)
	}
	if status := restored.coverage.Status("c", "bybit", "perpetual", batch.Minute, "BTCUSDT", now); status != coverageValid {
		t.Fatalf("BTC coverage=%s want=%s", status, coverageValid)
	}
	for _, symbol := range []string{"ETHUSDT", "SOLUSDT"} {
		if status := restored.coverage.Status("c", "bybit", "perpetual", batch.Minute, symbol, now); status != coverageMissing {
			t.Fatalf("%s coverage=%s want=%s", symbol, status, coverageMissing)
		}
	}
	coverageKey := coverageMinuteKey("c", "bybit", "perpetual", batch.Minute)
	restored.coverage.mu.Lock()
	shard := restored.coverage.minutes[coverageKey].shards[0]
	restored.coverage.mu.Unlock()
	if !shard.candleExceptionsArePresent || len(shard.candleExceptions) != 1 || shard.candleExceptions[0] != "BTCUSDT" {
		t.Fatalf("partial candle exceptions were not restored: %+v", shard)
	}
	water, ok := restored.StreamHighWater(snapshotTestStream)
	if !ok || water.MessageID != fmt.Sprintf("%d-0", batch.Minute) || water.BatchID != batch.BatchID || water.BatchMinute != batch.Minute || !water.ObservedAt.Equal(now) {
		t.Fatalf("restored high-water=(%+v,%v)", water, ok)
	}
}

func TestUnifiedSnapshotRestartContinuesHistoryAndHighWater(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	history, firstBatch := snapshotTestFixture(t, now)
	store := newUnifiedSnapshotStore(filepath.Join(t.TempDir(), "state.snapshot"))
	if err := store.Save(history, now); err != nil {
		t.Fatal(err)
	}
	restarted := newSnapshotTestHistory()
	if err := store.Load(restarted, now); err != nil {
		t.Fatal(err)
	}
	secondBatch := snapshotCandleBatch(firstBatch.Minute+60_000, true, []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"})
	secondBatch.BatchID = "snapshot-batch-2"
	secondBatch.Events[0].Metrics["close"] = MetricValue{Value: 101, Valid: true}
	secondMessageID := fmt.Sprintf("%d-2", secondBatch.Minute)
	if err := restarted.RegisterStreamMessages(snapshotTestStream, []string{secondMessageID}); err != nil {
		t.Fatalf("register after restart: %v", err)
	}
	if err := restarted.ObserveStreamBatch(snapshotTestStream, secondMessageID, secondBatch, now.Add(time.Minute)); err != nil {
		t.Fatalf("observe after restart: %v", err)
	}
	key := instrumentCacheKey("bybit", "perpetual", "BTCUSDT")
	if value, ok := restarted.Get(key, minuteKey(firstBatch.Minute), "close"); !ok || value != 100 {
		t.Fatalf("old value after restart=(%v,%v)", value, ok)
	}
	if value, ok := restarted.Get(key, minuteKey(secondBatch.Minute), "close"); !ok || value != 101 {
		t.Fatalf("new value after restart=(%v,%v)", value, ok)
	}
	water, ok := restarted.StreamHighWater(snapshotTestStream)
	if !ok || water.MessageID != secondMessageID || water.BatchID != secondBatch.BatchID {
		t.Fatalf("advanced high-water=(%+v,%v)", water, ok)
	}
	conflict := secondBatch
	conflict.BatchID = "conflicting-batch"
	if err := restarted.ObserveStreamBatch(snapshotTestStream, water.MessageID, conflict, now.Add(2*time.Minute)); err == nil {
		t.Fatal("same stream ID accepted a different batch identity")
	}
	if after, _ := restarted.StreamHighWater(snapshotTestStream); after != water {
		t.Fatalf("conflict mutated high-water: before=%+v after=%+v", water, after)
	}
	if err := store.Save(restarted, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	secondRestart := newSnapshotTestHistory()
	if err := store.Load(secondRestart, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if value, ok := secondRestart.Get(key, minuteKey(secondBatch.Minute), "close"); !ok || value != 101 {
		t.Fatalf("second restart value=(%v,%v)", value, ok)
	}
}

func TestUnifiedSnapshotRejectsCorruptionVersionAndTruncationWithoutMutation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	source, _ := snapshotTestFixture(t, now)
	validPath := filepath.Join(t.TempDir(), "valid.snapshot")
	if err := newUnifiedSnapshotStore(validPath).Save(source, now); err != nil {
		t.Fatal(err)
	}
	valid, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("checksum", func(t *testing.T) {
		corrupted := append([]byte(nil), valid...)
		corrupted[len(corrupted)-1] ^= 0xff
		err := loadSnapshotBytes(t, corrupted, now, snapshotSentinelHistory(now))
		if !errors.Is(err, ErrUnifiedSnapshotChecksum) {
			t.Fatalf("error=%v want checksum", err)
		}
	})

	t.Run("version", func(t *testing.T) {
		wrongVersion := append([]byte(nil), valid...)
		binary.LittleEndian.PutUint32(wrongVersion[8:12], unifiedSnapshotSchemaVersion+1)
		err := loadSnapshotBytes(t, wrongVersion, now, snapshotSentinelHistory(now))
		if !errors.Is(err, ErrUnifiedSnapshotVersion) {
			t.Fatalf("error=%v want version", err)
		}
	})

	for _, cut := range []int{0, 1, unifiedSnapshotHeaderSize - 1, unifiedSnapshotHeaderSize, len(valid) / 2, len(valid) - 1} {
		t.Run("truncated_"+strconvItoa(cut), func(t *testing.T) {
			err := loadSnapshotBytes(t, valid[:cut], now, snapshotSentinelHistory(now))
			if !errors.Is(err, ErrUnifiedSnapshotMalformed) {
				t.Fatalf("cut=%d error=%v want malformed", cut, err)
			}
		})
	}
}

func TestUnifiedSnapshotRejectsHostileHeadersBudgetsAndTimestamps(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, math.MaxUint32)
	hostileCount := snapshotFileForPayload(payload, now)
	if err := loadSnapshotBytes(t, hostileCount, now, newSnapshotTestHistory()); !errors.Is(err, ErrUnifiedSnapshotInvalidState) {
		t.Fatalf("hostile instrument count error=%v", err)
	}

	hostileLength := snapshotFileForPayload(nil, now)
	binary.LittleEndian.PutUint64(hostileLength[24:32], math.MaxUint64)
	if err := loadSnapshotBytes(t, hostileLength, now, newSnapshotTestHistory()); !errors.Is(err, ErrUnifiedSnapshotMalformed) {
		t.Fatalf("hostile length error=%v", err)
	}

	for name, createdAt := range map[string]time.Time{
		"future": now.Add(unifiedSnapshotFutureTolerance + time.Second),
		"stale":  now.Add(-unifiedSnapshotMaximumAge - time.Second),
	} {
		t.Run(name, func(t *testing.T) {
			data := snapshotFileForPayload(emptySnapshotPayload(t), createdAt)
			err := loadSnapshotBytes(t, data, now, newSnapshotTestHistory())
			if !errors.Is(err, ErrUnifiedSnapshotInvalidState) {
				t.Fatalf("timestamp error=%v", err)
			}
		})
	}

	var series bytes.Buffer
	if err := writeSnapshotString(&series, "close", 128); err != nil {
		t.Fatal(err)
	}
	_ = writeSnapshotUint32(&series, compactHistoryRetentionMinutes)
	_ = writeSnapshotInt64(&series, 100)
	_ = writeSnapshotBool(&series, true)
	_ = writeSnapshotUint32(&series, 0)
	reader := &snapshotReader{reader: bytes.NewReader(series.Bytes()), remaining: uint64(series.Len())}
	budget := &snapshotDecodeBudget{maxSeries: 1, maxBacking: 1}
	if _, _, err := decodeSnapshotSeries(reader, budget, now, unifiedSnapshotMaximumAge); !errors.Is(err, ErrUnifiedSnapshotTooLarge) {
		t.Fatalf("aggregate backing budget error=%v", err)
	}
}

func TestUnifiedSnapshotFailedSaveKeepsPreviousFileAndCleansTemp(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	history, _ := snapshotTestFixture(t, now)
	path := filepath.Join(t.TempDir(), "state", "snapshot.bin")
	store := newUnifiedSnapshotStore(path)
	if err := store.Save(history, now); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	history.mu.Lock()
	bad := history.streamHighWater[snapshotTestStream]
	bad.MessageID = "not-a-redis-id"
	history.streamHighWater[snapshotTestStream] = bad
	history.mu.Unlock()
	if err := store.Save(history, now); !errors.Is(err, ErrUnifiedSnapshotInvalidState) {
		t.Fatalf("invalid save error=%v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed save replaced the last valid snapshot")
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files after failed save=%v err=%v", matches, err)
	}
}

func TestUnifiedSnapshotObserveUsesConsistencyBarrier(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	history := newSnapshotTestHistory()
	batch := snapshotCandleBatch(now.UnixMilli(), true, []string{"BTCUSDT"})
	checkpoint := compactStreamHighWater{MessageID: "0-0", ObservedAt: now, ReadyAt: now, Bootstrapped: true}
	if err := history.SetStreamBootstrapCheckpoint(snapshotTestStream, checkpoint, now); err != nil {
		t.Fatal(err)
	}
	if err := history.RegisterStreamMessages(snapshotTestStream, []string{"1800000000000-0"}); err != nil {
		t.Fatal(err)
	}
	history.snapshotMu.Lock()
	done := make(chan error, 1)
	go func() {
		done <- history.ObserveStreamBatch(snapshotTestStream, "1800000000000-0", batch, now)
	}()
	select {
	case err := <-done:
		history.snapshotMu.Unlock()
		t.Fatalf("observe bypassed snapshot barrier: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	history.snapshotMu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("observe did not resume after snapshot barrier")
	}
}

func TestUnifiedSnapshotMissingFile(t *testing.T) {
	err := newUnifiedSnapshotStore(filepath.Join(t.TempDir(), "missing.snapshot")).Load(newSnapshotTestHistory(), time.Now().UTC())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing snapshot error=%v", err)
	}
}

func FuzzDecodeUnifiedSnapshotPayload(f *testing.F) {
	var empty bytes.Buffer
	for range 4 {
		_ = writeSnapshotUint32(&empty, 0)
	}
	f.Add(empty.Bytes())
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 1<<20 {
			t.Skip()
		}
		reader := &snapshotReader{reader: bytes.NewReader(payload), remaining: uint64(len(payload))}
		_, _ = decodeUnifiedSnapshotPayload(reader, newSnapshotTestHistory(), time.Now().UTC())
	})
}

func TestUnifiedSnapshotRejectsInvalidPartialCandleExceptions(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	history, batch := snapshotTestFixture(t, now)
	key := coverageMinuteKey("c", "bybit", "perpetual", batch.Minute)
	history.coverage.mu.Lock()
	shard := history.coverage.minutes[key].shards[0]
	shard.candleExceptions = []string{"SOLUSDT", "BTCUSDT"}
	history.coverage.minutes[key].shards[0] = shard
	history.coverage.mu.Unlock()
	err := newUnifiedSnapshotStore(filepath.Join(t.TempDir(), "bad.snapshot")).Save(history, now)
	if !errors.Is(err, ErrUnifiedSnapshotInvalidState) {
		t.Fatalf("invalid exception state error=%v", err)
	}
}

func TestRedisStreamIDSnapshotValidationIsCanonicalAndNumeric(t *testing.T) {
	for _, valid := range []string{"0-0", "1-0", "18446744073709551615-18446744073709551615"} {
		if _, _, ok := parseRedisStreamID(valid); !ok {
			t.Fatalf("valid stream id rejected: %q", valid)
		}
	}
	for _, invalid := range []string{"", "1", "1-", "-1", "+1-0", "01-0", "1-00", "1-0-0", " 1-0"} {
		if _, _, ok := parseRedisStreamID(invalid); ok {
			t.Fatalf("invalid stream id accepted: %q", invalid)
		}
	}
	if comparison, ok := compareRedisStreamIDs("10-2", "10-1"); !ok || comparison != 1 {
		t.Fatalf("numeric comparison=(%d,%v)", comparison, ok)
	}
}

func snapshotTestFixture(t *testing.T, now time.Time) (*sparseEngineState, compactDecodedBatch) {
	t.Helper()
	history := newSnapshotTestHistory()
	batch := snapshotCandleBatch(now.Truncate(time.Minute).UnixMilli(), false, []string{"BTCUSDT"})
	batch.BatchID = "snapshot-batch-1"
	messageID := fmt.Sprintf("%d-0", batch.Minute)
	checkpoint := compactStreamHighWater{MessageID: "0-0", ObservedAt: now, ReadyAt: now, Bootstrapped: true}
	if err := history.SetStreamBootstrapCheckpoint(snapshotTestStream, checkpoint, now); err != nil {
		t.Fatalf("set fixture checkpoint: %v", err)
	}
	if err := history.RegisterStreamMessages(snapshotTestStream, []string{messageID}); err != nil {
		t.Fatalf("register fixture message: %v", err)
	}
	if err := history.ObserveStreamBatch(snapshotTestStream, messageID, batch, now); err != nil {
		t.Fatalf("observe fixture: %v", err)
	}
	return history, batch
}

func newSnapshotTestHistory() *sparseEngineState {
	cache := NewRulesCache()
	cache.Set(map[string][]Rule{"bybit:perpetual:*": {{
		ID: 7, Exchange: "bybit", MarketType: "perpetual", Symbol: "*", WindowMinutes: 2,
		Conditions: []Condition{{Indicator: "price"}},
	}}})
	return newSparseEngineState(cache, false, 100)
}

func snapshotCandleBatch(minute int64, covered bool, present []string) compactDecodedBatch {
	catalog, version := compactCatalog([]string{"BTCUSDT", "ETHUSDT", "SOLUSDT"})
	events := make([]CandleEvent, 0, len(present))
	for _, symbol := range present {
		events = append(events, CandleEvent{
			Exchange: "bybit", MarketType: "perpetual", Symbol: symbol, TS: minute,
			Metrics: map[string]MetricValue{"close": {Value: 100, Valid: true}},
		})
	}
	return compactDecodedBatch{
		BatchID: "snapshot-batch", Kind: "c", Exchange: "bybit", MarketType: "perpetual", Minute: minute,
		ShardTotal: 1, Coverage: covered, CatalogVersion: version, Catalog: catalog, Events: events,
	}
}

func snapshotSentinelHistory(now time.Time) *sparseEngineState {
	history := newSnapshotTestHistory()
	batch := snapshotCandleBatch(now.Truncate(time.Minute).UnixMilli(), true, []string{"BTCUSDT"})
	batch.BatchID = "sentinel"
	_ = history.ObserveBatch(batch, now)
	return history
}

func loadSnapshotBytes(t *testing.T, data []byte, now time.Time, target *sparseEngineState) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.snapshot")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	key := instrumentCacheKey("bybit", "perpetual", "BTCUSDT")
	minute := minuteKey(now.UnixMilli())
	_, hadSentinel := target.Get(key, minute, "close")
	err := newUnifiedSnapshotStore(path).Load(target, now)
	if err != nil && hadSentinel {
		if value, ok := target.Get(key, minute, "close"); !ok || value != 100 {
			t.Fatalf("failed load partially mutated target: (%v,%v)", value, ok)
		}
	}
	return err
}

func snapshotFileForPayload(payload []byte, createdAt time.Time) []byte {
	digest := sha256.Sum256(payload)
	header := unifiedSnapshotHeader{
		Version: unifiedSnapshotSchemaVersion, CreatedAtNano: createdAt.UnixNano(), PayloadLength: uint64(len(payload)),
		Checksum: unifiedSnapshotChecksum(unifiedSnapshotSchemaVersion, createdAt.UnixNano(), uint64(len(payload)), digest[:]),
	}
	var output bytes.Buffer
	_ = writeUnifiedSnapshotHeader(&output, header)
	_, _ = output.Write(payload)
	return output.Bytes()
}

func emptySnapshotPayload(t *testing.T) []byte {
	t.Helper()
	var payload bytes.Buffer
	if err := writeSnapshotString(&payload, rulesDependencyFingerprint(NewRulesCache(), false), 128); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := writeSnapshotUint32(&payload, 0); err != nil {
			t.Fatal(err)
		}
	}
	return payload.Bytes()
}

func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buffer [32]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
