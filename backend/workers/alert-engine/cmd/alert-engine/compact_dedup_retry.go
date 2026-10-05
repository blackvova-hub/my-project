package main

import (
	"crypto/sha256"
	"encoding/hex"
)

// Forget removes only the exact payload reservation made by Observe. It is
// used when admission failed transiently, so a later delivery is evaluated
// again instead of being mistaken for an already-applied duplicate.
func (d *compactBatchDeduper) Forget(batchID string, payload []byte) bool {
	if d == nil {
		return false
	}
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:16])
	d.mu.Lock()
	defer d.mu.Unlock()
	record, exists := d.entries[batchID]
	if !exists || record.digest != digest {
		return false
	}
	delete(d.entries, batchID)
	return true
}
