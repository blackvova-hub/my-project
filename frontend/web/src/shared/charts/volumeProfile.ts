import type { Candle } from "./engine";
export function calculateVolumeProfile(candles: readonly Candle[], rows = 32) {
  if (!candles.length) return [];
  let low = Infinity,
    high = -Infinity;
  for (const c of candles) {
    low = Math.min(low, c.low);
    high = Math.max(high, c.high);
  }
  const step =
    (high - low) / rows || Math.max(Math.abs(low) * 0.000001, 0.00000001);
  const bins = Array.from({ length: rows }, (_, i) => ({
    low: low + i * step,
    high: low + (i + 1) * step,
    volume: 0,
  }));
  for (const c of candles) {
    if (!Number.isFinite(c.volume) || c.volume <= 0) continue;
    if (c.high === c.low) {
      bins[
        Math.max(0, Math.min(rows - 1, Math.floor((c.close - low) / step)))
      ].volume += c.volume;
      continue;
    }
    const first = Math.max(0, Math.floor((c.low - low) / step)),
      last = Math.min(rows - 1, Math.floor((c.high - low) / step));
    for (let i = first; i <= last; i++)
      bins[i].volume +=
        (c.volume *
          Math.max(
            0,
            Math.min(c.high, bins[i].high) - Math.max(c.low, bins[i].low),
          )) /
        (c.high - c.low);
  }
  return bins;
}
