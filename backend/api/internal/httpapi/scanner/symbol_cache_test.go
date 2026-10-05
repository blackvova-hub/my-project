package scannerapi

import (
	"testing"
	"time"
)

func TestSymbolCacheDeletesExpiredEntryOnRead(t *testing.T) {
	now := time.Now()
	a := &API{symbolCache: map[string]symbolCacheEntry{
		"bybit:perpetual:OLDUSDT": {Exists: true, CheckedAt: now.Add(-symbolCacheTTL - time.Second), LastAccessed: now.Add(-time.Minute)},
	}}

	if _, ok := a.getSymbolCache("bybit:perpetual:OLDUSDT"); ok {
		t.Fatal("expired cache entry was returned")
	}
	if len(a.symbolCache) != 0 {
		t.Fatalf("expired cache entry was not removed: %#v", a.symbolCache)
	}
}

func TestSymbolCacheSweepExpiresThenEvictsLeastRecentlyUsed(t *testing.T) {
	now := time.Now()
	a := &API{symbolCache: map[string]symbolCacheEntry{
		"expired": {CheckedAt: now.Add(-symbolCacheTTL - time.Second), LastAccessed: now},
		"old":     {CheckedAt: now, LastAccessed: now.Add(-2 * time.Minute)},
		"new":     {CheckedAt: now, LastAccessed: now.Add(-time.Minute)},
	}}

	a.sweepSymbolCacheLocked(now, 1)

	if len(a.symbolCache) != 1 {
		t.Fatalf("cache size=%d want=1", len(a.symbolCache))
	}
	if _, ok := a.symbolCache["new"]; !ok {
		t.Fatalf("least-recently-used eviction kept wrong entry: %#v", a.symbolCache)
	}
}
