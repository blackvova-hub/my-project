import type { PrimaryExchange } from "../exchange/primaryExchange";
import { normalizeCandles, type Candle } from "./engine";
import type {
  PerpPriceFormat,
  PerpStreamStatus,
} from "../market/perpMarketData";
export const CHART_INTERVALS = [
  { value: "1m", label: "1м", seconds: 60 },
  { value: "5m", label: "5м", seconds: 300 },
  { value: "15m", label: "15м", seconds: 900 },
  { value: "30m", label: "30м", seconds: 1800 },
  { value: "1h", label: "1ч", seconds: 3600 },
  { value: "4h", label: "4ч", seconds: 14400 },
  { value: "1d", label: "1д", seconds: 86400 },
  { value: "1w", label: "1н", seconds: 604800 },
] as const;
export type ChartInterval = (typeof CHART_INTERVALS)[number]["value"];
export type Market = "linear" | "spot";
export type Instrument = {
  exchange: PrimaryExchange;
  symbol: string;
  market: Market;
};
const bybitIntervals: Record<ChartInterval, string> = {
  "1m": "1",
  "5m": "5",
  "15m": "15",
  "30m": "30",
  "1h": "60",
  "4h": "240",
  "1d": "D",
  "1w": "W",
};
const base = (instrument: Instrument) =>
  instrument.exchange === "bybit"
    ? "https://api.bybit.com"
    : instrument.market === "spot"
      ? "https://api.binance.com"
      : "https://fapi.binance.com";
export const instrumentKey = (i: Instrument) =>
  `${i.exchange}:${i.market}:${i.symbol}`;
async function requestJson(url: string, signal: AbortSignal): Promise<unknown> {
  const response = await fetch(url, {
    signal: AbortSignal.any([signal, AbortSignal.timeout(15000)]),
  });
  if (!response.ok) throw new Error(`Источник ответил HTTP ${response.status}`);
  const raw = await response.json();
  if (raw?.retCode !== undefined && raw.retCode !== 0)
    throw new Error(raw.retMsg || "Ошибка биржи");
  return raw;
}
const pendingRequests = new Map<
  string,
  {
    controller: AbortController;
    promise: Promise<unknown>;
    consumers: number;
    settled: boolean;
  }
>();
function json(url: string, signal: AbortSignal): Promise<unknown> {
  if (signal.aborted)
    return Promise.reject(new DOMException("Request cancelled", "AbortError"));
  let request = pendingRequests.get(url);
  if (!request) {
    const controller = new AbortController();
    request = {
      controller,
      promise: requestJson(url, controller.signal),
      consumers: 0,
      settled: false,
    };
    pendingRequests.set(url, request);
    const current = request;
    const settled = () => {
      current.settled = true;
      if (pendingRequests.get(url) === current) pendingRequests.delete(url);
    };
    current.promise.then(settled, settled);
  }
  const shared = request;
  shared.consumers++;
  return new Promise((resolve, reject) => {
    let released = false;
    const release = () => {
      if (released) return;
      released = true;
      signal.removeEventListener("abort", abort);
      shared.consumers--;
      if (!shared.consumers && !shared.settled) {
        shared.controller.abort();
        if (pendingRequests.get(url) === shared) pendingRequests.delete(url);
      }
    };
    const abort = () => {
      release();
      reject(new DOMException("Request cancelled", "AbortError"));
    };
    signal.addEventListener("abort", abort, { once: true });
    shared.promise.then(
      (value) => {
        if (!released) {
          release();
          resolve(value);
        }
      },
      (error) => {
        if (!released) {
          release();
          reject(error);
        }
      },
    );
  });
}
export async function fetchMarketCandles(
  i: Instrument,
  interval: ChartInterval,
  signal: AbortSignal,
  before?: number,
): Promise<Candle[]> {
  const query = new URLSearchParams({
    symbol: i.symbol,
    interval: i.exchange === "bybit" ? bybitIntervals[interval] : interval,
    limit: "1000",
  });
  if (before)
    query.set(
      i.exchange === "bybit" ? "end" : "endTime",
      String(Math.floor(before * 1000) - 1),
    );
  if (i.exchange === "bybit") query.set("category", i.market);
  const raw = (await json(
    `${base(i)}${i.exchange === "bybit" ? "/v5/market/kline" : i.market === "spot" ? "/api/v3/klines" : "/fapi/v1/klines"}?${query}`,
    signal,
  )) as { result?: { list?: unknown[][] } } | unknown[][];
  const rows = Array.isArray(raw) ? raw : raw.result?.list;
  if (!Array.isArray(rows)) throw new Error("Источник не вернул свечи");
  return normalizeCandles(
    rows.map((r) => ({
      time: Number(r[0]) / 1000,
      open: Number(r[1]),
      high: Number(r[2]),
      low: Number(r[3]),
      close: Number(r[4]),
      volume: Number(r[5]),
    })),
  );
}
export async function fetchMarketPriceFormat(
  i: Instrument,
  signal: AbortSignal,
): Promise<PerpPriceFormat | null> {
  const url =
    i.exchange === "bybit"
      ? `${base(i)}/v5/market/instruments-info?category=${i.market}&symbol=${i.symbol}`
      : `${base(i)}${i.market === "spot" ? "/api/v3/exchangeInfo" : "/fapi/v1/exchangeInfo"}?symbol=${i.symbol}`;
  const raw = (await json(url, signal)) as {
    result?: { list?: { priceFilter?: { tickSize?: string } }[] };
    symbols?: {
      symbol: string;
      filters?: { filterType: string; tickSize?: string }[];
    }[];
  };
  const step =
    i.exchange === "bybit"
      ? raw.result?.list?.[0]?.priceFilter?.tickSize
      : raw.symbols
          ?.find((s) => s.symbol === i.symbol)
          ?.filters?.find((f) => f.filterType === "PRICE_FILTER")?.tickSize;
  if (!step || !Number.isFinite(Number(step)) || Number(step) <= 0) return null;
  const trimmed = step.replace(/0+$/, "");
  const precision = trimmed.includes("e-")
    ? Number(trimmed.split("e-")[1])
    : trimmed.includes(".")
      ? trimmed.length - trimmed.indexOf(".") - 1
      : 0;
  return {
    minMove: Number(step),
    precision: Math.max(0, Math.min(10, precision)),
  };
}
function connectMarketCandles(
  i: Instrument,
  interval: ChartInterval,
  onCandle: (c: Candle) => void,
  onStatus: (s: PerpStreamStatus) => void,
) {
  let socket: WebSocket | null = null,
    disposed = false,
    retries = 0,
    reconnect: ReturnType<typeof setTimeout> | null = null,
    heartbeat: ReturnType<typeof setInterval> | null = null;
  const connect = () => {
    if (disposed) return;
    onStatus(retries ? "reconnecting" : "connecting");
    const binance = i.exchange === "binance",
      url = binance
        ? `${i.market === "spot" ? "wss://stream.binance.com:9443/ws" : "wss://fstream.binance.com/ws"}/${i.symbol.toLowerCase()}@kline_${interval}`
        : `wss://stream.bybit.com/v5/public/${i.market}`;
    socket = new WebSocket(url);
    socket.onopen = () => {
      retries = 0;
      onStatus("live");
      if (!binance) {
        socket?.send(
          JSON.stringify({
            op: "subscribe",
            args: [`kline.${bybitIntervals[interval]}.${i.symbol}`],
          }),
        );
        heartbeat = setInterval(() => {
          if (socket?.readyState === WebSocket.OPEN)
            socket.send(JSON.stringify({ op: "ping" }));
        }, 20000);
      }
    };
    socket.onmessage = (e) => {
      try {
        const raw = JSON.parse(String(e.data));
        if (raw.success === false) {
          onStatus("offline");
          return;
        }
        const k = binance ? raw.k : raw.data?.[0];
        if (!k) return;
        const c = {
          time: Number(binance ? k.t : k.start) / 1000,
          open: Number(binance ? k.o : k.open),
          high: Number(binance ? k.h : k.high),
          low: Number(binance ? k.l : k.low),
          close: Number(binance ? k.c : k.close),
          volume: Number(binance ? k.v : k.volume),
        };
        const valid = normalizeCandles([c])[0];
        if (valid) onCandle(valid);
      } catch {
        /* Ignore heartbeat frames. */
      }
    };
    socket.onerror = () => socket?.close();
    socket.onclose = () => {
      if (heartbeat) clearInterval(heartbeat);
      heartbeat = null;
      if (!disposed) {
        onStatus("reconnecting");
        reconnect = setTimeout(connect, Math.min(1000 * 2 ** retries++, 15000));
      }
    };
  };
  connect();
  return () => {
    disposed = true;
    if (reconnect) clearTimeout(reconnect);
    if (heartbeat) clearInterval(heartbeat);
    socket?.close(1000, "chart_unmounted");
  };
}
type Listener = {
  candle: (c: Candle) => void;
  status: (s: PerpStreamStatus) => void;
};
const sharedStreams = new Map<
  string,
  {
    listeners: Set<Listener>;
    stop: () => void;
    status: PerpStreamStatus;
    last?: Candle;
  }
>();
// Windows/comparisons with the same instrument share one socket. The last
// subscriber owns shutdown; reconnecting never duplicates a transport.
export function subscribeMarketCandles(
  i: Instrument,
  interval: ChartInterval,
  onCandle: Listener["candle"],
  onStatus: Listener["status"],
) {
  const key = `${instrumentKey(i)}:${interval}`,
    listener = { candle: onCandle, status: onStatus };
  let shared = sharedStreams.get(key);
  if (!shared) {
    shared = { listeners: new Set(), stop: () => {}, status: "connecting" };
    sharedStreams.set(key, shared);
    const feed = shared;
    feed.stop = connectMarketCandles(
      i,
      interval,
      (c) => {
        feed.last = c;
        for (const target of feed.listeners) target.candle(c);
      },
      (status) => {
        feed.status = status;
        for (const target of feed.listeners) target.status(status);
      },
    );
  }
  shared.listeners.add(listener);
  onStatus(shared.status);
  if (shared.last) onCandle(shared.last);
  return () => {
    shared!.listeners.delete(listener);
    if (!shared!.listeners.size) {
      shared!.stop();
      sharedStreams.delete(key);
    }
  };
}
export type MarketTicker = {
  symbol: string;
  last: number | null;
  change: number | null;
  volume: number | null;
  funding: number | null;
  openInterest: number | null;
  high: number | null;
  low: number | null;
};
export async function fetchMarketTickers(
  i: Instrument,
  signal: AbortSignal,
): Promise<MarketTicker[]> {
  const raw = (await json(
    i.exchange === "bybit"
      ? `${base(i)}/v5/market/tickers?category=${i.market}`
      : `${base(i)}${i.market === "spot" ? "/api/v3/ticker/24hr" : "/fapi/v1/ticker/24hr"}`,
    signal,
  )) as
    | { result?: { list?: Record<string, string>[] } }
    | Record<string, string>[];
  const num = (v: unknown) =>
    v === undefined || v === null || v === ""
      ? null
      : Number.isFinite(Number(v))
        ? Number(v)
        : null;
  return (Array.isArray(raw) ? raw : (raw.result?.list ?? []))
    .map((t) => ({
      symbol: t.symbol,
      last: num(t.lastPrice),
      change:
        i.exchange === "bybit"
          ? num(t.price24hPcnt) === null
            ? null
            : Number(t.price24hPcnt) * 100
          : num(t.priceChangePercent),
      volume: num(t.turnover24h ?? t.quoteVolume),
      funding: num(t.fundingRate),
      openInterest: num(t.openInterestValue),
      high: num(t.highPrice24h ?? t.highPrice),
      low: num(t.lowPrice24h ?? t.lowPrice),
    }))
    .filter((t) => t.symbol.endsWith("USDT"));
}
