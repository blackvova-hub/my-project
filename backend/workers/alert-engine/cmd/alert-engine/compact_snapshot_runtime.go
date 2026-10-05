package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func bootstrapUnifiedSnapshotRuntime(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, cfg Config, rules *RulesCache) (*sparseEngineState, *unifiedSnapshotStore, *pgCompactBootstrapOwner, error) {
	if rules == nil {
		return nil, nil, nil, errors.New("invalid unified snapshot runtime configuration")
	}
	owner, acquired, err := acquireCompactBootstrapOwner(ctx, db, cfg.MarketConsumerGroup)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("acquire compact owner: %w", err)
	}
	if !acquired {
		return nil, nil, nil, errors.New("compact consumer group already has an active owner")
	}
	keepOwner := false
	defer func() {
		if !keepOwner {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = owner.Close(closeCtx)
		}
	}()

	newHistory := func() *sparseEngineState {
		return newSparseEngineState(rules, cfg.ImportantEventsEnabled, cfg.MaxInstruments)
	}
	history := newHistory()
	store := newUnifiedSnapshotStore(cfg.SnapshotPath)
	loadedSnapshot := false
	if err := store.Load(history, time.Now().UTC()); err != nil {
		switch {
		case os.IsNotExist(err):
			log.Printf("sparse snapshot does not exist; replaying retained compact streams")
		case discardableUnifiedSnapshotError(err):
			log.Printf("sparse snapshot rejected; replaying retained compact streams: %v", err)
			history = newHistory()
		default:
			return nil, nil, nil, fmt.Errorf("load sparse snapshot: %w", err)
		}
	} else {
		loadedSnapshot = true
		log.Printf("sparse snapshot loaded path=%s", cfg.SnapshotPath)
	}

	source := newRedisCompactBootstrapSource(rdb)
	report, bootstrapErr := BootstrapCompactStreams(ctx, source, owner, history, cfg.MarketStreams, cfg.MarketConsumerGroup, time.Now().UTC())
	if bootstrapErr != nil && loadedSnapshot && errors.Is(bootstrapErr, errCompactStreamRewound) {
		log.Printf("sparse snapshot offset is outside retained stream; restarting fresh replay: %v", bootstrapErr)
		history = newHistory()
		report, bootstrapErr = BootstrapCompactStreams(ctx, source, owner, history, cfg.MarketStreams, cfg.MarketConsumerGroup, time.Now().UTC())
	}
	if bootstrapErr != nil {
		return nil, nil, nil, bootstrapErr
	}
	if err := owner.AssertOwned(ctx); err != nil {
		return nil, nil, nil, err
	}
	if err := store.Save(history, time.Now().UTC()); err != nil {
		return nil, nil, nil, fmt.Errorf("save initial sparse snapshot: %w", err)
	}
	if err := owner.AssertOwned(ctx); err != nil {
		return nil, nil, nil, err
	}
	log.Printf("sparse bootstrap complete streams=%d ready=%v", len(report.Streams), report.Ready)
	keepOwner = true
	return history, store, owner, nil
}

func discardableUnifiedSnapshotError(err error) bool {
	return errors.Is(err, ErrUnifiedSnapshotMalformed) || errors.Is(err, ErrUnifiedSnapshotChecksum) ||
		errors.Is(err, ErrUnifiedSnapshotVersion) || errors.Is(err, ErrUnifiedSnapshotTooLarge) ||
		errors.Is(err, ErrUnifiedSnapshotInvalidState) || errors.Is(err, ErrUnifiedSnapshotRulesMismatch)
}

func runOwnedUnifiedSnapshotLoop(ctx context.Context, owner compactBootstrapOwner, store *unifiedSnapshotStore, history *sparseEngineState, every time.Duration) error {
	if owner == nil || store == nil || history == nil {
		return errors.New("invalid owned snapshot loop configuration")
	}
	if every <= 0 {
		every = 5 * time.Minute
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			if err := owner.AssertOwned(ctx); err != nil {
				return fmt.Errorf("snapshot owner check before save: %w", err)
			}
			if err := store.Save(history, now.UTC()); errors.Is(err, errCompactBootstrapRulesChanged) {
				log.Printf("sparse snapshot skipped while rules replay is pending: %v", err)
				continue
			} else if err != nil {
				return fmt.Errorf("periodic sparse snapshot save: %w", err)
			}
			if err := owner.AssertOwned(ctx); err != nil {
				return fmt.Errorf("snapshot owner check after save: %w", err)
			}
		}
	}
}

func saveFinalOwnedUnifiedSnapshot(owner compactBootstrapOwner, store *unifiedSnapshotStore, history *sparseEngineState) error {
	if owner == nil || store == nil || history == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := owner.AssertOwned(ctx); err != nil {
		return err
	}
	if err := store.Save(history, time.Now().UTC()); err != nil {
		return err
	}
	return owner.AssertOwned(ctx)
}
