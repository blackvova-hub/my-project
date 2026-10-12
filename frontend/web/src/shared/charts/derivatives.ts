import { useQuery } from "@tanstack/react-query";
import { alignBarTime } from "./seriesData";
import {
  CHART_INTERVALS,
  type ChartInterval,
  type Instrument,
} from "./marketData";
export const DERIVATIVE_PANELS = [
  { id: "oi", label: "Открытый интерес · базовый актив", color: "#38bdf8" },
  { id: "funding", label: "Funding · фактическая ставка, %", color: "#fbbf24" },
  { id: "basis", label: "Базис · mark/index, %", color: "#61d99b" },
  { id: "ratio", label: "Long/Short · отношение аккаунтов", color: "#c084fc" },
] as const;
export type DerivativeId = (typeof DERIVATIVE_PANELS)[number]["id"];
export type ExternalPane = {
  id: string;
  title: string;
  color: string;
  data: { time: number; value: number }[];
  percent?: boolean;
  error?: string;
};
async function get(url: string, signal: AbortSignal) {
  const r = await fetch(url, { signal });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  const data = await r.json();
  if (data.retCode !== undefined && data.retCode !== 0)
    throw new Error(data.retMsg ?? "Биржа не вернула данные");
  return data;
}
export async function fetchDerivative(
  i: Instrument,
  interval: ChartInterval,
  id: DerivativeId,
  signal: AbortSignal,
): Promise<ExternalPane> {
  const info = DERIVATIVE_PANELS.find((p) => p.id === id)!,
    seconds = CHART_INTERVALS.find((t) => t.value === interval)!.seconds,
    period = interval === "1m" ? "5m" : interval === "1w" ? "1d" : interval,
    bybit =
      period === "1d"
        ? "1d"
        : period === "1h"
          ? "1h"
          : period === "4h"
            ? "4h"
            : period.replace("m", "min");
  const prefix =
      i.exchange === "bybit"
        ? "https://api.bybit.com"
        : "https://fapi.binance.com",
    symbol = encodeURIComponent(i.symbol);
  let rows: { time: number; value: number }[] = [];
  if (id === "basis") {
    const iv =
      i.exchange === "bybit"
        ? interval === "1d"
          ? "D"
          : interval === "1w"
            ? "W"
            : String(seconds / 60)
        : interval;
    const markUrl =
      i.exchange === "bybit"
        ? `${prefix}/v5/market/mark-price-kline?category=linear&symbol=${symbol}&interval=${iv}&limit=1000`
        : `${prefix}/fapi/v1/markPriceKlines?symbol=${symbol}&interval=${iv}&limit=1000`;
    const indexUrl =
      i.exchange === "bybit"
        ? `${prefix}/v5/market/index-price-kline?category=linear&symbol=${symbol}&interval=${iv}&limit=1000`
        : `${prefix}/fapi/v1/indexPriceKlines?pair=${symbol}&interval=${iv}&limit=1000`;
    const [mark, index] = await Promise.all([
      get(markUrl, signal),
      get(indexUrl, signal),
    ]);
    const markRows: unknown[][] =
        i.exchange === "bybit" ? mark.result.list : mark,
      indexRows: unknown[][] =
        i.exchange === "bybit" ? index.result.list : index;
    const indexed = new Map(indexRows.map((v) => [Number(v[0]), Number(v[4])]));
    rows = markRows.flatMap((v) => {
      const price = indexed.get(Number(v[0]));
      return price && price > 0
        ? [
            {
              time: Number(v[0]) / 1000,
              value: (Number(v[4]) / price - 1) * 100,
            },
          ]
        : [];
    });
  } else {
    const url =
      i.exchange === "bybit"
        ? id === "oi"
          ? `${prefix}/v5/market/open-interest?category=linear&symbol=${symbol}&intervalTime=${bybit}&limit=200`
          : id === "funding"
            ? `${prefix}/v5/market/funding/history?category=linear&symbol=${symbol}&limit=200`
            : `${prefix}/v5/market/account-ratio?category=linear&symbol=${symbol}&period=${period}&limit=500`
        : id === "oi"
          ? `${prefix}/futures/data/openInterestHist?symbol=${symbol}&period=${period}&limit=500`
          : id === "funding"
            ? `${prefix}/fapi/v1/fundingRate?symbol=${symbol}&limit=1000`
            : `${prefix}/futures/data/globalLongShortAccountRatio?symbol=${symbol}&period=${period}&limit=500`;
    const raw = await get(url, signal);
    const list: Record<string, unknown>[] =
      i.exchange === "bybit" ? raw.result.list : raw;
    rows = list.map((v) => ({
      time:
        Number(v.timestamp ?? v.fundingRateTimestamp ?? v.fundingTime) / 1000,
      value:
        id === "oi"
          ? Number(v.openInterest ?? v.sumOpenInterest)
          : id === "funding"
            ? Number(v.fundingRate) * 100
            : i.exchange === "bybit"
              ? Number(v.buyRatio) / Number(v.sellRatio)
              : Number(v.longShortRatio),
    }));
  }
  const buckets = new Map<number, { time: number; value: number }>();
  for (const p of rows
    .filter(
      (v) => Number.isFinite(v.time) && v.time > 0 && Number.isFinite(v.value),
    )
    .sort((a, b) => a.time - b.time)) {
    const time = alignBarTime(p.time, seconds);
    buckets.set(time, { time, value: p.value });
  }
  return {
    id,
    title: info.label,
    color: info.color,
    percent: id === "basis" || id === "funding",
    data: [...buckets.values()],
  };
}
export function useDerivativePanels(
  i: Instrument,
  interval: ChartInterval,
  ids: DerivativeId[],
) {
  return useQuery({
    queryKey: [
      "terminal-derivatives",
      i.exchange,
      i.market,
      i.symbol,
      interval,
      ids.join(","),
    ],
    enabled: i.market === "linear" && !!ids.length,
    queryFn: async ({ signal }) =>
      Promise.all(
        ids.map(async (id) => {
          try {
            return await fetchDerivative(i, interval, id, signal);
          } catch (e) {
            const info = DERIVATIVE_PANELS.find((p) => p.id === id)!;
            return {
              id,
              title: info.label,
              color: info.color,
              data: [],
              error: (e as Error).message,
            } as ExternalPane;
          }
        }),
      ),
    staleTime: 60000,
    refetchInterval: 60000,
    refetchIntervalInBackground: false,
    retry: false,
  });
}
