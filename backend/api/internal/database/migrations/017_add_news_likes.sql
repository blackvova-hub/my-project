BEGIN;

CREATE TABLE IF NOT EXISTS news_likes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  news_id UUID NOT NULL REFERENCES news_items(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT uq_news_likes_user_item UNIQUE (news_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_news_likes_item_created
  ON news_likes(news_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_news_likes_user
  ON news_likes(user_id);

COMMIT;
