-- Fixed-catalog normalized observations and recoverable last successful cache.
-- Composite primary key supports a series/date history range without scans.
CREATE TABLE IF NOT EXISTS news_macro_cache (
    cache_key text PRIMARY KEY,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS news_macro_observations (
    series_id text NOT NULL,
    observed_on date NOT NULL,
    value double precision NOT NULL,
    source text NOT NULL,
    source_url text NOT NULL,
    fetched_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (series_id, observed_on),
    CHECK (value NOT IN ('Infinity'::double precision, '-Infinity'::double precision, 'NaN'::double precision))
);

-- The detection timestamp is not the original release vintage. It records when
-- this collector first noticed a revision, so analytics cannot claim otherwise.
CREATE TABLE IF NOT EXISTS news_macro_revisions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    series_id text NOT NULL,
    observed_on date NOT NULL,
    previous_value double precision NOT NULL,
    revised_value double precision NOT NULL,
    detected_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS news_macro_revisions_series_date_idx
    ON news_macro_revisions (series_id, observed_on, detected_at DESC);
