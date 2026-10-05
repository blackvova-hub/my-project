package main

import (
	"testing"
	"time"
)

func TestCompactBatchDeduperDistinguishesDuplicateAndConflict(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	deduper := newCompactBatchDeduper(10*time.Minute, 10)
	if got := deduper.Observe("batch", []byte("one"), now); got != compactBatchNew {
		t.Fatalf("first result=%d want new", got)
	}
	if got := deduper.Observe("batch", []byte("one"), now.Add(time.Second)); got != compactBatchDuplicate {
		t.Fatalf("same payload result=%d want duplicate", got)
	}
	if got := deduper.Observe("batch", []byte("two"), now.Add(2*time.Second)); got != compactBatchConflict {
		t.Fatalf("changed payload result=%d want conflict", got)
	}
}

func TestCompactBatchDeduperExpiresAndBoundsEntries(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	deduper := newCompactBatchDeduper(time.Minute, 2)
	deduper.Observe("one", []byte("1"), now)
	deduper.Observe("two", []byte("2"), now.Add(time.Second))
	deduper.Observe("three", []byte("3"), now.Add(2*time.Second))
	if got := deduper.Observe("one", []byte("1"), now.Add(3*time.Second)); got != compactBatchNew {
		t.Fatalf("evicted batch result=%d want new", got)
	}
	if got := deduper.Observe("one", []byte("1"), now.Add(2*time.Minute)); got != compactBatchNew {
		t.Fatalf("expired batch result=%d want new", got)
	}
}
