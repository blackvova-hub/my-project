CREATE TABLE IF NOT EXISTS news_summaries (
  id BIGSERIAL PRIMARY KEY,
  period_start TIMESTAMPTZ NOT NULL,
  period_end TIMESTAMPTZ NOT NULL,
  summary TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS news_summaries_created_at_idx ON news_summaries (created_at DESC);
CREATE INDEX IF NOT EXISTS news_summaries_period_end_idx ON news_summaries (period_end DESC);
