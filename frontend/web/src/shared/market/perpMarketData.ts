import type { CandlestickData, UTCTimestamp } from "lightweight-charts";

import {
  normalizePerpetualSymbol,
  type PrimaryExchange,
} from "../exchange/primaryExchange";

export type PerpInterval = "1m" | "5m" | "15m" | "1h" | "4h";
export type PerpStreamStatus = "connecting" | "live" | "reconnecting" | "offline";

export type PerpCandle = CandlestickData<UTCTimestamp> & { volume: number };
export type PerpPriceFormat = { precision: number; minMove: number };

const BINANCE_INTERVALS: Record<PerpInterval, string> = {
  "1m": "1m", "5m": "5m", "15m": "15m", "1h": "1h", "4h": "4h",
};

const BYBIT_INTERVALS: Record<PerpInterval, string> = {
  "1m": "1", "5m": "5", "15m": "15", "1h": "60", "4h": "240",
};

export function normalizeLinearPerpSymbol(symbol: string) {
  const normalized = normalizePerpetualSymbol(symbol);
  if (normalized.endsWith("USDT") || normalized.endsWith("USDC") || normalized.endsWith("USD")) {
    return normalized;
  }
  return `${normalized}USDT`;
}

function toFiniteNumber(value: unknown) {
  const parsed = typeof value === "number" ? value : Number(value);
  return Number.isFinite(parsed) ? parsed : null;
}

function precisionFromStep(rawStep: string) {
  const step = rawStep.trim().toLowerCase();
  if (step.includes("e-")) {
    const exponent = Number(step.split("e-")[1]);
    return Number.isInteger(exponent) ? Math.min(10, Math.max(0, exponent)) : null;
  }
  const normalized = step.replace(/0+$/, "");
  const dot = normalized.indexOf(".");
  return dot === -1 ? 0 : Math.min(10, normalized.length - dot - 1);
}

function priceFormatFromStep(rawStep: unknown, fallbackPrecision?: unknown): PerpPriceFormat | null {
  const stepText = typeof rawStep === "string" ? rawStep : String(rawStep ?? "");
  const minMove = Number(stepText);
  if (!Number.isFinite(minMove) || minMove <= 0) return null;
  const stepPrecision = precisionFromStep(stepText);
  const declaredPrecision = Number(fallbackPrecision);
  const precision = stepPrecision ?? (Number.isInteger(declaredPrecision) ? declaredPrecision : 2);
  return { precision: Math.min(10, Math.max(0, precision)), minMove };
}

export function inferPerpPriceFormat(candles: readonly PerpCandle[]): PerpPriceFormat {
  const reference = Math.abs(candles[candles.length - 1]?.close ?? 0);
  const precision = reference >= 1_000 ? 1
    : reference >= 100 ? 2
      : reference >= 1 ? 4
        : Math.min(10, Math.max(5, Math.ceil(-Math.log10(Math.max(reference, 1e-10))) + 4));
  return { precision, minMove: 10 ** -precision };
}

export async function fetchPerpPriceFormat(
  exchange: PrimaryExchange,
  rawSymbol: string,
  signal: AbortSignal,
): Promise<PerpPriceFormat | null> {
  const symbol = normalizeLinearPerpSymbol(rawSymbol);
  if (exchange === "binance") {
    const response = await fetch(`https://fapi.binance.com/fapi/v1/exchangeInfo?symbol=${encodeURIComponent(symbol)}`, { signal });
    if (!response.ok) return null;
    const payload = (await response.json()) as {
      symbols?: Array<{
        symbol?: string;
        pricePrecision?: number;
        filters?: Array<{ filterType?: string; tickSize?: string }>;
      }>;
    };
    // Binance currently returns the full exchangeInfo symbol list even when
    // the symbol query parameter is present. Never use the first row: it is
    // usually BTCUSDT and would round low-priced assets to BTC's 0.1 tick.
    const instrument = payload.symbols?.find(
      (candidate) => candidate.symbol?.toUpperCase() === symbol,
    );
    const tickSize = instrument?.filters?.find((filter) => filter.filterType === "PRICE_FILTER")?.tickSize;
    return priceFormatFromStep(tickSize, instrument?.pricePrecision);
  }

  const query = new URLSearchParams({ category: "linear", symbol });
  const response = await fetch(`https://api.bybit.com/v5/market/instruments-info?${query}`, { signal });
  if (!response.ok) return null;
  const payload = (await response.json()) as {
    retCode?: number;
    result?: { list?: Array<{ priceScale?: string; priceFilter?: { tickSize?: string } }> };
  };
  if (payload.retCode !== 0) return null;
  const instrument = payload.result?.list?.[0];
  return priceFormatFromStep(instrument?.priceFilter?.tickSize, instrument?.priceScale);
}

function toCandle(input: {
  timeMs: unknown; open: unknown; high: unknown; low: unknown; close: unknown; volume: unknown;
}): PerpCandle | null {
  const timeMs = toFiniteNumber(input.timeMs);
  const open = toFiniteNumber(input.open);
  const high = toFiniteNumber(input.high);
  const low = toFiniteNumber(input.low);
  const close = toFiniteNumber(input.close);
  const volume = toFiniteNumber(input.volume);
  if (timeMs === null || open === null || high === null || low === null || close === null || volume === null) {
    return null;
  }
  return {
    time: Math.floor(timeMs / 1000) as UTCTimestamp,
    open, high, low, close, volume,
  };
}

async function fetchBinanceCandles(
  symbol: string,
  interval: PerpInterval,
  signal: AbortSignal,
  before?: UTCTimestamp,
  limit = 1_000,
) {
  const query = new URLSearchParams({
    symbol,
    interval: BINANCE_INTERVALS[interval],
    limit: String(Math.min(limit, 1_500)),
  });
  if (before !== undefined) query.set("endTime", String(before * 1_000 - 1));
  const response = await fetch(`https://fapi.binance.com/fapi/v1/klines?${query}`, { signal });
  if (!response.ok) throw new Error("binance_history_failed");
  const rows = (await response.json()) as unknown;
  if (!Array.isArray(rows)) throw new Error("binance_history_invalid");
  return rows.map((row) => {
    if (!Array.isArray(row)) return null;
    return toCandle({ timeMs: row[0], open: row[1], high: row[2], low: row[3], close: row[4], volume: row[5] });
  }).filter((candle): candle is PerpCandle => candle !== null);
}

async function fetchBybitCandlePage(
  symbol: string,
  interval: PerpInterval,
  signal: AbortSignal,
  before?: UTCTimestamp,
  limit = 1_000,
) {
  const query = new URLSearchParams({
    category: "linear",
    symbol,
    interval: BYBIT_INTERVALS[interval],
    limit: String(Math.min(limit, 1_000)),
  });
  if (before !== undefined) query.set("end", String(before * 1_000 - 1));
  const response = await fetch(`https://api.bybit.com/v5/market/kline?${query}`, { signal });
  if (!response.ok) throw new Error("bybit_history_failed");
  const payload = (await response.json()) as { retCode?: number; result?: { list?: unknown[] } };
  if (payload.retCode !== 0 || !Array.isArray(payload.result?.list)) throw new Error("bybit_history_invalid");
  return payload.result.list.map((row) => {
    if (!Array.isArray(row)) return null;
    return toCandle({ timeMs: row[0], open: row[1], high: row[2], low: row[3], close: row[4], volume: row[5] });
  }).filter((candle): candle is PerpCandle => candle !== null).reverse();
}

async function fetchBybitCandles(
  symbol: string,
  interval: PerpInterval,
  signal: AbortSignal,
  before?: UTCTimestamp,
  limit = 1_000,
) {
  const requestedLimit = Math.max(1, limit);
  let remaining = requestedLimit;
  let cursor = before;
  let candles: PerpCandle[] = [];

  while (remaining > 0) {
    const pageLimit = Math.min(remaining, 1_000);
    const page = await fetchBybitCandlePage(symbol, interval, signal, cursor, pageLimit);
    if (page.length === 0) break;

    candles = [...page, ...candles];
    remaining -= page.length;
    if (page.length < pageLimit) break;
    cursor = page[0].time;
  }

  return candles;
}

export function fetchPerpCandles(
  exchange: PrimaryExchange,
  rawSymbol: string,
  interval: PerpInterval,
  signal: AbortSignal,
  options: { before?: UTCTimestamp; limit?: number } = {},
) {
  const symbol = normalizeLinearPerpSymbol(rawSymbol);
  return exchange === "binance"
    ? fetchBinanceCandles(symbol, interval, signal, options.before, options.limit)
    : fetchBybitCandles(symbol, interval, signal, options.before, options.limit);
}

function parseBinanceMessage(event: MessageEvent<string>) {
  const payload = JSON.parse(event.data) as {
    k?: { t?: number; o?: string; h?: string; l?: string; c?: string; v?: string };
  };
  const kline = payload.k;
  if (!kline) return null;
  return toCandle({
    timeMs: kline.t, open: kline.o, high: kline.h, low: kline.l, close: kline.c, volume: kline.v,
  });
}

function parseBybitMessage(event: MessageEvent<string>) {
  const payload = JSON.parse(event.data) as {
    data?: Array<{ start?: number; open?: string; high?: string; low?: string; close?: string; volume?: string }>;
  };
  const kline = payload.data?.[0];
  if (!kline) return null;
  return toCandle({
    timeMs: kline.start, open: kline.open, high: kline.high, low: kline.low, close: kline.close, volume: kline.volume,
  });
}

export function subscribePerpCandles(options: {
  exchange: PrimaryExchange;
  symbol: string;
  interval: PerpInterval;
  onCandle: (candle: PerpCandle) => void;
  onStatus: (status: PerpStreamStatus) => void;
}) {
  const symbol = normalizeLinearPerpSymbol(options.symbol);
  let disposed = false;
  let reconnectAttempt = 0;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  let socket: WebSocket | null = null;

  const clearHeartbeat = () => {
    if (heartbeatTimer) clearInterval(heartbeatTimer);
    heartbeatTimer = null;
  };

  const scheduleReconnect = () => {
    if (disposed) return;
    options.onStatus("reconnecting");
    const delay = Math.min(1_000 * 2 ** reconnectAttempt, 10_000);
    reconnectAttempt += 1;
    reconnectTimer = setTimeout(connect, delay);
  };

  const connect = () => {
    if (disposed) return;
    options.onStatus(reconnectAttempt > 0 ? "reconnecting" : "connecting");
    const isBinance = options.exchange === "binance";
    const interval = isBinance ? BINANCE_INTERVALS[options.interval] : BYBIT_INTERVALS[options.interval];
    const url = isBinance
      ? `wss://fstream.binance.com/market/ws/${symbol.toLowerCase()}@kline_${interval}`
      : "wss://stream.bybit.com/v5/public/linear";
    socket = new WebSocket(url);

    socket.onopen = () => {
      reconnectAttempt = 0;
      options.onStatus("live");
      if (!isBinance) {
        socket?.send(JSON.stringify({ op: "subscribe", args: [`kline.${interval}.${symbol}`] }));
        heartbeatTimer = setInterval(() => {
          if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ op: "ping" }));
        }, 20_000);
      }
    };

    socket.onmessage = (event: MessageEvent<string>) => {
      try {
        const candle = isBinance ? parseBinanceMessage(event) : parseBybitMessage(event);
        if (candle) options.onCandle(candle);
      } catch {
        // Public streams also send subscription acknowledgements and pong messages.
      }
    };

    socket.onerror = () => socket?.close();
    socket.onclose = () => {
      clearHeartbeat();
      scheduleReconnect();
    };
  };

  connect();

  return () => {
    disposed = true;
    if (reconnectTimer) clearTimeout(reconnectTimer);
    clearHeartbeat();
    socket?.close(1000, "chart_unmounted");
    socket = null;
  };
}
