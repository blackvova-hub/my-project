import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { PrimaryExchange } from "../exchange/primaryExchange";

export type DerivativesMarketData = {
  symbol: string;
  source: string;
  asOf: string;
  openInterest: {
    usd: number;
    change1h: number | null;
    change4h: number | null;
    change24h: number | null;
  };
  funding: {
    rate: number | null;
    nextFundingTime: number;
  };
  liquidations: {
    windowHours: number;
    totalUsd: number;
    byExchange: Array<{ exchange: string; usd: number }>;
  };
};

export type CexLiquiditySnapshot = {
  bidPrice: number;
  askPrice: number;
  spreadUsd: number;
  spreadPct: number;
  within05: { bidsUsd: number; asksUsd: number; totalUsd: number };
  within1: { bidsUsd: number; asksUsd: number; totalUsd: number };
  depth: number;
  source: "rest" | "websocket";
};

type OrderbookPayload = {
  type?: "snapshot" | "delta";
  topic?: string;
  bids?: string[][];
  asks?: string[][];
  result?: { b?: string[][]; a?: string[][] };
  data?: { b?: string[][]; a?: string[][] };
};

const EMPTY_LEVELS: string[][] = [];

export function useDerivativesMarketData(exchange: PrimaryExchange, symbol: string, enabled: boolean) {
  return useQuery({
    queryKey: ["derivativesMarketData", exchange, symbol] as const,
    queryFn: async ({ signal }): Promise<DerivativesMarketData> => {
      const params = new URLSearchParams({ exchange, symbol });
      const response = await fetch(`/api/market/derivatives?${params.toString()}`, { signal });
      if (!response.ok) throw new Error("derivatives_market_failed");
      return (await response.json()) as DerivativesMarketData;
    },
    enabled: enabled && Boolean(symbol),
    staleTime: 15_000,
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    retry: 1,
  });
}

function updateSide(side: Map<number, number>, levels: string[][]) {
  for (const level of levels) {
    const price = Number(level[0]);
    const size = Number(level[1]);
    if (!Number.isFinite(price) || price <= 0 || !Number.isFinite(size) || size < 0) continue;
    if (size === 0) side.delete(price);
    else side.set(price, size);
  }
}

function calculateLiquidity(
  bids: Map<number, number>,
  asks: Map<number, number>,
  source: CexLiquiditySnapshot["source"],
): CexLiquiditySnapshot | null {
  let bidPrice = 0;
  let askPrice = Number.POSITIVE_INFINITY;
  for (const price of bids.keys()) bidPrice = Math.max(bidPrice, price);
  for (const price of asks.keys()) askPrice = Math.min(askPrice, price);
  if (bidPrice <= 0 || !Number.isFinite(askPrice) || askPrice <= bidPrice) return null;

  const mid = (bidPrice + askPrice) / 2;
  let bids05 = 0;
  let asks05 = 0;
  let bids1 = 0;
  let asks1 = 0;
  for (const [price, size] of bids) {
    const notional = price * size;
    if (price >= mid * 0.995) bids05 += notional;
    if (price >= mid * 0.99) bids1 += notional;
  }
  for (const [price, size] of asks) {
    const notional = price * size;
    if (price <= mid * 1.005) asks05 += notional;
    if (price <= mid * 1.01) asks1 += notional;
  }

  const spreadUsd = askPrice - bidPrice;
  return {
    bidPrice,
    askPrice,
    spreadUsd,
    spreadPct: (spreadUsd / mid) * 100,
    within05: { bidsUsd: bids05, asksUsd: asks05, totalUsd: bids05 + asks05 },
    within1: { bidsUsd: bids1, asksUsd: asks1, totalUsd: bids1 + asks1 },
    depth: Math.min(bids.size, asks.size),
    source,
  };
}

export function useCexOrderbookLiquidity(exchange: PrimaryExchange, symbol: string, enabled: boolean) {
  const [snapshotState, setSnapshotState] = useState<{
    exchange: PrimaryExchange;
    symbol: string;
    data: CexLiquiditySnapshot;
  } | null>(null);

  useEffect(() => {
    if (!enabled || !symbol || typeof WebSocket === "undefined") {
      return;
    }

    const normalizedSymbol = symbol.toUpperCase();
    const bids = new Map<number, number>();
    const asks = new Map<number, number>();
    const controller = new AbortController();
    let socket: WebSocket | null = null;
    let reconnectTimer = 0;
    let heartbeatTimer = 0;
    let restRefreshTimer = 0;
    let disposed = false;
    let websocketSnapshotReceived = false;

    const publish = (source: CexLiquiditySnapshot["source"]) => {
      const next = calculateLiquidity(bids, asks, source);
      if (next && !disposed) setSnapshotState({ exchange, symbol: normalizedSymbol, data: next });
    };

    const applySnapshot = (bidLevels: string[][], askLevels: string[][], source: CexLiquiditySnapshot["source"]) => {
      bids.clear();
      asks.clear();
      updateSide(bids, bidLevels);
      updateSide(asks, askLevels);
      publish(source);
    };

    const fetchRestSnapshot = () => {
      const url = exchange === "binance"
        ? `https://fapi.binance.com/fapi/v1/depth?symbol=${encodeURIComponent(normalizedSymbol)}&limit=1000`
        : `https://api.bybit.com/v5/market/orderbook?category=linear&symbol=${encodeURIComponent(normalizedSymbol)}&limit=200`;
      void fetch(url, { signal: controller.signal })
        .then((response) => response.ok ? response.json() as Promise<OrderbookPayload> : null)
        .then((payload) => {
          if (!payload || websocketSnapshotReceived) return;
          if (exchange === "binance") {
            applySnapshot(payload.bids ?? EMPTY_LEVELS, payload.asks ?? EMPTY_LEVELS, "rest");
          } else if (payload.result) {
            applySnapshot(payload.result.b ?? EMPTY_LEVELS, payload.result.a ?? EMPTY_LEVELS, "rest");
          }
        })
        .catch(() => undefined);
    };

    fetchRestSnapshot();
    if (exchange === "binance") {
      restRefreshTimer = window.setInterval(fetchRestSnapshot, 10_000);
      return () => {
        disposed = true;
        controller.abort();
        window.clearInterval(restRefreshTimer);
      };
    }

    const connect = () => {
      if (disposed) return;
      socket = new WebSocket("wss://stream.bybit.com/v5/public/linear");
      socket.addEventListener("open", () => {
        socket?.send(JSON.stringify({ op: "subscribe", args: [`orderbook.1000.${normalizedSymbol}`] }));
        window.clearInterval(heartbeatTimer);
        heartbeatTimer = window.setInterval(() => {
          if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ op: "ping" }));
        }, 20_000);
      });
      socket.addEventListener("message", (event) => {
        let payload: OrderbookPayload;
        try {
          payload = JSON.parse(String(event.data)) as OrderbookPayload;
        } catch {
          return;
        }
        if (payload.topic !== `orderbook.1000.${normalizedSymbol}` || !payload.data) return;
        if (payload.type === "snapshot") {
          websocketSnapshotReceived = true;
          applySnapshot(payload.data.b ?? EMPTY_LEVELS, payload.data.a ?? EMPTY_LEVELS, "websocket");
          return;
        }
        updateSide(bids, payload.data.b ?? EMPTY_LEVELS);
        updateSide(asks, payload.data.a ?? EMPTY_LEVELS);
        publish("websocket");
      });
      socket.addEventListener("close", () => {
        window.clearInterval(heartbeatTimer);
        if (!disposed) reconnectTimer = window.setTimeout(connect, 2_000);
      });
    };

    connect();
    return () => {
      disposed = true;
      controller.abort();
      window.clearTimeout(reconnectTimer);
      window.clearInterval(heartbeatTimer);
      window.clearInterval(restRefreshTimer);
      socket?.close();
    };
  }, [enabled, exchange, symbol]);

  return enabled && snapshotState?.exchange === exchange && snapshotState.symbol === symbol.toUpperCase()
    ? snapshotState.data
    : null;
}
