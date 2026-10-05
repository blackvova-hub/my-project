BEGIN;

CREATE INDEX IF NOT EXISTS idx_news_items_assets_gin
  ON news_items USING GIN (assets jsonb_path_ops);

CREATE INDEX IF NOT EXISTS idx_news_items_search_gin
  ON news_items USING GIN (
    to_tsvector(
      'simple',
      COALESCE(title, '') || ' ' || COALESCE(summary, '') || ' ' || COALESCE(original_title, '')
    )
  )
  WHERE publication_eligible = TRUE;

COMMIT;
