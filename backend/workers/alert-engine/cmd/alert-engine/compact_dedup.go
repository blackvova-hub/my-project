package main

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type compactDedupResult uint8

const (
	compactBatchNew compactDedupResult = iota
	compactBatchDuplicate
	compactBatchConflict
)

type compactDedupRecord struct {
	digest string
	seenAt time.Time
}

type compactBatchDeduper struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	entries    map[string]compactDedupRecord
}

func newCompactBatchDeduper(ttl time.Duration, maxEntries int) *compactBatchDeduper {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if maxEntries <= 0 {
		maxEntries = 10_000
	}
	return &compactBatchDeduper{ttl: ttl, maxEntries: maxEntries, entries: make(map[string]compactDedupRecord)}
}

func (d *compactBatchDeduper) Observe(batchID string, payload []byte, now time.Time) compactDedupResult {
	if d == nil {
		return compactBatchNew
	}
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:16])
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expireLocked(now)
	if current, ok := d.entries[batchID]; ok {
		if current.digest == digest {
			return compactBatchDuplicate
		}
		return compactBatchConflict
	}
	if len(d.entries) >= d.maxEntries {
		d.evictOldestLocked()
	}
	d.entries[batchID] = compactDedupRecord{digest: digest, seenAt: now}
	return compactBatchNew
}

func (d *compactBatchDeduper) expireLocked(now time.Time) {
	cutoff := now.Add(-d.ttl)
	for batchID, record := range d.entries {
		if record.seenAt.Before(cutoff) {
			delete(d.entries, batchID)
		}
	}
}

func (d *compactBatchDeduper) evictOldestLocked() {
	oldestID := ""
	var oldest time.Time
	for batchID, record := range d.entries {
		if oldestID == "" || record.seenAt.Before(oldest) {
			oldestID, oldest = batchID, record.seenAt
		}
	}
	if oldestID != "" {
		delete(d.entries, oldestID)
	}
}
