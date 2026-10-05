BEGIN;

-- Добавляем порог по сумме ликвидаций (USD)
ALTER TABLE alerts
  ADD COLUMN IF NOT EXISTS threshold_amount DOUBLE PRECISION;

-- Делаем процент опциональным (нужен не для всех индикаторов)
ALTER TABLE alerts
  ALTER COLUMN threshold_percent DROP NOT NULL;

-- Проверки на корректные значения
ALTER TABLE alerts
  DROP CONSTRAINT IF EXISTS alerts_threshold_percent_check;

ALTER TABLE alerts
  DROP CONSTRAINT IF EXISTS alerts_threshold_amount_check;

ALTER TABLE alerts
  ADD CONSTRAINT alerts_threshold_percent_check
  CHECK (threshold_percent IS NULL OR threshold_percent > 0);

ALTER TABLE alerts
  ADD CONSTRAINT alerts_threshold_amount_check
  CHECK (threshold_amount IS NULL OR threshold_amount > 0);

COMMIT;
