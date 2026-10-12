import type { ExternalPane } from "./derivatives";
import { VolumeProfileLayer } from "./VolumeProfileLayer";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  AreaSeries,
  BarSeries,
  CandlestickSeries,
  HistogramSeries,
  LineSeries,
  PriceScaleMode,
  createSeriesMarkers,
  type ISeriesApi,
  type SeriesMarker,
  type Time,
  type IPriceLine,
} from "lightweight-charts";
import {
  createUnifiedChart,
  normalizeCandles,
  timestamp,
  type Candle,
} from "./engine";
import { calculateIndicatorBatch } from "../market/indicatorBatch";
import {
  INDICATOR_CATALOG,
  defaultIndicatorPeriod,
  isClassicIndicatorId,
  isSmartMoneyIndicatorId,
  type IndicatorId,
} from "../market/indicatorTypes";
import { calculateSmartMoney } from "../market/smartMoneyEngine";
import { SmartMoneyOverlay } from "../market/SmartMoneyOverlay";
import {
  inferPerpPriceFormat,
  type PerpCandle,
  type PerpPriceFormat,
} from "../market/perpMarketData";
import { useTheme } from "../theme/ThemeContext";
import { readChartTheme } from "../market/chartTheme";
import { DrawingEditor, type DrawingRuntime } from "./drawings/DrawingEditor";
import { useDrawingHistory } from "./drawings/useDrawingHistory";
import { type Drawing } from "./drawings/model";
import "./charts.css";
export type ChartMode = "candles" | "bars" | "line" | "area";
export type IndicatorInstance = {
  key: string;
  id: import("../market/indicatorTypes").ClassicIndicatorId;
  period?: number;
  color?: string;
};
export type ChartSurfaceProps = {
  instances?: IndicatorInstance[];
  candles: readonly Candle[];
  externalPanes?: ExternalPane[];
  title?: string;
  height?: number | string;
  interval?: number;
  mode?: ChartMode;
  indicators?: IndicatorId[];
  periods?: Partial<Record<IndicatorId, number>>;
  volume?: boolean;
  drawings?: boolean;
  drawingKey?: string;
  initialDrawings?: Drawing[];
  onDrawingsChange?: (items: Drawing[]) => void;
  onSave?: () => void;
  saveStatus?: string;
  priceFormat?: PerpPriceFormat;
  selecting?: boolean;
  onSelect?: (time: number) => void;
  onHover?: (c: Candle | null) => void;
  markers?: SeriesMarker<Time>[];
  priceLines?: { price: number; color?: string; title: string }[];
  onReady?: (api: DrawingRuntime | null) => void;
  onNeedHistory?: () => void;
  resetToken?: number;
  follow?: boolean;
  fit?: boolean;
  log?: boolean;
  autoScale?: boolean;
  grid?: boolean;
  children?: ReactNode;
  loading?: boolean;
  error?: string;
  initialBars?: number;
  className?: string;
  testId?: string;
};
const EMPTY_INSTANCES: IndicatorInstance[] = [];
const EMPTY_PANES: ExternalPane[] = [];
const EMPTY_CANDLES: Candle[] = [],
  EMPTY_IDS: IndicatorId[] = [],
  EMPTY_DRAWINGS: Drawing[] = [],
  EMPTY_MARKERS: SeriesMarker<Time>[] = [],
  EMPTY_LINES: { price: number; color?: string; title: string }[] = [];
export function ChartSurface({
  candles: raw,
  instances = EMPTY_INSTANCES,
  externalPanes = EMPTY_PANES,
  title,
  height = 500,
  interval = 60,
  mode = "candles",
  indicators = EMPTY_IDS,
  periods,
  volume = true,
  drawings = true,
  drawingKey = "chart",
  initialDrawings = EMPTY_DRAWINGS,
  onDrawingsChange,
  onSave,
  saveStatus,
  priceFormat,
  selecting,
  onSelect,
  onHover,
  markers = EMPTY_MARKERS,
  priceLines = EMPTY_LINES,
  onReady,
  onNeedHistory,
  resetToken = 0,
  follow = false,
  fit = false,
  log = false,
  autoScale = true,
  grid = true,
  children,
  loading,
  error,
  initialBars = 180,
  className = "",
  testId,
}: ChartSurfaceProps) {
  const { theme } = useTheme(),
    host = useRef<HTMLDivElement>(null),
    [api, setApi] = useState<DrawingRuntime | null>(null),
    [hover, setHover] = useState<Candle | null>(null),
    [indicatorError, setIndicatorError] = useState("");
  const [indicatorPoints, setIndicatorPoints] = useState<
    Record<string, number>
  >({});
  const candles = useMemo(() => normalizeCandles(raw), [raw]);
  const marketTimes = useMemo(
    () => new Set(candles.map((c) => c.time)),
    [candles],
  );
  const marketPoints = useMemo(
    () =>
      Object.fromEntries(
        externalPanes.map((p) => [
          p.id,
          p.data.filter((v) => marketTimes.has(v.time)).length,
        ]),
      ),
    [externalPanes, marketTimes],
  );
  const indicatorOwner = useRef<DrawingRuntime | null>(null);
  const calculationGeneration = useRef(0);
  const indicatorRuntime = useRef<
    Map<string, ISeriesApi<"Line" | "Histogram" | "Candlestick">>
  >(new Map());
  const lastReset = useRef(0);
  const history = useDrawingHistory(drawingKey, initialDrawings),
    previous = useRef<readonly Candle[]>(EMPTY_CANDLES),
    state = useRef({ candles, onSelect, onHover, onNeedHistory, selecting }),
    ready = useRef(onReady),
    loadedKey = useRef(drawingKey),
    loadedInitial = useRef(initialDrawings);
  useEffect(() => {
    state.current = { candles, onSelect, onHover, onNeedHistory, selecting };
    ready.current = onReady;
  });
  useEffect(() => {
    if (
      loadedKey.current !== drawingKey ||
      loadedInitial.current !== initialDrawings
    ) {
      history.replace(initialDrawings);
      loadedKey.current = drawingKey;
      loadedInitial.current = initialDrawings;
    }
  }, [drawingKey, initialDrawings, history]);
  useEffect(() => {
    onDrawingsChange?.(history.items);
  }, [history.items, onDrawingsChange]);
  useEffect(() => {
    const node = host.current;
    if (!node) return;
    const chart = createUnifiedChart(node),
      t = readChartTheme();
    const definition =
      mode === "line"
        ? LineSeries
        : mode === "area"
          ? AreaSeries
          : mode === "bars"
            ? BarSeries
            : CandlestickSeries;
    const series = chart.addSeries(definition as typeof CandlestickSeries, {
      upColor: t.up,
      downColor: t.down,
      wickUpColor: t.up,
      wickDownColor: t.down,
      borderVisible: false,
      ...(mode === "line" || mode === "area"
        ? {
            color: t.up,
            lineColor: t.up,
            topColor: t.up + "33",
            bottomColor: t.up + "00",
          }
        : {}),
    });
    const runtime = { chart, series } as DrawingRuntime;
    setApi(runtime);
    ready.current?.(runtime);
    previous.current = EMPTY_CANDLES;
    const click: Parameters<typeof chart.subscribeClick>[0] = (p) => {
      if (!state.current.selecting || p.paneIndex !== 0 || !p.time) return;
      state.current.onSelect?.(Number(p.time));
    };
    const crosshair: Parameters<typeof chart.subscribeCrosshairMove>[0] = (
      p,
    ) => {
      if (!p.time) {
        setHover(null);
        state.current.onHover?.(null);
        return;
      }
      const point =
        state.current.candles.find((c) => c.time === Number(p.time)) ?? null;
      setHover(point);
      state.current.onHover?.(point);
    };
    const range: Parameters<
      ReturnType<typeof chart.timeScale>["subscribeVisibleLogicalRangeChange"]
    >[0] = (range) => {
      if (range)
        host.current?.parentElement?.setAttribute(
          "data-range-from",
          String(range.from),
        );
      if (range && range.from < 25) state.current.onNeedHistory?.();
    };
    chart.subscribeClick(click);
    chart.subscribeCrosshairMove(crosshair);
    chart.timeScale().subscribeVisibleLogicalRangeChange(range);
    return () => {
      runtime.disposed = true;
      ready.current?.(null);
      chart.unsubscribeClick(click);
      chart.unsubscribeCrosshairMove(crosshair);
      chart.timeScale().unsubscribeVisibleLogicalRangeChange(range);
      chart.remove();
    };
  }, [mode]);
  useEffect(() => {
    if (!api || api.disposed) return;
    const t = readChartTheme();
    api.chart.applyOptions({
      layout: {
        background: { color: t.surface },
        textColor: t.foreground,
        panes: { separatorColor: t.grid, separatorHoverColor: t.crosshair },
      },
      grid: {
        vertLines: { color: t.grid, visible: grid },
        horzLines: { color: t.grid, visible: grid },
      },
      rightPriceScale: {
        borderColor: t.grid,
        autoScale,
        mode: log ? PriceScaleMode.Logarithmic : PriceScaleMode.Normal,
      },
      timeScale: { borderColor: t.grid },
      crosshair: {
        vertLine: { color: t.crosshair, labelBackgroundColor: t.label },
        horzLine: { color: t.crosshair, labelBackgroundColor: t.label },
      },
    });
    api.series.applyOptions({
      upColor: t.up,
      downColor: t.down,
      wickUpColor: t.up,
      wickDownColor: t.down,
      ...(mode === "line" || mode === "area"
        ? {
            color: t.up,
            lineColor: t.up,
            topColor: t.up + "33",
            bottomColor: t.up + "00",
          }
        : {}),
    } as never);
  }, [api, theme, log, autoScale, grid, mode]);
  useEffect(() => {
    if (!api || api.disposed) return;
    api.series.applyOptions({
      priceFormat: {
        type: "price",
        ...(priceFormat ?? inferPerpPriceFormat(candles as PerpCandle[])),
      },
    });
    const prior = previous.current,
      last = candles.at(-1);
    const single =
      prior.length > 0 &&
      candles[0]?.time === prior[0]?.time &&
      (candles.length === prior.length ||
        candles.length === prior.length + 1) &&
      candles[Math.max(0, prior.length - 2)] ===
        prior[Math.max(0, prior.length - 2)];
    if (single && last) {
      api.series.update(
        (mode === "line" || mode === "area"
          ? { time: timestamp(last.time), value: last.close }
          : { ...last, time: timestamp(last.time) }) as never,
      );
    } else {
      const range = api.chart.timeScale().getVisibleLogicalRange();
      api.series.setData(
        candles.map((c) =>
          mode === "line" || mode === "area"
            ? { time: timestamp(c.time), value: c.close }
            : { ...c, time: timestamp(c.time) },
        ) as never,
      );
      if (!prior.length) {
        if (fit) api.chart.timeScale().fitContent();
        else
          api.chart.timeScale().setVisibleLogicalRange({
            from: Math.max(-4, candles.length - initialBars),
            to: candles.length + 12,
          });
      } else if (range && prior[0] && candles[0]?.time < prior[0].time) {
        const count = candles.findIndex((c) => c.time === prior[0].time);
        if (count >= 0)
          api.chart.timeScale().setVisibleLogicalRange({
            from: range.from + count,
            to: range.to + count,
          });
      } else if (
        range &&
        prior[0] &&
        candles[0] &&
        candles[0].time > prior[0].time
      ) {
        const removed = prior.findIndex((c) => c.time === candles[0].time);
        if (removed > 0)
          api.chart.timeScale().setVisibleLogicalRange({
            from: range.from - removed,
            to: range.to - removed,
          });
      }
    }
    if (
      follow &&
      candles.length > prior.length &&
      prior.length &&
      api.chart.timeScale().scrollPosition() < 14
    )
      api.chart.timeScale().scrollToRealTime();
    previous.current = candles;
  }, [api, candles, mode, fit, initialBars, follow, priceFormat]);
  useEffect(() => {
    if (
      api &&
      !api.disposed &&
      resetToken &&
      lastReset.current !== resetToken
    ) {
      lastReset.current = resetToken;
      api.chart.priceScale("right").applyOptions({ autoScale: true });
      api.chart.timeScale().setVisibleLogicalRange({
        from: Math.max(-4, candles.length - initialBars),
        to: candles.length + 12,
      });
    }
  }, [api, resetToken, candles.length, initialBars]);
  useEffect(() => {
    if (!api || api.disposed || !volume) return;
    const series = api.chart.addSeries(
      HistogramSeries,
      {
        title: "Объём",
        priceFormat: { type: "volume" },
        priceLineVisible: false,
        lastValueVisible: true,
      },
      1,
    );
    api.chart.panes()[1]?.setStretchFactor(1);
    api.chart.panes()[0]?.setStretchFactor(5);
    return () => {
      if (!api.disposed) api.chart.removeSeries(series);
    };
  }, [api, volume]);
  useEffect(() => {
    if (!api || api.disposed || !volume) return;
    const pane = api.chart.panes()[1];
    const series = pane?.getSeries()[0] as ISeriesApi<"Histogram"> | undefined;
    if (!series) return;
    const t = readChartTheme();
    series.setData(
      candles.map((c) => ({
        time: timestamp(c.time),
        value: c.volume,
        color: c.close >= c.open ? t.up + "80" : t.down + "80",
      })),
    );
  }, [api, candles, volume, theme]);
  useEffect(() => {
    if (!api || api.disposed) return;
    const runtime = indicatorRuntime.current;
    calculationGeneration.current++;
    for (const series of runtime.values())
      if (indicatorOwner.current === api && !api.disposed)
        api.chart.removeSeries(series);
    runtime.clear();
    indicatorOwner.current = api;
  }, [api, indicators, periods, volume, instances, drawingKey, interval]);
  useEffect(() => {
    if (!api || api.disposed) return;
    let disposed = false;
    const generation = calculationGeneration.current,
      abort = new AbortController();
    const timer = setTimeout(async () => {
      try {
        const definitions = [
          ...indicators.filter(isClassicIndicatorId).map((id) => ({
            key: id,
            id,
            period: periods?.[id],
            color: undefined,
          })),
          ...instances,
        ];
        const output = await calculateIndicatorBatch(
          candles as PerpCandle[],
          definitions,
          abort.signal,
          interval,
        );
        if (api.disposed || generation !== calculationGeneration.current)
          return;
        let pane = volume ? 2 : 1;
        for (const { definition, indicator } of output) {
          const paneIndex = indicator.placement === "pane" ? pane++ : 0;
          for (const line of indicator.series) {
            const key = `${definition.key}:${line.key}`;
            let series = indicatorRuntime.current.get(key);
            if (!series) {
              api.chart.panes()[0]?.setStretchFactor(5);
              series =
                line.kind === "line"
                  ? api.chart.addSeries(
                      LineSeries,
                      {
                        title: line.title,
                        color: definition.color ?? line.color,
                        lineWidth: line.lineWidth ?? 1,
                        lineStyle: line.lineStyle ?? 0,
                        lineVisible: line.lineVisible ?? true,
                        pointMarkersVisible: line.pointMarkersVisible ?? false,
                        priceLineVisible: false,
                        lastValueVisible: indicator.placement === "pane",
                      },
                      paneIndex,
                    )
                  : api.chart.addSeries(
                      HistogramSeries,
                      {
                        title: line.title,
                        color: definition.color ?? line.color,
                        priceLineVisible: false,
                      },
                      paneIndex,
                    );
              indicatorRuntime.current.set(key, series);
              if (indicator.placement === "pane")
                api.chart.panes()[paneIndex]?.setStretchFactor(1.25);
            }
            series.setData(
              (definition.color && line.kind === "histogram"
                ? line.data.map((point) => ({
                    ...point,
                    color: definition.color,
                  }))
                : line.data) as never,
            );
          }
        }
        setIndicatorError("");
        setIndicatorPoints(
          Object.fromEntries(
            output.map(({ definition, indicator }) => [
              definition.key,
              indicator.series.reduce((total, s) => total + s.data.length, 0),
            ]),
          ),
        );
      } catch {
        if (!disposed && !api.disposed)
          setIndicatorError("Не удалось рассчитать индикаторы");
      }
    }, 180);
    return () => {
      disposed = true;
      clearTimeout(timer);
      abort.abort();
    };
  }, [
    api,
    candles,
    indicators,
    periods,
    volume,
    instances,
    drawingKey,
    interval,
  ]);
  useEffect(() => {
    if (!api || api.disposed) return;
    const times = new Set(candles.map((c) => c.time));
    const plugin = createSeriesMarkers(
      api.series,
      markers
        .filter((m) => times.has(Number(m.time)))
        .sort((a, b) => Number(a.time) - Number(b.time)),
    );
    return () => {
      if (!api.disposed) plugin.detach();
    };
  }, [api, markers, candles]);
  useEffect(() => {
    if (!api || api.disposed) return;
    const lines: IPriceLine[] = [];
    for (const p of priceLines)
      if (Number.isFinite(p.price) && p.price > 0)
        lines.push(
          api.series.createPriceLine({
            price: p.price,
            color: p.color ?? readChartTheme().foreground,
            lineWidth: 1,
            lineStyle: 2,
            axisLabelVisible: true,
            title: p.title,
          }),
        );
    return () => {
      if (!api.disposed) for (const p of lines) api.series.removePriceLine(p);
    };
  }, [api, priceLines]);
  useEffect(() => {
    if (!api || api.disposed) return;
    const active = new Set(externalPanes.map((p) => p.id));
    for (const [key, series] of indicatorRuntime.current) {
      if (key.startsWith("external:") && !active.has(key.split(":")[1])) {
        api.chart.removeSeries(series);
        indicatorRuntime.current.delete(key);
      }
    }
    let pane =
      (volume ? 2 : 1) +
      indicators.filter(
        (id) =>
          INDICATOR_CATALOG.find((i) => i.id === id)?.placement === "pane",
      ).length +
      instances.filter(
        (i) =>
          INDICATOR_CATALOG.find((p) => p.id === i.id)?.placement === "pane",
      ).length;
    const times = new Set(candles.map((c) => c.time));
    for (const data of externalPanes) {
      const definitions = data.series ?? [
        {
          key: "main",
          kind: "line" as const,
          title: data.title,
          color: data.color,
          data: data.data,
        },
      ];
      for (const definition of definitions) {
        const key = `external:${data.id}:${definition.key}`;
        let series = indicatorRuntime.current.get(key);
        if (!series) {
          series = api.chart.addSeries(
            definition.kind === "candles"
              ? CandlestickSeries
              : definition.kind === "histogram"
                ? HistogramSeries
                : LineSeries,
            {
              title: definition.title,
              color: definition.color,
              upColor: "#34d399",
              downColor: "#fb7185",
              wickUpColor: "#34d399",
              wickDownColor: "#fb7185",
              borderVisible: false,
              lineWidth: 1,
              priceLineVisible: false,
              priceFormat: data.percent
                ? {
                    type: "custom",
                    formatter: (v: number) => v.toFixed(4) + "%",
                  }
                : { type: "volume" },
            },
            pane,
          );
          indicatorRuntime.current.set(key, series);
          api.chart.panes()[pane]?.setStretchFactor(1.25);
        }
        series.moveToPane(pane);
        series.setData(
          definition.data
            .filter((p) => times.has(p.time))
            .map((p) => ({
              ...p,
              time: timestamp(p.time),
              ...(definition.kind === "histogram" && "value" in p
                ? { color: p.value < 0 ? "#fb7185" : definition.color }
                : {}),
            })) as never,
        );
      }
      pane++;
    }
  }, [api, externalPanes, candles, indicators, volume, periods, instances]);
  const classic = indicators.map((id) =>
      INDICATOR_CATALOG.find((i) => i.id === id),
    ),
    current = hover ?? candles.at(-1),
    precision =
      priceFormat?.precision ??
      inferPerpPriceFormat(candles as PerpCandle[]).precision;
  return (
    <div
      className={`ct-surface ${drawings ? "ct-with-drawings" : ""} ${className}`}
      style={{ height }}
      data-chart-surface
      tabIndex={0}
      data-testid={testId}
      data-visible-count={candles.length}
      data-last-time={candles.at(-1)?.time}
      data-drawing-count={history.items.length}
      data-indicator-points={JSON.stringify(indicatorPoints)}
      data-market-points={JSON.stringify(marketPoints)}
    >
      <div
        className="ct-chart-host"
        ref={host}
        role="img"
        aria-label={title ?? "Интерактивный график"}
      />
      {current && (
        <div className="ct-ohlc" data-testid="ohlc">
          <strong>{title}</strong>
          <span className={current.close >= current.open ? "ct-up" : "ct-down"}>
            {[
              ["ОТКР", current.open],
              ["МАКС", current.high],
              ["МИН", current.low],
              ["ЗАКР", current.close],
            ].map(([label, value]) => (
              <span key={label}>
                {label}{" "}
                <b>
                  {Number(value).toLocaleString("ru-RU", {
                    minimumFractionDigits: precision,
                    maximumFractionDigits: precision,
                  })}
                </b>{" "}
              </span>
            ))}
            <b>{((current.close / current.open - 1) * 100).toFixed(2)}%</b>
          </span>
        </div>
      )}
      {(!!classic.length || !!instances.length) && (
        <div className="ct-indicator-legend">
          {instances.map((i) => (
            <span
              key={i.key}
              style={{
                color:
                  i.color ??
                  INDICATOR_CATALOG.find((p) => p.id === i.id)?.color,
              }}
            >
              {i.id.toUpperCase()} {i.period ?? ""}
              {indicatorPoints[i.key] === 0 &&
                !i.id.includes("volume_profile") && (
                  <span title="Проверьте глубину истории и таймфрейм; подтверждённых событий может не быть">
                    {" "}
                    · нет значений
                  </span>
                )}
            </span>
          ))}
          {classic.map(
            (i) =>
              i && (
                <span key={i.id} style={{ color: i.color }}>
                  {defaultIndicatorPeriod(i.id) !== undefined
                    ? `${i.id === "supertrend" ? "ST" : i.shortLabel.replace(/\s+\d+$/, "")} ${periods?.[i.id] ?? defaultIndicatorPeriod(i.id)}${i.id === "supertrend" ? "×3" : ""}`
                    : i.shortLabel}
                  {indicatorPoints[i.id] === 0 &&
                    !i.id.includes("volume_profile") && (
                      <span title="Проверьте глубину истории и таймфрейм; подтверждённых событий может не быть">
                        {" "}
                        · нет значений
                      </span>
                    )}
                </span>
              ),
          )}
        </div>
      )}
      {api && drawings && (
        <DrawingEditor
          api={api}
          candles={candles}
          interval={interval}
          history={history}
          disabled={selecting}
          onSave={onSave}
          cloudStatus={saveStatus}
        />
      )}
      {api && indicators.some(isSmartMoneyIndicatorId) && (
        <SmartMoneyLayer api={api} candles={candles} indicators={indicators} />
      )}
      {api &&
        [
          ...indicators.map((id) => ({
            key: id,
            id,
            period: periods?.[id],
            color: undefined,
          })),
          ...instances,
        ]
          .filter(
            (i) =>
              i.id === "volume_profile" || i.id === "visible_volume_profile",
          )
          .map((i) => (
            <VolumeProfileLayer
              key={i.key}
              api={api}
              candles={candles}
              period={i.period ?? 100}
              color={i.color}
              visible={i.id === "visible_volume_profile"}
            />
          ))}
      {externalPanes.some((p) => p.error || !p.data.length) && (
        <div className="ct-external-errors">
          {externalPanes
            .filter((p) => p.error || !p.data.length)
            .map((p) => (
              <span key={p.id}>
                {p.title}: {p.error ?? "история недоступна"}
              </span>
            ))}
        </div>
      )}
      {!!externalPanes.length && (
        <details className="ct-external-notes">
          <summary>Источники и границы данных ({externalPanes.length})</summary>
          <div>
            {externalPanes.map((p) => (
              <p key={p.id}>
                <strong>{p.title}</strong>
                <br />
                {p.error || p.note || "История выбранной биржи"}
              </p>
            ))}
          </div>
        </details>
      )}
      {selecting && (
        <div className="ct-selection-hint">
          Выберите свечу начала воспроизведения
        </div>
      )}
      {(loading || error || indicatorError || !candles.length) && (
        <div
          className={`ct-chart-message ${candles.length ? "ct-message-inline" : ""}`}
          role={error ? "alert" : "status"}
        >
          {error ||
            indicatorError ||
            (loading
              ? "Загружаем реальные рыночные данные…"
              : "Нет свечей для отображения")}
        </div>
      )}
      {children}
    </div>
  );
}
function SmartMoneyLayer({
  api,
  candles,
  indicators,
}: {
  api: DrawingRuntime;
  candles: Candle[];
  indicators: IndicatorId[];
}) {
  const [, render] = useState(0);
  const active = useMemo(
      () => new Set(indicators.filter(isSmartMoneyIndicatorId)),
      [indicators],
    ),
    output = useMemo(
      () => calculateSmartMoney(candles as PerpCandle[]),
      [candles],
    );
  useEffect(() => {
    let frame: number | null = null;
    const update = () => {
      if (frame === null)
        frame = requestAnimationFrame(() => {
          frame = null;
          render((n) => n + 1);
        });
    };
    api.chart.timeScale().subscribeVisibleLogicalRangeChange(update);
    api.chart.subscribeCrosshairMove(update);
    return () => {
      api.chart.timeScale().unsubscribeVisibleLogicalRangeChange(update);
      api.chart.unsubscribeCrosshairMove(update);
      if (frame !== null) cancelAnimationFrame(frame);
    };
  }, [api]);
  return (
    <svg
      className="ct-smart-overlay"
      width="100%"
      height={api.chart.panes()[0]?.getHeight() ?? 0}
    >
      <SmartMoneyOverlay
        output={output}
        active={active}
        width={api.chart.timeScale().width()}
        project={(time, price) => {
          const x = api.chart.timeScale().timeToCoordinate(timestamp(time)),
            y = api.series.priceToCoordinate(price);
          return x !== null && y !== null ? { x, y } : null;
        }}
      />
    </svg>
  );
}
