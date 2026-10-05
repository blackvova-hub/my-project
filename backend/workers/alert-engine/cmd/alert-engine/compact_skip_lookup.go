package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type compactSkipLedgerSource interface {
	GetCompactSkipLedger(context.Context, string) (string, error)
}

func (s *redisCompactBootstrapSource) GetCompactSkipLedger(ctx context.Context, key string) (string, error) {
	if s == nil || s.client == nil {
		return "", errors.New("nil redis compact skip ledger source")
	}
	return s.client.Get(ctx, key).Result()
}

func compactSkipLedgerKey(stream, group, messageID string) string {
	return streamSkipLedgerKey(stream, group, messageID)
}

func (s *redisCompactBootstrapSource) LookupSkipDigests(ctx context.Context, stream, group string, messages []redis.XMessage) (map[string]string, error) {
	result := make(map[string]string)
	if len(messages) == 0 {
		return result, nil
	}
	if s == nil || s.client == nil {
		return nil, errors.New("nil redis compact skip ledger source")
	}
	pipe := s.client.Pipeline()
	commands := make([]*redis.StringCmd, len(messages))
	payloadDigests := make([]string, len(messages))
	for index, message := range messages {
		payloadDigest, err := streamMessagePayloadSHA256(message)
		if err != nil {
			return nil, err
		}
		payloadDigests[index] = payloadDigest
		commands[index] = pipe.Get(ctx, streamSkipLedgerKey(stream, group, message.ID))
	}
	_, execErr := pipe.Exec(ctx)
	if execErr != nil && !errors.Is(execErr, redis.Nil) {
		return nil, fmt.Errorf("lookup compact skip ledgers: %w", execErr)
	}
	for index, command := range commands {
		value, err := command.Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lookup compact skip ledger %s: %w", messages[index].ID, err)
		}
		skipDigest, valid := validateStreamSkipLedgerValue(value, payloadDigests[index])
		if !valid {
			return nil, fmt.Errorf("compact skip ledger %s does not match source payload", messages[index].ID)
		}
		result[messages[index].ID] = skipDigest
	}
	return result, nil
}

func resolveBootstrapCompactSkip(ctx context.Context, source compactStreamBootstrapSource, stream, group string, message redis.XMessage) (string, bool, error) {
	ledgerSource, ok := source.(compactSkipLedgerSource)
	if !ok {
		return "", false, nil
	}
	raw, err := json.Marshal(message.Values)
	if err != nil {
		return "", false, fmt.Errorf("marshal compact message for skip lookup: %w", err)
	}
	payloadDigest := sha256.Sum256(raw)
	value, err := ledgerSource.GetCompactSkipLedger(ctx, compactSkipLedgerKey(stream, group, message.ID))
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read compact skip ledger: %w", err)
	}
	skipDigest, valid := validateStreamSkipLedgerValue(value, hex.EncodeToString(payloadDigest[:]))
	if !valid {
		return "", false, errors.New("compact skip ledger does not match source payload")
	}
	return skipDigest, true, nil
}
