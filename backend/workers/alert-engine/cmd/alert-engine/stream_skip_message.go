package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
)

func streamMessageSkipLedgerRecord(message redis.XMessage, sourceStream, group string, reason streamFailureReason) (streamSkipLedgerRecord, error) {
	payloadDigest, err := streamMessagePayloadSHA256(message)
	if err != nil {
		return streamSkipLedgerRecord{}, err
	}
	return buildStreamSkipLedgerRecord(sourceStream, group, message.ID, payloadDigest, reason)
}

func streamMessagePayloadSHA256(message redis.XMessage) (string, error) {
	raw, err := json.Marshal(message.Values)
	if err != nil {
		return "", fmt.Errorf("marshal stream message for skip ledger: %w", err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
