package main

import (
	"errors"
	"sync"
	"time"
)

type catalogRecord struct {
	symbols  []string
	lastSeen time.Time
}

// catalogRegistry stores each immutable catalog once. Minute coverage keeps
// only its version, avoiding a full symbol-list allocation per batch/minute.
type catalogRegistry struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	entries    map[string]catalogRecord
}

func newCatalogRegistry(maxEntries int, ttl time.Duration) *catalogRegistry {
	if maxEntries <= 0 {
		maxEntries = 2048
	}
	if ttl <= 0 {
		ttl = 27 * time.Hour
	}
	return &catalogRegistry{maxEntries: maxEntries, ttl: ttl, entries: make(map[string]catalogRecord)}
}

func (r *catalogRegistry) Register(version string, symbols []string, now time.Time) error {
	if r == nil {
		return errors.New("nil catalog registry")
	}
	normalized, calculated := compactCatalog(symbols)
	if version == "" || calculated != version || len(normalized) != len(symbols) {
		return errors.New("catalog content does not match version")
	}
	for index := range normalized {
		if normalized[index] != symbols[index] {
			return errors.New("catalog must be normalized, unique and sorted")
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(now)
	if current, ok := r.entries[version]; ok {
		if !equalStringSlices(current.symbols, symbols) {
			return errors.New("immutable catalog version changed content")
		}
		current.lastSeen = now
		r.entries[version] = current
		return nil
	}
	if len(r.entries) >= r.maxEntries {
		r.evictOldestLocked()
	}
	r.entries[version] = catalogRecord{symbols: append([]string(nil), symbols...), lastSeen: now}
	return nil
}

func (r *catalogRegistry) Lookup(version string, now time.Time) ([]string, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(now)
	record, ok := r.entries[version]
	if !ok {
		return nil, false
	}
	return append([]string(nil), record.symbols...), true
}

func (r *catalogRegistry) Contains(version, symbol string, now time.Time) (bool, bool) {
	if r == nil {
		return false, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(now)
	record, ok := r.entries[version]
	if !ok {
		return false, false
	}
	return sortedCatalogContains(record.symbols, symbol), true
}

func (r *catalogRegistry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

func (r *catalogRegistry) expireLocked(now time.Time) {
	cutoff := now.Add(-r.ttl)
	for version, record := range r.entries {
		if record.lastSeen.Before(cutoff) {
			delete(r.entries, version)
		}
	}
}

func (r *catalogRegistry) evictOldestLocked() {
	oldestVersion := ""
	var oldest time.Time
	for version, record := range r.entries {
		if oldestVersion == "" || record.lastSeen.Before(oldest) {
			oldestVersion, oldest = version, record.lastSeen
		}
	}
	if oldestVersion != "" {
		delete(r.entries, oldestVersion)
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
