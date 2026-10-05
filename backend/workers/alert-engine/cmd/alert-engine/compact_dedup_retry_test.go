package main

import (
	"testing"
	"time"
)

func TestCompactBatchDeduperForgetOnlyExactTransientReservation(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	deduper := newCompactBatchDeduper(time.Minute, 10)
	if got := deduper.Observe("batch", []byte("payload"), now); got != compactBatchNew {
		t.Fatalf("first observation=%d", got)
	}
	if deduper.Forget("batch", []byte("different")) {
		t.Fatal("different payload removed reservation")
	}
	if got := deduper.Observe("batch", []byte("payload"), now); got != compactBatchDuplicate {
		t.Fatalf("reservation disappeared after mismatched Forget: %d", got)
	}
	if !deduper.Forget("batch", []byte("payload")) {
		t.Fatal("exact transient reservation was not removed")
	}
	if got := deduper.Observe("batch", []byte("payload"), now); got != compactBatchNew {
		t.Fatalf("retry result=%d want new", got)
	}
}
