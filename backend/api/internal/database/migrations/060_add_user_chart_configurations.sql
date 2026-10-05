BEGIN;

CREATE TABLE IF NOT EXISTS user_chart_configurations (
  user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  config JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT user_chart_configurations_config_object
    CHECK (jsonb_typeof(config) = 'object')
);

COMMIT;
