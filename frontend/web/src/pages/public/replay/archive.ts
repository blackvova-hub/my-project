import { http } from "../../../shared/api/http";
import { intervals, validateCandles, type ArchiveQuery, type ArchiveSymbol, type Candle } from "./model";

const maxChunkMilliseconds = 30 * 86400000;

export function replayWindow(query: ArchiveQuery, symbol: ArchiveSymbol, selectedTime: number) {
  const step = intervals[query.timeframe] * 1000;
  const first = Math.ceil(symbol.first / step) * step;
  const last = Math.floor(Math.min(symbol.last + step, Date.now()) / step) * step;
  const anchor = Math.max(first, Math.min(last - step, Math.ceil(selectedTime * 1000 / step) * step));
  return {
    from: Math.max(first, anchor - 2000 * step),
    to: Math.min(last, anchor + 5001 * step),
    anchor: anchor / 1000,
  };
}

export async function fetchReplayRange(query: ArchiveQuery, from: number, to: number, signal: AbortSignal) {
  const step = intervals[query.timeframe] * 1000;
  const chunk = Math.floor(Math.min(maxChunkMilliseconds, 10000 * step) / step) * step;
  const windows: { from: number; to: number }[] = [];
  for (let at = from; at < to; at += chunk) windows.push({ from: at, to: Math.min(to, at + chunk) });
  const responses: { candles: Candle[]; missing: number }[] = [];
  for (let offset = 0; offset < windows.length; offset += 4) {
    const batch = await Promise.all(windows.slice(offset, offset + 4).map(async (window) => {
      const params = new URLSearchParams({
        exchange: query.exchange,
        market: query.market,
        symbol: query.symbol,
        timeframe: query.timeframe,
        from: String(window.from),
        to: String(window.to),
      });
      return http<{ candles: Candle[]; missing: number }>(`/replay/candles?${params}`, { signal });
    }));
    responses.push(...batch);
  }
  const candles = validateCandles(responses.flatMap((part) => (part.candles ?? []).map((candle) => ({ ...candle, time: candle.time / 1000 }))));
  return { candles, missing: responses.reduce((sum, part) => sum + part.missing, 0) };
}
