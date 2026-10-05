import { useMemo } from "react";
import { useQueries } from "@tanstack/react-query";

import type { PrimaryExchange } from "../exchange/primaryExchange";

export type MarketQuote = {
  last: number | null;
  changePct: number | null;
};

const EMPTY_QUOTE: MarketQuote = { last: null, changePct: null };

async function fetchTicker(
  exchange: PrimaryExchange,
  symbol: string,
  signal: AbortSignal,
): Promise<MarketQuote> {
  if (exchange === "binance") {
    const response = await fetch(
      `https://fapi.binance.com/fapi/v1/ticker/24hr?symbol=${encodeURIComponent(symbol)}`,
      { signal },
    );
    if (!response.ok) throw new Error("binance_ticker_failed");
    const item = (await response.json()) as {
      lastPrice?: string;
      priceChangePercent?: string;
    };
    const last = Number(item.lastPrice);
    const changePct = Number(item.priceChangePercent);
    return {
      last: Number.isFinite(last) ? last : null,
      changePct: Number.isFinite(changePct) ? changePct : null,
    };
  }

  const response = await fetch(
    `https://api.bybit.com/v5/market/tickers?category=linear&symbol=${encodeURIComponent(symbol)}`,
    { signal },
  );
  if (!response.ok) throw new Error("bybit_ticker_failed");
  const data = (await response.json()) as {
    result?: { list?: Array<{ lastPrice?: string; price24hPcnt?: string }> };
  };
  const item = data.result?.list?.[0];
  if (!item) return EMPTY_QUOTE;
  const last = Number(item.lastPrice);
  const changePct = Number(item.price24hPcnt);
  return {
    last: Number.isFinite(last) ? last : null,
    changePct: Number.isFinite(changePct) ? changePct * 100 : null,
  };
}

export function useTickerQuotes(
  exchange: PrimaryExchange | null,
  symbols: string[],
  refetchInterval: number,
) {
  const normalizedSymbols = useMemo(
    () => Array.from(new Set(symbols.map((symbol) => symbol.trim().toUpperCase()).filter(Boolean))),
    [symbols],
  );
  const results = useQueries({
    queries: normalizedSymbols.map((symbol) => ({
      queryKey: ["marketTicker", exchange, symbol] as const,
      queryFn: ({ signal }: { signal: AbortSignal }) => fetchTicker(exchange!, symbol, signal),
      enabled: exchange !== null,
      staleTime: 15_000,
      refetchInterval,
      refetchIntervalInBackground: false,
      refetchOnWindowFocus: true,
      retry: 1,
    })),
  });

  return useMemo(() => {
    const quotes: Record<string, MarketQuote> = {};
    normalizedSymbols.forEach((symbol, index) => {
      quotes[symbol] = results[index]?.data ?? EMPTY_QUOTE;
    });
    return quotes;
  }, [normalizedSymbols, results]);
}
