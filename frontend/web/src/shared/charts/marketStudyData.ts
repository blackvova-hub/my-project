import { alignBarTime } from "./seriesData";
import {
  CHART_INTERVALS,
  type ChartInterval,
  type Instrument,
} from "./marketData";
import {
  DERIVATIVE_PANELS,
  derivativeAvailability,
  type DerivativeId,
} from "./derivativeCatalog";
export type Observation = { time: number; value: number };
export type StudySeries = {
  key: string;
  title: string;
  color: string;
  kind: "line" | "histogram" | "candles";
  data: (
    | Observation
    | { time: number; open: number; high: number; low: number; close: number }
  )[];
};
export type ExternalPane = {
  id: string;
  title: string;
  color: string;
  data: Observation[];
  series?: StudySeries[];
  percent?: boolean;
  error?: string;
  note?: string;
};
export type JsonRequest = (url: string) => Promise<unknown>;
type JsonObject = Record<string, unknown>;
function numeric(value: unknown) {
  return typeof value === "number" ||
    (typeof value === "string" && value.trim() !== "")
    ? Number(value)
    : Number.NaN;
}
function object(value: unknown): JsonObject {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Некорректный ответ API");
  return value as JsonObject;
}
function list(raw: unknown, exchange: Instrument["exchange"]): unknown[] {
  const value = exchange === "bybit" ? object(object(raw).result).list : raw;
  if (!Array.isArray(value)) throw new Error("API не вернул историю");
  return value;
}
export function bucketObservations(
  rows: readonly Observation[],
  seconds: number,
) {
  const map = new Map<number, Observation>();
  for (const p of [...rows]
    .filter(
      (v) => v.time > 0 && Number.isFinite(v.time) && Number.isFinite(v.value),
    )
    .sort((a, b) => a.time - b.time)) {
    const time = alignBarTime(p.time, seconds);
    map.set(time, { time, value: p.value });
  }
  return [...map.values()];
}
export function observationCandles(
  rows: readonly Observation[],
  seconds: number,
) {
  const map = new Map<
    number,
    { time: number; open: number; high: number; low: number; close: number }
  >();
  for (const p of [...rows]
    .filter(
      (v) => v.time > 0 && Number.isFinite(v.time) && Number.isFinite(v.value),
    )
    .sort((a, b) => a.time - b.time)) {
    const time = alignBarTime(p.time, seconds),
      prev = map.get(time);
    if (prev) {
      prev.high = Math.max(prev.high, p.value);
      prev.low = Math.min(prev.low, p.value);
      prev.close = p.value;
    } else
      map.set(time, {
        time,
        open: p.value,
        high: p.value,
        low: p.value,
        close: p.value,
      });
  }
  return [...map.values()];
}
export function takerRows(raw: unknown) {
  if (!Array.isArray(raw)) throw new Error("Binance не вернул свечи");
  return raw
    .map((v) => {
      if (!Array.isArray(v) || v.length < 11)
        throw new Error("В свечах отсутствуют taker volumes");
      const row = {
        time: numeric(v[0]) / 1000,
        total: numeric(v[5]),
        quote: numeric(v[7]),
        buy: numeric(v[9]),
        buyQuote: numeric(v[10]),
      };
      if (
        !Object.values(row).every(Number.isFinite) ||
        row.time <= 0 ||
        row.buy < 0 ||
        row.buy > row.total + 1e-8 ||
        row.buyQuote < 0 ||
        row.buyQuote > row.quote + 1e-6
      )
        throw new Error("Некорректные объёмы API");
      return {
        ...row,
        sell: Math.max(0, row.total - row.buy),
        sellQuote: Math.max(0, row.quote - row.buyQuote),
      };
    })
    .sort((a, b) => a.time - b.time);
}
function klineUrl(i: Instrument, iv: ChartInterval) {
  const symbol = encodeURIComponent(i.symbol);
  return i.exchange === "bybit"
    ? `https://api.bybit.com/v5/market/kline?category=${i.market}&symbol=${symbol}&interval=${bybitInterval(iv)}&limit=1000`
    : `https://${i.market === "spot" ? "api.binance.com/api/v3" : "fapi.binance.com/fapi/v1"}/klines?symbol=${symbol}&interval=${iv}&limit=1000`;
}
function bybitInterval(iv: ChartInterval) {
  return iv === "1d"
    ? "D"
    : iv === "1w"
      ? "W"
      : String(CHART_INTERVALS.find((t) => t.value === iv)!.seconds / 60);
}
function quoteRows(raw: unknown, exchange: Instrument["exchange"]) {
  return list(raw, exchange)
    .flatMap((v) => {
      if (!Array.isArray(v)) return [];
      const time = numeric(v[0]) / 1000,
        value = numeric(v[exchange === "bybit" ? 6 : 7]);
      return time > 0 && Number.isFinite(value) && value >= 0
        ? [{ time, value }]
        : [];
    })
    .sort((a, b) => a.time - b.time);
}
export async function fetchMarketStudy(
  i: Instrument,
  iv: ChartInterval,
  id: DerivativeId,
  get: JsonRequest,
): Promise<ExternalPane> {
  const info = DERIVATIVE_PANELS.find((p) => p.id === id)!,
    availability = derivativeAvailability(i, id);
  if (availability) throw new Error(availability);
  const seconds = CHART_INTERVALS.find((t) => t.value === iv)!.seconds,
    period = iv === "1m" ? "5m" : iv === "1w" ? "1d" : iv,
    symbol = encodeURIComponent(i.symbol),
    prefix =
      i.exchange === "bybit"
        ? "https://api.bybit.com"
        : "https://fapi.binance.com";
  let rows: Observation[] = [],
    series: StudySeries[] | undefined,
    note = "Данные выбранной биржи · обновление 60с",
    percent = false;
  if (id === "fear_greed") {
    const raw = object(await get("/api/market/overview")),
      sentiment = object(raw.sentiment);
    if (!Array.isArray(sentiment.history90))
      throw new Error("История настроения рынка отсутствует");
    rows = sentiment.history90.map((v) => {
      const p = object(v);
      return { time: numeric(p.ts), value: numeric(p.value) };
    });
    note =
      "Alternative.me · общий рынок, не выбранный актив · дневные значения, до 90 дней · без интерполяции";
  } else if (id === "liquidations" || id === "aggregate_liquidations") {
    const raw = object(
      await get(
        `/api/market/liquidations/history?symbol=${symbol}&exchange=${id === "aggregate_liquidations" ? "all" : i.exchange}`,
      ),
    );
    if (!Array.isArray(raw.points))
      throw new Error("История ликвидаций отсутствует");
    const buckets = new Map<number, number>();
    for (const value of raw.points) {
      const p = object(value),
        time = alignBarTime(numeric(p.time), seconds),
        usd = numeric(p.value);
      if (time > 0 && Number.isFinite(usd) && usd > 0)
        buckets.set(time, (buckets.get(time) ?? 0) + usd);
    }
    rows = [...buckets]
      .map(([time, value]) => ({ time, value }))
      .sort((a, b) => a.time - b.time);
    series = [
      {
        key: id,
        title: info.label,
        color: info.color,
        kind: "histogram",
        data: rows,
      },
    ];
    note =
      "Существующие сканеры · доступные события последних 24ч · неполное покрытие · отсутствие точки не означает нулевые ликвидации · без деления по стороне";
  } else if (id === "aggregate_oi") {
    const [a, b] = await Promise.all([
        fetchMarketStudy({ ...i, exchange: "bybit" }, iv, "oi", get),
        fetchMarketStudy({ ...i, exchange: "binance" }, iv, "oi", get),
      ]),
      right = new Map(b.data.map((p) => [p.time, p.value]));
    rows = a.data.flatMap((v) =>
      right.has(v.time)
        ? [{ time: v.time, value: v.value + right.get(v.time)! }]
        : [],
    );
    note =
      "Bybit + Binance · OI базового актива · только общие интервалы · не все биржи";
  } else if (
    [
      "taker_volume",
      "taker_delta",
      "taker_value",
      "taker_value_delta",
      "cvd",
    ].includes(id)
  ) {
    const source = takerRows(await get(klineUrl(i, iv))),
      valueMode = id.includes("value");
    if (id === "taker_volume" || id === "taker_value") {
      const buy = source.map((v) => ({
          time: v.time,
          value: valueMode ? v.buyQuote : v.buy,
        })),
        sell = source.map((v) => ({
          time: v.time,
          value: -(valueMode ? v.sellQuote : v.sell),
        }));
      rows = buy;
      series = [
        {
          key: "buy",
          title: "Taker Buy",
          kind: "histogram",
          color: "#34d399",
          data: buy,
        },
        {
          key: "sell",
          title: "Taker Sell (−)",
          kind: "histogram",
          color: "#fb7185",
          data: sell,
        },
      ];
    } else {
      let cvd = 0;
      rows = source.map((v) => {
        const delta = valueMode ? v.buyQuote - v.sellQuote : v.buy - v.sell;
        return { time: v.time, value: id === "cvd" ? (cvd += delta) : delta };
      });
      series = [
        {
          key: id,
          title: info.label,
          kind: id === "cvd" ? "line" : "histogram",
          color: info.color,
          data: rows,
        },
      ];
    }
    note = `Binance ${i.market} · ${source.length} баров · ${valueMode ? "USDT" : "базовый актив"}${id === "cvd" && source.length ? ` · CVD от ${new Date(source[0].time * 1000).toISOString().slice(0, 16)} UTC` : ""} · CVD не показывает внутрисвечные экстремумы`;
  } else if (id === "volume_ratio" || id === "aggregate_volume") {
    const a = i,
      b =
        id === "volume_ratio"
          ? {
              ...i,
              market:
                i.market === "spot" ? ("linear" as const) : ("spot" as const),
            }
          : {
              ...i,
              exchange:
                i.exchange === "bybit"
                  ? ("binance" as const)
                  : ("bybit" as const),
            };
    const [rawA, rawB] = await Promise.all([
        get(klineUrl(a, iv)),
        get(klineUrl(b, iv)),
      ]),
      left = quoteRows(rawA, a.exchange),
      right = new Map(
        quoteRows(rawB, b.exchange).map((v) => [v.time, v.value]),
      );
    rows = left.flatMap((v) => {
      const other = right.get(v.time);
      if (other === undefined) return [];
      const value =
        id === "aggregate_volume"
          ? v.value + other
          : i.market === "linear"
            ? other > 0
              ? v.value / other
              : NaN
            : v.value > 0
              ? other / v.value
              : NaN;
      return Number.isFinite(value) ? [{ time: v.time, value }] : [];
    });
    note =
      id === "volume_ratio"
        ? "Futures quote volume / Spot quote volume · только совпадающие бары"
        : "Bybit + Binance · сумма фактического quote volume · только общие бары · не все биржи";
  } else if (id === "basis" || id === "index_price" || id === "premium") {
    const interval = i.exchange === "bybit" ? bybitInterval(iv) : iv;
    const indexUrl =
      i.exchange === "bybit"
        ? `${prefix}/v5/market/index-price-kline?category=linear&symbol=${symbol}&interval=${interval}&limit=1000`
        : `${prefix}/fapi/v1/indexPriceKlines?pair=${symbol}&interval=${interval}&limit=1000`;
    if (id === "basis") {
      const markUrl =
        i.exchange === "bybit"
          ? `${prefix}/v5/market/mark-price-kline?category=linear&symbol=${symbol}&interval=${interval}&limit=1000`
          : `${prefix}/fapi/v1/markPriceKlines?symbol=${symbol}&interval=${interval}&limit=1000`;
      const [mark, index] = await Promise.all([get(markUrl), get(indexUrl)]),
        indexed = new Map(
          list(index, i.exchange)
            .filter(Array.isArray)
            .map((v) => [numeric(v[0]), numeric(v[4])]),
        );
      rows = list(mark, i.exchange)
        .filter(Array.isArray)
        .flatMap((v) => {
          const price = indexed.get(numeric(v[0]));
          return price && price > 0
            ? [
                {
                  time: numeric(v[0]) / 1000,
                  value: (numeric(v[4]) / price - 1) * 100,
                },
              ]
            : [];
        });
      percent = true;
    } else {
      const url =
        id === "index_price"
          ? indexUrl
          : i.exchange === "bybit"
            ? `${prefix}/v5/market/premium-index-price-kline?category=linear&symbol=${symbol}&interval=${interval}&limit=1000`
            : `${prefix}/fapi/v1/premiumIndexKlines?symbol=${symbol}&interval=${interval}&limit=1000`;
      rows = list(await get(url), i.exchange)
        .filter(Array.isArray)
        .map((v) => ({
          time: numeric(v[0]) / 1000,
          value: numeric(v[4]) * (id === "premium" ? 100 : 1),
        }));
      percent = id === "premium";
    }
  } else {
    const oi = ["oi", "oi_delta", "oi_candles", "oi_value"].includes(id),
      funding = id.startsWith("funding"),
      p = id === "oi_candles" ? "5m" : period,
      bybit = p.endsWith("m") ? p.replace("m", "min") : p;
    const url =
      i.exchange === "bybit"
        ? oi
          ? `${prefix}/v5/market/open-interest?category=linear&symbol=${symbol}&intervalTime=${bybit}&limit=200`
          : funding
            ? `${prefix}/v5/market/funding/history?category=linear&symbol=${symbol}&limit=200`
            : `${prefix}/v5/market/account-ratio?category=linear&symbol=${symbol}&period=${period}&limit=500`
        : oi
          ? `${prefix}/futures/data/openInterestHist?symbol=${symbol}&period=${p}&limit=500`
          : funding
            ? `${prefix}/fapi/v1/fundingRate?symbol=${symbol}&limit=1000`
            : `${prefix}/futures/data/${id === "top_accounts" ? "topLongShortAccountRatio" : id === "top_positions" ? "topLongShortPositionRatio" : "globalLongShortAccountRatio"}?symbol=${symbol}&period=${period}&limit=500`;
    rows = list(await get(url), i.exchange)
      .map((value) => {
        const v = object(value);
        return {
          time:
            numeric(v.timestamp ?? v.fundingRateTimestamp ?? v.fundingTime) /
            1000,
          value: oi
            ? numeric(
                id === "oi_value"
                  ? v.sumOpenInterestValue
                  : (v.openInterest ?? v.sumOpenInterest),
              )
            : funding
              ? numeric(v.fundingRate) * 100
              : i.exchange === "bybit"
                ? numeric(v.buyRatio) / numeric(v.sellRatio)
                : numeric(v.longShortRatio),
        };
      })
      .sort((a, b) => a.time - b.time);
    percent = funding;
    note = funding
      ? "Фактические выплаты funding · последние 200 Bybit / 1000 Binance · ставки не суммируются"
      : `Снимки ${id === "oi_candles" ? "5м" : period} · до ${i.exchange === "bybit" && oi ? 200 : 500} наблюдений · без интерполяции`;
    if (id === "oi_delta") {
      rows = bucketObservations(rows, seconds);
      rows = rows
        .slice(1)
        .map((v, j) => ({ time: v.time, value: v.value - rows[j].value }));
    }
    if (id === "oi_candles" || id === "funding_candles") {
      const candles = observationCandles(rows, seconds);
      series = [
        {
          key: id,
          title: info.label,
          color: info.color,
          kind: "candles",
          data: candles,
        },
      ];
      note += " · OHLC доступных снимков, не полный внутрибара ряд";
    }
  }
  rows = bucketObservations(rows, seconds);
  if (
    !rows.length &&
    (id === "liquidations" || id === "aggregate_liquidations")
  )
    throw new Error(
      "Нет наблюдавшихся событий за 24ч; полнота покрытия не гарантируется",
    );
  if (!rows.length)
    throw new Error(
      "История отсутствует для выбранного инструмента / интервала",
    );
  return {
    id,
    title: info.label,
    color: info.color,
    data: rows,
    series,
    percent,
    note,
  };
}
