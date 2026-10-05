BEGIN;

CREATE TABLE IF NOT EXISTS ai_usage (
  user_id BIGINT NOT NULL REFERENCES users(num_id) ON DELETE CASCADE,
  day DATE NOT NULL,
  tokens_used INT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, day)
);

CREATE INDEX IF NOT EXISTS idx_ai_usage_day
  ON ai_usage (day);

CREATE OR REPLACE FUNCTION trg_ai_usage_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS ai_usage_set_updated_at ON ai_usage;
CREATE TRIGGER ai_usage_set_updated_at
BEFORE UPDATE ON ai_usage
FOR EACH ROW EXECUTE FUNCTION trg_ai_usage_updated_at();

COMMIT;
