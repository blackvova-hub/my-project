BEGIN;

WITH retiring AS (
  SELECT id, occurrence_count, event_type, family, priority, confidence, severity, peak_severity,
         exchange, market_type, symbol, direction, window_minutes, event_at, title, details,
         source_kind, source_ref, catalyst_event_id, entity_id, confirmed_venues, venue_count, metadata
  FROM important_events
  WHERE status IN ('confirmed','active')
    AND event_type NOT IN (
      'price_shock','open_interest_shock','large_aggressive_trade',
      'exchange_announcement','onchain_transfer','major_statement'
    )
  FOR UPDATE
), queued AS (
  INSERT INTO important_event_outbox(event_id,operation,version,payload)
  SELECT id,'resolved',occurrence_count,jsonb_build_object(
    'kind','important_event','audience','all','userId',0,'operation','resolved','eventId',id::text,
    'eventType',event_type,'family',family,'priority',priority,'confidence',confidence,
    'severity',severity,'peakSeverity',peak_severity,'status','resolved','sourceKind',source_kind,
    'sourceRef',source_ref,'catalystEventId',catalyst_event_id,'entityId',entity_id,
    'exchange',exchange,'marketType',market_type,'symbol',symbol,'direction',direction,
    'windowMinutes',window_minutes,'occurrenceCount',occurrence_count,'eventAt',event_at,
    'title',title,'details',details,'confirmedVenues',confirmed_venues,'venueCount',venue_count,
    'metrics',metadata
  )
  FROM retiring
  ON CONFLICT(event_id,operation,version) DO NOTHING
)
UPDATE important_events
SET status='resolved',resolved_at=COALESCE(resolved_at,now())
WHERE status IN ('candidate','confirmed','active')
  AND event_type NOT IN (
    'price_shock','open_interest_shock','large_aggressive_trade',
    'exchange_announcement','onchain_transfer','major_statement'
  );

ALTER TABLE important_events DROP CONSTRAINT IF EXISTS important_events_public_type_chk;
ALTER TABLE important_events ADD CONSTRAINT important_events_public_type_chk CHECK (
  status='resolved' OR event_type IN (
    'price_shock','open_interest_shock','large_aggressive_trade',
    'exchange_announcement','onchain_transfer','major_statement'
  )
);

COMMIT;
