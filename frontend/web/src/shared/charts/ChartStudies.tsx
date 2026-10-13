import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowUp,
  ArrowDown,
  Eye,
  EyeOff,
  Maximize2,
  Minimize2,
  Settings2,
  X,
  Copy,
} from "lucide-react";
import {
  AreaSeries,
  LineSeries,
  HistogramSeries,
  CandlestickSeries,
  PriceScaleMode,
  type ISeriesApi,
  type IPaneApi,
  type IPriceLine,
  type Time,
} from "lightweight-charts";
import {
  calculateIndicatorBatch,
  type StudyDefinition,
} from "../market/indicatorBatch";
import {
  INDICATOR_CATALOG,
  defaultIndicatorPeriod,
  isClassicIndicatorId,
  type IndicatorId,
} from "../market/indicatorTypes";
import { readChartTheme } from "../market/chartTheme";
import { useTheme } from "../theme/ThemeContext";
import { timestamp, type Candle } from "./engine";
import type { DrawingRuntime } from "./drawings/DrawingEditor";
import type { ExternalPane } from "./derivatives";
import {
  EMPTY_PANEL_LAYOUT,
  type IndicatorInstance,
  type PanelLayout,
  type StudySettings,
} from "./studySettings";
import { StudyInspector } from "./StudyInspector";

type Series = ISeriesApi<"Line" | "Histogram" | "Candlestick" | "Area">;
type Data = {
  time: number;
  value?: number;
  open?: number;
  high?: number;
  low?: number;
  close?: number;
  color?: string;
};
type Component = {
  key: string;
  title: string;
  color: string;
  kind: "line" | "histogram" | "candles" | "area";
  data: Data[];
  width?: number;
  dash?: number;
  lineVisible?: boolean;
  pointMarkersVisible?: boolean;
};
type Group = {
  key: string;
  id?: IndicatorId;
  title: string;
  placement: "overlay" | "pane";
  components: Component[];
  percent?: boolean;
  note?: string;
  error?: string;
};
const EMPTY_SETTINGS: Record<string, StudySettings> = {};
type Props = {
  api: DrawingRuntime;
  candles: Candle[];
  interval: number;
  volume: boolean;
  indicators: IndicatorId[];
  periods?: Partial<Record<IndicatorId, number>>;
  instances: IndicatorInstance[];
  external: ExternalPane[];
  settings?: Record<string, StudySettings>;
  layout?: PanelLayout;
  editable?: boolean;
  identity: string;
  onSettings?: (key: string, value: StudySettings) => void;
  onRemove?: (key: string) => void;
  onDuplicate?: (key: string) => void;
  onLayout?: (layout: PanelLayout) => void;
};
export function ChartStudies({
  api,
  candles,
  interval,
  volume,
  indicators,
  periods,
  instances,
  external,
  settings = EMPTY_SETTINGS,
  layout = EMPTY_PANEL_LAYOUT,
  editable,
  onSettings,
  onRemove,
  onDuplicate,
  onLayout,
  identity,
}: Props) {
  const { theme } = useTheme();
  const series = useRef(new Map<string, Series>()),
    panes = useRef(new Map<string, IPaneApi<Time>>()),
    priceLines = useRef(new Map<string, IPriceLine[]>());
  const [groups, setGroups] = useState<Group[]>([]),
    [geometry, setGeometry] = useState<
      Record<string, { top: number; height: number }>
    >({}),
    [hoverTime, setHoverTime] = useState<number | null>(null),
    [inspector, setInspector] = useState<string | null>(null),
    [error, setError] = useState("");
  const live = useRef({ groups, layout, onLayout, settings });
  useEffect(() => {
    live.current = { groups, layout, onLayout, settings };
  });
  const measure = useCallback(() => {
    if (api.disposed) return;
    let top = 0;
    const next: Record<string, { top: number; height: number }> = {};
    for (const pane of api.chart.panes()) {
      const key =
        pane.paneIndex() === 0
          ? "price"
          : [...panes.current].find(([, p]) => p === pane)?.[0];
      if (key) next[key] = { top, height: pane.getHeight() };
      top += pane.getHeight() + 1;
    }
    setGeometry((previous) =>
      JSON.stringify(previous) === JSON.stringify(next) ? previous : next,
    );
  }, [api]);
  const definitions = useMemo<StudyDefinition[]>(
    () => [
      ...indicators.filter(isClassicIndicatorId).map((id) => ({
        key: id,
        id,
        period: periods?.[id] ?? defaultIndicatorPeriod(id),
        ...settings[id],
      })),
      ...instances.map((i) => ({ ...i, ...settings[i.key] })),
    ],
    [indicators, periods, instances, settings],
  );
  useEffect(() => {
    series.current.clear();
    panes.current.clear();
    priceLines.current.clear();
    const chart = api.chart;
    const crosshair: Parameters<typeof chart.subscribeCrosshairMove>[0] = (p) =>
      setHoverTime(p.time ? Number(p.time) : null);
    chart.subscribeCrosshairMove(crosshair);
    const ro = new ResizeObserver(measure);
    const root = chart.chartElement();
    ro.observe(root);
    let frame = 0;
    let dragging = false;
    const pointerDown = () => {
      dragging = true;
    };
    const pointerMove = () => {
      if (dragging) measure();
    };
    const resized = () => {
      dragging = false;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        measure();
        if (!live.current.onLayout || api.disposed) return;
        const heights = { ...live.current.layout.heights },
          scales = { ...live.current.layout.scales };
        for (const [key, pane] of panes.current) {
          heights[key] = Math.round(pane.getHeight());
          const priceScale = pane.priceScale("right"),
            auto = priceScale.options().autoScale;
          scales[key] = {
            ...scales[key],
            auto,
            range: auto
              ? undefined
              : (priceScale.getVisibleRange() ?? undefined),
          };
        }
        heights.price = Math.round(chart.panes()[0]?.getHeight() ?? 300);
        if (
          !live.current.layout.maximized &&
          (Object.entries(heights).some(
            ([key, v]) =>
              Math.abs(v - (live.current.layout.heights[key] ?? 0)) > 2,
          ) ||
            JSON.stringify(scales) !==
              JSON.stringify(live.current.layout.scales))
        )
          live.current.onLayout({ ...live.current.layout, heights, scales });
      });
    };
    root.addEventListener("pointerup", resized);
    root.addEventListener("pointerdown", pointerDown);
    root.addEventListener("pointermove", pointerMove);
    return () => {
      ro.disconnect();
      cancelAnimationFrame(frame);
      root.removeEventListener("pointerdown", pointerDown);
      root.removeEventListener("pointermove", pointerMove);
      root.removeEventListener("pointerup", resized);
      chart.unsubscribeCrosshairMove(crosshair);
    };
  }, [api, measure]);
  useEffect(() => {
    if (api.disposed) return;
    const abort = new AbortController();
    let disposed = false;
    const timer = setTimeout(async () => {
      try {
        const calculated = await calculateIndicatorBatch(
          candles as never,
          definitions,
          abort.signal,
          interval,
        );
        if (disposed || api.disposed) return;
        const t = readChartTheme(),
          mainFormat = api.series.options().priceFormat;
        const next: Group[] = [];
        if (volume)
          next.push({
            key: "volume",
            title: "Объём",
            placement: "pane",
            components: [
              {
                key: "volume",
                title: "Объём",
                color: t.up,
                kind: "histogram",
                data: candles.map((c) => ({
                  time: c.time,
                  value: c.volume,
                  color: (c.close >= c.open ? t.up : t.down) + "80",
                })),
              },
            ],
          });
        for (const { definition, indicator } of calculated)
          next.push({
            key: definition.key,
            id: definition.id,
            title:
              INDICATOR_CATALOG.find(
                (i) => i.id === definition.id,
              )?.shortLabel.replace(/\s+\d+$/, "") +
              (definition.period ? ` ${definition.period}` : ""),
            placement: indicator.placement,
            components: indicator.series.map((s) => ({
              key: s.key,
              title: s.title,
              color:
                definition.color ??
                (/^#[0-9a-f]{6}$/i.test(s.color)
                  ? s.color
                  : (INDICATOR_CATALOG.find((i) => i.id === definition.id)
                      ?.color ?? t.up)),
              kind:
                definition.id === "rsi" && definition.fill !== false
                  ? "area"
                  : s.kind,
              data: s.data as Data[],
              width: s.kind === "line" ? s.lineWidth : undefined,
              dash: s.kind === "line" ? s.lineStyle : undefined,
              lineVisible: s.kind === "line" ? s.lineVisible : undefined,
              pointMarkersVisible:
                s.kind === "line" ? s.pointMarkersVisible : undefined,
            })),
          });
        const times = new Set(candles.map((c) => c.time));
        for (const p of external)
          next.push({
            key: `external:${p.id}`,
            title: p.title,
            placement: "pane",
            percent: p.percent,
            note: p.note,
            error: p.error,
            components: (
              p.series ?? [
                {
                  key: "main",
                  title: p.title,
                  color: p.color,
                  kind: "line",
                  data: p.data,
                },
              ]
            ).map((s) => ({
              ...s,
              data: s.data.filter((v) => times.has(v.time)),
            })),
          });
        const active = new Set(
          next.flatMap((g) => g.components.map((c) => `${g.key}/${c.key}`)),
        );
        for (const [key, s] of series.current)
          if (!active.has(key)) {
            api.chart.removeSeries(s);
            series.current.delete(key);
            priceLines.current.delete(key);
          }
        for (const [key, pane] of panes.current)
          if (!next.some((g) => g.key === key && g.placement === "pane")) {
            api.chart.removePane(pane.paneIndex());
            panes.current.delete(key);
          }
        for (const g of next) {
          let pane = panes.current.get(g.key);
          if (g.placement === "pane" && !pane) {
            pane = api.chart.addPane(true);
            panes.current.set(g.key, pane);
            pane.setStretchFactor(1);
          }
          const style =
            settings[g.key] ?? definitions.find((d) => d.key === g.key) ?? {};
          for (const original of g.components) {
            const representation = !g.id ? style.representation : undefined;
            const c =
              representation &&
              (representation !== "candles" || original.kind === "candles")
                ? {
                    ...original,
                    kind: representation,
                    data:
                      representation === "candles"
                        ? original.data
                        : original.data.map((p) => ({
                            time: p.time,
                            value: p.value ?? p.close,
                            color: p.color,
                          })),
                  }
                : original;
            const key = `${g.key}/${c.key}`,
              componentStyle = style.components?.[c.key] ?? {};
            const color = componentStyle.color ?? style.color ?? c.color;
            let s = series.current.get(key);
            if (
              s &&
              s.seriesType().toLowerCase() !==
                c.kind.replace("candles", "candlestick")
            ) {
              api.chart.removeSeries(s);
              series.current.delete(key);
              priceLines.current.delete(key);
              s = undefined;
            }
            if (!s) {
              s = api.chart.addSeries(
                (c.kind === "candles"
                  ? CandlestickSeries
                  : c.kind === "histogram"
                    ? HistogramSeries
                    : c.kind === "area"
                      ? AreaSeries
                      : LineSeries) as typeof LineSeries,
                {},
                pane?.paneIndex() ?? 0,
              ) as Series;
              series.current.set(key, s);
            }
            s.applyOptions({
              // Names stay in the pane legend; scale labels show values only.
              title: "",
              visible: !style.hidden && !componentStyle.hidden,
              color,
              lineColor: color,
              topColor: color + "25",
              bottomColor: color + "00",
              upColor: t.up,
              downColor: t.down,
              wickUpColor: t.up,
              wickDownColor: t.down,
              borderVisible: false,
              lineWidth: componentStyle.width ?? style.width ?? c.width ?? 1,
              lineStyle: componentStyle.dash ?? style.dash ?? c.dash ?? 0,
              lineVisible: c.lineVisible ?? true,
              pointMarkersVisible: c.pointMarkersVisible ?? false,
              priceLineVisible: false,
              lastValueVisible: g.placement === "pane",
              priceFormat: g.percent
                ? {
                    type: "custom",
                    minMove: 0.0001,
                    formatter: (v: number) => v.toFixed(4) + "%",
                  }
                : g.placement === "pane"
                  ? { type: "volume" }
                  : mainFormat,
            } as never);
            s.setData(
              c.data.map((p) => ({
                ...p,
                time: timestamp(p.time),
                ...(c.kind === "histogram" &&
                (componentStyle.color || style.color)
                  ? { color }
                  : {}),
              })) as never,
            );
            for (const line of priceLines.current.get(key) ?? [])
              s.removePriceLine(line);
            priceLines.current.set(
              key,
              (c.kind === "line" || c.kind === "area") && g.components[0] === c
                ? (style.levels ?? (g.id === "rsi" ? [30, 70] : [])).map(
                    (price) =>
                      s!.createPriceLine({
                        price,
                        color,
                        lineWidth: 1,
                        lineStyle: 2,
                        axisLabelVisible: false,
                        lineVisible: !style.hidden,
                        title: "",
                      }),
                  )
                : [],
            );
          }
        }
        const surface = api.chart
          .chartElement()
          .closest("[data-chart-surface]");
        surface?.setAttribute(
          "data-indicator-points",
          JSON.stringify(
            Object.fromEntries(
              next
                .filter((g) => g.id)
                .map((g) => [
                  g.key,
                  g.components.reduce((n, c) => n + c.data.length, 0),
                ]),
            ),
          ),
        );
        setGroups(next);
        setError("");
      } catch (e) {
        if (!disposed && !api.disposed && (e as Error).name !== "AbortError")
          setError("Не удалось рассчитать индикаторы");
      }
    }, 120);
    return () => {
      disposed = true;
      abort.abort();
      clearTimeout(timer);
    };
  }, [
    api,
    candles,
    definitions,
    external,
    interval,
    volume,
    identity,
    settings,
    theme,
  ]);
  const paneKeys = groups
    .filter((g) => g.placement === "pane")
    .map((g) => g.key)
    .join("|");
  useEffect(() => {
    if (api.disposed) return;
    const keys = paneKeys ? paneKeys.split("|") : [];
    const order = [
      ...layout.order.filter((k) => keys.includes(k)),
      ...keys.filter((k) => !layout.order.includes(k)),
    ];
    const main = api.chart.panes()[0];
    main?.setStretchFactor(
      layout.maximized
        ? layout.maximized === "price"
          ? 10000
          : 0.01
        : (layout.heights.price ?? 400),
    );
    for (let i = 0; i < order.length; i++) {
      const key = order[i],
        pane = panes.current.get(key);
      if (!pane) continue;
      pane.moveTo(i + 1);
      pane.setStretchFactor(
        layout.maximized
          ? layout.maximized === key
            ? 10000
            : 0.01
          : settings[key]?.hidden
            ? 26
            : (layout.heights[key] ?? 100),
      );
      const scale = layout.scales[key];
      pane.priceScale("right").applyOptions({
        autoScale: scale?.auto !== false,
        invertScale: scale?.inverted === true,
        mode: scale?.log ? PriceScaleMode.Logarithmic : PriceScaleMode.Normal,
      });
      if (scale?.auto === false && scale.range)
        pane.priceScale("right").setVisibleRange(scale.range);
    }
    const frame = requestAnimationFrame(measure);
    return () => cancelAnimationFrame(frame);
  }, [api, paneKeys, layout, measure, settings]);
  const points = useMemo(
    () =>
      new Map(
        groups.flatMap((g) =>
          g.components.map(
            (c) =>
              [
                `${g.key}/${c.key}`,
                new Map(c.data.map((p) => [p.time, p])),
              ] as const,
          ),
        ),
      ),
    [groups],
  );
  const time = hoverTime ?? candles.at(-1)?.time;
  const changeOrder = (key: string, direction: number) => {
    const current = api.chart
      .panes()
      .slice(1)
      .map((p) => [...panes.current].find(([, pane]) => p === pane)?.[0])
      .filter((k): k is string => !!k);
    const index = current.indexOf(key),
      target = index + direction;
    if (target < 0 || target >= current.length) return;
    [current[index], current[target]] = [current[target], current[index]];
    onLayout?.({ ...layout, order: current });
  };
  const inspecting = groups.find((g) => g.key === inspector);
  return (
    <>
      {error && (
        <span className="ct-study-error" role="alert">
          {error}
        </span>
      )}
      {groups.map((g) => {
        if (
          layout.maximized &&
          layout.maximized !== (g.placement === "overlay" ? "price" : g.key)
        )
          return null;
        const geometryKey = g.placement === "overlay" ? "price" : g.key,
          bounds = geometry[geometryKey];
        const style =
          settings[g.key] ?? definitions.find((d) => d.key === g.key) ?? {};
        const overlayIndex = groups
          .filter((i) => i.placement === "overlay")
          .findIndex((i) => i.key === g.key);
        return (
          <div
            className={`ct-pane-legend ${editable ? "ct-pane-editable" : ""}`}
            key={g.key}
            style={{
              top:
                (bounds?.top ?? 0) +
                (g.placement === "overlay" ? 28 + overlayIndex * 21 : 4),
            }}
            data-pane-key={g.key}
          >
            <span
              className="ct-pane-title"
              title={g.note ?? g.title}
              style={{ opacity: style.hidden ? 0.5 : 1 }}
            >
              {g.title}
            </span>
            {!style.hidden && (
              <span className="ct-pane-values">
                {g.components.map((c) => {
                  const p = time
                    ? points.get(`${g.key}/${c.key}`)?.get(time)
                    : undefined;
                  if (style.components?.[c.key]?.hidden) return null;
                  const value = p?.value ?? p?.close;
                  return (
                    <span
                      key={c.key}
                      title={`${c.title}: ${value === undefined ? "—" : value.toLocaleString("ru-RU", { maximumFractionDigits: 8 })}${g.percent ? "%" : ""}`}
                      style={{
                        color:
                          style.components?.[c.key]?.color ??
                          style.color ??
                          c.color,
                      }}
                    >
                      {value === undefined
                        ? "—"
                        : value.toLocaleString(
                            g.placement === "pane" && Math.abs(value) >= 1000
                              ? "en-US"
                              : "ru-RU",
                            {
                              maximumFractionDigits: g.percent ? 4 : 2,
                              notation:
                                g.placement === "pane" &&
                                Math.abs(value) >= 1000
                                  ? "compact"
                                  : "standard",
                            },
                          )}
                      {g.percent && value !== undefined ? "%" : ""}
                    </span>
                  );
                })}
              </span>
            )}
            {g.error && (
              <span className="ct-pane-error" title={g.error}>
                Источник недоступен
              </span>
            )}
            {editable && (
              <span className="ct-pane-actions">
                <button
                  aria-label={`Видимость ${g.key}`}
                  aria-pressed={!style.hidden}
                  onClick={() =>
                    onSettings?.(g.key, { ...style, hidden: !style.hidden })
                  }
                >
                  {style.hidden ? <EyeOff size={12} /> : <Eye size={12} />}
                </button>
                <button
                  aria-label={`Настроить ${g.key}`}
                  onClick={() => setInspector(g.key)}
                >
                  <Settings2 size={12} />
                </button>
                {g.placement === "pane" && (
                  <>
                    <button
                      aria-label={`Поднять ${g.key}`}
                      onClick={() => changeOrder(g.key, -1)}
                    >
                      <ArrowUp size={12} />
                    </button>
                    <button
                      aria-label={`Опустить ${g.key}`}
                      onClick={() => changeOrder(g.key, 1)}
                    >
                      <ArrowDown size={12} />
                    </button>
                    <button
                      aria-label={`Развернуть ${g.key}`}
                      aria-pressed={layout.maximized === g.key}
                      onClick={() =>
                        onLayout?.({
                          ...layout,
                          maximized:
                            layout.maximized === g.key ? undefined : g.key,
                        })
                      }
                    >
                      {layout.maximized === g.key ? (
                        <Minimize2 size={12} />
                      ) : (
                        <Maximize2 size={12} />
                      )}
                    </button>
                  </>
                )}
                {g.id && (
                  <button
                    aria-label={`Дублировать ${g.key}`}
                    onClick={() => onDuplicate?.(g.key)}
                  >
                    <Copy size={12} />
                  </button>
                )}
                <button
                  aria-label={`Удалить ${g.key}`}
                  onClick={() => onRemove?.(g.key)}
                >
                  <X size={12} />
                </button>
              </span>
            )}
          </div>
        );
      })}
      {inspecting && (
        <StudyInspector
          key={inspecting.key}
          studyKey={inspecting.key}
          id={inspecting.id}
          title={inspecting.title}
          components={inspecting.components}
          value={
            settings[inspecting.key] ??
            definitions.find((d) => d.key === inspecting.key) ??
            {}
          }
          onApply={(v) => onSettings?.(inspecting.key, v)}
          onClose={() => setInspector(null)}
          onDuplicate={
            inspecting.id
              ? () => {
                  onDuplicate?.(inspecting.key);
                  setInspector(null);
                }
              : undefined
          }
        />
      )}
    </>
  );
}
