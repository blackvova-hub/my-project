package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const (
	streamSkipLedgerPrefix = "compact:v3:skip:"
	streamSkipLedgerMinTTL = 30 * time.Hour
	streamSkipLedgerTTL    = 48 * time.Hour
)

type streamSkipLedgerRecord struct {
	Key        string
	Value      string
	SkipDigest string
	TTL        time.Duration
}

func buildStreamSkipLedgerRecord(sourceStream, group, sourceID, payloadSHA256 string, reason streamFailureReason) (streamSkipLedgerRecord, error) {
	sourceStream = strings.TrimSpace(sourceStream)
	group = strings.TrimSpace(group)
	sourceID = strings.TrimSpace(sourceID)
	payloadSHA256 = strings.ToLower(strings.TrimSpace(payloadSHA256))
	if sourceStream == "" || group == "" || sourceID == "" || !validSHA256Hex(payloadSHA256) || strings.TrimSpace(string(reason)) == "" {
		return streamSkipLedgerRecord{}, errors.New("invalid stream skip ledger identity")
	}
	reasonHash := sha256.Sum256([]byte(reason))
	value := "v1:" + payloadSHA256 + ":" + hex.EncodeToString(reasonHash[:])
	skipHash := sha256.Sum256([]byte(value))
	return streamSkipLedgerRecord{
		Key:        streamSkipLedgerKey(sourceStream, group, sourceID),
		Value:      value,
		SkipDigest: hex.EncodeToString(skipHash[:]),
		TTL:        streamSkipLedgerTTL,
	}, nil
}

func streamSkipLedgerKey(sourceStream, group, sourceID string) string {
	identity := sha256.Sum256([]byte(strings.TrimSpace(group) + "\x00" + strings.TrimSpace(sourceStream) + "\x00" + strings.TrimSpace(sourceID)))
	return streamSkipLedgerPrefix + hex.EncodeToString(identity[:])
}

// validateStreamSkipLedgerValue is shared by bootstrap: a poison message may
// be skipped only when the exact per-source ledger key exists and its payload
// digest matches the bytes read back from the immutable source stream.
func validateStreamSkipLedgerValue(value, payloadSHA256 string) (string, bool) {
	parts := strings.Split(value, ":")
	payloadSHA256 = strings.ToLower(strings.TrimSpace(payloadSHA256))
	if len(parts) != 3 || parts[0] != "v1" || parts[1] != strings.ToLower(parts[1]) || parts[2] != strings.ToLower(parts[2]) || !validSHA256Hex(parts[1]) || !validSHA256Hex(parts[2]) || !validSHA256Hex(payloadSHA256) {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(parts[1])), []byte(payloadSHA256)) != 1 {
		return "", false
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:]), true
}

func validSHA256Hex(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
