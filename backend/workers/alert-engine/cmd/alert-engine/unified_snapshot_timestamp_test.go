package main

import (
	"bytes"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"
)

func TestDecodeSnapshotSeriesRejectsHostileMinutes(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	maximumAge := 54 * time.Hour
	for name, minute := range map[string]int64{
		"future": now.Add(unifiedSnapshotFutureTolerance+time.Minute).Unix() / 60,
		"stale":  now.Add(-maximumAge-time.Minute).Unix() / 60,
	} {
		t.Run(name, func(t *testing.T) {
			var payload bytes.Buffer
			_ = writeSnapshotString(&payload, "close", 128)
			_ = writeSnapshotUint32(&payload, 1)
			_ = writeSnapshotInt64(&payload, minute)
			_ = writeSnapshotBool(&payload, true)
			_ = writeSnapshotUint32(&payload, 1)
			_ = writeSnapshotInt64(&payload, minute)
			_ = writeSnapshotUint64(&payload, math.Float64bits(1))
			_ = writeSnapshotBool(&payload, true)
			reader := &snapshotReader{reader: bytes.NewReader(payload.Bytes()), remaining: uint64(payload.Len())}
			if _, _, err := decodeSnapshotSeries(reader, newSnapshotDecodeBudget(), now, maximumAge); !errors.Is(err, ErrUnifiedSnapshotInvalidState) {
				t.Fatalf("hostile minute error=%v", err)
			}
		})
	}
}

func TestSnapshotCoverageAndHighWaterRejectFuturePayloadTime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Minute)
	future := now.Add(unifiedSnapshotFutureTolerance + time.Minute).UnixMilli()
	key := coverageMinuteKey("c", "bybit", "perpetual", future)
	if err := validateSnapshotCoverageKeyAt(key, now, 54*time.Hour); !errors.Is(err, ErrUnifiedSnapshotInvalidState) {
		t.Fatalf("future coverage key error=%v", err)
	}
	water := compactStreamHighWater{
		MessageID: strconv.FormatInt(future, 10) + "-0", BatchMinute: future, BatchID: "batch",
		ObservedAt: now, ReadyAt: now, Bootstrapped: true,
	}
	if err := validateCompactStreamHighWater("stream", water); err != nil {
		t.Fatalf("structural high-water validation failed before timestamp check: %v", err)
	}
	if err := validateCompactStreamHighWaterAt(water, now); !errors.Is(err, ErrUnifiedSnapshotInvalidState) {
		t.Fatalf("future high-water timestamp error=%v", err)
	}
}
