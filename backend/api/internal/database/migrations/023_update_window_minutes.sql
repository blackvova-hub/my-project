-- Расширяем допустимое окно правил до 240 минут
ALTER TABLE alerts
  DROP CONSTRAINT IF EXISTS alerts_window_minutes_check;

ALTER TABLE alerts
  ADD CONSTRAINT alerts_window_minutes_check CHECK (window_minutes BETWEEN 1 AND 240);
