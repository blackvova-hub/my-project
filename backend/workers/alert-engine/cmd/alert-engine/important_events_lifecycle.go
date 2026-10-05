package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const fundamentalMonitorAdvisoryLock int64 = 0x46554e44414d454e // "FUNDAMEN"

const (
	importantResolutionLeaderLock   int64 = 0x494d505245534f4c // "IMPRESOL"
	importantLifecycleAdvisoryLock  int64 = 0x494d504c49464543 // "IMPLIFEC"
	importantResolutionInterval           = 30 * time.Second
	importantResolutionBatchLimit         = 500
	importantCandidateResolutionAge       = 3 * time.Minute
	importantConfirmedResolutionAge       = 8 * time.Minute
)

func startImportantEventResolutionMonitor(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, cfg Config) {
	if db == nil || rdb == nil {
		return
	}
	for ctx.Err() == nil {
		conn, err := db.Acquire(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("important-event resolver acquire failed: %v", err)
			}
			if !waitForDispatcherRetry(ctx) {
				return
			}
			continue
		}
		var leader bool
		err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, importantResolutionLeaderLock).Scan(&leader)
		if err != nil || !leader {
			conn.Release()
			if err != nil && ctx.Err() == nil {
				log.Printf("important-event resolver leader election failed: %v", err)
			}
			if !waitForDispatcherRetry(ctx) {
				return
			}
			continue
		}

		log.Printf("important-event resolver became leader")
		serveImportantEventResolutionLeader(ctx, conn, db, rdb, cfg)
		unlockCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, unlockErr := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, importantResolutionLeaderLock)
		cancel()
		if unlockErr != nil && ctx.Err() == nil {
			log.Printf("important-event resolver unlock failed: %v", unlockErr)
		}
		conn.Release()
	}
}

func serveImportantEventResolutionLeader(ctx context.Context, leader *pgxpool.Conn, db *pgxpool.Pool, rdb *redis.Client, cfg Config) {
	poll := func() bool {
		resolved, published, err := resolveExpiredImportantEvents(ctx, leader)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("important-event batch resolution failed: %v", err)
			}
			return false
		}
		if resolved > 0 {
			log.Printf("important-event batch resolved=%d broadcasts=%d", resolved, published)
		}
		if published > 0 {
			if err := deliverImportantEventOutbox(ctx, db, rdb, cfg); err != nil && ctx.Err() == nil {
				log.Printf("important-event resolved broadcast failed: %v", err)
			}
		}
		return true
	}
	ticker := time.NewTicker(importantResolutionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !poll() {
				return
			}
		}
	}
}

func resolveExpiredImportantEvents(ctx context.Context, conn *pgxpool.Conn) (int64, int64, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, importantLifecycleAdvisoryLock); err != nil {
		return 0, 0, err
	}
	var resolved, published int64
	err = tx.QueryRow(ctx, `
		WITH expiring AS (
			SELECT id, status AS previous_status
			FROM important_events
			WHERE status IN ('candidate','confirmed')
			  AND source_kind='market'
			  AND last_ingested_at < now() - CASE
				WHEN status='candidate' THEN ($1::bigint * interval '1 second')
				ELSE ($2::bigint * interval '1 second')
			  END
			ORDER BY last_ingested_at, id
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		), resolved AS (
			UPDATE important_events e
			SET status='resolved', resolved_at=now()
			FROM expiring x
			WHERE e.id=x.id
			RETURNING e.id,e.event_type,e.family,e.confidence,e.exchange,e.market_type,e.symbol,e.direction,
			          e.window_minutes,e.occurrence_count,e.event_at,e.title,e.details,e.source_kind,e.source_ref,
			          e.catalyst_event_id,e.entity_id,e.confirmed_venues,e.venue_count,e.metadata,x.previous_status
		), inserted AS (
			INSERT INTO important_event_outbox(event_id,operation,version,payload)
			SELECT id,'resolved',occurrence_count,jsonb_build_object(
				'kind','important_event','audience','all','userId',0,'operation','resolved','eventId',id::text,
				'eventType',event_type,'family',family,'confidence',confidence,'status','resolved','sourceKind',source_kind,'sourceRef',source_ref,
				'catalystEventId',catalyst_event_id,'entityId',entity_id,'exchange',exchange,'marketType',market_type,
				'symbol',symbol,'direction',direction,'windowMinutes',window_minutes,'occurrenceCount',occurrence_count,
				'eventAt',event_at,'title',title,'details',details,'confirmedVenues',confirmed_venues,'venueCount',venue_count,'metrics',metadata)
			FROM resolved
			WHERE previous_status='confirmed'
			ON CONFLICT(event_id,operation,version) DO NOTHING
			RETURNING 1
		)
		SELECT (SELECT count(*) FROM resolved), (SELECT count(*) FROM inserted)
	`, int64(importantCandidateResolutionAge/time.Second), int64(importantConfirmedResolutionAge/time.Second), importantResolutionBatchLimit).Scan(&resolved, &published)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return resolved, published, nil
}

func startFundamentalImportantEventMonitor(ctx context.Context, db *pgxpool.Pool, rdb *redis.Client, cfg Config) {
	poll := func() {
		conn, err := db.Acquire(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("fundamental monitor acquire failed: %v", err)
			}
			return
		}
		defer conn.Release()
		var locked bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, fundamentalMonitorAdvisoryLock).Scan(&locked); err != nil || !locked {
			if err != nil && ctx.Err() == nil {
				log.Printf("fundamental monitor lock failed: %v", err)
			}
			return
		}
		defer func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, fundamentalMonitorAdvisoryLock); err != nil {
				log.Printf("fundamental monitor unlock failed: %v", err)
			}
		}()

		events, err := readSixFundamentalImportantEvents(ctx, db)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("fundamental event lookup failed: %v", err)
			}
			return
		}
		changed, err := syncFundamentalImportantEvents(ctx, db, events)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("fundamental event sync failed: %v", err)
			}
			return
		}
		for _, event := range changed {
			log.Printf("important event lifecycle=%s type=%s source=%s catalyst=%s", event.Metadata["lifecycleStatus"], event.EventType, event.SourceRef, event.CatalystEventID)
		}
		if err := deliverImportantEventOutbox(ctx, db, rdb, cfg); err != nil && ctx.Err() == nil {
			log.Printf("fundamental event broadcast failed: %v", err)
		}
	}

	poll()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}

func syncFundamentalImportantEvents(ctx context.Context, db *pgxpool.Pool, events []importantEvent) ([]importantEvent, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE important_events
		SET status='resolved',resolved_at=now()
		WHERE status='candidate' AND source_kind='news' AND confirmation_deadline < now()`); err != nil {
		return nil, err
	}
	changed := make([]importantEvent, 0, len(events))
	for i := range events {
		event := &events[i]
		finalizeImportantEvent(event)
		applied, err := upsertQualityImportantEvent(ctx, tx, *event)
		if err != nil {
			return nil, err
		}
		if applied {
			changed = append(changed, *event)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return changed, nil
}

func syncQualityImportantEvents(ctx context.Context, db *pgxpool.Pool, evaluation importantEvaluation) ([]importantEvent, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, importantLifecycleAdvisoryLock); err != nil {
		return nil, err
	}
	changed := make([]importantEvent, 0, len(evaluation.Events))
	for i := range evaluation.Events {
		event := &evaluation.Events[i]
		finalizeImportantEvent(event)
		applied, err := upsertQualityImportantEvent(ctx, tx, *event)
		if err != nil {
			return nil, err
		}
		if applied {
			changed = append(changed, *event)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return changed, nil
}

func upsertQualityImportantEvent(ctx context.Context, tx pgx.Tx, event importantEvent) (bool, error) {
	var id, status, oldType string
	var lastObserved, firstSeen time.Time
	var observations, oldVenueCount int
	var oldAmount *float64
	var oldMetadata []byte
	clusterMinutes := maxInt(1, metadataInt(event.Metadata, "clusterWindowMinutes"))
	err := tx.QueryRow(ctx, `
		SELECT id::text,status,event_type,last_observed_at,first_seen_at,observation_count,metadata,venue_count,amount_usd
		FROM important_events
		WHERE status IN ('candidate','confirmed') AND (
			identity_key=$1 OR (
				$2='market' AND source_kind='market' AND event_type=$3
				AND (COALESCE(exchange,'')=$4 OR ($3='price_shock' AND ($4='multiple' OR exchange='multiple')))
				AND COALESCE(market_type,'')=$5
				AND COALESCE(symbol,'')=$6 AND COALESCE(direction,'')=$7
				AND last_observed_at >= $8::timestamptz-($9::double precision*interval '1 minute')
			)
		)
		ORDER BY CASE WHEN identity_key=$1 THEN 0 ELSE 1 END,last_observed_at DESC
		LIMIT 1 FOR UPDATE`,
		event.IdentityKey, event.SourceKind, event.EventType, event.Exchange, event.MarketType,
		event.Symbol, event.Direction, event.EventAt, clusterMinutes,
	).Scan(&id, &status, &oldType, &lastObserved, &firstSeen, &observations, &oldMetadata, &oldVenueCount, &oldAmount)
	if err != nil && err != pgx.ErrNoRows {
		return false, err
	}
	required := 2
	if value := metadataInt(event.Metadata, "requiredObservations"); value > 0 {
		required = value
	}
	window := 5 * time.Minute
	if value := metadataInt(event.Metadata, "confirmationWindowMinutes"); value > 0 {
		window = time.Duration(value) * time.Minute
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	canConfirm := eventCanConfirm(event.Metadata)
	if err == pgx.ErrNoRows {
		status = "candidate"
		observations = 1
		if required <= 1 && canConfirm {
			status = "confirmed"
		}
		event.Metadata["lifecycleStatus"] = status
		metadata, marshalErr := json.Marshal(event.Metadata)
		if marshalErr != nil {
			return false, marshalErr
		}
		var insertedID string
		insertErr := tx.QueryRow(ctx, `INSERT INTO important_events(event_type,family,confidence,title,details,exchange,market_type,symbol,direction,amount_usd,change_percent,baseline_value,baseline_ratio,percentile,window_minutes,event_at,first_seen_at,last_seen_at,last_observed_at,identity_key,dedup_key,status,metadata,observation_count,confirmation_started_at,confirmation_deadline,factor_count,confirmed_venues,venue_count,cluster_start_at,cluster_end_at,source_kind,source_ref,catalyst_event_id) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,$11,$12,$13,$14,$15,$16,$16,$16,$16,$17,$18,$19,$20::jsonb,$21,$22::timestamptz,($22::timestamptz+($23::double precision*interval '1 second')),$24,$25::jsonb,$26,$22::timestamptz,$22::timestamptz,$27,$28,NULLIF($29,'')::uuid) RETURNING id::text`, event.EventType, event.Family, event.Confidence, event.Title, event.Details, event.Exchange, event.MarketType, event.Symbol, event.Direction, event.AmountUSD, event.ChangePercent, event.BaselineValue, event.BaselineRatio, event.Percentile, event.WindowMinutes, event.EventAt, event.IdentityKey, event.DedupKey, status, string(metadata), observations, event.EventAt, window.Seconds(), eventFactorCount(event), jsonStringSlice(metadataStrings(event.Metadata, "confirmed_venues")), eventVenueCount(event), event.SourceKind, event.SourceRef, event.CatalystEventID).Scan(&insertedID)
		if insertErr != nil {
			return false, insertErr
		}
		if err := persistImportantFactors(ctx, tx, insertedID, event); err != nil {
			return false, err
		}
		if status == "confirmed" {
			if err := enqueueImportantEventOutbox(ctx, tx, insertedID, "created", observations, event); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, nil
	}
	if !event.EventAt.After(lastObserved) || (status == "candidate" && event.EventAt.After(firstSeen.Add(window))) {
		return false, nil
	}
	previousStatus := status
	observations++
	if len(oldMetadata) > 0 {
		var previous map[string]any
		_ = json.Unmarshal(oldMetadata, &previous)
		event.Metadata = importantEventMetadataWithHistory(event.Metadata, previous, event.EventAt, "updated")
	}
	if canConfirm {
		status = lifecycleTransition(status, observations, required)
	}
	event.Metadata["lifecycleStatus"] = status
	metadata, marshalErr := json.Marshal(event.Metadata)
	if marshalErr != nil {
		return false, marshalErr
	}
	_, err = tx.Exec(ctx, `UPDATE important_events SET status=$2,event_type=$3,confidence=$4,title=$5,details=$6,amount_usd=$7,change_percent=$8,window_minutes=$9,event_at=$10,last_seen_at=$10,last_observed_at=$10,last_ingested_at=now(),occurrence_count=occurrence_count+1,observation_count=$11,metadata=$12::jsonb,factor_count=$13,confirmed_venues=$14::jsonb,venue_count=$15,cluster_end_at=$10,exchange=NULLIF($16,'') WHERE id=$1::uuid`, id, status, event.EventType, event.Confidence, event.Title, event.Details, event.AmountUSD, event.ChangePercent, event.WindowMinutes, event.EventAt, observations, string(metadata), eventFactorCount(event), jsonStringSlice(metadataStrings(event.Metadata, "confirmed_venues")), eventVenueCount(event), event.Exchange)
	if err != nil {
		return false, err
	}
	if err := persistImportantFactors(ctx, tx, id, event); err != nil {
		return false, err
	}
	if previousStatus == "candidate" && status == "confirmed" {
		if err := enqueueImportantEventOutbox(ctx, tx, id, "created", observations, event); err != nil {
			return false, err
		}
		return true, nil
	}
	if previousStatus == "confirmed" && qualityUpdateIsMaterial(oldVenueCount, oldAmount, event) {
		if err := enqueueImportantEventOutbox(ctx, tx, id, "updated", observations, event); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func qualityUpdateIsMaterial(oldVenueCount int, oldAmount *float64, event importantEvent) bool {
	if eventVenueCount(event) > oldVenueCount {
		return true
	}
	if oldAmount != nil && event.AmountUSD != nil && *oldAmount > 0 {
		return math.Abs(*event.AmountUSD-*oldAmount)/(*oldAmount) >= .20
	}
	return false
}
func lifecycleTransition(status string, observations, required int) string {
	if required < 1 {
		required = 1
	}
	if observations < 1 {
		observations = 1
	}
	switch status {
	case "candidate":
		if observations >= required {
			return "confirmed"
		}
		return "candidate"
	case "confirmed":
		return "confirmed"
	default:
		return status
	}
}

func eventCanConfirm(metadata map[string]any) bool {
	return !metadataBool(metadata, "requiresOfficialConfirmation") || metadataBool(metadata, "official")
}

func persistImportantFactors(ctx context.Context, tx pgx.Tx, eventID string, event importantEvent) error {
	for _, name := range metadataStrings(event.Metadata, "factorNames") {
		if _, err := tx.Exec(ctx, `INSERT INTO important_event_factors(event_id,factor_type,observed_at,metadata) VALUES($1::uuid,$2,$3,$4::jsonb)`, eventID, name, event.EventAt, `{"source":"quality_engine"}`); err != nil {
			return err
		}
	}
	return nil
}

func eventFactorCount(event importantEvent) int {
	return len(metadataStrings(event.Metadata, "factorNames"))
}
func eventVenueCount(event importantEvent) int {
	if v := metadataInt(event.Metadata, "venue_count"); v > 0 {
		return v
	}
	return len(metadataStrings(event.Metadata, "confirmed_venues"))
}
func jsonStringSlice(values []string) string { b, _ := json.Marshal(values); return string(b) }
func metadataInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	}
	return 0
}

func metadataValue(metadata map[string]any, keys ...string) any {
	for _, key := range keys {
		if metadata != nil {
			if value, ok := metadata[key]; ok {
				return value
			}
		}
	}
	return nil
}
