import { logicalIndexAtTime } from "./seriesData";
export type TimeViewport = { from: number; to: number };
export function timeAtLogicalIndex(
  candles: readonly { time: number }[],
  index: number,
  interval: number,
) {
  if (!candles.length) return null;
  const last = candles.length - 1;
  if (index <= 0) return candles[0].time + index * interval;
  if (index >= last) return candles[last].time + (index - last) * interval;
  const left = Math.floor(index);
  return (
    candles[left].time +
    (index - left) * (candles[left + 1].time - candles[left].time)
  );
}
export function temporalViewport(
  candles: readonly { time: number }[],
  range: { from: number; to: number },
  interval: number,
): TimeViewport | null {
  const from = timeAtLogicalIndex(candles, range.from, interval),
    to = timeAtLogicalIndex(candles, range.to, interval);
  return from !== null && to !== null && to > from
    ? { from: Math.max(1, from), to }
    : null;
}
export function logicalViewport(
  candles: readonly { time: number }[],
  range: TimeViewport,
  interval: number,
) {
  const from = logicalIndexAtTime(candles, range.from, interval),
    to = logicalIndexAtTime(candles, range.to, interval);
  return from !== null && to !== null && to > from ? { from, to } : null;
}
export function exactCandleAt(
  candles: readonly { time: number; close: number }[],
  time: number,
  interval: number,
) {
  // Crosshair uses the containing real bar, never invents a price inside a gap.
  let left = 0,
    right = candles.length - 1;
  while (left <= right) {
    const mid = (left + right) >>> 1;
    if (candles[mid].time <= time) left = mid + 1;
    else right = mid - 1;
  }
  const point = candles[right];
  return point && time < point.time + interval ? point : null;
}
export function barCountdown(time: number, interval: number, now: number) {
  const remaining = Math.max(0, Math.ceil(time + interval - now));
  if (!remaining) return "закрыта";
  const days = Math.floor(remaining / 86400),
    hours = Math.floor((remaining % 86400) / 3600),
    minutes = Math.floor((remaining % 3600) / 60),
    seconds = remaining % 60;
  return `${days ? days + "д " : ""}${hours ? String(hours).padStart(2, "0") + ":" : ""}${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
}
