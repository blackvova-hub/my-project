export type ClosedPrice = { time: number; close: number };
export type ReactionResult = { value: number | null; reason: string };

/** Exact completed 5m closes, never interpolated across missing observations. */
export function releaseReaction(
  candles: readonly ClosedPrice[],
  eventAt: number,
  horizon: number,
  now: number,
): ReactionResult {
  const missing = (reason: string): ReactionResult => ({ value: null, reason });
  if (![eventAt, horizon, now].every(Number.isFinite) || horizon <= 0)
    return missing("Некорректная временная граница");
  const target = eventAt + horizon;
  if (target > now) return missing("Горизонт ещё не наступил");
  const prices = [
    ...new Map(
      candles
        .filter(
          (p) =>
            Number.isFinite(p.time) &&
            Number.isFinite(p.close) &&
            p.close > 0 &&
            p.time + 300 <= now,
        )
        .map((p) => [p.time, p]),
    ).values(),
  ].sort((a, b) => a.time - b.time);
  const baseline = prices.filter((p) => p.time + 300 <= eventAt).at(-1);
  if (!baseline || eventAt - baseline.time - 300 >= 300)
    return missing("Нет полной свечи непосредственно перед событием");
  const last = prices.filter((p) => p.time + 300 <= target).at(-1);
  if (!last || target - last.time - 300 >= 300 || last.time <= baseline.time)
    return missing("Нет полной свечи у выбранной границы");
  const interval = prices.filter(
    (p) => p.time >= baseline.time && p.time <= last.time,
  );
  if (interval.some((p, i) => i > 0 && p.time - interval[i - 1].time !== 300))
    return missing("В истории есть пропущенные пятиминутные свечи");
  return {
    value: (last.close / baseline.close - 1) * 100,
    reason: "Изменение закрытий полных свечей Bybit Spot",
  };
}
