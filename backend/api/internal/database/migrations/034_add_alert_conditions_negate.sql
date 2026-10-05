BEGIN;

-- Добавляем признак логического отрицания (NOT) внутрь каждой JSONB-condition.
-- Формат: { indicator, direction, threshold_percent, threshold_amount, negate }

UPDATE alerts
SET conditions = (
  SELECT jsonb_agg(
    CASE
      WHEN (c ? 'negate') THEN c
      ELSE (c || jsonb_build_object('negate', false))
    END
  )
  FROM jsonb_array_elements(COALESCE(conditions, '[]'::jsonb)) AS c
)
WHERE conditions IS NOT NULL;

COMMIT;
