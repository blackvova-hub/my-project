import { lazy, Suspense, useEffect, useId, useState } from "react";
import { createPortal } from "react-dom";
import { LayoutGroup, motion, useReducedMotion } from "framer-motion";
import { useQuery } from "@tanstack/react-query";
import { useAuth } from "../../../../shared/auth/AuthContext";
import {
  coinGlassTVURL,
  exchangeTradeURL,
  normalizePerpetualSymbol,
  tradingViewPerpetualSymbol,
  type PrimaryExchange,
} from "../../../../shared/exchange/primaryExchange";
import { useCoinInfo } from "../../../../shared/market/coinInfoQueries";
import { SimilarityPanel } from "../../../../shared/market/SimilarityPanel";
import { useSymbolTradeStats } from "../../scanners/queries";

const PerpChart = lazy(() =>
  import("../../../../shared/market/PerpChart").then((module) => ({ default: module.PerpChart })),
);
const CoinResearchPanel = lazy(() =>
  import("../../../../shared/market/CoinResearchPanel").then((module) => ({ default: module.CoinResearchPanel })),
);

type SentimentPoint = {
  ts: number;
  value: number;
};

type MarketMode = "spot" | "futures";

type PriceTrend = {
  up: number;
  flat: number;
  down: number;
  thresholdPct: number;
  total: number;
};

type TopMover = {
  symbol: string;
  displayName: string;
  changePct: number;
  direction: "up" | "down" | "flat";
  lastPrice: number;
};

type MarketSlice = {
  priceTrend: PriceTrend;
  topMovers: TopMover[];
};

type MarketOverview = {
  sentiment: {
    value: number;
    classification: string;
    history30: SentimentPoint[];
    history90: SentimentPoint[];
  };
  marketCap: {
    usd: number;
    change24h: number;
    change24hAvailable: boolean;
    series: number[];
    source: string;
    sourceUpdatedAt: string;
  };
  volume24h: {
    usd: number;
    change24h: number;
    change24hAvailable: boolean;
    series: number[];
    source: string;
    sourceUpdatedAt: string;
  };
  priceTrend: PriceTrend;
  topMovers: TopMover[];
  markets?: Partial<Record<MarketMode, MarketSlice>>;
  exchanges?: Partial<Record<PrimaryExchange, Partial<Record<MarketMode, MarketSlice>>>>;
  updatedAt: string;
};

const MARKET_OVERVIEW_REFRESH_MS = 120_000;

function baseSymbol(symbol: string) {
  const normalized = normalizePerpetualSymbol(symbol);
  const quotes = ["USDT", "USDC", "BUSD", "FDUSD", "USD", "BTC", "ETH"];
  for (const quote of quotes) {
    if (normalized.endsWith(quote) && normalized.length > quote.length) {
      return normalized.slice(0, -quote.length);
    }
  }
  return normalized;
}

function MetricChart({ values, positive }: { values: number[]; positive: boolean }) {
  const gradId = useId();
  const strokeColor = positive ? "#38d996" : "#ff6577";
  const fillColor = positive ? "#33c98a" : "#f45b6d";
  const safeValues = values.filter((value) => Number.isFinite(value) && value > 0);

  if (safeValues.length < 2) {
    return (
      <div className="flex h-[88px] w-full items-center justify-center text-[11px] text-muted-foreground">
        История накапливается
      </div>
    );
  }

  const min = Math.min(...safeValues);
  const max = Math.max(...safeValues);
  const isFlat = max === min;
  const range = max - min || 1;
  const width = 260;
  const height = 88;
  const top = 7;
  const bottom = 82;
  const chartPoints = safeValues.map((v, i) => {
    const x = (i / Math.max(safeValues.length - 1, 1)) * width;
    const y = isFlat ? (top + bottom) / 2 : bottom - ((v - min) / range) * (bottom - top);
    return { x, y };
  });
  const firstPoint = chartPoints[0];
  const lastPoint = chartPoints.at(-1)!;
  let linePath = `M ${firstPoint.x} ${firstPoint.y}`;
  for (let index = 1; index < chartPoints.length; index += 1) {
    const previous = chartPoints[index - 1];
    const current = chartPoints[index];
    const middleX = (previous.x + current.x) / 2;
    linePath += ` C ${middleX} ${previous.y}, ${middleX} ${current.y}, ${current.x} ${current.y}`;
  }
  const areaPath = `${linePath} L ${lastPoint.x} ${height} L ${firstPoint.x} ${height} Z`;

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      className="h-[88px] w-full overflow-hidden"
      aria-hidden="true"
    >
      <defs>
        <linearGradient id={gradId} x1="0" x2="0" y1="0" y2="1">
          <stop offset="0%" stopColor={fillColor} stopOpacity="0.3" />
          <stop offset="72%" stopColor={fillColor} stopOpacity="0.08" />
          <stop offset="100%" stopColor={fillColor} stopOpacity="0.015" />
        </linearGradient>
      </defs>
      <path d={areaPath} fill={`url(#${gradId})`} />
      <path
        d={linePath}
        fill="none"
        stroke={strokeColor}
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="1.7"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}

function DistributionBar({ down, flat, up }: { down: number; flat: number; up: number }) {
  const total = Math.max(down + flat + up, 1);
  const downPct = (down / total) * 100;
  const flatPct = (flat / total) * 100;
  const upPct = (up / total) * 100;

  return (
    <div className="space-y-3">
      <div className="h-5 overflow-hidden rounded-full border border-border bg-background">
        <div className="flex h-full w-full">
          <div style={{ width: `${downPct}%` }} className="bg-rose-500/90" />
          <div style={{ width: `${flatPct}%` }} className="bg-muted-foreground/70" />
          <div style={{ width: `${upPct}%` }} className="bg-emerald-500/90" />
        </div>
      </div>
      <div className="grid grid-cols-3 gap-2 text-xs">
        <div className="rounded-xl border border-border bg-background px-4 py-3 text-destructive">
          <div className="text-2xl font-semibold leading-none">{down}</div>
          <div className="mt-0.5 text-[11px] text-destructive">{downPct.toFixed(1)}%</div>
          <div className="mt-1 text-muted-foreground">Падение</div>
        </div>
        <div className="rounded-xl border border-border bg-background px-4 py-3 text-foreground">
          <div className="text-2xl font-semibold leading-none">{flat}</div>
          <div className="mt-0.5 text-[11px] text-foreground">{flatPct.toFixed(1)}%</div>
          <div className="mt-1 text-muted-foreground">Без изменений</div>
        </div>
        <div className="rounded-xl border border-border bg-background px-4 py-3 text-primary">
          <div className="text-2xl font-semibold leading-none">{up}</div>
          <div className="mt-0.5 text-[11px] text-primary">{upPct.toFixed(1)}%</div>
          <div className="mt-1 text-muted-foreground">Рост</div>
        </div>
      </div>
    </div>
  );
}

function blendColor(a: [number, number, number], b: [number, number, number], t: number) {
  const safeT = Math.max(0, Math.min(1, t));
  const r = Math.round(a[0] + (b[0] - a[0]) * safeT);
  const g = Math.round(a[1] + (b[1] - a[1]) * safeT);
  const bch = Math.round(a[2] + (b[2] - a[2]) * safeT);
  return `rgb(${r}, ${g}, ${bch})`;
}

function sentimentTone(value: number) {
  const safe = Math.max(0, Math.min(100, value));
  const red: [number, number, number] = [239, 68, 68];
  const orange: [number, number, number] = [245, 158, 11];
  const green: [number, number, number] = [34, 197, 94];
  if (safe <= 50) {
    return blendColor(red, orange, safe / 50);
  }
  return blendColor(orange, green, (safe - 50) / 50);
}

function SentimentGauge({ value, label }: { value: number; label: string }) {
  const gradId = useId();
  const safeValue = Math.max(0, Math.min(100, value));
  const tone = sentimentTone(safeValue);
  const angle = 180 - (safeValue / 100) * 180;
  const rad = (angle * Math.PI) / 180;
  const cx = 100;
  const cy = 88;
  const needleRadius = 62;
  const nx = cx + Math.cos(rad) * needleRadius;
  const ny = cy - Math.sin(rad) * needleRadius;

  return (
    <div className="relative rounded-2xl border border-border bg-background p-4">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">Настроение рынка</div>
      <svg viewBox="0 0 200 120" className="mt-2 h-28 w-full">
        <defs>
          <linearGradient id={gradId} x1="0" x2="1" y1="0" y2="0">
            <stop offset="0%" stopColor="#ef4444" />
            <stop offset="50%" stopColor="#f59e0b" />
            <stop offset="100%" stopColor="#22c55e" />
          </linearGradient>
        </defs>
        <path d="M20 88 A80 80 0 0 1 180 88" stroke={`url(#${gradId})`} strokeWidth="8" fill="none" />
        <path d="M30 88 A70 70 0 0 1 170 88" stroke="var(--border-strong)" strokeWidth="1" fill="none" />
        <line x1={cx} y1={cy} x2={nx} y2={ny} stroke="var(--foreground)" strokeWidth="2" strokeLinecap="round" />
        <circle cx={cx} cy={cy} r="3.5" fill="var(--foreground)" opacity="0.95" />
      </svg>
      <div className="-mt-8 text-center">
        <div className="text-4xl font-semibold text-foreground">{safeValue.toFixed(0)}</div>
        <div
          className="inline-block rounded-full border border-border bg-muted px-2.5 py-0.5 text-sm"
          style={{ color: tone }}
        >
          {label || "Настроение"}
        </div>
      </div>
    </div>
  );
}

function translateSentimentLabel(raw: string) {
  const value = raw.trim();
  if (!value) return "";
  const lower = value.toLowerCase();
  if (lower.includes("extreme fear")) return "Экстремальный страх";
  if (lower.includes("fear")) return "Страх";
  if (lower.includes("neutral")) return "Нейтрально";
  if (lower.includes("extreme greed")) return "Экстремальная жадность";
  if (lower.includes("greed")) return "Жадность";
  return value;
}

function fmtBillions(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "—";
  const billions = value / 1_000_000_000;
  return `${billions.toLocaleString("ru-RU", { minimumFractionDigits: 2, maximumFractionDigits: 2 })} млрд долларов США`;
}

function fmtPct(value: number) {
  if (!Number.isFinite(value)) return "—";
  const normalized = Math.abs(value) < 0.005 ? 0 : value;
  return `${normalized >= 0 ? "+" : ""}${normalized.toFixed(2)}%`;
}

type GlobalMetric = MarketOverview["marketCap"];

function formatSource(metric?: GlobalMetric) {
  if (!metric?.source) return "Источник недоступен";
  const updatedAt = new Date(metric.sourceUpdatedAt);
  if (Number.isNaN(updatedAt.getTime())) return `Источник: ${metric.source}`;
  return `Источник: ${metric.source} · ${updatedAt.toLocaleTimeString("ru-RU", {
    hour: "2-digit",
    minute: "2-digit",
  })}`;
}

function GlobalMetricCard({ title, metric }: { title: string; metric?: GlobalMetric }) {
  const hasValue = Number.isFinite(metric?.usd) && (metric?.usd ?? 0) > 0;
  const changeAvailable = hasValue && metric?.change24hAvailable === true && Number.isFinite(metric.change24h);
  const change = changeAvailable ? metric.change24h : 0;

  return (
    <div className="flex min-h-[214px] flex-col overflow-hidden rounded-2xl border border-border bg-background px-4 pt-4">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <span>{title}</span>
        <span
          className="inline-flex h-3.5 w-3.5 items-center justify-center rounded-full border border-border-strong text-[9px] text-muted-foreground"
          title="Глобальный показатель крипторынка. Источник и время его обновления указаны внизу карточки."
        >
          i
        </span>
      </div>
      <div
        className={`mt-1 text-[1.35rem] font-semibold leading-none ${
          changeAvailable ? (change >= 0 ? "text-primary" : "text-destructive") : "text-muted-foreground"
        }`}
      >
        {changeAvailable ? fmtPct(change) : "—"}
      </div>
      <div className="mt-1.5 text-[12px] font-semibold text-foreground">{hasValue ? fmtBillions(metric!.usd) : "—"}</div>
      <div className="mt-2 min-h-[88px] flex-1">
        <MetricChart values={metric?.series ?? []} positive={!changeAvailable || change >= 0} />
      </div>
      <div className="pb-3 pt-1 text-[10px] text-muted-foreground">{formatSource(metric)}</div>
    </div>
  );
}

export function MarketInsightsWidget() {
  const { primaryExchange } = useAuth();
	const reduceMotion = useReducedMotion();
  const [marketMode, setMarketMode] = useState<MarketMode>("spot");
  const [infoSymbol, setInfoSymbol] = useState<string | null>(null);
  const overviewQuery = useQuery({
    queryKey: ["marketOverview"] as const,
    queryFn: async ({ signal }) => {
      const response = await fetch("/api/market/overview", { signal });
      if (!response.ok) throw new Error("market_overview_failed");
      return (await response.json()) as MarketOverview;
    },
    staleTime: 60_000,
    refetchInterval: MARKET_OVERVIEW_REFRESH_MS,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    retry: 1,
  });
  const data = overviewQuery.data ?? null;
  const loading = overviewQuery.isPending;
  const error = overviewQuery.error instanceof Error ? overviewQuery.error.message : "";

  const emptyMarket: MarketSlice = {
    priceTrend: { up: 0, flat: 0, down: 0, thresholdPct: 0.01, total: 0 },
    topMovers: [],
  };
  const legacySpotMarket: MarketSlice = {
    priceTrend: data?.priceTrend ?? { up: 0, flat: 0, down: 0, thresholdPct: 0.5, total: 0 },
    topMovers: data?.topMovers ?? [],
  };
  const selectedMarket =
    (primaryExchange ? data?.exchanges?.[primaryExchange]?.[marketMode] : undefined) ??
    data?.markets?.[marketMode] ??
    (marketMode === "spot" ? legacySpotMarket : emptyMarket);
  const marketModeLabel = marketMode === "spot" ? "спота" : "фьючерсов";
  const exchangeLabel = primaryExchange === "binance" ? "Binance" : "Bybit";
  const infoExchange = primaryExchange ?? "bybit";
  const normalizedInfoSymbol = infoSymbol ? normalizePerpetualSymbol(infoSymbol) : "";
  const infoBase = normalizedInfoSymbol ? baseSymbol(normalizedInfoSymbol) : "";
  const coinInfoQuery = useCoinInfo(infoBase, Boolean(infoSymbol));
  const symbolStatsQuery = useSymbolTradeStats(normalizedInfoSymbol, Boolean(infoSymbol));
  const coinInfo = coinInfoQuery.data ?? null;
  const symbolStats = symbolStatsQuery.data ?? null;
  const statsLoading = symbolStatsQuery.isPending && symbolStatsQuery.isFetching;
  const infoError = coinInfoQuery.error
    ? coinInfoQuery.error.message === "coin_not_found"
      ? "Монета не найдена на CoinGecko."
      : "Не удалось загрузить данные CoinGecko."
    : "";
  const infoLinks = infoSymbol ? {
    bybit: exchangeTradeURL("bybit", normalizedInfoSymbol),
    binance: exchangeTradeURL("binance", normalizedInfoSymbol),
    coinglass: coinGlassTVURL(infoExchange, normalizedInfoSymbol),
    tradingview: `https://www.tradingview.com/chart/?symbol=${encodeURIComponent(tradingViewPerpetualSymbol(infoExchange, normalizedInfoSymbol))}`,
  } : null;

  useEffect(() => {
    if (!infoSymbol || typeof document === "undefined") return;
    const previousOverflow = document.body.style.overflow;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setInfoSymbol(null);
    };
    document.body.style.overflow = "hidden";
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [infoSymbol]);

  return (
    <section className="rounded-2xl border border-border bg-card p-4 md:p-5 min-w-0">
      <div>
        <h3 className="text-lg font-semibold tracking-tight text-foreground">Настроение рынка</h3>
        <p className="mt-1 text-xs text-muted-foreground">Глобальный срез рынка: настроение, капитализация, объем, тренд и лидеры движения.</p>
      </div>

      {error ? (
        <div className="mt-3 rounded-xl border border-rose-400/30 bg-rose-500/10 px-3 py-2 text-xs text-destructive">
          Не удалось загрузить обзор рынка.
        </div>
      ) : null}

      <div className="mt-3 grid gap-2.5 lg:grid-cols-[minmax(0,1fr)_300px]">
        <div className="grid min-w-0 gap-2.5 md:grid-cols-3">
          <SentimentGauge
            value={data?.sentiment.value ?? 0}
            label={translateSentimentLabel(data?.sentiment.classification ?? "")}
          />

          <GlobalMetricCard title="Рыночная капитализация" metric={data?.marketCap} />

          <GlobalMetricCard title="Объем торгов" metric={data?.volume24h} />

          <div className="rounded-2xl border border-border bg-background p-4 md:col-span-3">
            <div className="flex items-center justify-between">
              <div className="text-base font-semibold text-foreground">Распределение ценового тренда</div>
              <div className="text-xs text-muted-foreground">
                {exchangeLabel}, {marketModeLabel}, всего монет: {selectedMarket.priceTrend.total}
              </div>
            </div>
            <div className="mt-3">
              <DistributionBar
                down={selectedMarket.priceTrend.down}
                flat={selectedMarket.priceTrend.flat}
                up={selectedMarket.priceTrend.up}
              />
            </div>
          </div>
        </div>

        <div className="h-full rounded-2xl border border-border bg-background p-3.5">
		  <LayoutGroup id="market-mode-tabs">
			<div className="mb-3 grid grid-cols-2 rounded-xl border border-border bg-background p-1 text-xs font-semibold">
			{[
              ["spot", "Спот"],
              ["futures", "Фьючерсы"],
			].map(([mode, label]) => {
			  const active = marketMode === mode;
			  return (
			  <motion.button
                key={mode}
                type="button"
                onClick={() => setMarketMode(mode as MarketMode)}
				whileTap={reduceMotion ? undefined : { scale: 0.98 }}
                className={[
				  "relative isolate rounded-lg px-3 py-1.5 transition-colors duration-200",
				  active
				    ? "text-primary"
                    : "text-muted-foreground hover:bg-card hover:text-foreground",
                ].join(" ")}
              >
				{active ? (
				  <motion.span
					layoutId="market-mode-active"
					className="absolute inset-0 -z-10 rounded-lg bg-accent ring-1 ring-border"
					transition={reduceMotion ? { duration: 0 } : { type: "spring", stiffness: 520, damping: 38, mass: 0.7 }}
				  />
				) : null}
				<span className="relative z-10">{label}</span>
			  </motion.button>
			  );
			})}
			</div>
		  </LayoutGroup>
          <div className="text-base font-semibold text-foreground">Лидеры движения</div>
          <div className="mt-2.5 space-y-1.5">
            {selectedMarket.topMovers.slice(0, 6).map((row) => (
              <div
                key={row.symbol}
                className="flex w-full items-center justify-between rounded-xl border border-border bg-card px-2.5 py-2 text-left"
              >
                <div>
                  <button
                    type="button"
                    onClick={() => setInfoSymbol(row.symbol)}
                    aria-label={`Открыть информацию о ${row.displayName || row.symbol}`}
                    className="rounded-sm text-sm font-semibold text-foreground transition-colors hover:text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    {row.displayName || row.symbol}
                  </button>
                  <div className="text-[11px] text-muted-foreground">{row.changePct >= 0 ? "Рост за 24ч" : "Падение за 24ч"}</div>
                </div>
                <span className={`text-sm font-semibold ${row.changePct >= 0 ? "text-primary" : "text-destructive"}`}>
                  {fmtPct(row.changePct)}
                </span>
              </div>
            ))}
            {!loading && selectedMarket.topMovers.length === 0 ? (
              <div className="text-xs text-muted-foreground">Пока нет данных по муверам.</div>
            ) : null}
          </div>
        </div>
      </div>

      {infoSymbol && typeof document !== "undefined"
        ? createPortal(
            <div className="fixed inset-0 z-[90] flex items-center justify-center px-4 py-6" role="dialog" aria-modal="true" aria-label={`Информация о ${normalizedInfoSymbol}`}>
              <button
                type="button"
                aria-label="Закрыть информацию о монете"
                className="absolute inset-0 bg-scrim"
                onClick={() => setInfoSymbol(null)}
              />
              <div className="relative max-h-[94vh] w-full max-w-[96vw] overflow-y-auto rounded-3xl border border-border-strong bg-surface-raised p-4 text-sm text-foreground shadow-2xl sm:p-6">
                <div className="flex items-center gap-3 overflow-x-auto whitespace-nowrap pb-1 [scrollbar-width:thin]">
                  <h3 className="shrink-0 text-xl font-semibold text-foreground sm:text-2xl">
                    {normalizedInfoSymbol} {coinInfo ? `• ${coinInfo.name}` : ""}
                  </h3>
                  {infoLinks ? (
                    <div className="flex shrink-0 items-center gap-1.5">
                      <a href={infoLinks.bybit} target="_blank" rel="noreferrer" className="rounded-full border border-border bg-secondary px-3 py-1.5 text-[11px] font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground">Bybit</a>
                      <a href={infoLinks.binance} target="_blank" rel="noreferrer" className="rounded-full border border-border bg-secondary px-3 py-1.5 text-[11px] font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground">Binance</a>
                      <a href={infoLinks.coinglass} target="_blank" rel="noreferrer" className="rounded-full border border-border bg-secondary px-3 py-1.5 text-[11px] font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground">Coinglass</a>
                      <a href={infoLinks.tradingview} target="_blank" rel="noreferrer" className="rounded-full border border-border bg-secondary px-3 py-1.5 text-[11px] font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground">TradingView</a>
                    </div>
                  ) : null}
                  <button
                    type="button"
                    onClick={() => setInfoSymbol(null)}
                    className="ml-auto shrink-0 rounded-lg border border-border-strong bg-accent px-3 py-2 text-xs text-foreground hover:bg-accent"
                  >
                    Закрыть
                  </button>
                </div>

                <Suspense fallback={<div className="mt-4 h-[520px] animate-pulse rounded-2xl border border-border bg-background" />}>
                  <div className="mt-4 grid gap-4">
                    <PerpChart exchange={infoExchange} symbol={normalizedInfoSymbol} interval="1m" />
                  </div>
                  <CoinResearchPanel exchange={infoExchange} base={infoBase} symbol={normalizedInfoSymbol} coinInfo={coinInfo} infoError={infoError} />
                </Suspense>

                <div className="mt-4 rounded-2xl border border-border bg-secondary p-4">
                  <div className="text-xs uppercase tracking-wide text-muted-foreground">Статистика</div>
                  {statsLoading ? (
                    <div className="mt-3 text-xs text-muted-foreground">Загрузка...</div>
                  ) : symbolStats ? (
                    <div className="mt-3 space-y-2 text-xs text-foreground">
                      <div className="flex items-center justify-between"><span>Сделок всего</span><span className="font-semibold text-foreground">{symbolStats.total_trades}</span></div>
                      <div className="flex items-center justify-between"><span>Закрыто</span><span className="font-semibold text-foreground">{symbolStats.closed_trades}</span></div>
                      <div className="flex items-center justify-between"><span>Открыто</span><span className="font-semibold text-foreground">{symbolStats.open_trades}</span></div>
                      <div className="flex items-center justify-between"><span>Win rate</span><span className="font-semibold text-primary">{symbolStats.win_rate !== null && symbolStats.win_rate !== undefined ? `${symbolStats.win_rate.toFixed(1)}%` : "—"}</span></div>
                      <div className="flex items-center justify-between"><span>Avg %</span><span className="font-semibold text-foreground">{symbolStats.avg_profit_percent !== null && symbolStats.avg_profit_percent !== undefined ? `${symbolStats.avg_profit_percent.toFixed(2)}%` : "—"}</span></div>
                      <div className="flex items-center justify-between"><span>Sum %</span><span className="font-semibold text-foreground">{symbolStats.sum_profit_percent !== null && symbolStats.sum_profit_percent !== undefined ? `${symbolStats.sum_profit_percent.toFixed(2)}%` : "—"}</span></div>
                      <div className="flex items-center justify-between"><span>Avg $</span><span className="font-semibold text-foreground">{symbolStats.avg_profit_usd !== null && symbolStats.avg_profit_usd !== undefined ? `$${symbolStats.avg_profit_usd.toFixed(2)}` : "—"}</span></div>
                    </div>
                  ) : (
                    <div className="mt-3 text-xs text-muted-foreground">Нет данных</div>
                  )}
                </div>
                <SimilarityPanel symbol={normalizedInfoSymbol} exchange={infoExchange} />
              </div>
            </div>,
            document.body,
          )
        : null}
    </section>
  );
}
