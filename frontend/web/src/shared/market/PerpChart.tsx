import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type {
  CandlestickData,
  IChartApi,
  IPaneApi,
  ISeriesApi,
  Time,
  UTCTimestamp,
} from "lightweight-charts";

import type { PrimaryExchange } from "../exchange/primaryExchange";
import { useToast } from "../ui/ToastProvider";
import { useTheme } from "../theme/ThemeContext";
import { readChartTheme } from "./chartTheme";
import { fetchChartConfig, saveChartConfig } from "./chartConfigApi";
import {
  cloneDrawing,
  DRAWING_TOOLS,
  drawingSetsSignature,
  type ChartDrawing,
  type DrawingPoint,
  type DrawingSets,
  type DrawingTool,
} from "./chartDrawings";
import { ToolIcon } from "./ChartToolIcon";
import { IndicatorMenu } from "./IndicatorMenu";
import { calculateIndicator, calculateIndicatorTail, type IndicatorOutput } from "./indicatorEngine";
import {
  INDICATOR_CATALOG,
  isClassicIndicatorId,
  isSmartMoneyIndicatorId,
  type IndicatorId,
  type SmartMoneyIndicatorId,
} from "./indicatorTypes";
import { SmartMoneyOverlay } from "./SmartMoneyOverlay";
import { calculateSmartMoney, type SmartMoneyOutput } from "./smartMoneyEngine";
import {
  fetchPerpCandles,
  fetchPerpPriceFormat,
  inferPerpPriceFormat,
  normalizeLinearPerpSymbol,
  subscribePerpCandles,
  type PerpCandle,
  type PerpInterval,
  type PerpPriceFormat,
  type PerpStreamStatus,
} from "./perpMarketData";

type ChartStatus = "loading" | "error" | PerpStreamStatus;
type IndicatorSeriesApi = ISeriesApi<"Line"> | ISeriesApi<"Histogram">;
type IndicatorRuntime = {
  pane: IPaneApi<Time> | null;
  series: Map<string, IndicatorSeriesApi>;
};
type DrawingDrag = {
  drawingId: number;
  pointerId: number;
  origin: DrawingPoint;
  original: ChartDrawing;
  handleIndex: number | null;
};
type TextDraft = {
  drawingId?: number;
  point: DrawingPoint;
  x: number;
  y: number;
  value: string;
};

const EMPTY_DRAWINGS: ChartDrawing[] = [];
const FIBONACCI_LEVELS = [0, 0.236, 0.382, 0.5, 0.618, 0.786, 1];
const DEFAULT_PRICE_FORMAT: PerpPriceFormat = { precision: 2, minMove: 0.01 };
const EMPTY_SMART_MONEY_OUTPUT: SmartMoneyOutput = { markers: [], levels: [], zones: [] };
const FUTURE_TIMELINE_BARS = 300;
const VISIBLE_FUTURE_BARS = 24;

const INTERVAL_SECONDS: Record<PerpInterval, number> = {
  "1m": 60,
  "5m": 5 * 60,
  "15m": 15 * 60,
  "1h": 60 * 60,
  "4h": 4 * 60 * 60,
};

const INTERVALS: Array<{ value: PerpInterval; label: string }> = [
  { value: "1m", label: "1м" },
  { value: "5m", label: "5м" },
  { value: "15m", label: "15м" },
  { value: "1h", label: "1ч" },
  { value: "4h", label: "4ч" },
];

const STATUS_LABELS: Record<ChartStatus, string> = {
  loading: "Загрузка истории",
  connecting: "Подключение",
  live: "Онлайн",
  reconnecting: "Переподключение",
  offline: "Отключено",
  error: "Ошибка",
};

const STATUS_COLORS: Record<ChartStatus, string> = {
  loading: "bg-amber-300",
  connecting: "bg-amber-300",
  live: "bg-emerald-400",
  reconnecting: "bg-amber-300",
  offline: "bg-accent",
  error: "bg-rose-400",
};

const INDICATOR_REFERENCE_LEVELS: Partial<Record<IndicatorId, { key: string; values: number[] }>> = {
  rsi: { key: "rsi", values: [30, 70] },
  macd: { key: "macd", values: [0] },
  stochastic: { key: "k", values: [20, 80] },
  adx: { key: "adx", values: [25] },
  cci: { key: "cci", values: [-100, 100] },
  roc: { key: "roc", values: [0] },
};

export function ResetIcon() {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true" className="h-4 w-4">
      <path d="M5.2 6.1A6 6 0 1 1 4 10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M2.8 4.2v3.5h3.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function UndoIcon() {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
      <path d="M7.2 5 3.5 8.4l3.7 3.4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M4 8.4h7.3a4.2 4.2 0 0 1 4.2 4.2V15" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

function TrashIcon() {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
      <path d="M5.5 6.5h9l-.6 10h-7.8l-.6-10ZM7.5 6.5V4h5v2.5M4 6.5h12" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function ClearAllIcon() {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
      <path d="M4 6h7l5 5-5 5H4l-2-5 2-5Z" fill="none" stroke="currentColor" strokeWidth="1.35" strokeLinejoin="round" />
      <path d="m7 9 4 4m0-4-4 4" stroke="currentColor" strokeWidth="1.35" strokeLinecap="round" />
    </svg>
  );
}

function LockIcon({ locked }: { locked: boolean }) {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
      <rect x="4" y="8" width="12" height="9" rx="2" fill="none" stroke="currentColor" strokeWidth="1.35" />
      <path d={locked ? "M7 8V6a3 3 0 0 1 6 0v2" : "M7 8V6a3 3 0 0 1 5.8-1"} fill="none" stroke="currentColor" strokeWidth="1.35" strokeLinecap="round" />
      <circle cx="10" cy="12.5" r="1" fill="currentColor" />
    </svg>
  );
}

function EyeIcon({ visible }: { visible: boolean }) {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
      <path d="M2.5 10s2.7-4.5 7.5-4.5 7.5 4.5 7.5 4.5-2.7 4.5-7.5 4.5S2.5 10 2.5 10Z" fill="none" stroke="currentColor" strokeWidth="1.35" />
      <circle cx="10" cy="10" r="2.2" fill="none" stroke="currentColor" strokeWidth="1.35" />
      {!visible ? <path d="m3 3 14 14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /> : null}
    </svg>
  );
}

function MagnetIcon({ enabled }: { enabled: boolean }) {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true" className="h-[18px] w-[18px]">
      <path d="M5 3v7a5 5 0 0 0 10 0V3h-3v7a2 2 0 0 1-4 0V3H5Z" fill={enabled ? "currentColor" : "none"} fillOpacity=".2" stroke="currentColor" strokeWidth="1.35" strokeLinejoin="round" />
      <path d="M5 6h3m4 0h3" stroke="currentColor" strokeWidth="1.35" />
    </svg>
  );
}

function SaveIcon({ busy = false }: { busy?: boolean }) {
  return (
    <svg viewBox="0 0 20 20" aria-hidden="true" className={`h-[18px] w-[18px] ${busy ? "animate-pulse" : ""}`}>
      <path d="M4 3.5h10.2L16.5 6v10.5h-13v-13Z" fill="none" stroke="currentColor" strokeWidth="1.35" strokeLinejoin="round" />
      <path d="M6.5 3.5v5h6.5v-5M6.5 16.5v-5h7v5" fill="none" stroke="currentColor" strokeWidth="1.35" strokeLinejoin="round" />
    </svg>
  );
}

function mergeCandles(...groups: PerpCandle[][]) {
  const byTime = new Map<number, PerpCandle>();
  for (const group of groups) {
    for (const candle of group) byTime.set(Number(candle.time), candle);
  }
  return Array.from(byTime.values()).sort((a, b) => Number(a.time) - Number(b.time));
}

function nearestCandleTime(candles: PerpCandle[], target: number): UTCTimestamp | null {
  if (candles.length === 0) return null;
  let low = 0;
  let high = candles.length - 1;
  while (low < high) {
    const middle = Math.floor((low + high) / 2);
    if (Number(candles[middle].time) < target) low = middle + 1;
    else high = middle;
  }
  if (low === 0) return candles[0].time;
  const previous = candles[low - 1];
  const current = candles[low];
  return target - Number(previous.time) <= Number(current.time) - target
    ? previous.time
    : current.time;
}

function futureTimelineData(candles: readonly PerpCandle[], interval: PerpInterval) {
  const latest = candles[candles.length - 1];
  if (!latest) return [];
  const step = INTERVAL_SECONDS[interval];
  return Array.from({ length: FUTURE_TIMELINE_BARS }, (_, index) => ({
    time: (Number(latest.time) + step * (index + 1)) as UTCTimestamp,
  }));
}

function volumeData(candles: PerpCandle[]) {
  return candles.map((candle) => ({
    time: candle.time,
    value: candle.volume,
    color: candle.close >= candle.open ? "rgba(52, 211, 153, 0.3)" : "rgba(251, 113, 133, 0.3)",
  }));
}

function formatPrice(value: number, precision: number) {
  return new Intl.NumberFormat("ru-RU", {
    minimumFractionDigits: precision,
    maximumFractionDigits: precision,
  }).format(value);
}

function formatDuration(seconds: number) {
  const totalMinutes = Math.floor(Math.abs(seconds) / 60);
  if (totalMinutes < 60) return `${totalMinutes} мин`;

  const totalHours = Math.floor(totalMinutes / 60);
  const days = Math.floor(totalHours / 24);
  const hours = totalHours % 24;
  const minutes = totalMinutes % 60;
  const parts: string[] = [];
  if (days > 0) parts.push(`${days} д`);
  if (hours > 0) parts.push(`${hours} ч`);
  if (minutes > 0) parts.push(`${minutes} мин`);
  return parts.join(" ");
}

function updateOhlc(
  node: HTMLDivElement | null,
  candle: PerpCandle | CandlestickData<UTCTimestamp>,
  precision: number,
) {
  if (!node) return;
  const delta = candle.close - candle.open;
  const percent = candle.open === 0 ? 0 : (delta / candle.open) * 100;
  node.textContent = `O ${formatPrice(candle.open, precision)}   H ${formatPrice(candle.high, precision)}   L ${formatPrice(candle.low, precision)}   C ${formatPrice(candle.close, precision)}   ${delta >= 0 ? "+" : ""}${formatPrice(delta, precision)} (${percent >= 0 ? "+" : ""}${percent.toFixed(2)}%)`;
  node.dataset.direction = delta >= 0 ? "up" : "down";
}

function updateVolume(node: HTMLDivElement | null, value: number) {
  if (!node) return;
  node.textContent = `Объём ${new Intl.NumberFormat("ru-RU", { notation: "compact", maximumFractionDigits: 2 }).format(value)}`;
}

export function PerpChart({
  exchange,
  symbol,
  interval = "1m",
}: {
  exchange: PrimaryExchange;
  symbol: string;
  interval?: PerpInterval;
}) {
  const { theme } = useTheme();
  const containerRef = useRef<HTMLDivElement>(null);
  const interactionRef = useRef<HTMLDivElement>(null);
  const previewLineRef = useRef<SVGLineElement>(null);
  const ohlcRef = useRef<HTMLDivElement>(null);
  const volumeLabelRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const chartLibraryRef = useRef<typeof import("lightweight-charts") | null>(null);
  const candleSeriesRef = useRef<ISeriesApi<"Candlestick"> | null>(null);
  const indicatorRuntimesRef = useRef<Map<IndicatorId, IndicatorRuntime>>(new Map());
  const activeIndicatorsRef = useRef<IndicatorId[]>([]);
  const indicatorCalculationRef = useRef(0);
  const liveCalculationRef = useRef(0);
  const candlesRef = useRef<PerpCandle[]>([]);
  const candlesByTimeRef = useRef<Map<number, PerpCandle>>(new Map());
  const priceFormatRef = useRef<PerpPriceFormat>(DEFAULT_PRICE_FORMAT);
  const draftPointRef = useRef<DrawingPoint | null>(null);
  const dragRef = useRef<DrawingDrag | null>(null);
  const brushDrawingIdRef = useRef<number | null>(null);
  const drawingIdRef = useRef(0);
  const redrawFrameRef = useRef<number | null>(null);

  const [selectedInterval, setSelectedInterval] = useState<PerpInterval>(interval);
  const [status, setStatus] = useState<ChartStatus>("loading");
  const [error, setError] = useState("");
  const [reloadKey, setReloadKey] = useState(0);
  const [activeTool, setActiveTool] = useState<DrawingTool>("cursor");
  const [priceFormat, setPriceFormat] = useState<PerpPriceFormat>(DEFAULT_PRICE_FORMAT);
  const [drawingSets, setDrawingSets] = useState<DrawingSets>({});
  const [selectedDrawingId, setSelectedDrawingId] = useState<number | null>(null);
  const [drawingsVisible, setDrawingsVisible] = useState(true);
  const [magnetEnabled, setMagnetEnabled] = useState(true);
  const [textDraft, setTextDraft] = useState<TextDraft | null>(null);
  const [activeIndicators, setActiveIndicators] = useState<IndicatorId[]>([]);
  const [isConfigLoaded, setIsConfigLoaded] = useState(false);
  const [isSavingConfig, setIsSavingConfig] = useState(false);
  const [savedConfigSignature, setSavedConfigSignature] = useState("");
  const [, setHistoryCount] = useState(0);
  const [dataRevision, setDataRevision] = useState(0);
  const [smartMoneyRevision, setSmartMoneyRevision] = useState(0);
  const [smartMoneyOutput, setSmartMoneyOutput] = useState<SmartMoneyOutput>(EMPTY_SMART_MONEY_OUTPUT);
  const [, setIsLoadingOlder] = useState(false);
  const [, setRenderRevision] = useState(0);
  const normalizedSymbol = useMemo(() => normalizeLinearPerpSymbol(symbol), [symbol]);
  const classicIndicators = useMemo(
    () => activeIndicators.filter(isClassicIndicatorId),
    [activeIndicators],
  );
  const smartMoneyIndicators = useMemo(
    () => activeIndicators.filter(isSmartMoneyIndicatorId),
    [activeIndicators],
  );
  const smartMoneyActiveSet = useMemo<ReadonlySet<SmartMoneyIndicatorId>>(
    () => new Set(smartMoneyIndicators),
    [smartMoneyIndicators],
  );
  const drawingSetKey = `${exchange}:${normalizedSymbol}:${selectedInterval}`;
  const drawings = drawingSets[drawingSetKey] ?? EMPTY_DRAWINGS;
  const selectedDrawing = selectedDrawingId === null
    ? null
    : drawings.find((drawing) => drawing.id === selectedDrawingId) ?? null;
  const toast = useToast();
  const configSignature = useMemo(
    () => `${selectedInterval}:${[...activeIndicators].sort().join(",")}:${drawingSetsSignature(drawingSets)}`,
    [activeIndicators, drawingSets, selectedInterval],
  );
  const hasUnsavedConfig = isConfigLoaded && configSignature !== savedConfigSignature;

  const updateDrawings = useCallback((updater: (current: ChartDrawing[]) => ChartDrawing[]) => {
    setDrawingSets((currentSets) => {
      const next = updater(currentSets[drawingSetKey] ?? EMPTY_DRAWINGS);
      if (next.length === 0) {
        if (!(drawingSetKey in currentSets)) return currentSets;
        const remaining = { ...currentSets };
        delete remaining[drawingSetKey];
        return remaining;
      }
      return { ...currentSets, [drawingSetKey]: next };
    });
  }, [drawingSetKey]);

  const scheduleDrawingRender = useCallback(() => {
    if (redrawFrameRef.current !== null) return;
    redrawFrameRef.current = requestAnimationFrame(() => {
      redrawFrameRef.current = null;
      setRenderRevision((value) => value + 1);
    });
  }, []);

  const hidePreview = useCallback(() => {
    draftPointRef.current = null;
    brushDrawingIdRef.current = null;
    previewLineRef.current?.setAttribute("opacity", "0");
  }, []);

  const selectTool = useCallback((tool: DrawingTool) => {
    hidePreview();
    setTextDraft(null);
    if (tool !== "cursor") setSelectedDrawingId(null);
    setActiveTool(tool);
  }, [hidePreview]);

  useEffect(() => {
    setSelectedInterval(interval);
  }, [interval]);

  useEffect(() => {
    activeIndicatorsRef.current = activeIndicators;
  }, [activeIndicators]);

  useEffect(() => {
    const controller = new AbortController();
    void fetchChartConfig(controller.signal)
      .then((config) => {
        setSelectedInterval(config.selected_interval);
        setActiveIndicators(config.active_indicator_ids);
        setDrawingSets(config.drawing_sets);
        drawingIdRef.current = Math.max(0, ...Object.values(config.drawing_sets).flatMap((items) => items.map((item) => item.id)));
        setSavedConfigSignature(`${config.selected_interval}:${[...config.active_indicator_ids].sort().join(",")}:${drawingSetsSignature(config.drawing_sets)}`);
      })
      .catch((caught: unknown) => {
        if (!controller.signal.aborted) {
          setSavedConfigSignature(`${interval}::[]`);
          console.warn("Unable to load chart config", caught);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setIsConfigLoaded(true);
      });
    return () => controller.abort();
  }, [interval]);

  const toggleIndicator = useCallback((id: IndicatorId) => {
    setActiveIndicators((current) => current.includes(id)
      ? current.filter((item) => item !== id)
      : [...current, id]);
  }, []);

  const persistConfig = useCallback(async () => {
    if (isSavingConfig) return;
    setIsSavingConfig(true);
    try {
      const saved = await saveChartConfig({
        version: 1,
        selected_interval: selectedInterval,
        active_indicator_ids: activeIndicators,
        drawing_sets: drawingSets,
      });
      setDrawingSets(saved.drawing_sets);
      setSavedConfigSignature(`${saved.selected_interval}:${[...saved.active_indicator_ids].sort().join(",")}:${drawingSetsSignature(saved.drawing_sets)}`);
      toast.success("Конфигурация графика сохранена");
    } catch {
      toast.error("Не удалось сохранить конфигурацию графика");
    } finally {
      setIsSavingConfig(false);
    }
  }, [activeIndicators, drawingSets, isSavingConfig, selectedInterval, toast]);

  const removeIndicatorRuntime = useCallback((id: IndicatorId) => {
    const chart = chartRef.current;
    const runtime = indicatorRuntimesRef.current.get(id);
    if (!chart || !runtime) return;
    const paneIndex = runtime.pane?.paneIndex() ?? -1;
    for (const series of runtime.series.values()) chart.removeSeries(series);
    if (paneIndex > 0 && paneIndex < chart.panes().length) chart.removePane(paneIndex);
    indicatorRuntimesRef.current.delete(id);
  }, []);

  const applyIndicatorOutput = useCallback((output: IndicatorOutput, replaceData: boolean) => {
    const chart = chartRef.current;
    const library = chartLibraryRef.current;
    if (!chart || !library) return;

    let runtime = indicatorRuntimesRef.current.get(output.id);
    if (!runtime) {
      const pane = output.placement === "pane" ? chart.addPane(true) : null;
      pane?.setStretchFactor(1);
      chart.panes()[0]?.setStretchFactor(4);
      const newRuntime: IndicatorRuntime = { pane, series: new Map() };
      indicatorRuntimesRef.current.set(output.id, newRuntime);
      runtime = newRuntime;
    }
    const paneIndex = runtime.pane?.paneIndex();

    for (const definition of output.series) {
      let series = runtime.series.get(definition.key);
      if (!series) {
        if (definition.kind === "line") {
          const lineSeries = chart.addSeries(library.LineSeries, {
            title: "",
            color: definition.color,
            lineWidth: definition.lineWidth ?? 1,
            lineStyle: definition.lineStyle ?? 0,
            priceLineVisible: false,
            lastValueVisible: true,
            crosshairMarkerVisible: false,
          }, paneIndex);
          const reference = INDICATOR_REFERENCE_LEVELS[output.id];
          if (reference?.key === definition.key) {
            for (const price of reference.values) {
              lineSeries.createPriceLine({
                price,
                color: "rgba(148,163,184,.22)",
                lineWidth: 1,
                lineStyle: 2,
                axisLabelVisible: false,
                title: "",
              });
            }
          }
          series = lineSeries;
        } else {
          series = chart.addSeries(library.HistogramSeries, {
            title: "",
            color: definition.color,
            priceLineVisible: false,
            lastValueVisible: true,
          }, paneIndex);
        }
        runtime.series.set(definition.key, series);
      }
      if (definition.kind === "line") {
        const lineSeries = series as ISeriesApi<"Line">;
        if (replaceData) lineSeries.setData(definition.data);
        else if (definition.data[0]) lineSeries.update(definition.data[0]);
      } else {
        const histogramSeries = series as ISeriesApi<"Histogram">;
        if (replaceData) histogramSeries.setData(definition.data);
        else if (definition.data[0]) histogramSeries.update(definition.data[0]);
      }
    }
  }, []);

  const updateActiveIndicatorTails = useCallback(async (candles: PerpCandle[]) => {
    const ids = activeIndicatorsRef.current
      .filter(isClassicIndicatorId)
      .filter((id) => indicatorRuntimesRef.current.has(id));
    if (ids.length === 0) return;
    const revision = liveCalculationRef.current += 1;
    const outputs = await Promise.all(ids.map((id) => calculateIndicatorTail(candles, id)));
    if (revision !== liveCalculationRef.current) return;
    for (const output of outputs) applyIndicatorOutput(output, false);
  }, [applyIndicatorOutput]);

  useEffect(() => {
    if (smartMoneyIndicators.length === 0) {
      setSmartMoneyOutput(EMPTY_SMART_MONEY_OUTPUT);
      return;
    }
    // The WebSocket's last candle is still forming. Excluding it keeps all
    // SMC signals close-confirmed and prevents intrabar repainting.
    const closedCandles = candlesRef.current.slice(0, -1);
    setSmartMoneyOutput(calculateSmartMoney(closedCandles));
    scheduleDrawingRender();
  }, [dataRevision, scheduleDrawingRender, smartMoneyIndicators, smartMoneyRevision]);

  useEffect(() => {
    chartRef.current?.applyOptions({
      handleScroll: activeTool === "cursor",
      handleScale: activeTool === "cursor",
    });
  }, [activeTool]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const isEditable = target?.tagName === "INPUT" || target?.tagName === "TEXTAREA" || target?.isContentEditable;
      if (isEditable) return;
      if (event.key === "Escape") {
        hidePreview();
        setTextDraft(null);
        setSelectedDrawingId(null);
        return;
      }
      if ((event.key === "Delete" || event.key === "Backspace") && selectedDrawingId !== null) {
        event.preventDefault();
        updateDrawings((current) => current.filter((drawing) => drawing.id !== selectedDrawingId));
        setSelectedDrawingId(null);
        return;
      }
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "z") {
        event.preventDefault();
        updateDrawings((current) => current.slice(0, -1));
        setSelectedDrawingId(null);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [hidePreview, selectedDrawingId, updateDrawings]);

  useEffect(() => {
    hidePreview();
    setSelectedDrawingId(null);
    setTextDraft(null);
  }, [drawingSetKey, hidePreview]);

  useEffect(() => {
    const activeSet = new Set<IndicatorId>(classicIndicators);
    for (const id of indicatorRuntimesRef.current.keys()) {
      if (!activeSet.has(id)) removeIndicatorRuntime(id);
    }
    const candles = candlesRef.current;
    if (!chartRef.current || candles.length === 0 || classicIndicators.length === 0) return;
    const revision = indicatorCalculationRef.current += 1;
    void Promise.all(classicIndicators.map((id) => calculateIndicator(candles, id)))
      .then((outputs) => {
        if (revision !== indicatorCalculationRef.current) return;
        for (const output of outputs) {
          if (activeIndicatorsRef.current.includes(output.id)) applyIndicatorOutput(output, true);
        }
        scheduleDrawingRender();
      })
      .catch((caught: unknown) => console.warn("Unable to calculate indicators", caught));
    return () => {
      indicatorCalculationRef.current += 1;
    };
  }, [applyIndicatorOutput, classicIndicators, dataRevision, removeIndicatorRuntime, scheduleDrawingRender]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container || !normalizedSymbol || !isConfigLoaded) return;

    const controller = new AbortController();
    const indicatorRuntimes = indicatorRuntimesRef.current;
    let disposed = false;
    let resizeObserver: ResizeObserver | null = null;
    let stopStream: (() => void) | null = null;
    let loadingOlder = false;
    let hasOlderHistory = true;
    let removeRangeSubscription: (() => void) | null = null;
    let removeCrosshairSubscription: (() => void) | null = null;
    let removeClickSubscription: (() => void) | null = null;

    setStatus("loading");
    setError("");
    setHistoryCount(0);

    async function initialize(chartContainer: HTMLDivElement) {
      try {
        const [lightweightCharts, initialCandles, exchangePriceFormat] = await Promise.all([
          import("lightweight-charts"),
          fetchPerpCandles(exchange, normalizedSymbol, selectedInterval, controller.signal, { limit: 1_500 }),
          fetchPerpPriceFormat(exchange, normalizedSymbol, controller.signal).catch(() => null),
        ]);
        if (disposed) return;
        if (initialCandles.length === 0) throw new Error("empty_history");
        const resolvedPriceFormat = exchangePriceFormat ?? inferPerpPriceFormat(initialCandles);
        const chartTheme = readChartTheme();
        priceFormatRef.current = resolvedPriceFormat;
        setPriceFormat(resolvedPriceFormat);

        const chart = lightweightCharts.createChart(chartContainer, {
          width: chartContainer.clientWidth,
          height: chartContainer.clientHeight,
          layout: {
            background: { type: lightweightCharts.ColorType.Solid, color: chartTheme.surface },
            textColor: chartTheme.foreground,
            fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
            fontSize: 12,
          },
          localization: { locale: "ru-RU" },
          grid: {
            vertLines: { color: chartTheme.grid, style: lightweightCharts.LineStyle.Dotted },
            horzLines: { color: chartTheme.grid, style: lightweightCharts.LineStyle.Dotted },
          },
          crosshair: {
            mode: lightweightCharts.CrosshairMode.Normal,
            vertLine: { color: chartTheme.crosshair, labelBackgroundColor: chartTheme.label },
            horzLine: { color: chartTheme.crosshair, labelBackgroundColor: chartTheme.label },
          },
          rightPriceScale: {
            borderColor: chartTheme.grid,
            scaleMargins: { top: 0.1, bottom: 0.22 },
          },
          timeScale: {
            borderColor: chartTheme.grid,
            timeVisible: true,
            secondsVisible: false,
            rightOffset: VISIBLE_FUTURE_BARS,
            barSpacing: 8,
            minBarSpacing: 2,
          },
          handleScroll: true,
          handleScale: true,
        });
        chartRef.current = chart;
        chartLibraryRef.current = lightweightCharts;

        const candleSeries = chart.addSeries(lightweightCharts.CandlestickSeries, {
          upColor: "#34d399",
          downColor: "#fb7185",
          borderVisible: false,
          wickUpColor: "#6ee7b7",
          wickDownColor: "#fda4af",
          priceLineColor: "rgba(110, 231, 183, 0.55)",
          priceFormat: { type: "price", ...resolvedPriceFormat },
        });
        candleSeriesRef.current = candleSeries;

        const volumeSeries = chart.addSeries(lightweightCharts.HistogramSeries, {
          priceScaleId: "",
          priceFormat: { type: "volume" },
          lastValueVisible: false,
          priceLineVisible: false,
        });
        volumeSeries.priceScale().applyOptions({ scaleMargins: { top: 0.82, bottom: 0 } });

        // Whitespace points extend the actual Lightweight Charts time scale.
        // Drawings can then own real future timestamps and remain projectable
        // after zooming, dragging, saving and restoring the configuration.
        const futureTimelineSeries = chart.addSeries(lightweightCharts.LineSeries, {
          color: "transparent",
          lineVisible: false,
          lastValueVisible: false,
          priceLineVisible: false,
          crosshairMarkerVisible: false,
        });

        const applyAllData = (candles: PerpCandle[]) => {
          candlesRef.current = candles;
          candlesByTimeRef.current = new Map(candles.map((candle) => [Number(candle.time), candle]));
          candleSeries.setData(candles);
          volumeSeries.setData(volumeData(candles));
          futureTimelineSeries.setData(futureTimelineData(candles, selectedInterval));
          setHistoryCount(candles.length);
          setDataRevision((value) => value + 1);
        };

        applyAllData(initialCandles);
        const recentStart = Math.max(0, initialCandles.length - 180);
        chart.timeScale().setVisibleLogicalRange({ from: recentStart, to: initialCandles.length + VISIBLE_FUTURE_BARS });
        updateOhlc(ohlcRef.current, initialCandles[initialCandles.length - 1], resolvedPriceFormat.precision);
        updateVolume(volumeLabelRef.current, initialCandles[initialCandles.length - 1].volume);

        const loadOlder = async () => {
          const current = candlesRef.current;
          if (disposed || loadingOlder || !hasOlderHistory || current.length === 0) return;
          loadingOlder = true;
          setIsLoadingOlder(true);
          const visibleRange = chart.timeScale().getVisibleLogicalRange();
          try {
            const older = await fetchPerpCandles(
              exchange,
              normalizedSymbol,
              selectedInterval,
              controller.signal,
              { before: current[0].time, limit: 1_000 },
            );
            if (disposed) return;
            const genuinelyOlder = older.filter((candle) => Number(candle.time) < Number(current[0].time));
            if (genuinelyOlder.length === 0) {
              hasOlderHistory = false;
              return;
            }
            const merged = mergeCandles(genuinelyOlder, current);
            applyAllData(merged);
            if (visibleRange) {
              chart.timeScale().setVisibleLogicalRange({
                from: visibleRange.from + genuinelyOlder.length,
                to: visibleRange.to + genuinelyOlder.length,
              });
            }
          } catch (caught) {
            if (!controller.signal.aborted) console.warn("Unable to load older candles", caught);
          } finally {
            loadingOlder = false;
            if (!disposed) setIsLoadingOlder(false);
          }
        };

        const timeScale = chart.timeScale();
        const onVisibleRangeChange: Parameters<typeof timeScale.subscribeVisibleLogicalRangeChange>[0] = (range) => {
          scheduleDrawingRender();
          if (range && range.from < 60) void loadOlder();
        };
        timeScale.subscribeVisibleLogicalRangeChange(onVisibleRangeChange);
        removeRangeSubscription = () => timeScale.unsubscribeVisibleLogicalRangeChange(onVisibleRangeChange);

        const onCrosshairMove: Parameters<typeof chart.subscribeCrosshairMove>[0] = (param) => {
          const candle = param.seriesData.get(candleSeries);
          if (candle && "open" in candle && "close" in candle && "high" in candle && "low" in candle) {
            const candleData = candle as CandlestickData<UTCTimestamp>;
            updateOhlc(ohlcRef.current, candleData, priceFormatRef.current.precision);
            const matching = candlesByTimeRef.current.get(Number(candleData.time));
            if (matching) updateVolume(volumeLabelRef.current, matching.volume);
          }
        };
        chart.subscribeCrosshairMove(onCrosshairMove);
        removeCrosshairSubscription = () => chart.unsubscribeCrosshairMove(onCrosshairMove);

        const onChartClick: Parameters<typeof chart.subscribeClick>[0] = () => setSelectedDrawingId(null);
        chart.subscribeClick(onChartClick);
        removeClickSubscription = () => chart.unsubscribeClick(onChartClick);

        resizeObserver = new ResizeObserver(([entry]) => {
          if (!entry) return;
          const { width, height } = entry.contentRect;
          chart.resize(Math.floor(width), Math.floor(height));
          scheduleDrawingRender();
        });
        resizeObserver.observe(chartContainer);

        stopStream = subscribePerpCandles({
          exchange,
          symbol: normalizedSymbol,
          interval: selectedInterval,
          onStatus: setStatus,
          onCandle: (candle) => {
            const current = candlesRef.current;
            const last = current[current.length - 1];
            const startedNewCandle = Boolean(last && Number(last.time) !== Number(candle.time));
            if (last && Number(last.time) === Number(candle.time)) current[current.length - 1] = candle;
            else current.push(candle);
            if (startedNewCandle) futureTimelineSeries.setData(futureTimelineData(current, selectedInterval));
            candlesByTimeRef.current.set(Number(candle.time), candle);
            candleSeries.update(candle);
            volumeSeries.update(volumeData([candle])[0]);
            void updateActiveIndicatorTails(current);
            if (startedNewCandle && activeIndicatorsRef.current.some(isSmartMoneyIndicatorId)) {
              setSmartMoneyRevision((value) => value + 1);
            }
            updateOhlc(ohlcRef.current, candle, priceFormatRef.current.precision);
            updateVolume(volumeLabelRef.current, candle.volume);
          },
        });
      } catch (caught) {
        if (disposed || controller.signal.aborted) return;
        setStatus("error");
        setError(caught instanceof Error ? caught.message : "chart_load_failed");
      }
    }

    void initialize(container);

    return () => {
      disposed = true;
      indicatorCalculationRef.current += 1;
      liveCalculationRef.current += 1;
      controller.abort();
      stopStream?.();
      removeRangeSubscription?.();
      removeCrosshairSubscription?.();
      removeClickSubscription?.();
      resizeObserver?.disconnect();
      chartRef.current?.remove();
      chartRef.current = null;
      chartLibraryRef.current = null;
      candleSeriesRef.current = null;
      indicatorRuntimes.clear();
      candlesRef.current = [];
      candlesByTimeRef.current.clear();
    };
  }, [exchange, isConfigLoaded, normalizedSymbol, reloadKey, scheduleDrawingRender, selectedInterval, updateActiveIndicatorTails]);

  useEffect(() => {
    const chart = chartRef.current;
    const library = chartLibraryRef.current;
    if (!chart || !library) return;
    const colors = readChartTheme();
    chart.applyOptions({
      layout: {
        background: { type: library.ColorType.Solid, color: colors.surface },
        textColor: colors.foreground,
      },
      grid: {
        vertLines: { color: colors.grid },
        horzLines: { color: colors.grid },
      },
      crosshair: {
        vertLine: { color: colors.crosshair, labelBackgroundColor: colors.label },
        horzLine: { color: colors.crosshair, labelBackgroundColor: colors.label },
      },
      rightPriceScale: { borderColor: colors.grid },
      timeScale: { borderColor: colors.grid },
    });
  }, [theme]);

  const pointFromClient = useCallback((clientX: number, clientY: number): DrawingPoint | null => {
    const chart = chartRef.current;
    const candleSeries = candleSeriesRef.current;
    const overlay = interactionRef.current;
    if (!chart || !candleSeries || !overlay) return null;
    const bounds = overlay.getBoundingClientRect();
    const x = clientX - bounds.left;
    const y = clientY - bounds.top;
    const time = chart.timeScale().coordinateToTime(x);
    const convertedPrice = candleSeries.coordinateToPrice(y);
    if (typeof time !== "number" || convertedPrice === null) return null;
    let price = Number(convertedPrice);
    const latest = candlesRef.current[candlesRef.current.length - 1];
    const snappedTime = latest && time > Number(latest.time)
      ? (Number(latest.time) + Math.max(1, Math.round((time - Number(latest.time)) / INTERVAL_SECONDS[selectedInterval])) * INTERVAL_SECONDS[selectedInterval]) as UTCTimestamp
      : nearestCandleTime(candlesRef.current, time);
    if (snappedTime === null) return null;

    if (magnetEnabled) {
      const candle = candlesByTimeRef.current.get(Number(snappedTime));
      if (candle) {
        const candidates = [candle.open, candle.high, candle.low, candle.close];
        let nearestPrice = price;
        let nearestDistance = Number.POSITIVE_INFINITY;
        for (const candidate of candidates) {
          const coordinate = candleSeries.priceToCoordinate(candidate);
          if (coordinate === null) continue;
          const distance = Math.abs(coordinate - y);
          if (distance < nearestDistance) {
            nearestDistance = distance;
            nearestPrice = candidate;
          }
        }
        if (nearestDistance <= 10) price = nearestPrice;
      }
    }
    return { time: Number(snappedTime), price };
  }, [magnetEnabled, selectedInterval]);

  const onDrawingPointerMove = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    const brushDrawingId = brushDrawingIdRef.current;
    if (activeTool === "brush" && brushDrawingId !== null) {
      const point = pointFromClient(event.clientX, event.clientY);
      if (!point) return;
      updateDrawings((current) => current.map((drawing) => {
        if (drawing.id !== brushDrawingId || drawing.type !== "brush") return drawing;
        const last = drawing.points[drawing.points.length - 1];
        if (last && last.time === point.time) {
          const nextPoints = [...drawing.points];
          nextPoints[nextPoints.length - 1] = point;
          return { ...drawing, points: nextPoints };
        }
        if (drawing.points.length >= 800) return drawing;
        return { ...drawing, points: [...drawing.points, point] };
      }));
      return;
    }

    const draft = draftPointRef.current;
    const chart = chartRef.current;
    const candleSeries = candleSeriesRef.current;
    const preview = previewLineRef.current;
    if (!draft || !chart || !candleSeries || !preview) return;
    const point = pointFromClient(event.clientX, event.clientY);
    if (!point) return;
    const startX = chart.timeScale().timeToCoordinate(draft.time as UTCTimestamp);
    const startY = candleSeries.priceToCoordinate(draft.price);
    const endX = chart.timeScale().timeToCoordinate(point.time as UTCTimestamp);
    const endY = candleSeries.priceToCoordinate(point.price);
    if (startX === null || startY === null || endX === null || endY === null) return;
    preview.setAttribute("x1", String(startX));
    preview.setAttribute("y1", String(startY));
    preview.setAttribute("x2", String(endX));
    preview.setAttribute("y2", String(endY));
    preview.setAttribute("opacity", "1");
  }, [activeTool, pointFromClient, updateDrawings]);

  const onDrawingPointerDown = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (activeTool === "cursor") return;
    const point = pointFromClient(event.clientX, event.clientY);
    if (!point) return;
    if (activeTool === "horizontal" || activeTool === "vertical") {
      const id = drawingIdRef.current += 1;
      updateDrawings((current) => [...current, { id, type: activeTool, points: [point] }]);
      setSelectedDrawingId(id);
      setActiveTool("cursor");
      return;
    }
    if (activeTool === "text") {
      const bounds = interactionRef.current?.getBoundingClientRect();
      if (!bounds) return;
      setTextDraft({ point, x: event.clientX - bounds.left, y: event.clientY - bounds.top, value: "" });
      return;
    }
    if (activeTool === "brush") {
      const id = drawingIdRef.current += 1;
      brushDrawingIdRef.current = id;
      event.currentTarget.setPointerCapture(event.pointerId);
      updateDrawings((current) => [...current, { id, type: "brush", points: [point] }]);
      setSelectedDrawingId(id);
      return;
    }
    const draft = draftPointRef.current;
    if (!draft) {
      draftPointRef.current = point;
      const chart = chartRef.current;
      const candleSeries = candleSeriesRef.current;
      const preview = previewLineRef.current;
      const x = chart?.timeScale().timeToCoordinate(point.time as UTCTimestamp);
      const y = candleSeries?.priceToCoordinate(point.price);
      if (preview && x !== null && x !== undefined && y !== null && y !== undefined) {
        preview.setAttribute("x1", String(x));
        preview.setAttribute("y1", String(y));
        preview.setAttribute("x2", String(x));
        preview.setAttribute("y2", String(y));
        preview.setAttribute("opacity", "1");
      }
      return;
    }
    const id = drawingIdRef.current += 1;
    const type = activeTool as "trend" | "ray" | "rectangle" | "fibonacci" | "ruler";
    updateDrawings((current) => [...current, { id, type, points: [draft, point] }]);
    setSelectedDrawingId(id);
    hidePreview();
    setActiveTool("cursor");
  }, [activeTool, hidePreview, pointFromClient, updateDrawings]);

  const onDrawingPointerUp = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (brushDrawingIdRef.current === null) return;
    brushDrawingIdRef.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
  }, []);

  const onDrawingDragStart = useCallback((
    event: React.PointerEvent<SVGElement>,
    drawing: ChartDrawing,
    handleIndex: number | null = null,
  ) => {
    if (activeTool !== "cursor") return;
    event.preventDefault();
    event.stopPropagation();
    setSelectedDrawingId(drawing.id);
    if (drawing.locked) return;
    const origin = pointFromClient(event.clientX, event.clientY);
    if (!origin) return;
    dragRef.current = {
      drawingId: drawing.id,
      pointerId: event.pointerId,
      origin,
      original: cloneDrawing(drawing),
      handleIndex,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
  }, [activeTool, pointFromClient]);

  const onDrawingDragMove = useCallback((event: React.PointerEvent<SVGElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    event.preventDefault();
    event.stopPropagation();
    const point = pointFromClient(event.clientX, event.clientY);
    if (!point) return;
    updateDrawings((current) => current.map((drawing) => {
      if (drawing.id !== drag.drawingId) return drawing;
      if (drag.handleIndex !== null) {
        const points = drag.original.points.map((item, index) => index === drag.handleIndex ? point : { ...item });
        return { ...drag.original, points } as ChartDrawing;
      }
      const timeDelta = point.time - drag.origin.time;
      const priceDelta = point.price - drag.origin.price;
      const points = drag.original.points.map((item) => ({
        time: Math.max(1, item.time + timeDelta),
        price: item.price + priceDelta,
      }));
      return { ...drag.original, points } as ChartDrawing;
    }));
  }, [pointFromClient, updateDrawings]);

  const onDrawingDragEnd = useCallback((event: React.PointerEvent<SVGElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    dragRef.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
  }, []);

  const drawingPointerProps = useCallback((drawing: ChartDrawing, handleIndex: number | null = null) => ({
    onPointerDown: (event: React.PointerEvent<SVGElement>) => onDrawingDragStart(event, drawing, handleIndex),
    onPointerMove: onDrawingDragMove,
    onPointerUp: onDrawingDragEnd,
    onPointerCancel: onDrawingDragEnd,
    style: {
      pointerEvents: "all" as const,
      cursor: activeTool === "cursor" ? (drawing.locked ? "pointer" : handleIndex === null ? "move" : "grab") : "crosshair",
    },
  }), [activeTool, onDrawingDragEnd, onDrawingDragMove, onDrawingDragStart]);

  const commitTextDraft = useCallback(() => {
    if (!textDraft) return;
    const text = textDraft.value.trim();
    if (!text) {
      setTextDraft(null);
      setActiveTool("cursor");
      return;
    }
    if (textDraft.drawingId !== undefined) {
      updateDrawings((current) => current.map((drawing) => drawing.id === textDraft.drawingId && drawing.type === "text"
        ? { ...drawing, text: text.slice(0, 200) }
        : drawing));
      setSelectedDrawingId(textDraft.drawingId);
    } else {
      const id = drawingIdRef.current += 1;
      updateDrawings((current) => [...current, { id, type: "text", points: [textDraft.point], text: text.slice(0, 200) }]);
      setSelectedDrawingId(id);
    }
    setTextDraft(null);
    setActiveTool("cursor");
  }, [textDraft, updateDrawings]);

  const project = (point: DrawingPoint) => {
    const chart = chartRef.current;
    const candleSeries = candleSeriesRef.current;
    if (!chart || !candleSeries) return null;
    const x = chart.timeScale().timeToCoordinate(point.time as UTCTimestamp);
    const y = candleSeries.priceToCoordinate(point.price);
    return x === null || y === null ? null : { x, y };
  };

  const resetToRecent = () => {
    const chart = chartRef.current;
    const count = candlesRef.current.length;
    if (!chart || count === 0) return;
    chart.timeScale().setVisibleLogicalRange({ from: Math.max(0, count - 180), to: count + VISIBLE_FUTURE_BARS });
  };

  return (
    <div className="overflow-hidden rounded-2xl border border-border bg-background">
      <div className="flex min-h-14 items-center gap-3 overflow-x-auto border-b border-border px-3 py-2 [scrollbar-width:thin]">
        <div className="flex shrink-0 items-center gap-2 border-r border-border pr-3 text-[11px] uppercase tracking-wide text-muted-foreground">
          <span className="font-bold text-foreground">{normalizedSymbol}</span>
          <span>{exchange === "binance" ? "Binance" : "Bybit"} Perpetual</span>
          {status !== "live" ? (
            <span className="flex items-center gap-1.5 normal-case">
              <span className={`h-2 w-2 rounded-full ${STATUS_COLORS[status]}`} />
              {STATUS_LABELS[status]}
            </span>
          ) : null}
        </div>

        <div className="flex shrink-0 items-center gap-1 rounded-lg border border-border bg-card p-1">
          {INTERVALS.map((item) => (
            <button
              key={item.value}
              type="button"
              onClick={() => setSelectedInterval(item.value)}
              aria-pressed={selectedInterval === item.value}
              className={`min-w-9 rounded-md px-2 py-1.5 text-[11px] font-semibold transition-colors ${
                selectedInterval === item.value
                  ? "bg-accent text-primary ring-1 ring-ring"
                  : "text-muted-foreground hover:bg-card hover:text-foreground"
              }`}
            >
              {item.label}
            </button>
          ))}
        </div>

        <div className="flex shrink-0 items-center gap-2 border-l border-border pl-3">
          <IndicatorMenu
            active={activeIndicators}
            onToggle={toggleIndicator}
            onClear={() => setActiveIndicators([])}
          />
          <div className="hidden max-w-[280px] items-center gap-1 overflow-hidden xl:flex">
            {activeIndicators.slice(0, 3).map((id) => {
              const item = INDICATOR_CATALOG.find((candidate) => candidate.id === id);
              return item ? (
                <button
                  key={id}
                  type="button"
                  onClick={() => toggleIndicator(id)}
                  title={`Выключить ${item.label}`}
                  className="flex items-center gap-1.5 rounded-md bg-card px-2 py-1.5 text-[10px] font-medium text-muted-foreground hover:bg-secondary hover:text-foreground"
                >
                  <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: item.color }} />
                  {item.shortLabel}
                </button>
              ) : null;
            })}
            {activeIndicators.length > 3 ? <span className="text-[9px] text-muted-foreground">+{activeIndicators.length - 3}</span> : null}
          </div>
        </div>

        <button
          type="button"
          onClick={resetToRecent}
          className="ml-auto flex shrink-0 items-center gap-1.5 rounded-lg px-2.5 py-2 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-card hover:text-foreground"
          title="Вернуться к последним 180 свечам"
        >
          <ResetIcon />
          Сбросить масштаб
        </button>
      </div>

      <div className="flex h-[440px] min-h-[360px] lg:h-[560px]">
        <aside className="z-10 flex w-12 shrink-0 flex-col items-center gap-0.5 overflow-y-auto border-r border-border bg-background py-2 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
          {DRAWING_TOOLS.map((tool) => (
            <button
              key={tool.value}
              type="button"
              onClick={() => selectTool(tool.value)}
              aria-label={tool.label}
              aria-pressed={activeTool === tool.value}
              title={tool.label}
              className={`grid h-8 w-9 shrink-0 place-items-center rounded-lg transition-colors ${
                activeTool === tool.value
                  ? "bg-accent text-primary ring-1 ring-ring"
                  : "text-muted-foreground hover:bg-card hover:text-foreground"
              }`}
            >
              <ToolIcon tool={tool.value} />
            </button>
          ))}

          <div className="my-1 h-px w-6 bg-secondary" />
          <button
            type="button"
            onClick={() => {
              updateDrawings((current) => current.slice(0, -1));
              setSelectedDrawingId(null);
            }}
            disabled={drawings.length === 0}
            aria-label="Отменить последний объект"
            title="Отменить последний объект (Ctrl+Z)"
            className="grid h-8 w-9 shrink-0 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-card hover:text-foreground disabled:pointer-events-none disabled:opacity-25"
          >
            <UndoIcon />
          </button>
          <button
            type="button"
            onClick={() => {
              if (selectedDrawingId === null) return;
              updateDrawings((current) => current.filter((drawing) => drawing.id !== selectedDrawingId));
              setSelectedDrawingId(null);
            }}
            disabled={selectedDrawingId === null}
            aria-label="Удалить выбранный объект"
            title="Удалить выбранный объект (Delete)"
            className="grid h-8 w-9 shrink-0 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-rose-500/12 hover:text-destructive disabled:pointer-events-none disabled:opacity-25"
          >
            <TrashIcon />
          </button>
          <button
            type="button"
            onClick={() => {
              hidePreview();
              updateDrawings(() => []);
              setSelectedDrawingId(null);
            }}
            disabled={drawings.length === 0}
            aria-label="Удалить все объекты"
            title="Удалить все объекты"
            className="grid h-8 w-9 shrink-0 place-items-center rounded-lg text-muted-foreground transition-colors hover:bg-rose-500/12 hover:text-destructive disabled:pointer-events-none disabled:opacity-25"
          >
            <ClearAllIcon />
          </button>
          <button
            type="button"
            onClick={() => {
              if (!selectedDrawing) return;
              updateDrawings((current) => current.map((drawing) => drawing.id === selectedDrawing.id
                ? { ...drawing, locked: !drawing.locked }
                : drawing));
            }}
            disabled={!selectedDrawing}
            aria-label={selectedDrawing?.locked ? "Разблокировать объект" : "Заблокировать объект"}
            title={selectedDrawing?.locked ? "Разблокировать выбранный объект" : "Заблокировать выбранный объект"}
            className={`grid h-8 w-9 shrink-0 place-items-center rounded-lg transition-colors disabled:pointer-events-none disabled:opacity-25 ${
              selectedDrawing?.locked ? "bg-amber-400/12 text-amber-700 dark:text-amber-200" : "text-muted-foreground hover:bg-card hover:text-foreground"
            }`}
          >
            <LockIcon locked={selectedDrawing?.locked === true} />
          </button>
          <button
            type="button"
            onClick={() => {
              setDrawingsVisible((visible) => !visible);
              setSelectedDrawingId(null);
            }}
            disabled={drawings.length === 0}
            aria-label={drawingsVisible ? "Скрыть объекты" : "Показать объекты"}
            title={drawingsVisible ? "Скрыть все объекты" : "Показать все объекты"}
            className={`grid h-8 w-9 shrink-0 place-items-center rounded-lg transition-colors disabled:pointer-events-none disabled:opacity-25 ${
              drawingsVisible ? "text-muted-foreground hover:bg-card hover:text-foreground" : "bg-card text-muted-foreground"
            }`}
          >
            <EyeIcon visible={drawingsVisible} />
          </button>
          <button
            type="button"
            onClick={() => setMagnetEnabled((enabled) => !enabled)}
            aria-pressed={magnetEnabled}
            aria-label="Магнит к OHLC"
            title={magnetEnabled ? "Магнит к OHLC включён" : "Магнит к OHLC выключен"}
            className={`grid h-8 w-9 shrink-0 place-items-center rounded-lg transition-colors ${
              magnetEnabled ? "bg-sky-400/10 text-sky-700 dark:text-sky-200" : "text-muted-foreground hover:bg-card hover:text-foreground"
            }`}
          >
            <MagnetIcon enabled={magnetEnabled} />
          </button>
          <div className="my-1 h-px w-6 bg-secondary" />
          <button
            type="button"
            onClick={() => void persistConfig()}
            disabled={!isConfigLoaded || isSavingConfig}
            aria-label="Сохранить конфигурацию графика"
            title={hasUnsavedConfig ? "Сохранить конфигурацию" : "Конфигурация сохранена"}
            className={`relative grid h-8 w-9 shrink-0 place-items-center rounded-lg transition-colors disabled:pointer-events-none disabled:opacity-35 ${
              hasUnsavedConfig
                ? "bg-accent text-primary ring-1 ring-ring hover:bg-secondary"
                : "text-muted-foreground hover:bg-card hover:text-foreground"
            }`}
          >
            <SaveIcon busy={isSavingConfig} />
            {hasUnsavedConfig ? <span className="absolute right-1 top-1 h-1.5 w-1.5 rounded-full bg-amber-300 ring-2 ring-card" /> : null}
          </button>
        </aside>

        <div className="relative min-w-0 flex-1">
          <div ref={containerRef} className="absolute inset-0" />

          <div className="pointer-events-none absolute left-3 top-2 z-[3] max-w-[calc(100%-90px)] font-mono text-[11px] leading-5">
            <div ref={ohlcRef} data-direction="up" className="truncate text-primary data-[direction=down]:text-destructive" />
            <div ref={volumeLabelRef} className="text-muted-foreground" />
          </div>

          <svg className="pointer-events-none absolute inset-x-0 bottom-7 top-0 z-[4] h-[calc(100%-1.75rem)] w-full overflow-hidden" aria-hidden="true">
            <SmartMoneyOverlay
              output={smartMoneyOutput}
              active={smartMoneyActiveSet}
              width={interactionRef.current?.clientWidth ?? 0}
              project={(time, price) => project({ time, price })}
            />
            {drawingsVisible ? drawings.map((drawing) => {
              const start = project(drawing.points[0]);
              if (!start) return null;
              const isSelected = drawing.id === selectedDrawingId;
              if (drawing.type === "horizontal") {
                return (
                  <g key={drawing.id} data-drawing-id={drawing.id} data-drawing-type={drawing.type}>
                    <line x1="0" y1={start.y} x2="100%" y2={start.y} stroke={isSelected ? "#a7f3d0" : "#facc15"} strokeWidth={isSelected ? 2 : 1.25} strokeDasharray="5 4" />
                    <line x1="0" y1={start.y} x2="100%" y2={start.y} stroke="transparent" strokeWidth="14" {...drawingPointerProps(drawing)} />
                    <rect x="8" y={start.y - 17} width="76" height="18" rx="4" fill="#29301c" stroke="rgba(250,204,21,.45)" />
                    <text x="14" y={start.y - 5} fill="#fde68a" fontSize="10">{formatPrice(drawing.points[0].price, priceFormat.precision)}</text>
                    {isSelected && !drawing.locked ? <circle cx="92" cy={start.y} r="4.5" fill="#0d1711" stroke="#a7f3d0" strokeWidth="1.5" {...drawingPointerProps(drawing, 0)} /> : null}
                  </g>
                );
              }
              if (drawing.type === "vertical") {
                return (
                  <g key={drawing.id} data-drawing-id={drawing.id} data-drawing-type={drawing.type}>
                    <line x1={start.x} y1="0" x2={start.x} y2="100%" stroke={isSelected ? "#a7f3d0" : "#c084fc"} strokeWidth={isSelected ? 2 : 1.25} strokeDasharray="5 4" />
                    <line x1={start.x} y1="0" x2={start.x} y2="100%" stroke="transparent" strokeWidth="14" {...drawingPointerProps(drawing)} />
                    {isSelected && !drawing.locked ? <circle cx={start.x} cy="30" r="4.5" fill="#0d1711" stroke="#a7f3d0" strokeWidth="1.5" {...drawingPointerProps(drawing, 0)} /> : null}
                  </g>
                );
              }
              if (drawing.type === "text") {
                const textWidth = Math.max(54, Math.min(240, drawing.text.length * 7.2 + 16));
                return (
                  <g key={drawing.id} data-drawing-id={drawing.id} data-drawing-type={drawing.type}>
                    <rect x={start.x} y={start.y - 22} width={textWidth} height="25" rx="5" fill="rgba(15,35,24,.9)" stroke={isSelected ? "#a7f3d0" : "rgba(110,231,183,.38)"} strokeWidth={isSelected ? 1.5 : 1} />
                    <text x={start.x + 8} y={start.y - 6} fill="#d1fae5" fontSize="11">{drawing.text}</text>
                    <rect
                      x={start.x}
                      y={start.y - 22}
                      width={textWidth}
                      height="25"
                      fill="transparent"
                      {...drawingPointerProps(drawing)}
                      onDoubleClick={(event) => {
                        event.stopPropagation();
                        setTextDraft({ drawingId: drawing.id, point: drawing.points[0], x: start.x, y: start.y, value: drawing.text });
                      }}
                    />
                    {isSelected && !drawing.locked ? <circle cx={start.x} cy={start.y} r="4.5" fill="#0d1711" stroke="#a7f3d0" strokeWidth="1.5" {...drawingPointerProps(drawing, 0)} /> : null}
                  </g>
                );
              }
              if (drawing.type === "brush") {
                const projected = drawing.points.map(project).filter((point) => point !== null);
                if (projected.length === 0) return null;
                const path = projected.map((point, index) => `${index === 0 ? "M" : "L"}${point.x} ${point.y}`).join(" ");
                return (
                  <g key={drawing.id} data-drawing-id={drawing.id} data-drawing-type={drawing.type}>
                    <path d={path} fill="none" stroke={isSelected ? "#a7f3d0" : "#f9a8d4"} strokeWidth={isSelected ? 2.5 : 2} strokeLinecap="round" strokeLinejoin="round" />
                    <path d={path} fill="none" stroke="transparent" strokeWidth="14" strokeLinecap="round" strokeLinejoin="round" {...drawingPointerProps(drawing)} />
                  </g>
                );
              }
              if (drawing.points.length < 2) return null;
              const endPoint = drawing.points[1];
              if (!endPoint) return null;
              const end = project(endPoint);
              if (!end) return null;
              const isRuler = drawing.type === "ruler";
              const isRectangle = drawing.type === "rectangle";
              const isFibonacci = drawing.type === "fibonacci";
              const delta = endPoint.price - drawing.points[0].price;
              const percent = drawing.points[0].price === 0 ? 0 : (delta / drawing.points[0].price) * 100;
              const duration = formatDuration(endPoint.time - drawing.points[0].time);
              const labelParts = [
                `${delta >= 0 ? "+" : ""}${formatPrice(delta, priceFormat.precision)}`,
                `${percent >= 0 ? "+" : ""}${percent.toFixed(2)}%`,
                duration,
              ];
              const labelWidth = Math.max(92, labelParts.join("").length * 6.1 + 50);
              const labelX = Math.min(start.x, end.x) + Math.abs(end.x - start.x) / 2 - labelWidth / 2;
              const labelY = Math.min(start.y, end.y) - 25;
              const left = Math.min(start.x, end.x);
              const top = Math.min(start.y, end.y);
              const width = Math.abs(end.x - start.x);
              const height = Math.abs(end.y - start.y);
              const overlayWidth = interactionRef.current?.clientWidth ?? Math.max(start.x, end.x);
              const rayX = drawing.type === "ray" ? (end.x >= start.x ? overlayWidth : 0) : end.x;
              const rayY = drawing.type === "ray" && end.x !== start.x
                ? start.y + ((end.y - start.y) / (end.x - start.x)) * (rayX - start.x)
                : end.y;
              const lineColor = isSelected ? "#a7f3d0" : isRuler ? "#4ade80" : drawing.type === "ray" ? "#fb923c" : "#60a5fa";
              return (
                <g key={drawing.id} data-drawing-id={drawing.id} data-drawing-type={drawing.type}>
                  {isRectangle ? (
                    <>
                      <rect x={left} y={top} width={width} height={height} fill="rgba(96,165,250,.08)" stroke={lineColor} strokeWidth={isSelected ? 2 : 1.5} />
                      <rect x={left} y={top} width={Math.max(width, 3)} height={Math.max(height, 3)} fill="transparent" stroke="transparent" strokeWidth="12" {...drawingPointerProps(drawing)} />
                    </>
                  ) : null}
                  {isFibonacci ? (
                    <>
                      <rect x={left} y={top} width={width} height={height} fill="rgba(167,243,208,.025)" stroke="rgba(167,243,208,.25)" strokeWidth="1" />
                      {FIBONACCI_LEVELS.map((level) => {
                        const y = start.y + (end.y - start.y) * level;
                        return (
                          <g key={level}>
                            <line x1={start.x} y1={y} x2={end.x} y2={y} stroke={isSelected ? "#a7f3d0" : "#fbbf24"} strokeWidth={level === 0 || level === 1 ? 1.3 : 1} opacity={level === 0 || level === 1 ? 1 : 0.75} />
                            <text x={left + 4} y={y - 3} fill="#fde68a" fontSize="9">{level}</text>
                          </g>
                        );
                      })}
                      <rect x={left} y={top} width={Math.max(width, 3)} height={Math.max(height, 3)} fill="transparent" stroke="transparent" strokeWidth="12" {...drawingPointerProps(drawing)} />
                    </>
                  ) : null}
                  {!isRectangle && !isFibonacci ? (
                    <>
                      <line x1={start.x} y1={start.y} x2={rayX} y2={rayY} stroke={lineColor} strokeWidth={isSelected ? 2 : 1.5} />
                      <line x1={start.x} y1={start.y} x2={rayX} y2={rayY} stroke="transparent" strokeWidth="14" {...drawingPointerProps(drawing)} />
                    </>
                  ) : null}
                  {isSelected && !drawing.locked ? (
                    <>
                      <circle cx={start.x} cy={start.y} r="4.5" fill="#0d1711" stroke="#a7f3d0" strokeWidth="1.5" {...drawingPointerProps(drawing, 0)} />
                      <circle cx={end.x} cy={end.y} r="4.5" fill="#0d1711" stroke="#a7f3d0" strokeWidth="1.5" {...drawingPointerProps(drawing, 1)} />
                    </>
                  ) : null}
                  {isRuler ? (
                    <g>
                      <rect x={labelX} y={labelY} width={labelWidth} height="20" rx="4" fill="#173524" stroke="rgba(74,222,128,.5)" />
                      <text x={labelX + 7} y={labelY + 13.5} fill="#bbf7d0" fontSize="10">
                        {labelParts.map((part, index) => <tspan key={index} dx={index === 0 ? 0 : 18}>{part}</tspan>)}
                      </text>
                    </g>
                  ) : null}
                </g>
              );
            }) : null}
            <line ref={previewLineRef} opacity="0" stroke="#6ee7b7" strokeWidth="1.5" strokeDasharray="5 4" />
          </svg>

          <div
            ref={interactionRef}
            onPointerDown={onDrawingPointerDown}
            onPointerMove={onDrawingPointerMove}
            onPointerUp={onDrawingPointerUp}
            onPointerCancel={onDrawingPointerUp}
            className={`absolute inset-x-0 bottom-7 top-0 z-[5] ${activeTool === "cursor" ? "pointer-events-none" : "cursor-crosshair"}`}
          />

          {textDraft ? (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                commitTextDraft();
              }}
              className="absolute z-[8] flex w-[230px] gap-1 rounded-lg border border-border-strong bg-background/98 p-1.5 shadow-2xl"
              style={{ left: Math.max(8, Math.min(textDraft.x, (interactionRef.current?.clientWidth ?? 250) - 238)), top: Math.max(42, textDraft.y - 36) }}
            >
              <input
                autoFocus
                value={textDraft.value}
                maxLength={200}
                onChange={(event) => setTextDraft((current) => current ? { ...current, value: event.target.value } : null)}
                onKeyDown={(event) => {
                  if (event.key === "Escape") {
                    setTextDraft(null);
                    setActiveTool("cursor");
                  }
                }}
                placeholder="Текст заметки"
                className="min-w-0 flex-1 rounded-md border border-border bg-background px-2 py-1.5 text-[11px] text-foreground outline-none placeholder:text-muted-foreground focus:border-ring"
              />
              <button type="submit" className="rounded-md bg-accent px-2 text-[10px] font-semibold text-primary hover:bg-accent">
                OK
              </button>
            </form>
          ) : null}

          {activeTool !== "cursor" ? (
            <div className="pointer-events-none absolute bottom-10 left-3 z-[6] rounded-md border border-border-strong bg-background/92 px-2.5 py-1.5 text-[10px] text-primary shadow-lg">
              {activeTool === "horizontal" || activeTool === "vertical"
                ? "Кликните для установки линии"
                : activeTool === "brush"
                  ? "Зажмите мышь и рисуйте · Esc — отмена"
                  : activeTool === "text"
                    ? "Кликните в месте заметки"
                    : "Выберите две точки · Esc — отмена"}
            </div>
          ) : null}

          {activeTool === "cursor" && selectedDrawing ? (
            <div className="pointer-events-none absolute bottom-10 left-3 z-[6] rounded-md border border-border-strong bg-background/94 px-2.5 py-1.5 text-[10px] text-primary shadow-lg">
              {selectedDrawing.locked ? "Объект заблокирован" : "Тяните объект или его опорные точки"} · Delete — удалить
            </div>
          ) : null}

          {status === "loading" ? (
            <div className="pointer-events-none absolute inset-0 z-[7] flex items-center justify-center bg-background/88 text-sm text-muted-foreground">
              Загружаю 1 000 свечей {exchange === "binance" ? "Binance" : "Bybit"}…
            </div>
          ) : null}
          {status === "error" ? (
            <div className="absolute inset-0 z-[7] flex flex-col items-center justify-center gap-3 bg-background/95 px-6 text-center">
              <div className="text-sm font-semibold text-destructive">Не удалось загрузить график</div>
              <div className="max-w-md text-xs text-muted-foreground">{error}</div>
              <button
                type="button"
                onClick={() => setReloadKey((value) => value + 1)}
                className="rounded-lg border border-border-strong bg-accent px-4 py-2 text-xs font-semibold text-primary hover:bg-accent"
              >
                Повторить
              </button>
            </div>
          ) : null}
        </div>
      </div>

    </div>
  );
}
