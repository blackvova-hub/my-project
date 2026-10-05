BEGIN;

-- UUID generator (для auth/session если понадобится)
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------------------------------------------------------
-- NOTE: ВАЖНО
-- Раньше тут был DEV RESET (DROP TABLE ...). Это приводило к полному
-- стиранию схемы/данных при повторном прогоне миграций.
-- Удалено для безопасной работы в проде.
-- ---------------------------------------------------------

-- ---------------------------------------------------------
-- USERS / SESSIONS
-- ---------------------------------------------------------
-- Делаем два идентификатора:
-- 1) id UUID (если бекенд/auth уже на UUID)
-- 2) num_id BIGSERIAL (для воркера/WS которые сейчас на int64)
CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  num_id BIGSERIAL NOT NULL UNIQUE,

  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_active ON sessions(user_id, expires_at)
  WHERE revoked_at IS NULL;

-- ---------------------------------------------------------
-- ALERTS (ЭТО ТО, ЧТО НУЖНО ВОРКЕРУ)
-- ---------------------------------------------------------
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'alert_direction') THEN
    CREATE TYPE alert_direction AS ENUM ('up','down','both');
  END IF;
END $$;

-- Воркер ожидает:
-- id (int64), user_id (int64), symbol, window_minutes, threshold_percent, direction, cooldown_seconds, enabled, updated_at
CREATE TABLE IF NOT EXISTS alerts (
  id BIGSERIAL PRIMARY KEY,

  -- важно: BIGINT под воркер (Rule.UserID int64)
  -- привязываем к users.num_id
  user_id BIGINT NOT NULL REFERENCES users(num_id) ON DELETE CASCADE,

  -- символ: либо конкретный (BTCUSDT), либо '*' для ALL (воркер это нормализует)
  symbol TEXT NOT NULL,

  window_minutes INT NOT NULL CHECK (window_minutes BETWEEN 1 AND 720),
  threshold_percent DOUBLE PRECISION NOT NULL CHECK (threshold_percent > 0),

  direction alert_direction NOT NULL DEFAULT 'both',
  cooldown_seconds INT NOT NULL DEFAULT 60 CHECK (cooldown_seconds >= 0),
  enabled BOOLEAN NOT NULL DEFAULT TRUE,

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Индексы под воркер
CREATE INDEX IF NOT EXISTS idx_alerts_enabled_symbol ON alerts(enabled, symbol);
CREATE INDEX IF NOT EXISTS idx_alerts_updated_at ON alerts(updated_at);
CREATE INDEX IF NOT EXISTS idx_alerts_user ON alerts(user_id);

-- Авто-updated_at
CREATE OR REPLACE FUNCTION trg_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS alerts_set_updated_at ON alerts;
CREATE TRIGGER alerts_set_updated_at
BEFORE UPDATE ON alerts
FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------
-- SIGNALS (опционально, но без конфликтов)
-- ---------------------------------------------------------
-- Это история, НЕ мешает воркеру. Делается “правильно”, чтобы не было ошибки rule_id.
CREATE TABLE IF NOT EXISTS signals (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

  -- rule_id = alerts.id (BIGINT)
  rule_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,

  -- user_id = users.num_id (BIGINT), чтобы фронт/WS мог фильтровать
  user_id BIGINT NOT NULL REFERENCES users(num_id) ON DELETE CASCADE,

  symbol TEXT NOT NULL,
  tf TEXT NOT NULL DEFAULT '1m',
  ts BIGINT NOT NULL,
  payload JSONB NOT NULL,

  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- дедуп: один сигнал на rule+symbol+tf+ts
CREATE UNIQUE INDEX IF NOT EXISTS uq_signals_rule_symbol_tf_ts ON signals(rule_id, symbol, tf, ts);
CREATE INDEX IF NOT EXISTS idx_signals_user_created ON signals(user_id, created_at DESC);

COMMIT;

-- =====================================================
-- 002_scanner.sql (LEGACY)
--
-- Историческая миграция (раньше создавала users/alerts/signals под воркер).
-- На текущий момент актуальная схема для чистой БД применяется из:
--   db/baseline/001_base_current.sql
--
-- Данный файл оставлен только для совместимости и намеренно NO-OP,
-- чтобы не конфликтовать с текущей схемой.
--
-- Новые миграции добавляйте как migrations/034_*.sql и далее.
-- =====================================================

-- noop
SELECT 1;
