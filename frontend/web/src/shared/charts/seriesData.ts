export function splitLineData(
  points: readonly { time: number; value: number | null }[],
  gapSeconds?: number,
) {
  const rows = [
    ...new Map(
      points
        .filter(
          (p) =>
            Number.isFinite(p.time) &&
            p.time > 0 &&
            (p.value === null || Number.isFinite(p.value)),
        )
        .map((p) => [p.time, p]),
    ).values(),
  ].sort((a, b) => a.time - b.time);
  const groups: { time: number; value: number }[][] = [];
  let group: { time: number; value: number }[] = [];
  for (const p of rows) {
    if (p.value === null) {
      if (group.length) groups.push(group);
      group = [];
      continue;
    }
    if (
      group.length &&
      gapSeconds &&
      p.time - group.at(-1)!.time > gapSeconds
    ) {
      groups.push(group);
      group = [];
    }
    group.push({ time: p.time, value: p.value });
  }
  if (group.length) groups.push(group);
  return groups;
}
/** Exchange weekly candles start on Monday UTC; Unix epoch began on Thursday. */
export function alignBarTime(time: number, seconds: number) {
  const anchor = seconds === 604800 ? 4 * 86400 : 0;
  return Math.floor((time - anchor) / seconds) * seconds + anchor;
}

export function logicalIndexAtTime(
  candles: readonly { time: number }[],
  time: number,
  interval: number,
) {
  if (!candles.length) return null;
  if (time < candles[0].time) return (time - candles[0].time) / interval;
  const last = candles.length - 1;
  if (time > candles[last].time)
    return last + (time - candles[last].time) / interval;
  let lo = 0,
    hi = last;
  while (lo < hi) {
    const mid = (lo + hi) >>> 1;
    if (candles[mid].time < time) lo = mid + 1;
    else hi = mid;
  }
  if (candles[lo].time === time || lo === 0) return lo;
  return (
    lo -
    1 +
    (time - candles[lo - 1].time) / (candles[lo].time - candles[lo - 1].time)
  );
}
