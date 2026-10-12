// Compatibility adapter; all market transport is owned by shared/charts.
import type { CandlestickData, UTCTimestamp } from "lightweight-charts";
import {
  normalizePerpetualSymbol,
  type PrimaryExchange,
} from "../exchange/primaryExchange";
import {
  fetchMarketCandles,
  fetchMarketPriceFormat,
  subscribeMarketCandles,
  type ChartInterval,
} from "../charts/marketData";
export type PerpInterval = ChartInterval;
export type PerpStreamStatus =
  | "connecting"
  | "live"
  | "reconnecting"
  | "offline";
export type PerpCandle = CandlestickData<UTCTimestamp> & { volume: number };
export type PerpPriceFormat = { precision: number; minMove: number };
export function normalizeLinearPerpSymbol(symbol: string) {
  const normalized = normalizePerpetualSymbol(symbol);
  if (
    normalized.endsWith("USDT") ||
    normalized.endsWith("USDC") ||
    normalized.endsWith("USD")
  ) {
    return normalized;
  }
  return `${normalized}USDT`;
}

export function inferPerpPriceFormat(
  candles: readonly PerpCandle[],
): PerpPriceFormat {
  const reference = Math.abs(candles[candles.length - 1]?.close ?? 0);
  const precision =
    reference >= 1_000
      ? 1
      : reference >= 100
        ? 2
        : reference >= 1
          ? 4
          : Math.min(
              10,
              Math.max(
                5,
                Math.ceil(-Math.log10(Math.max(reference, 1e-10))) + 4,
              ),
            );
  return { precision, minMove: 10 ** -precision };
}

export async function fetchPerpPriceFormat(
  exchange: PrimaryExchange,
  rawSymbol: string,
  signal: AbortSignal,
) {
  try {
    return await fetchMarketPriceFormat(
      {
        exchange,
        market: "linear",
        symbol: normalizeLinearPerpSymbol(rawSymbol),
      },
      signal,
    );
  } catch {
    return null;
  }
}
export async function fetchPerpCandles(
  exchange: PrimaryExchange,
  rawSymbol: string,
  interval: PerpInterval,
  signal: AbortSignal,
  options: { before?: UTCTimestamp; limit?: number } = {},
) {
  const instrument = {
      exchange,
      market: "linear" as const,
      symbol: normalizeLinearPerpSymbol(rawSymbol),
    },
    limit = Math.min(15000, Math.max(1, options.limit ?? 1000));
  let data: PerpCandle[] = [],
    cursor: number | undefined = options.before;
  while (data.length < limit) {
    const page = (await fetchMarketCandles(
      instrument,
      interval,
      signal,
      cursor,
    )) as PerpCandle[];
    if (!page.length) break;
    data = [...page, ...data];
    if (page.length < 1000) break;
    cursor = page[0].time;
  }
  return data.slice(-limit);
}
export function subscribePerpCandles(options: {
  exchange: PrimaryExchange;
  symbol: string;
  interval: PerpInterval;
  onCandle: (c: PerpCandle) => void;
  onStatus: (status: PerpStreamStatus) => void;
}) {
  return subscribeMarketCandles(
    {
      exchange: options.exchange,
      market: "linear",
      symbol: normalizeLinearPerpSymbol(options.symbol),
    },
    options.interval,
    (c) => options.onCandle({ ...c, time: c.time as UTCTimestamp }),
    options.onStatus,
  );
}
