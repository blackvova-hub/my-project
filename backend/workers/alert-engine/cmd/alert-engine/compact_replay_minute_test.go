package main

import (
	"testing"
	"time"
)

func TestValidateCompactReplayMinuteIncludesTruncatedRetentionBoundary(t *testing.T) {
	now := time.Date(2026, 8, 9, 14, 30, 59, 0, time.UTC)
	boundary := now.Add(-compactHistoryRetentionMinutes*time.Minute - compactReplayPastTolerance).Truncate(time.Minute)
	if err := validateCompactReplayMinute(boundary.UnixMilli(), now); err != nil {
		t.Fatalf("minute-aligned retention boundary rejected: %v", err)
	}
	if err := validateCompactReplayMinute(boundary.Add(-time.Minute).UnixMilli(), now); err == nil {
		t.Fatal("minute older than retention+tolerance was accepted")
	}
}
