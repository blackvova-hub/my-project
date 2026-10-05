package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	unifiedSnapshotSchemaVersion     uint32 = 5
	unifiedSnapshotHeaderSize               = 64
	unifiedSnapshotMaxFileBytes      int64  = 256 << 20
	unifiedSnapshotMaxStringBytes           = 1 << 20
	unifiedSnapshotMaxStreams               = 1024
	unifiedSnapshotMaxShards                = 4096
	unifiedSnapshotMaxSeries                = 250_000
	unifiedSnapshotMaxBackingBytes   int64  = 256 << 20
	unifiedSnapshotMaxCatalogSymbols        = 2_000_000
	unifiedSnapshotMaxCoverageShards        = 250_000
	unifiedSnapshotMaxExceptions            = 2_000_000
	unifiedSnapshotFutureTolerance          = 5 * time.Minute
	unifiedSnapshotMaximumAge               = 28 * time.Hour
)

var (
	unifiedSnapshotMagic            = [8]byte{'U', 'V', '3', 'S', 'N', 'A', 'P', 0}
	ErrUnifiedSnapshotMalformed     = errors.New("malformed sparse snapshot")
	ErrUnifiedSnapshotChecksum      = errors.New("sparse snapshot checksum mismatch")
	ErrUnifiedSnapshotVersion       = errors.New("unsupported sparse snapshot schema version")
	ErrUnifiedSnapshotTooLarge      = errors.New("sparse snapshot exceeds size limit")
	ErrUnifiedSnapshotInvalidState  = errors.New("invalid sparse snapshot state")
	ErrUnifiedSnapshotRulesMismatch = errors.New("sparse snapshot rules dependencies mismatch")
)

type compactStreamHighWater struct {
	MessageID    string
	BatchMinute  int64
	BatchID      string
	SkipDigest   string
	Skipped      uint64
	ObservedAt   time.Time
	ReadyAt      time.Time
	Bootstrapped bool
}

type unifiedSnapshotHeader struct {
	Version       uint32
	CreatedAtNano int64
	PayloadLength uint64
	Checksum      [sha256.Size]byte
}

type unifiedSnapshotStore struct {
	path string
	mu   sync.Mutex
}

func newUnifiedSnapshotStore(path string) *unifiedSnapshotStore {
	return &unifiedSnapshotStore{path: strings.TrimSpace(path)}
}

func (s *unifiedSnapshotStore) Save(history *sparseEngineState, now time.Time) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("%w: snapshot path is empty", ErrUnifiedSnapshotInvalidState)
	}
	if history == nil {
		return fmt.Errorf("%w: history is nil", ErrUnifiedSnapshotInvalidState)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create snapshot temp file: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod snapshot temp file: %w", err)
	}
	if _, err := temporary.Write(make([]byte, unifiedSnapshotHeaderSize)); err != nil {
		return fmt.Errorf("write snapshot header placeholder: %w", err)
	}

	payloadHash := sha256.New()
	counter := &snapshotCountingWriter{writer: io.MultiWriter(temporary, payloadHash)}
	if err := history.writeSnapshotPayload(counter, now); err != nil {
		return err
	}
	if counter.count > uint64(unifiedSnapshotMaxFileBytes-unifiedSnapshotHeaderSize) {
		return ErrUnifiedSnapshotTooLarge
	}
	payloadDigest := payloadHash.Sum(nil)
	header := unifiedSnapshotHeader{
		Version:       unifiedSnapshotSchemaVersion,
		CreatedAtNano: now.UnixNano(),
		PayloadLength: counter.count,
		Checksum:      unifiedSnapshotChecksum(unifiedSnapshotSchemaVersion, now.UnixNano(), counter.count, payloadDigest),
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek snapshot header: %w", err)
	}
	if err := writeUnifiedSnapshotHeader(temporary, header); err != nil {
		return fmt.Errorf("write snapshot header: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("fsync snapshot file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close snapshot file: %w", err)
	}
	if err := replaceSnapshotFile(temporaryPath, s.path); err != nil {
		return fmt.Errorf("atomic snapshot rename: %w", err)
	}
	if err := syncSnapshotDirectory(directory); err != nil {
		return fmt.Errorf("fsync snapshot directory: %w", err)
	}
	committed = true
	return nil
}

func (s *unifiedSnapshotStore) Load(history *sparseEngineState, now time.Time) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("%w: snapshot path is empty", ErrUnifiedSnapshotInvalidState)
	}
	if history == nil {
		return fmt.Errorf("%w: history is nil", ErrUnifiedSnapshotInvalidState)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat snapshot: %w", err)
	}
	if stat.Size() < unifiedSnapshotHeaderSize {
		return fmt.Errorf("%w: truncated header", ErrUnifiedSnapshotMalformed)
	}
	if stat.Size() > unifiedSnapshotMaxFileBytes {
		return ErrUnifiedSnapshotTooLarge
	}
	header, err := readUnifiedSnapshotHeader(file)
	if err != nil {
		return err
	}
	if header.PayloadLength != uint64(stat.Size()-unifiedSnapshotHeaderSize) {
		return fmt.Errorf("%w: payload length does not match file size", ErrUnifiedSnapshotMalformed)
	}
	payloadHash := sha256.New()
	if _, err := io.CopyN(payloadHash, file, int64(header.PayloadLength)); err != nil {
		return fmt.Errorf("%w: hash payload: %v", ErrUnifiedSnapshotMalformed, err)
	}
	want := unifiedSnapshotChecksum(header.Version, header.CreatedAtNano, header.PayloadLength, payloadHash.Sum(nil))
	if subtle.ConstantTimeCompare(want[:], header.Checksum[:]) != 1 {
		return ErrUnifiedSnapshotChecksum
	}
	if err := validateSnapshotTimestamp(time.Unix(0, header.CreatedAtNano).UTC(), now, unifiedSnapshotMaximumAge); err != nil {
		return fmt.Errorf("%w: invalid snapshot creation time: %v", ErrUnifiedSnapshotInvalidState, err)
	}
	if _, err := file.Seek(unifiedSnapshotHeaderSize, io.SeekStart); err != nil {
		return fmt.Errorf("seek snapshot payload: %w", err)
	}
	reader := &snapshotReader{reader: io.LimitReader(file, int64(header.PayloadLength)), remaining: header.PayloadLength}
	state, err := decodeUnifiedSnapshotPayload(reader, history, now)
	if err != nil {
		return err
	}
	if reader.remaining != 0 {
		return fmt.Errorf("%w: %d trailing payload bytes", ErrUnifiedSnapshotMalformed, reader.remaining)
	}
	history.installSnapshotState(state)
	return nil
}

func startUnifiedSnapshotLoop(ctx context.Context, store *unifiedSnapshotStore, history *sparseEngineState, every time.Duration) {
	if store == nil || history == nil {
		return
	}
	if every <= 0 {
		every = 5 * time.Minute
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := store.Save(history, now.UTC()); err != nil {
				log.Printf("sparse snapshot save failed: %v", err)
			}
		}
	}
}

func (h *sparseEngineState) writeSnapshotPayload(writer io.Writer, now time.Time) error {
	if h == nil {
		return fmt.Errorf("%w: nil history", ErrUnifiedSnapshotInvalidState)
	}
	h.snapshotMu.Lock()
	defer h.snapshotMu.Unlock()
	fingerprint := h.currentDependencyFingerprint()
	h.mu.Lock()
	bootstrapped := h.bootstrapFingerprint
	if bootstrapped == "" {
		h.bootstrapFingerprint = fingerprint
		bootstrapped = fingerprint
	}
	h.mu.Unlock()
	if bootstrapped != fingerprint {
		return fmt.Errorf("%w: bootstrapped=%s current=%s", errCompactBootstrapRulesChanged, bootstrapped, fingerprint)
	}
	if !validSHA256Hex(fingerprint) || fingerprint != strings.ToLower(fingerprint) {
		return fmt.Errorf("%w: invalid rules dependency fingerprint", ErrUnifiedSnapshotInvalidState)
	}
	if err := writeSnapshotString(writer, fingerprint, 128); err != nil {
		return err
	}
	h.expireSnapshotState(now)
	if err := h.writeSnapshotInstruments(writer, now); err != nil {
		return err
	}
	if err := h.writeSnapshotCatalogs(writer, now); err != nil {
		return err
	}
	if err := h.writeSnapshotCoverage(writer, now); err != nil {
		return err
	}
	return h.writeSnapshotHighWater(writer, now)
}

func (h *sparseEngineState) expireSnapshotState(now time.Time) {
	h.mu.Lock()
	h.lastCleanup = time.Time{}
	h.cleanupLocked(now)
	h.mu.Unlock()
	h.coverage.mu.Lock()
	h.coverage.expireLocked(now)
	h.coverage.mu.Unlock()
	h.catalogs.mu.Lock()
	h.catalogs.expireLocked(now)
	h.catalogs.mu.Unlock()
}

func (h *sparseEngineState) writeSnapshotInstruments(writer io.Writer, now time.Time) error {
	rules, _ := h.rules.Snapshot()
	dependencies := newHistoryDependencyIndex(rules, h.includeImportant)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.instruments) > h.maxInstruments {
		return fmt.Errorf("%w: instrument count exceeds configured maximum", ErrUnifiedSnapshotInvalidState)
	}
	keys := sortedMapKeys(h.instruments)
	budget := newSnapshotDecodeBudget()
	if err := writeSnapshotUint32(writer, len(keys)); err != nil {
		return err
	}
	for _, key := range keys {
		record := h.instruments[key]
		if record == nil || record.history == nil || record.lastSeen.IsZero() {
			return fmt.Errorf("%w: invalid instrument %q", ErrUnifiedSnapshotInvalidState, key)
		}
		if err := validateSnapshotTimestamp(record.lastSeen, now, h.ttl); err != nil {
			return fmt.Errorf("%w: instrument %q timestamp: %v", ErrUnifiedSnapshotInvalidState, key, err)
		}
		if err := validateSnapshotInstrumentKey(key); err != nil {
			return err
		}
		if err := writeSnapshotString(writer, key, unifiedSnapshotMaxStringBytes); err != nil {
			return err
		}
		if err := writeSnapshotInt64(writer, record.lastSeen.UnixNano()); err != nil {
			return err
		}
		record.history.mu.RLock()
		if err := validateSnapshotHistoryPlan(key, record.history, dependencies); err != nil {
			record.history.mu.RUnlock()
			return err
		}
		metrics := sortedMapKeys(record.history.series)
		if err := writeSnapshotUint32(writer, len(metrics)); err != nil {
			record.history.mu.RUnlock()
			return err
		}
		for _, metric := range metrics {
			series := record.history.series[metric]
			if series == nil {
				record.history.mu.RUnlock()
				return fmt.Errorf("%w: nil sparse series %q", ErrUnifiedSnapshotInvalidState, metric)
			}
			if err := budget.addSeries(uint32(series.Capacity())); err != nil {
				record.history.mu.RUnlock()
				return err
			}
			if err := writeSnapshotSeries(writer, metric, series); err != nil {
				record.history.mu.RUnlock()
				return fmt.Errorf("instrument %s: %w", key, err)
			}
		}
		record.history.mu.RUnlock()
	}
	return nil
}

func writeSnapshotSeries(writer io.Writer, metric string, series *sparseSeries) error {
	if !isHistoryMetric(metric) || series == nil {
		return fmt.Errorf("%w: invalid metric %q", ErrUnifiedSnapshotInvalidState, metric)
	}
	capacity := series.Capacity()
	if capacity < 1 || capacity > compactHistoryRetentionMinutes || len(series.values) != capacity || len(series.occupied) != sparseWordCount(capacity) || len(series.valid) != sparseWordCount(capacity) {
		return fmt.Errorf("%w: invalid sparse series shape for %q", ErrUnifiedSnapshotInvalidState, metric)
	}
	if err := writeSnapshotString(writer, metric, 128); err != nil {
		return err
	}
	if err := writeSnapshotUint32(writer, capacity); err != nil {
		return err
	}
	if err := writeSnapshotInt64(writer, series.lastMinute); err != nil {
		return err
	}
	if err := writeSnapshotBool(writer, series.hasLast); err != nil {
		return err
	}
	points := 0
	for index := range series.minuteKeys {
		if sparseBit(series.occupied, index) {
			points++
		}
	}
	if (!series.hasLast && points != 0) || (series.hasLast && points == 0) {
		return fmt.Errorf("%w: sparse series last-minute invariant failed", ErrUnifiedSnapshotInvalidState)
	}
	if err := writeSnapshotUint32(writer, points); err != nil {
		return err
	}
	sawLast := false
	for index, minute := range series.minuteKeys {
		if !sparseBit(series.occupied, index) {
			continue
		}
		valid := sparseBit(series.valid, index)
		value := series.values[index]
		if minute <= 0 || (series.hasLast && (minute > series.lastMinute || minute < series.lastMinute-int64(capacity)+1)) || (valid && (math.IsNaN(value) || math.IsInf(value, 0))) {
			return fmt.Errorf("%w: invalid sparse point for %q", ErrUnifiedSnapshotInvalidState, metric)
		}
		if minute == series.lastMinute {
			sawLast = true
		}
		if err := writeSnapshotInt64(writer, minute); err != nil {
			return err
		}
		if err := writeSnapshotUint64(writer, math.Float64bits(value)); err != nil {
			return err
		}
		if err := writeSnapshotBool(writer, valid); err != nil {
			return err
		}
	}
	if series.hasLast && !sawLast {
		return fmt.Errorf("%w: sparse series does not contain last minute", ErrUnifiedSnapshotInvalidState)
	}
	return nil
}

func (h *sparseEngineState) writeSnapshotCatalogs(writer io.Writer, now time.Time) error {
	h.catalogs.mu.Lock()
	defer h.catalogs.mu.Unlock()
	if len(h.catalogs.entries) > h.catalogs.maxEntries {
		return fmt.Errorf("%w: catalog count exceeds configured maximum", ErrUnifiedSnapshotInvalidState)
	}
	versions := sortedMapKeys(h.catalogs.entries)
	totalSymbols := uint64(0)
	if err := writeSnapshotUint32(writer, len(versions)); err != nil {
		return err
	}
	for _, version := range versions {
		record := h.catalogs.entries[version]
		if uint64(len(record.symbols)) > unifiedSnapshotMaxCatalogSymbols-totalSymbols {
			return fmt.Errorf("%w: aggregate catalog symbol budget exceeded", ErrUnifiedSnapshotTooLarge)
		}
		totalSymbols += uint64(len(record.symbols))
		normalized, calculated := compactCatalog(record.symbols)
		if version == "" || version != calculated || !equalStringSlices(normalized, record.symbols) || record.lastSeen.IsZero() {
			return fmt.Errorf("%w: invalid catalog %q", ErrUnifiedSnapshotInvalidState, version)
		}
		if err := validateSnapshotTimestamp(record.lastSeen, now, h.catalogs.ttl); err != nil {
			return fmt.Errorf("%w: catalog %q timestamp: %v", ErrUnifiedSnapshotInvalidState, version, err)
		}
		if err := writeSnapshotString(writer, version, 256); err != nil {
			return err
		}
		if err := writeSnapshotInt64(writer, record.lastSeen.UnixNano()); err != nil {
			return err
		}
		if err := writeSnapshotUint32(writer, len(record.symbols)); err != nil {
			return err
		}
		for _, symbol := range record.symbols {
			if err := writeSnapshotString(writer, symbol, 256); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *sparseEngineState) writeSnapshotCoverage(writer io.Writer, now time.Time) error {
	h.coverage.mu.Lock()
	defer h.coverage.mu.Unlock()
	h.catalogs.mu.Lock()
	defer h.catalogs.mu.Unlock()
	if len(h.coverage.minutes) > h.coverage.maxEntries {
		return fmt.Errorf("%w: coverage count exceeds configured maximum", ErrUnifiedSnapshotInvalidState)
	}
	keys := sortedMapKeys(h.coverage.minutes)
	totalShards, totalExceptions := uint64(0), uint64(0)
	if err := writeSnapshotUint32(writer, len(keys)); err != nil {
		return err
	}
	for _, key := range keys {
		state := h.coverage.minutes[key]
		if state == nil || state.shardTotal < 1 || state.shardTotal > unifiedSnapshotMaxShards || len(state.shards) > state.shardTotal || state.lastSeen.IsZero() {
			return fmt.Errorf("%w: invalid coverage %q", ErrUnifiedSnapshotInvalidState, key)
		}
		if uint64(len(state.shards)) > unifiedSnapshotMaxCoverageShards-totalShards {
			return fmt.Errorf("%w: aggregate coverage shard budget exceeded", ErrUnifiedSnapshotTooLarge)
		}
		totalShards += uint64(len(state.shards))
		if err := validateSnapshotTimestamp(state.lastSeen, now, h.coverage.ttl); err != nil {
			return fmt.Errorf("%w: coverage %q timestamp: %v", ErrUnifiedSnapshotInvalidState, key, err)
		}
		if err := validateSnapshotCoverageKey(key); err != nil {
			return err
		}
		if err := writeSnapshotString(writer, key, unifiedSnapshotMaxStringBytes); err != nil {
			return err
		}
		if err := writeSnapshotUint32(writer, state.shardTotal); err != nil {
			return err
		}
		if err := writeSnapshotInt64(writer, state.lastSeen.UnixNano()); err != nil {
			return err
		}
		indexes := make([]int, 0, len(state.shards))
		for index := range state.shards {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		if err := writeSnapshotUint32(writer, len(indexes)); err != nil {
			return err
		}
		for _, index := range indexes {
			shard := state.shards[index]
			if uint64(len(shard.candleExceptions)) > unifiedSnapshotMaxExceptions-totalExceptions {
				return fmt.Errorf("%w: aggregate candle exception budget exceeded", ErrUnifiedSnapshotTooLarge)
			}
			totalExceptions += uint64(len(shard.candleExceptions))
			if index < 0 || index >= state.shardTotal || strings.TrimSpace(shard.batchID) == "" || strings.TrimSpace(shard.catalogVersion) == "" {
				return fmt.Errorf("%w: invalid coverage shard for %q", ErrUnifiedSnapshotInvalidState, key)
			}
			if _, ok := h.catalogs.entries[shard.catalogVersion]; !ok {
				return fmt.Errorf("%w: coverage references unknown catalog", ErrUnifiedSnapshotInvalidState)
			}
			catalog := h.catalogs.entries[shard.catalogVersion].symbols
			if err := validateSnapshotCandleExceptions(key[:1], shard, catalog); err != nil {
				return err
			}
			if err := writeSnapshotUint32(writer, index); err != nil {
				return err
			}
			if err := writeSnapshotString(writer, shard.batchID, 512); err != nil {
				return err
			}
			if err := writeSnapshotString(writer, shard.catalogVersion, 256); err != nil {
				return err
			}
			if err := writeSnapshotBool(writer, shard.coverage); err != nil {
				return err
			}
			if err := writeSnapshotBool(writer, shard.candleExceptionsArePresent); err != nil {
				return err
			}
			if err := writeSnapshotUint32(writer, len(shard.candleExceptions)); err != nil {
				return err
			}
			for _, symbol := range shard.candleExceptions {
				if err := writeSnapshotString(writer, symbol, 256); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (h *sparseEngineState) writeSnapshotHighWater(writer io.Writer, now time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.streamHighWater) > unifiedSnapshotMaxStreams {
		return fmt.Errorf("%w: stream high-water count exceeds maximum", ErrUnifiedSnapshotInvalidState)
	}
	streams := sortedMapKeys(h.streamHighWater)
	if err := writeSnapshotUint32(writer, len(streams)); err != nil {
		return err
	}
	for _, stream := range streams {
		water := h.streamHighWater[stream]
		if err := validateCompactStreamHighWater(stream, water); err != nil {
			return err
		}
		if err := validateSnapshotNotFuture(water.ObservedAt, now); err != nil {
			return fmt.Errorf("%w: stream %q observation time: %v", ErrUnifiedSnapshotInvalidState, stream, err)
		}
		if err := validateSnapshotReadyAt(water.ReadyAt, now); err != nil {
			return fmt.Errorf("%w: stream %q readiness time: %v", ErrUnifiedSnapshotInvalidState, stream, err)
		}
		if err := writeSnapshotString(writer, stream, 512); err != nil {
			return err
		}
		if err := writeSnapshotString(writer, water.MessageID, 128); err != nil {
			return err
		}
		if err := writeSnapshotInt64(writer, water.BatchMinute); err != nil {
			return err
		}
		if err := writeSnapshotString(writer, water.BatchID, 512); err != nil {
			return err
		}
		if err := writeSnapshotString(writer, water.SkipDigest, 128); err != nil {
			return err
		}
		if err := writeSnapshotUint64(writer, water.Skipped); err != nil {
			return err
		}
		if err := writeSnapshotInt64(writer, water.ObservedAt.UnixNano()); err != nil {
			return err
		}
		if err := writeSnapshotInt64(writer, water.ReadyAt.UnixNano()); err != nil {
			return err
		}
		if err := writeSnapshotBool(writer, water.Bootstrapped); err != nil {
			return err
		}
	}
	return nil
}

type decodedUnifiedSnapshot struct {
	dependencyFingerprint string
	instruments           map[string]*unifiedInstrumentHistory
	catalogs              *catalogRegistry
	coverage              *coverageTracker
	highWater             map[string]compactStreamHighWater
}

type snapshotDecodeBudget struct {
	series         uint64
	backingBytes   uint64
	catalogSymbols uint64
	coverageShards uint64
	exceptions     uint64
	maxSeries      uint64
	maxBacking     uint64
}

func newSnapshotDecodeBudget() *snapshotDecodeBudget {
	return &snapshotDecodeBudget{maxSeries: unifiedSnapshotMaxSeries, maxBacking: uint64(unifiedSnapshotMaxBackingBytes)}
}

func (b *snapshotDecodeBudget) addSeries(capacity uint32) error {
	if b == nil || b.maxSeries == 0 || b.maxBacking == 0 {
		return fmt.Errorf("%w: invalid sparse history budget", ErrUnifiedSnapshotInvalidState)
	}
	backingBytes := uint64(capacity)*16 + uint64(sparseWordCount(int(capacity)))*16
	if b.series >= b.maxSeries || b.backingBytes > b.maxBacking || backingBytes > b.maxBacking-b.backingBytes {
		return fmt.Errorf("%w: aggregate sparse history budget exceeded", ErrUnifiedSnapshotTooLarge)
	}
	b.series++
	b.backingBytes += backingBytes
	return nil
}

func decodeUnifiedSnapshotPayload(reader *snapshotReader, target *sparseEngineState, now time.Time) (*decodedUnifiedSnapshot, error) {
	if reader == nil || target == nil {
		return nil, fmt.Errorf("%w: nil decoder target", ErrUnifiedSnapshotInvalidState)
	}
	state := &decodedUnifiedSnapshot{
		instruments: make(map[string]*unifiedInstrumentHistory),
		highWater:   make(map[string]compactStreamHighWater),
	}
	fingerprint, err := reader.string(128)
	if err != nil {
		return nil, err
	}
	if fingerprint != strings.ToLower(fingerprint) || !validSHA256Hex(fingerprint) {
		return nil, fmt.Errorf("%w: invalid rules dependency fingerprint", ErrUnifiedSnapshotInvalidState)
	}
	currentFingerprint := target.currentDependencyFingerprint()
	if fingerprint != currentFingerprint {
		return nil, fmt.Errorf("%w: snapshot=%s current=%s", ErrUnifiedSnapshotRulesMismatch, fingerprint, currentFingerprint)
	}
	state.dependencyFingerprint = fingerprint
	rulesSnapshot, _ := target.rules.Snapshot()
	dependencies := newHistoryDependencyIndex(rulesSnapshot, target.includeImportant)
	budget := newSnapshotDecodeBudget()
	instrumentCount, err := reader.uint32()
	if err != nil {
		return nil, err
	}
	if int(instrumentCount) > target.maxInstruments {
		return nil, fmt.Errorf("%w: instrument count %d exceeds maximum %d", ErrUnifiedSnapshotInvalidState, instrumentCount, target.maxInstruments)
	}
	for range instrumentCount {
		key, err := reader.string(unifiedSnapshotMaxStringBytes)
		if err != nil {
			return nil, err
		}
		if err := validateSnapshotInstrumentKey(key); err != nil {
			return nil, err
		}
		if _, duplicate := state.instruments[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate instrument %q", ErrUnifiedSnapshotInvalidState, key)
		}
		lastSeenNano, err := reader.int64()
		if err != nil || lastSeenNano <= 0 {
			return nil, fmt.Errorf("%w: invalid instrument timestamp", ErrUnifiedSnapshotInvalidState)
		}
		lastSeen := time.Unix(0, lastSeenNano).UTC()
		if err := validateSnapshotTimestamp(lastSeen, now, target.ttl); err != nil {
			return nil, fmt.Errorf("%w: invalid instrument timestamp: %v", ErrUnifiedSnapshotInvalidState, err)
		}
		seriesCount, err := reader.uint32()
		if err != nil {
			return nil, err
		}
		if int(seriesCount) > len(compactMetricLayout)+1 {
			return nil, fmt.Errorf("%w: too many series for %q", ErrUnifiedSnapshotInvalidState, key)
		}
		history := &sparseHistory{series: make(map[string]*sparseSeries, int(seriesCount))}
		for range seriesCount {
			metric, series, err := decodeSnapshotSeries(reader, budget, now, target.ttl+compactHistoryRetentionMinutes*time.Minute)
			if err != nil {
				return nil, fmt.Errorf("instrument %s: %w", key, err)
			}
			if _, duplicate := history.series[metric]; duplicate {
				return nil, fmt.Errorf("%w: duplicate series %q", ErrUnifiedSnapshotInvalidState, metric)
			}
			history.series[metric] = series
		}
		if err := validateSnapshotHistoryPlan(key, history, dependencies); err != nil {
			return nil, err
		}
		state.instruments[key] = &unifiedInstrumentHistory{history: history, lastSeen: lastSeen}
	}

	catalogMax, catalogTTL := target.catalogs.maxEntries, target.catalogs.ttl
	state.catalogs = newCatalogRegistry(catalogMax, catalogTTL)
	catalogCount, err := reader.uint32()
	if err != nil {
		return nil, err
	}
	if int(catalogCount) > catalogMax {
		return nil, fmt.Errorf("%w: catalog count exceeds configured maximum", ErrUnifiedSnapshotInvalidState)
	}
	for range catalogCount {
		version, err := reader.string(256)
		if err != nil {
			return nil, err
		}
		lastSeenNano, err := reader.int64()
		if err != nil || lastSeenNano <= 0 {
			return nil, fmt.Errorf("%w: invalid catalog timestamp", ErrUnifiedSnapshotInvalidState)
		}
		lastSeen := time.Unix(0, lastSeenNano).UTC()
		if err := validateSnapshotTimestamp(lastSeen, now, catalogTTL); err != nil {
			return nil, fmt.Errorf("%w: invalid catalog timestamp: %v", ErrUnifiedSnapshotInvalidState, err)
		}
		symbolCount, err := reader.uint32()
		if err != nil {
			return nil, err
		}
		if symbolCount == 0 || symbolCount > compactMaxCatalogSize {
			return nil, fmt.Errorf("%w: invalid catalog symbol count", ErrUnifiedSnapshotInvalidState)
		}
		if uint64(symbolCount) > unifiedSnapshotMaxCatalogSymbols-budget.catalogSymbols {
			return nil, fmt.Errorf("%w: aggregate catalog symbol budget exceeded", ErrUnifiedSnapshotTooLarge)
		}
		budget.catalogSymbols += uint64(symbolCount)
		symbols := make([]string, int(symbolCount))
		for index := range symbols {
			symbols[index], err = reader.string(256)
			if err != nil {
				return nil, err
			}
		}
		if _, duplicate := state.catalogs.entries[version]; duplicate {
			return nil, fmt.Errorf("%w: duplicate catalog %q", ErrUnifiedSnapshotInvalidState, version)
		}
		normalized, calculated := compactCatalog(symbols)
		if version == "" || calculated != version || !equalStringSlices(normalized, symbols) {
			return nil, fmt.Errorf("%w: catalog content does not match version", ErrUnifiedSnapshotInvalidState)
		}
		state.catalogs.entries[version] = catalogRecord{symbols: symbols, lastSeen: lastSeen}
	}

	coverageMax, coverageTTL := target.coverage.maxEntries, target.coverage.ttl
	state.coverage = newCoverageTracker(state.catalogs, coverageMax, coverageTTL)
	coverageCount, err := reader.uint32()
	if err != nil {
		return nil, err
	}
	if int(coverageCount) > coverageMax {
		return nil, fmt.Errorf("%w: coverage count exceeds configured maximum", ErrUnifiedSnapshotInvalidState)
	}
	for range coverageCount {
		key, err := reader.string(unifiedSnapshotMaxStringBytes)
		if err != nil {
			return nil, err
		}
		if err := validateSnapshotCoverageKeyAt(key, now, coverageTTL+compactHistoryRetentionMinutes*time.Minute); err != nil {
			return nil, err
		}
		if _, duplicate := state.coverage.minutes[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate coverage %q", ErrUnifiedSnapshotInvalidState, key)
		}
		shardTotal, err := reader.uint32()
		if err != nil || shardTotal == 0 || shardTotal > unifiedSnapshotMaxShards {
			return nil, fmt.Errorf("%w: invalid shard total", ErrUnifiedSnapshotInvalidState)
		}
		lastSeenNano, err := reader.int64()
		if err != nil || lastSeenNano <= 0 {
			return nil, fmt.Errorf("%w: invalid coverage timestamp", ErrUnifiedSnapshotInvalidState)
		}
		lastSeen := time.Unix(0, lastSeenNano).UTC()
		if err := validateSnapshotTimestamp(lastSeen, now, coverageTTL); err != nil {
			return nil, fmt.Errorf("%w: invalid coverage timestamp: %v", ErrUnifiedSnapshotInvalidState, err)
		}
		shardCount, err := reader.uint32()
		if err != nil || shardCount > shardTotal {
			return nil, fmt.Errorf("%w: invalid coverage shard count", ErrUnifiedSnapshotInvalidState)
		}
		if uint64(shardCount) > unifiedSnapshotMaxCoverageShards-budget.coverageShards {
			return nil, fmt.Errorf("%w: aggregate coverage shard budget exceeded", ErrUnifiedSnapshotTooLarge)
		}
		budget.coverageShards += uint64(shardCount)
		minute := &minuteCoverage{shardTotal: int(shardTotal), shards: make(map[int]shardCoverage, int(shardCount)), lastSeen: lastSeen}
		for range shardCount {
			index, err := reader.uint32()
			if err != nil || index >= shardTotal {
				return nil, fmt.Errorf("%w: invalid shard index", ErrUnifiedSnapshotInvalidState)
			}
			if _, duplicate := minute.shards[int(index)]; duplicate {
				return nil, fmt.Errorf("%w: duplicate shard index", ErrUnifiedSnapshotInvalidState)
			}
			batchID, err := reader.string(512)
			if err != nil {
				return nil, err
			}
			catalogVersion, err := reader.string(256)
			if err != nil {
				return nil, err
			}
			covered, err := reader.boolean()
			if err != nil {
				return nil, err
			}
			exceptionsArePresent, err := reader.boolean()
			if err != nil {
				return nil, err
			}
			exceptionCount, err := reader.uint32()
			if err != nil || exceptionCount > compactMaxCatalogSize {
				return nil, fmt.Errorf("%w: invalid candle exception count", ErrUnifiedSnapshotInvalidState)
			}
			if uint64(exceptionCount) > unifiedSnapshotMaxExceptions-budget.exceptions {
				return nil, fmt.Errorf("%w: aggregate candle exception budget exceeded", ErrUnifiedSnapshotTooLarge)
			}
			budget.exceptions += uint64(exceptionCount)
			exceptions := make([]string, int(exceptionCount))
			for exceptionIndex := range exceptions {
				exceptions[exceptionIndex], err = reader.string(256)
				if err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(batchID) == "" {
				return nil, fmt.Errorf("%w: empty coverage batch id", ErrUnifiedSnapshotInvalidState)
			}
			if _, ok := state.catalogs.entries[catalogVersion]; !ok {
				return nil, fmt.Errorf("%w: coverage references unknown catalog", ErrUnifiedSnapshotInvalidState)
			}
			shard := shardCoverage{
				batchID: batchID, catalogVersion: catalogVersion, coverage: covered,
				candleExceptions: exceptions, candleExceptionsArePresent: exceptionsArePresent,
			}
			if err := validateSnapshotCandleExceptions(key[:1], shard, state.catalogs.entries[catalogVersion].symbols); err != nil {
				return nil, err
			}
			minute.shards[int(index)] = shard
		}
		state.coverage.minutes[key] = minute
	}

	streamCount, err := reader.uint32()
	if err != nil {
		return nil, err
	}
	if streamCount > unifiedSnapshotMaxStreams {
		return nil, fmt.Errorf("%w: too many stream high-water records", ErrUnifiedSnapshotInvalidState)
	}
	for range streamCount {
		stream, err := reader.string(512)
		if err != nil {
			return nil, err
		}
		messageID, err := reader.string(128)
		if err != nil {
			return nil, err
		}
		batchMinute, err := reader.int64()
		if err != nil {
			return nil, err
		}
		batchID, err := reader.string(512)
		if err != nil {
			return nil, err
		}
		skipDigest, err := reader.string(128)
		if err != nil {
			return nil, err
		}
		skipped, err := reader.uint64()
		if err != nil {
			return nil, err
		}
		observedNano, err := reader.int64()
		if err != nil {
			return nil, err
		}
		readyNano, err := reader.int64()
		if err != nil {
			return nil, err
		}
		bootstrapped, err := reader.boolean()
		if err != nil {
			return nil, err
		}
		water := compactStreamHighWater{
			MessageID: messageID, BatchMinute: batchMinute, BatchID: batchID, SkipDigest: skipDigest, Skipped: skipped,
			ObservedAt: time.Unix(0, observedNano).UTC(), ReadyAt: time.Unix(0, readyNano).UTC(), Bootstrapped: bootstrapped,
		}
		if err := validateCompactStreamHighWater(stream, water); err != nil {
			return nil, err
		}
		if err := validateCompactStreamHighWaterAt(water, now); err != nil {
			return nil, err
		}
		if err := validateSnapshotNotFuture(water.ObservedAt, now); err != nil {
			return nil, fmt.Errorf("%w: invalid stream observation time: %v", ErrUnifiedSnapshotInvalidState, err)
		}
		if err := validateSnapshotReadyAt(water.ReadyAt, now); err != nil {
			return nil, fmt.Errorf("%w: invalid stream readiness time: %v", ErrUnifiedSnapshotInvalidState, err)
		}
		if _, duplicate := state.highWater[stream]; duplicate {
			return nil, fmt.Errorf("%w: duplicate stream %q", ErrUnifiedSnapshotInvalidState, stream)
		}
		state.highWater[stream] = water
	}
	return state, nil
}

func validateSnapshotHistoryPlan(key string, history *sparseHistory, dependencies *historyDependencyIndex) error {
	if history == nil || dependencies == nil {
		return fmt.Errorf("%w: nil history dependency state", ErrUnifiedSnapshotInvalidState)
	}
	exchange, marketType, symbol, ok := splitInstrumentKey(key)
	if !ok {
		return fmt.Errorf("%w: invalid instrument %q", ErrUnifiedSnapshotInvalidState, key)
	}
	plan := dependencies.Plan(exchange, marketType, symbol)
	if len(plan) == 0 || len(history.series) != len(plan) {
		return fmt.Errorf("%w: instrument %q series do not match rule dependencies", ErrUnifiedSnapshotInvalidState, key)
	}
	for metric, capacity := range plan {
		series := history.series[metric]
		if series == nil || series.Capacity() != capacity {
			return fmt.Errorf("%w: instrument %q metric %q capacity does not match rule dependencies", ErrUnifiedSnapshotInvalidState, key, metric)
		}
	}
	return nil
}

func decodeSnapshotSeries(reader *snapshotReader, budget *snapshotDecodeBudget, now time.Time, maximumAge time.Duration) (string, *sparseSeries, error) {
	metric, err := reader.string(128)
	if err != nil {
		return "", nil, err
	}
	if !isHistoryMetric(metric) {
		return "", nil, fmt.Errorf("%w: invalid metric %q", ErrUnifiedSnapshotInvalidState, metric)
	}
	capacity, err := reader.uint32()
	if err != nil || capacity == 0 || capacity > compactHistoryRetentionMinutes {
		return "", nil, fmt.Errorf("%w: invalid series capacity", ErrUnifiedSnapshotInvalidState)
	}
	if budget == nil {
		return "", nil, fmt.Errorf("%w: nil decode budget", ErrUnifiedSnapshotInvalidState)
	}
	if err := budget.addSeries(capacity); err != nil {
		return "", nil, err
	}
	lastMinute, err := reader.int64()
	if err != nil {
		return "", nil, err
	}
	hasLast, err := reader.boolean()
	if err != nil {
		return "", nil, err
	}
	pointCount, err := reader.uint32()
	if err != nil || pointCount > capacity || (!hasLast && pointCount != 0) || (hasLast && (lastMinute <= 0 || pointCount == 0)) {
		return "", nil, fmt.Errorf("%w: invalid sparse point count", ErrUnifiedSnapshotInvalidState)
	}
	if hasLast {
		if err := validateSnapshotMinuteKey(lastMinute, now, maximumAge); err != nil {
			return "", nil, fmt.Errorf("%w: invalid sparse last minute: %v", ErrUnifiedSnapshotInvalidState, err)
		}
	}
	series := &sparseSeries{
		minuteKeys: make([]int64, int(capacity)),
		values:     make([]float64, int(capacity)),
		occupied:   make([]uint64, sparseWordCount(int(capacity))),
		valid:      make([]uint64, sparseWordCount(int(capacity))),
		lastMinute: lastMinute,
		hasLast:    hasLast,
	}
	sawLast := false
	for range pointCount {
		minute, err := reader.int64()
		if err != nil {
			return "", nil, err
		}
		valueBits, err := reader.uint64()
		if err != nil {
			return "", nil, err
		}
		valid, err := reader.boolean()
		if err != nil {
			return "", nil, err
		}
		value := math.Float64frombits(valueBits)
		if minute <= 0 || (hasLast && (minute > lastMinute || minute < lastMinute-int64(capacity)+1)) || (valid && (math.IsNaN(value) || math.IsInf(value, 0))) {
			return "", nil, fmt.Errorf("%w: invalid sparse point", ErrUnifiedSnapshotInvalidState)
		}
		if err := validateSnapshotMinuteKey(minute, now, maximumAge); err != nil {
			return "", nil, fmt.Errorf("%w: invalid sparse point minute: %v", ErrUnifiedSnapshotInvalidState, err)
		}
		if minute == lastMinute {
			sawLast = true
		}
		index := ringIndex(minute, int(capacity))
		if sparseBit(series.occupied, index) {
			return "", nil, fmt.Errorf("%w: sparse points collide", ErrUnifiedSnapshotInvalidState)
		}
		series.minuteKeys[index] = minute
		series.values[index] = value
		setSparseBit(series.occupied, index, true)
		setSparseBit(series.valid, index, valid)
	}
	if hasLast && !sawLast {
		return "", nil, fmt.Errorf("%w: sparse series does not contain last minute", ErrUnifiedSnapshotInvalidState)
	}
	return metric, series, nil
}

func (h *sparseEngineState) installSnapshotState(state *decodedUnifiedSnapshot) {
	if h == nil || state == nil {
		return
	}
	h.snapshotMu.Lock()
	defer h.snapshotMu.Unlock()
	h.mu.Lock()
	h.instruments = state.instruments
	h.streamHighWater = state.highWater
	h.streamProgress = make(map[string]*compactStreamProgress)
	h.bootstrapFingerprint = state.dependencyFingerprint
	h.lastCleanup = time.Time{}
	h.ruleGeneration = 0
	h.dependencies = newHistoryDependencyIndex(nil, h.includeImportant)
	h.catalogs = state.catalogs
	h.coverage = state.coverage
	h.mu.Unlock()
}

func validateSnapshotInstrumentKey(key string) error {
	exchange, marketType, symbol, ok := splitInstrumentKey(key)
	if !ok || instrumentCacheKey(exchange, marketType, symbol) != key {
		return fmt.Errorf("%w: non-canonical instrument key %q", ErrUnifiedSnapshotInvalidState, key)
	}
	return nil
}

func validateSnapshotCoverageKey(key string) error {
	parts := strings.Split(key, ":")
	if len(parts) != 4 || (parts[0] != "c" && parts[0] != "l") || normalizeExchange(parts[1]) != parts[1] || normalizeMarketType(parts[2]) != parts[2] {
		return fmt.Errorf("%w: invalid coverage key %q", ErrUnifiedSnapshotInvalidState, key)
	}
	minute, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || minute <= 0 || coverageMinuteKey(parts[0], parts[1], parts[2], minute) != key {
		return fmt.Errorf("%w: invalid coverage key %q", ErrUnifiedSnapshotInvalidState, key)
	}
	return nil
}

func validateSnapshotCoverageKeyAt(key string, now time.Time, maximumAge time.Duration) error {
	if err := validateSnapshotCoverageKey(key); err != nil {
		return err
	}
	parts := strings.Split(key, ":")
	minute, _ := strconv.ParseInt(parts[3], 10, 64)
	if minute > math.MaxInt64/1_000_000 {
		return fmt.Errorf("%w: coverage minute overflows time", ErrUnifiedSnapshotInvalidState)
	}
	if err := validateSnapshotTimestamp(time.UnixMilli(minute).UTC(), now, maximumAge); err != nil {
		return fmt.Errorf("%w: invalid coverage minute: %v", ErrUnifiedSnapshotInvalidState, err)
	}
	return nil
}

func validateSnapshotCandleExceptions(kind string, shard shardCoverage, catalog []string) error {
	if kind != "c" || shard.coverage {
		if shard.candleExceptionsArePresent || len(shard.candleExceptions) != 0 {
			return fmt.Errorf("%w: candle exceptions are invalid for covered/non-candle shard", ErrUnifiedSnapshotInvalidState)
		}
		return nil
	}
	if len(shard.candleExceptions)*2 > len(catalog) {
		return fmt.Errorf("%w: candle exception list is not the smaller catalog side", ErrUnifiedSnapshotInvalidState)
	}
	if len(shard.candleExceptions)*2 == len(catalog) && !shard.candleExceptionsArePresent {
		return fmt.Errorf("%w: candle exception tie must use canonical present side", ErrUnifiedSnapshotInvalidState)
	}
	previous := ""
	for _, symbol := range shard.candleExceptions {
		if symbol == "" || symbol != strings.ToUpper(strings.TrimSpace(symbol)) || (previous != "" && symbol <= previous) || !sortedCatalogContains(catalog, symbol) {
			return fmt.Errorf("%w: candle exceptions must be sorted, unique catalog members", ErrUnifiedSnapshotInvalidState)
		}
		previous = symbol
	}
	return nil
}

func validateSnapshotTimestamp(timestamp, now time.Time, maximumAge time.Duration) error {
	if timestamp.IsZero() || now.IsZero() || maximumAge <= 0 {
		return errors.New("zero timestamp or invalid maximum age")
	}
	timestamp, now = timestamp.UTC(), now.UTC()
	if timestamp.After(now.Add(unifiedSnapshotFutureTolerance)) {
		return errors.New("timestamp is too far in the future")
	}
	if timestamp.Before(now.Add(-maximumAge)) {
		return errors.New("timestamp is older than retention")
	}
	return nil
}

func validateSnapshotMinuteKey(minute int64, now time.Time, maximumAge time.Duration) error {
	if minute <= 0 || minute > math.MaxInt64/60 {
		return errors.New("minute key is outside time range")
	}
	return validateSnapshotTimestamp(time.Unix(minute*60, 0).UTC(), now, maximumAge)
}

func validateSnapshotNotFuture(timestamp, now time.Time) error {
	if timestamp.IsZero() || now.IsZero() {
		return errors.New("zero timestamp")
	}
	if timestamp.UTC().After(now.UTC().Add(unifiedSnapshotFutureTolerance)) {
		return errors.New("timestamp is too far in the future")
	}
	return nil
}

func validateSnapshotReadyAt(readyAt, now time.Time) error {
	return validateSnapshotNotFuture(readyAt, now.Add(compactHistoryRetentionMinutes*time.Minute))
}

func validateCompactStreamHighWater(stream string, water compactStreamHighWater) error {
	if strings.TrimSpace(stream) == "" || stream != strings.TrimSpace(stream) || len(stream) > 512 || water.ObservedAt.IsZero() || water.ReadyAt.IsZero() || !water.Bootstrapped {
		return fmt.Errorf("%w: invalid stream high-water metadata", ErrUnifiedSnapshotInvalidState)
	}
	if _, _, ok := parseRedisStreamID(water.MessageID); !ok {
		return fmt.Errorf("%w: invalid redis stream id %q", ErrUnifiedSnapshotInvalidState, water.MessageID)
	}
	if water.MessageID == "0-0" {
		if water.BatchMinute != 0 || water.BatchID != "" || water.SkipDigest != "" || water.Skipped != 0 {
			return fmt.Errorf("%w: zero stream offset has batch metadata", ErrUnifiedSnapshotInvalidState)
		}
	} else {
		batchCheckpoint := water.BatchMinute > 0 && water.BatchMinute%60_000 == 0 && strings.TrimSpace(water.BatchID) != "" && water.BatchID == strings.TrimSpace(water.BatchID) && water.SkipDigest == ""
		skipCheckpoint := water.BatchMinute == 0 && water.BatchID == "" && water.SkipDigest == strings.ToLower(water.SkipDigest) && validSHA256Hex(water.SkipDigest) && water.Skipped > 0
		if batchCheckpoint == skipCheckpoint {
			return fmt.Errorf("%w: nonzero stream offset must contain exactly one checkpoint kind", ErrUnifiedSnapshotInvalidState)
		}
	}
	return nil
}

func validateCompactStreamHighWaterAt(water compactStreamHighWater, now time.Time) error {
	milliseconds, _, ok := parseRedisStreamID(water.MessageID)
	if !ok || milliseconds > math.MaxInt64 {
		return fmt.Errorf("%w: invalid stream message time", ErrUnifiedSnapshotInvalidState)
	}
	if water.MessageID != "0-0" && time.UnixMilli(int64(milliseconds)).UTC().After(now.UTC().Add(unifiedSnapshotFutureTolerance)) {
		return fmt.Errorf("%w: stream message id is too far in the future", ErrUnifiedSnapshotInvalidState)
	}
	if water.BatchMinute > 0 && time.UnixMilli(water.BatchMinute).UTC().After(now.UTC().Add(unifiedSnapshotFutureTolerance)) {
		return fmt.Errorf("%w: stream batch minute is too far in the future", ErrUnifiedSnapshotInvalidState)
	}
	return nil
}

func parseRedisStreamID(value string) (uint64, uint64, bool) {
	parts := strings.Split(value, "-")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, false
	}
	milliseconds, errMilliseconds := strconv.ParseUint(parts[0], 10, 64)
	sequence, errSequence := strconv.ParseUint(parts[1], 10, 64)
	if errMilliseconds != nil || errSequence != nil {
		return 0, 0, false
	}
	if strconv.FormatUint(milliseconds, 10) != parts[0] || strconv.FormatUint(sequence, 10) != parts[1] {
		return 0, 0, false
	}
	return milliseconds, sequence, true
}

func compareRedisStreamIDs(left, right string) (int, bool) {
	leftMilliseconds, leftSequence, leftOK := parseRedisStreamID(left)
	rightMilliseconds, rightSequence, rightOK := parseRedisStreamID(right)
	if !leftOK || !rightOK {
		return 0, false
	}
	if leftMilliseconds < rightMilliseconds || (leftMilliseconds == rightMilliseconds && leftSequence < rightSequence) {
		return -1, true
	}
	if leftMilliseconds > rightMilliseconds || (leftMilliseconds == rightMilliseconds && leftSequence > rightSequence) {
		return 1, true
	}
	return 0, true
}

func writeUnifiedSnapshotHeader(writer io.Writer, header unifiedSnapshotHeader) error {
	buffer := make([]byte, unifiedSnapshotHeaderSize)
	copy(buffer[:8], unifiedSnapshotMagic[:])
	binary.LittleEndian.PutUint32(buffer[8:12], header.Version)
	// Bytes 12:16 are reserved and must remain zero.
	binary.LittleEndian.PutUint64(buffer[16:24], uint64(header.CreatedAtNano))
	binary.LittleEndian.PutUint64(buffer[24:32], header.PayloadLength)
	copy(buffer[32:64], header.Checksum[:])
	_, err := writer.Write(buffer)
	return err
}

func readUnifiedSnapshotHeader(reader io.Reader) (unifiedSnapshotHeader, error) {
	buffer := make([]byte, unifiedSnapshotHeaderSize)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		return unifiedSnapshotHeader{}, fmt.Errorf("%w: read header: %v", ErrUnifiedSnapshotMalformed, err)
	}
	if subtle.ConstantTimeCompare(buffer[:8], unifiedSnapshotMagic[:]) != 1 {
		return unifiedSnapshotHeader{}, fmt.Errorf("%w: invalid magic", ErrUnifiedSnapshotMalformed)
	}
	version := binary.LittleEndian.Uint32(buffer[8:12])
	if version != unifiedSnapshotSchemaVersion {
		return unifiedSnapshotHeader{}, fmt.Errorf("%w: got %d want %d", ErrUnifiedSnapshotVersion, version, unifiedSnapshotSchemaVersion)
	}
	if binary.LittleEndian.Uint32(buffer[12:16]) != 0 {
		return unifiedSnapshotHeader{}, fmt.Errorf("%w: reserved header bits are set", ErrUnifiedSnapshotMalformed)
	}
	createdAt := int64(binary.LittleEndian.Uint64(buffer[16:24]))
	if createdAt <= 0 {
		return unifiedSnapshotHeader{}, fmt.Errorf("%w: invalid creation timestamp", ErrUnifiedSnapshotMalformed)
	}
	return unifiedSnapshotHeader{
		Version:       version,
		CreatedAtNano: createdAt,
		PayloadLength: binary.LittleEndian.Uint64(buffer[24:32]),
		Checksum:      [sha256.Size]byte(buffer[32:64]),
	}, nil
}

func unifiedSnapshotChecksum(version uint32, createdAtNano int64, payloadLength uint64, payloadDigest []byte) [sha256.Size]byte {
	hasher := sha256.New()
	_, _ = hasher.Write(unifiedSnapshotMagic[:])
	var metadata [20]byte
	binary.LittleEndian.PutUint32(metadata[0:4], version)
	binary.LittleEndian.PutUint64(metadata[4:12], uint64(createdAtNano))
	binary.LittleEndian.PutUint64(metadata[12:20], payloadLength)
	_, _ = hasher.Write(metadata[:])
	_, _ = hasher.Write(payloadDigest)
	var checksum [sha256.Size]byte
	copy(checksum[:], hasher.Sum(nil))
	return checksum
}

type snapshotCountingWriter struct {
	writer io.Writer
	count  uint64
}

func (w *snapshotCountingWriter) Write(payload []byte) (int, error) {
	written, err := w.writer.Write(payload)
	w.count += uint64(written)
	if w.count > uint64(unifiedSnapshotMaxFileBytes-unifiedSnapshotHeaderSize) {
		return written, ErrUnifiedSnapshotTooLarge
	}
	return written, err
}

type snapshotReader struct {
	reader    io.Reader
	remaining uint64
}

func (r *snapshotReader) readFull(destination []byte) error {
	if uint64(len(destination)) > r.remaining {
		return fmt.Errorf("%w: truncated payload", ErrUnifiedSnapshotMalformed)
	}
	if _, err := io.ReadFull(r.reader, destination); err != nil {
		return fmt.Errorf("%w: read payload: %v", ErrUnifiedSnapshotMalformed, err)
	}
	r.remaining -= uint64(len(destination))
	return nil
}

func (r *snapshotReader) uint32() (uint32, error) {
	var buffer [4]byte
	if err := r.readFull(buffer[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buffer[:]), nil
}

func (r *snapshotReader) uint64() (uint64, error) {
	var buffer [8]byte
	if err := r.readFull(buffer[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(buffer[:]), nil
}

func (r *snapshotReader) int64() (int64, error) {
	value, err := r.uint64()
	return int64(value), err
}

func (r *snapshotReader) boolean() (bool, error) {
	var buffer [1]byte
	if err := r.readFull(buffer[:]); err != nil {
		return false, err
	}
	switch buffer[0] {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("%w: invalid boolean", ErrUnifiedSnapshotMalformed)
	}
}

func (r *snapshotReader) string(maximum int) (string, error) {
	length, err := r.uint32()
	if err != nil {
		return "", err
	}
	if maximum < 0 || uint64(length) > uint64(maximum) {
		return "", fmt.Errorf("%w: string length %d exceeds maximum %d", ErrUnifiedSnapshotInvalidState, length, maximum)
	}
	buffer := make([]byte, int(length))
	if err := r.readFull(buffer); err != nil {
		return "", err
	}
	return string(buffer), nil
}

func writeSnapshotUint32(writer io.Writer, value int) error {
	if value < 0 || uint64(value) > math.MaxUint32 {
		return fmt.Errorf("%w: integer does not fit uint32", ErrUnifiedSnapshotInvalidState)
	}
	var buffer [4]byte
	binary.LittleEndian.PutUint32(buffer[:], uint32(value))
	_, err := writer.Write(buffer[:])
	return err
}

func writeSnapshotUint64(writer io.Writer, value uint64) error {
	var buffer [8]byte
	binary.LittleEndian.PutUint64(buffer[:], value)
	_, err := writer.Write(buffer[:])
	return err
}

func writeSnapshotInt64(writer io.Writer, value int64) error {
	return writeSnapshotUint64(writer, uint64(value))
}

func writeSnapshotBool(writer io.Writer, value bool) error {
	var encoded byte
	if value {
		encoded = 1
	}
	_, err := writer.Write([]byte{encoded})
	return err
}

func writeSnapshotString(writer io.Writer, value string, maximum int) error {
	if len(value) > maximum {
		return fmt.Errorf("%w: string exceeds maximum length", ErrUnifiedSnapshotInvalidState)
	}
	if err := writeSnapshotUint32(writer, len(value)); err != nil {
		return err
	}
	_, err := io.WriteString(writer, value)
	return err
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
