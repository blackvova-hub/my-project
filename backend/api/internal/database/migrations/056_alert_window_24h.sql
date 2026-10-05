-- Expand the database guardrail to 24 hours without rewriting existing rows.
-- API/UI feature flags continue to enforce the legacy 12-hour limit until soak completes.
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'alerts_window_minutes_24h_check'
      AND conrelid = 'public.alerts'::regclass
  ) THEN
    ALTER TABLE public.alerts
      ADD CONSTRAINT alerts_window_minutes_24h_check
      CHECK (window_minutes BETWEEN 1 AND 1440) NOT VALID;
  END IF;
END
$$;

ALTER TABLE public.alerts
  VALIDATE CONSTRAINT alerts_window_minutes_24h_check;

ALTER TABLE public.alerts
  DROP CONSTRAINT IF EXISTS alerts_window_minutes_check;
