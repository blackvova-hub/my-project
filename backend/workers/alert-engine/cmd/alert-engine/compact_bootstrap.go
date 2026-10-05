package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const compactBootstrapPageSize int64 = 500

var (
	errCompactBootstrapOwnerRequired = errors.New("compact bootstrap requires an exclusive owner")
	errCompactBootstrapRulesNotReady = errors.New("compact bootstrap rules are not ready")
	errCompactBootstrapRulesChanged  = errors.New("compact bootstrap rules changed during replay")
)

type compactBootstrapOwner interface {
	AssertOwned(context.Context) error
}

type compactStreamBounds struct {
	FirstID string
	UpperID string
}

type compactStreamPage struct {
	FirstID  string
	Messages []redis.XMessage
}

type compactStreamBootstrapSource interface {
	CaptureBounds(ctx context.Context, stream string) (compactStreamBounds, error)
	RangePage(ctx context.Context, stream, startExclusive, endInclusive string, count int64) (compactStreamPage, error)
	LookupSkipDigests(ctx context.Context, stream, group string, messages []redis.XMessage) (map[string]string, error)
	SetGroupStart(ctx context.Context, stream, group, startID string) error
}

type redisCompactBootstrapSource struct {
	client redis.UniversalClient
}

func newRedisCompactBootstrapSource(client redis.UniversalClient) *redisCompactBootstrapSource {
	return &redisCompactBootstrapSource{client: client}
}

func (s *redisCompactBootstrapSource) CaptureBounds(ctx context.Context, stream string) (compactStreamBounds, error) {
	if s == nil || s.client == nil {
		return compactStreamBounds{}, errors.New("nil redis bootstrap source")
	}
	var latestCommand, earliestCommand *redis.XMessageSliceCmd
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		latestCommand = pipe.XRevRangeN(ctx, stream, "+", "-", 1)
		earliestCommand = pipe.XRangeN(ctx, stream, "-", "+", 1)
		return nil
	})
	if err != nil && !errors.Is(err, redis.Nil) {
		return compactStreamBounds{}, err
	}
	latest, latestErr := latestCommand.Result()
	earliest, earliestErr := earliestCommand.Result()
	if (latestErr != nil && !errors.Is(latestErr, redis.Nil)) || (earliestErr != nil && !errors.Is(earliestErr, redis.Nil)) {
		return compactStreamBounds{}, errors.Join(latestErr, earliestErr)
	}
	if len(latest) == 0 {
		return compactStreamBounds{FirstID: "0-0", UpperID: "0-0"}, nil
	}
	if len(earliest) == 0 {
		return compactStreamBounds{}, errors.New("stream disappeared while capturing bounds")
	}
	return compactStreamBounds{FirstID: earliest[0].ID, UpperID: latest[0].ID}, nil
}

func (s *redisCompactBootstrapSource) RangePage(ctx context.Context, stream, startExclusive, endInclusive string, count int64) (compactStreamPage, error) {
	if s == nil || s.client == nil {
		return compactStreamPage{}, errors.New("nil redis bootstrap source")
	}
	if count <= 0 {
		count = compactBootstrapPageSize
	}
	var firstCommand, pageCommand *redis.XMessageSliceCmd
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		firstCommand = pipe.XRangeN(ctx, stream, "-", "+", 1)
		pageCommand = pipe.XRangeN(ctx, stream, "("+startExclusive, endInclusive, count)
		return nil
	})
	if err != nil && !errors.Is(err, redis.Nil) {
		return compactStreamPage{}, err
	}
	first, firstErr := firstCommand.Result()
	messages, pageErr := pageCommand.Result()
	if (firstErr != nil && !errors.Is(firstErr, redis.Nil)) || (pageErr != nil && !errors.Is(pageErr, redis.Nil)) {
		return compactStreamPage{}, errors.Join(firstErr, pageErr)
	}
	firstID := "0-0"
	if len(first) > 0 {
		firstID = first[0].ID
	}
	return compactStreamPage{FirstID: firstID, Messages: messages}, nil
}

func (s *redisCompactBootstrapSource) SetGroupStart(ctx context.Context, stream, group, startID string) error {
	if s == nil || s.client == nil {
		return errors.New("nil redis bootstrap source")
	}
	err := s.client.XGroupCreateMkStream(ctx, stream, group, startID).Err()
	if err == nil {
		return nil
	}
	if !strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP") {
		return err
	}
	// SETID may leave old PEL entries. Those are safe duplicates: their IDs are
	// at or below the persisted contiguous checkpoint and sparse Put is replay
	// safe. We intentionally do not destroy a potentially shared group.
	return s.client.XGroupSetID(ctx, stream, group, startID).Err()
}

type compactStreamBootstrapResult struct {
	Stream             string
	SnapshotOffset     string
	FirstAvailableID   string
	CapturedUpperBound string
	Replayed           int
	ReadyAt            time.Time
	UsedSnapshot       bool
}

type compactBootstrapReport struct {
	Streams map[string]compactStreamBootstrapResult
	Ready   bool
}

// BootstrapCompactStreams captures an independent upper bound for every
// stream before replay begins, replays (snapshotOffset, capturedBound], and
// moves that stream's consumer group to exactly its captured bound. Redis has
// no cross-stream atomic barrier, so readiness is false until every configured
// per-stream barrier has completed successfully.
func BootstrapCompactStreams(
	ctx context.Context,
	source compactStreamBootstrapSource,
	owner compactBootstrapOwner,
	history *sparseEngineState,
	streams []string,
	group string,
	now time.Time,
) (compactBootstrapReport, error) {
	report := compactBootstrapReport{Streams: make(map[string]compactStreamBootstrapResult)}
	if source == nil || history == nil || strings.TrimSpace(group) == "" || len(streams) == 0 {
		return report, errors.New("invalid compact bootstrap configuration")
	}
	if owner == nil {
		return report, errCompactBootstrapOwnerRequired
	}
	if err := owner.AssertOwned(ctx); err != nil {
		return report, fmt.Errorf("%w: %v", errCompactBootstrapOwnerRequired, err)
	}
	rulesSnapshot, rulesGeneration := history.rules.Snapshot()
	if rulesGeneration == 0 {
		return report, errCompactBootstrapRulesNotReady
	}
	dependencyFingerprint := rulesDependencyFingerprintForSnapshot(rulesSnapshot, history.includeImportant)
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	normalizedStreams := make([]string, 0, len(streams))
	seen := make(map[string]struct{}, len(streams))
	for _, raw := range streams {
		stream := strings.TrimSpace(raw)
		if stream == "" || len(stream) > 512 {
			return report, fmt.Errorf("invalid compact stream %q", raw)
		}
		if _, duplicate := seen[stream]; duplicate {
			continue
		}
		seen[stream] = struct{}{}
		normalizedStreams = append(normalizedStreams, stream)
	}
	if len(normalizedStreams) == 0 {
		return report, errors.New("no compact streams to bootstrap")
	}

	// Capture all per-stream barriers first. This does not pretend to be a
	// cross-stream atomic snapshot; it makes the non-atomic boundary explicit.
	boundsByStream := make(map[string]compactStreamBounds, len(normalizedStreams))
	for _, stream := range normalizedStreams {
		if err := owner.AssertOwned(ctx); err != nil {
			return report, fmt.Errorf("compact bootstrap owner lost before bounds capture: %w", err)
		}
		bounds, err := source.CaptureBounds(ctx, stream)
		if err != nil {
			return report, fmt.Errorf("capture stream bounds %s: %w", stream, err)
		}
		if err := validateCompactStreamBounds(bounds, now); err != nil {
			return report, fmt.Errorf("stream %s: %w", stream, err)
		}
		boundsByStream[stream] = bounds
	}

	for _, stream := range normalizedStreams {
		result, err := bootstrapOneCompactStream(ctx, source, owner, history, stream, group, boundsByStream[stream], now, dependencyFingerprint)
		if err != nil {
			return report, fmt.Errorf("bootstrap stream %s: %w", stream, err)
		}
		report.Streams[stream] = result
	}
	if err := history.setBootstrapDependencyFingerprint(dependencyFingerprint); err != nil {
		return report, err
	}
	report.Ready = history.CompactStreamsReady(normalizedStreams, now)
	return report, nil
}

func bootstrapOneCompactStream(
	ctx context.Context,
	source compactStreamBootstrapSource,
	owner compactBootstrapOwner,
	history *sparseEngineState,
	stream, group string,
	bounds compactStreamBounds,
	now time.Time,
	dependencyFingerprint string,
) (compactStreamBootstrapResult, error) {
	result := compactStreamBootstrapResult{
		Stream: stream, FirstAvailableID: bounds.FirstID, CapturedUpperBound: bounds.UpperID,
	}
	offset := "0-0"
	readyAt, err := compactReadyAtFromFirstID(bounds.FirstID, now)
	if err != nil {
		return result, err
	}
	checkpoint, restored := history.StreamHighWater(stream)
	if restored && checkpoint.Bootstrapped {
		offset = checkpoint.MessageID
		if offset != "0-0" || checkpoint.ReadyAt.After(readyAt) {
			readyAt = checkpoint.ReadyAt
		}
		result.UsedSnapshot = true
	}
	result.SnapshotOffset = offset
	result.ReadyAt = readyAt
	comparison, valid := compareRedisStreamIDs(offset, bounds.UpperID)
	if !valid {
		return result, ErrUnifiedSnapshotInvalidState
	}
	if comparison > 0 {
		return result, fmt.Errorf("%w: offset=%s bound=%s", errCompactStreamRewound, offset, bounds.UpperID)
	}
	if restored && offset != "0-0" && bounds.FirstID != "0-0" {
		firstOrder, ok := compareRedisStreamIDs(offset, bounds.FirstID)
		if !ok {
			return result, ErrUnifiedSnapshotInvalidState
		}
		if firstOrder < 0 {
			return result, fmt.Errorf("%w: snapshot offset=%s first_available=%s", errCompactStreamRewound, offset, bounds.FirstID)
		}
	}

	cursor := offset
	lastWater := checkpoint
	if !restored {
		lastWater = compactStreamHighWater{}
	}
	for cursor != bounds.UpperID {
		if err := owner.AssertOwned(ctx); err != nil {
			return result, fmt.Errorf("compact bootstrap owner lost before range: %w", err)
		}
		page, err := source.RangePage(ctx, stream, cursor, bounds.UpperID, compactBootstrapPageSize)
		if err != nil {
			return result, err
		}
		if err := owner.AssertOwned(ctx); err != nil {
			return result, fmt.Errorf("compact bootstrap owner lost after range: %w", err)
		}
		if page.FirstID == "0-0" {
			return result, fmt.Errorf("%w: stream became empty before captured bound %s", errCompactStreamRewound, bounds.UpperID)
		}
		firstOrder, firstValid := compareRedisStreamIDs(page.FirstID, cursor)
		if !firstValid {
			return result, ErrUnifiedSnapshotInvalidState
		}
		if cursor == "0-0" {
			if page.FirstID != bounds.FirstID {
				return result, fmt.Errorf("%w: captured first=%s current first=%s", errCompactStreamRewound, bounds.FirstID, page.FirstID)
			}
		} else if firstOrder > 0 {
			return result, fmt.Errorf("%w: current first=%s moved beyond cursor=%s", errCompactStreamRewound, page.FirstID, cursor)
		}
		messages := page.Messages
		if len(messages) == 0 {
			return result, fmt.Errorf("captured upper bound %s was not reachable after %s", bounds.UpperID, cursor)
		}
		previous := cursor
		pageIDs := make(map[string]struct{}, len(messages))
		for _, message := range messages {
			order, ok := compareRedisStreamIDs(message.ID, previous)
			upperOrder, upperOK := compareRedisStreamIDs(message.ID, bounds.UpperID)
			if !ok || !upperOK || order <= 0 || upperOrder > 0 {
				return result, fmt.Errorf("invalid XRANGE order id=%s previous=%s bound=%s", message.ID, previous, bounds.UpperID)
			}
			pageIDs[message.ID] = struct{}{}
			previous = message.ID
		}
		if history.currentDependencyFingerprint() != dependencyFingerprint {
			return result, errCompactBootstrapRulesChanged
		}
		if err := owner.AssertOwned(ctx); err != nil {
			return result, fmt.Errorf("compact bootstrap owner lost before skip lookup: %w", err)
		}
		skipDigests, err := source.LookupSkipDigests(ctx, stream, group, messages)
		if err != nil {
			return result, err
		}
		if err := owner.AssertOwned(ctx); err != nil {
			return result, fmt.Errorf("compact bootstrap owner lost after skip lookup: %w", err)
		}
		for messageID, digest := range skipDigests {
			if _, exists := pageIDs[messageID]; !exists || digest != strings.ToLower(digest) || !validSHA256Hex(digest) {
				return result, fmt.Errorf("invalid compact skip digest for page message %q", messageID)
			}
		}
		for _, message := range messages {
			if skipDigest, skipped := skipDigests[message.ID]; skipped {
				if lastWater.Skipped == ^uint64(0) {
					return result, ErrUnifiedSnapshotTooLarge
				}
				lastWater = compactStreamHighWater{
					MessageID: message.ID, SkipDigest: skipDigest, Skipped: lastWater.Skipped + 1,
					ObservedAt: now, ReadyAt: readyAt, Bootstrapped: true,
				}
			} else {
				decoded, processingErr := decodeBootstrapCompactMessage(stream, message)
				if processingErr == nil {
					processingErr = validateCompactReplayMinute(decoded.Minute, now)
				}
				if processingErr == nil {
					processingErr = history.ObserveBatch(decoded, now)
				}
				if processingErr != nil {
					return result, fmt.Errorf("message %s: %w", message.ID, processingErr)
				}
				lastWater = compactStreamHighWater{
					MessageID: message.ID, BatchMinute: decoded.Minute, BatchID: decoded.BatchID, Skipped: lastWater.Skipped,
					ObservedAt: now, ReadyAt: readyAt, Bootstrapped: true,
				}
			}
			cursor = message.ID
			result.Replayed++
		}
		if history.currentDependencyFingerprint() != dependencyFingerprint {
			return result, errCompactBootstrapRulesChanged
		}
	}
	if history.currentDependencyFingerprint() != dependencyFingerprint {
		return result, errCompactBootstrapRulesChanged
	}
	if err := owner.AssertOwned(ctx); err != nil {
		return result, fmt.Errorf("compact bootstrap owner lost before group barrier: %w", err)
	}
	if err := source.SetGroupStart(ctx, stream, group, bounds.UpperID); err != nil {
		return result, err
	}
	if bounds.UpperID == "0-0" {
		lastWater = compactStreamHighWater{
			MessageID: "0-0", ObservedAt: now, ReadyAt: readyAt, Bootstrapped: true,
		}
	} else if lastWater.MessageID != bounds.UpperID {
		return result, fmt.Errorf("missing metadata for captured upper bound %s", bounds.UpperID)
	} else {
		lastWater.ObservedAt = now
		lastWater.ReadyAt = readyAt
		lastWater.Bootstrapped = true
	}
	if err := owner.AssertOwned(ctx); err != nil {
		return result, fmt.Errorf("compact bootstrap owner lost before checkpoint: %w", err)
	}
	if err := history.SetStreamBootstrapCheckpoint(stream, lastWater, now); err != nil {
		return result, err
	}
	if err := owner.AssertOwned(ctx); err != nil {
		return result, fmt.Errorf("compact bootstrap owner lost after checkpoint: %w", err)
	}
	return result, nil
}

func validateCompactStreamBounds(bounds compactStreamBounds, now time.Time) error {
	if _, _, ok := parseRedisStreamID(bounds.FirstID); !ok {
		return fmt.Errorf("invalid first stream id %q", bounds.FirstID)
	}
	upperMilliseconds, _, ok := parseRedisStreamID(bounds.UpperID)
	if !ok {
		return fmt.Errorf("invalid upper stream id %q", bounds.UpperID)
	}
	order, _ := compareRedisStreamIDs(bounds.FirstID, bounds.UpperID)
	if order > 0 || (bounds.UpperID == "0-0" && bounds.FirstID != "0-0") {
		return errors.New("invalid stream bounds order")
	}
	if bounds.UpperID != "0-0" && time.UnixMilli(int64(upperMilliseconds)).UTC().After(now.Add(unifiedSnapshotFutureTolerance)) {
		return errors.New("stream upper bound is too far in the future")
	}
	return nil
}

func compactReadyAtFromFirstID(firstID string, now time.Time) (time.Time, error) {
	if firstID == "0-0" {
		return now.Add(compactHistoryRetentionMinutes * time.Minute), nil
	}
	milliseconds, _, ok := parseRedisStreamID(firstID)
	if !ok || milliseconds > uint64(^uint64(0)>>1) {
		return time.Time{}, errors.New("invalid first stream id time")
	}
	first := time.UnixMilli(int64(milliseconds)).UTC()
	if first.After(now.Add(unifiedSnapshotFutureTolerance)) {
		return time.Time{}, errors.New("first stream id is too far in the future")
	}
	return first.Add(compactHistoryRetentionMinutes * time.Minute), nil
}

func decodeBootstrapCompactMessage(stream string, message redis.XMessage) (compactDecodedBatch, error) {
	raw, ok := compactMessageJSON(message.Values["json"])
	if !ok {
		return compactDecodedBatch{}, errors.New("compact message has no json")
	}
	decoded, err := decodeCompactBatch(raw)
	if err != nil {
		return compactDecodedBatch{}, err
	}
	lowerStream := strings.ToLower(strings.TrimSpace(stream))
	if (strings.Contains(lowerStream, "liquidation") && decoded.Kind != "l") || (strings.Contains(lowerStream, "candle") && decoded.Kind != "c") {
		return compactDecodedBatch{}, errors.New("compact batch kind does not match stream")
	}
	identity := CandleEvent{Exchange: decoded.Exchange, MarketType: decoded.MarketType}
	if !prepareCandleEventForStream(&identity, stream) {
		return compactDecodedBatch{}, errors.New("compact batch venue does not match stream")
	}
	for index := range decoded.Events {
		if !prepareCandleEventForStream(&decoded.Events[index], stream) {
			return compactDecodedBatch{}, errors.New("compact event venue does not match stream")
		}
	}
	return decoded, nil
}
