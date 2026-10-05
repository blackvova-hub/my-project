import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  CandlestickSeries,
  ColorType,
  CrosshairMode,
  HistogramSeries,
  LineSeries,
  createChart,
  type ISeriesApi,
  type IPaneApi,
  type Time,
  type UTCTimestamp,
} from "lightweight-charts";
import { calculateIndicator, calculateIndicatorTail } from "../../../shared/market/indicatorEngine";
import { INDICATOR_CATALOG, isClassicIndicatorId, isSmartMoneyIndicatorId, type IndicatorId, type ClassicIndicatorId, type SmartMoneyIndicatorId } from "../../../shared/market/indicatorTypes";
import { IndicatorMenu } from "../../../shared/market/IndicatorMenu";
import { calculateSmartMoney } from "../../../shared/market/smartMoneyEngine";
import { inferPerpPriceFormat } from "../../../shared/market/perpMarketData";
import type { PerpCandle } from "../../../shared/market/perpMarketData";
import {
  DrawingCanvas,
  DrawingToolbar,
  type DrawingAPI,
  type Tool,
} from "./Drawings";
import { useDrawings } from "./useDrawings";
import { price, utc, type Candle, type Position } from "./model";
import { Icon } from "./Icons";
import { useTheme } from "../../../shared/theme/ThemeContext";
import { readChartTheme } from "../../../shared/market/chartTheme";

type IndicatorSeries = ISeriesApi<"Line"> | ISeriesApi<"Histogram">;
type IndicatorRuntime = { pane: IPaneApi<Time> | null; series: Map<string, IndicatorSeries> };
const rightOffset = (width: number) =>
  Math.max(4, Math.min(24, Math.round(width / 60)));
const colorVolume = (c: Candle) => ({
  time: c.time as UTCTimestamp,
  value: c.volume,
  color: c.close >= c.open ? "#34d39955" : "#fb718555",
});

export function ReplayChart({
  candles,
  selecting,
  onSelect,
  title,
  interval,
  returnToken,
  session,
  position,
  onArchive,
  children,
}: {
  candles: Candle[];
  selecting: boolean;
  onSelect: (time: number) => void;
  title: string;
  interval: number;
  returnToken: number;
  session: number;
  position: Position | null;
  onArchive: () => void;
  children: ReactNode;
}) {
  const { theme } = useTheme();
  const root = useRef<HTMLDivElement>(null),
    apiRef = useRef<DrawingAPI | null>(null),
    volumeRef = useRef<ISeriesApi<"Histogram"> | null>(null);
  const [api, setApi] = useState<DrawingAPI | null>(null),
    [hover, setHover] = useState<Candle | null>(null),
    [volume, setVolume] = useState(true);
  const [indicators, setIndicators] = useState<IndicatorId[]>(["ema"]),
    [indicatorError, setIndicatorError] = useState("");
  const [toolState, setToolState] = useState<{ session: number; value: Tool }>({
      session,
      value: "cursor",
    });
  const tool = toolState.session === session ? toolState.value : "cursor";
  const setTool = useCallback(
    (value: Tool) => setToolState({ session, value }),
    [session],
  );
  const drawings = useDrawings(session);
  const [selectedDrawingState, setSelectedDrawingState] = useState<{ session: number; index: number | null }>({ session, index: null });
  const selectedIndex = selectedDrawingState.session === session ? selectedDrawingState.index : null;
  const selectedDrawing = selectedIndex !== null ? drawings.drawings[selectedIndex] ?? null : null;
  const [drawingsVisible, setDrawingsVisible] = useState(true);
  const [magnetEnabled, setMagnetEnabled] = useState(true);
  const selectDrawing = useCallback((index: number | null) => setSelectedDrawingState({ session, index }), [session]);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (target?.closest("input, textarea, [contenteditable='true'], dialog")) return;
      if (event.key === "Delete" && selectedIndex !== null && selectedDrawing) {
        drawings.remove(selectedIndex);
        selectDrawing(null);
      } else if (event.key.toLowerCase() === "z" && (event.ctrlKey || event.metaKey) && drawings.drawings.length) {
        event.preventDefault();
        drawings.undo();
        selectDrawing(null);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [drawings, selectedIndex, selectedDrawing, selectDrawing]);
  const indicatorSeries = useRef(new Map<ClassicIndicatorId, IndicatorRuntime>());
  const indicatorPrefix = useRef<Candle[]>([]);
  const stateRef = useRef({ candles, selecting, onSelect });
  const smartMoneyActive = useMemo(() => new Set<SmartMoneyIndicatorId>(indicators.filter(isSmartMoneyIndicatorId)), [indicators]);
  const smartMoneyOutput = useMemo(() => smartMoneyActive.size ? calculateSmartMoney(candles as PerpCandle[]) : { markers: [], levels: [], zones: [] }, [candles, smartMoneyActive]);
  const follow = useRef(true),
    manualView = useRef(false),
    previousSession = useRef(session),
    previous = useRef<Candle[]>([]),
    selectionLine = useRef<HTMLDivElement>(null),
    animation = useRef<number | null>(null);
  useLayoutEffect(() => {
    stateRef.current = { candles, selecting, onSelect };
    if (!selecting && selectionLine.current)
      selectionLine.current.style.display = "none";
  });
  // Chart lifecycle is independent of playback and React renders.
  useEffect(() => {
    if (!root.current) return;
    const colors = readChartTheme();
    const chartRoot = root.current;
    const runtimes = indicatorSeries.current;
    const chart = createChart(root.current, {
      autoSize: true,
      layout: {
        background: { type: ColorType.Solid, color: colors.surface },
        textColor: colors.foreground,
        fontFamily: "system-ui, sans-serif",
        fontSize: 12,
        attributionLogo: true,
      },
      grid: {
        vertLines: { color: colors.grid },
        horzLines: { color: colors.grid },
      },
      crosshair: {
        mode: CrosshairMode.Normal,
        vertLine: { color: colors.crosshair, labelBackgroundColor: colors.label },
        horzLine: { color: colors.crosshair, labelBackgroundColor: colors.label },
      },
      rightPriceScale: {
        borderColor: colors.grid,
        scaleMargins: { top: 0.13, bottom: 0.23 },
      },
      timeScale: {
        borderColor: colors.grid,
        timeVisible: true,
        secondsVisible: false,
        rightOffset: 24,
        barSpacing: 8,
        shiftVisibleRangeOnNewBar: false,
      },
      localization: {
        locale: "ru-RU",
        timeFormatter: (t: UTCTimestamp) => utc(Number(t)) + " UTC",
      },
      handleScroll: {
        mouseWheel: true,
        pressedMouseMove: true,
        horzTouchDrag: true,
        vertTouchDrag: false,
      },
    });
    const series = chart.addSeries(CandlestickSeries, {
      upColor: "#34d399",
      downColor: "#fb7185",
      borderVisible: false,
      wickUpColor: "#34d399",
      wickDownColor: "#fb7185",
    });
    const volumes = chart.addSeries(HistogramSeries, {
      priceFormat: { type: "volume" },
      priceScaleId: "volume",
      lastValueVisible: false,
      priceLineVisible: false,
    });
    volumes
      .priceScale()
      .applyOptions({ scaleMargins: { top: 0.83, bottom: 0.02 } });
    const current = { chart, series };
    apiRef.current = current;
    volumeRef.current = volumes;
    setApi(current);
    chart.subscribeCrosshairMove((param) => {
      const visible = stateRef.current.candles;
      const value = param.seriesData.get(series);
      setHover(
        value && "open" in value
          ? (visible.find((c) => c.time === Number(value.time)) ?? null)
          : null,
      );
      if (selectionLine.current) {
        selectionLine.current.style.display =
          stateRef.current.selecting && param.point && param.time
            ? "block"
            : "none";
        if (param.point)
          selectionLine.current.style.left = `${param.point.x}px`;
      }
    });
    chart.subscribeClick((param) => {
      if (stateRef.current.selecting && param.time)
        stateRef.current.onSelect(Number(param.time));
    });
    let pointer: { id: number; x: number; y: number; moved: boolean; wasFollowing: boolean } | null = null;
    const pointerDown = (event: PointerEvent) => {
      if (event.button !== 0 || stateRef.current.selecting) return;
      pointer = { id: event.pointerId, x: event.clientX, y: event.clientY, moved: false, wasFollowing: follow.current };
      follow.current = false;
    };
    const pointerMove = (event: PointerEvent) => {
      if (!pointer || pointer.id !== event.pointerId || pointer.moved) return;
      if (Math.hypot(event.clientX - pointer.x, event.clientY - pointer.y) < 4) return;
      pointer.moved = true;
      manualView.current = true;
      follow.current = false;
    };
    const pointerEnd = (event: PointerEvent) => {
      if (!pointer || pointer.id !== event.pointerId) return;
      const wasFollowing = pointer.wasFollowing;
      const moved = pointer.moved;
      pointer = null;
      if (!moved && !manualView.current) {
        const range = chart.timeScale().getVisibleLogicalRange();
        follow.current = wasFollowing && !!range && range.to >= stateRef.current.candles.length - 2;
      }
    };
    chartRoot.addEventListener("pointerdown", pointerDown, true);
    window.addEventListener("pointermove", pointerMove, true);
    window.addEventListener("pointerup", pointerEnd, true);
    window.addEventListener("pointercancel", pointerEnd, true);
    chart.timeScale().subscribeVisibleLogicalRangeChange((range) => {
      if (range) {
        chartRoot.setAttribute("data-visible-span", (range.to - range.from).toFixed(4));
        chartRoot.setAttribute("data-visible-right", range.to.toFixed(4));
        follow.current = !manualView.current && !pointer && range.to >= stateRef.current.candles.length - 2;
      }
    });
    chart.timeScale().subscribeSizeChange((width) => {
      if (!manualView.current && follow.current)
        chart.timeScale().scrollToPosition(rightOffset(width), false);
    });
    return () => {
      chartRoot.removeEventListener("pointerdown", pointerDown, true);
      window.removeEventListener("pointermove", pointerMove, true);
      window.removeEventListener("pointerup", pointerEnd, true);
      window.removeEventListener("pointercancel", pointerEnd, true);
      if (animation.current !== null) cancelAnimationFrame(animation.current);
      runtimes.clear();
      chart.remove();
      apiRef.current = null;
      volumeRef.current = null;
      previous.current = [];
      manualView.current = false;
    };
  }, []);

  useEffect(() => {
    if (!api) return;
    const colors = readChartTheme();
    api.chart.applyOptions({
      layout: { background: { type: ColorType.Solid, color: colors.surface }, textColor: colors.foreground },
      grid: { vertLines: { color: colors.grid }, horzLines: { color: colors.grid } },
      crosshair: {
        vertLine: { color: colors.crosshair, labelBackgroundColor: colors.label },
        horzLine: { color: colors.crosshair, labelBackgroundColor: colors.label },
      },
      rightPriceScale: { borderColor: colors.grid },
      timeScale: { borderColor: colors.grid },
    });
  }, [api, theme]);

  useLayoutEffect(() => {
    if (!api || !volumeRef.current || !candles.length) return;
    const { chart, series } = api,
      old = previous.current,
      last = candles.at(-1)!;
    const restarted = previousSession.current !== session;
    previousSession.current = session;
    const forwardExtension = old.length > 0 && candles.length > old.length && candles[0] === old[0] && candles[old.length - 1] === old.at(-1);
    const oneStep = forwardExtension && candles.length === old.length + 1;
    const oneBack = candles.length === old.length - 1 && candles[0] === old[0] && candles.at(-1) === old[candles.length - 1];
    const sameVisible = old.length === candles.length && candles[0] === old[0] && candles.at(-1) === old.at(-1);
    const preserveView = !restarted && (forwardExtension || oneBack || sameVisible);
    if (!preserveView) manualView.current = false;
    const shouldFollow = follow.current && !manualView.current;
    const visibleRange = chart.timeScale().getVisibleLogicalRange();
    if (animation.current !== null) cancelAnimationFrame(animation.current);
    if (oneStep) {
      series.update(old.at(-1)! as PerpCandle);
      series.update(last as PerpCandle);
      volumeRef.current.update(colorVolume(last));
      if (!matchMedia("(prefers-reduced-motion: reduce)").matches) {
        const started = performance.now(),
          up = last.close >= last.open;
        const reveal = (now: number) => {
          const alpha = Math.round(
            (0.25 + Math.min(1, (now - started) / 140) * 0.75) * 255,
          )
            .toString(16)
            .padStart(2, "0");
          const color = (up ? "#34d399" : "#fb7185") + alpha;
          series.update({
            ...last,
            time: last.time as UTCTimestamp,
            color,
            wickColor: color,
          });
          if (now - started < 140)
            animation.current = requestAnimationFrame(reveal);
          else {
            series.update(last as PerpCandle);
            animation.current = null;
          }
        };
        animation.current = requestAnimationFrame(reveal);
      }
    } else {
      series.setData(candles as PerpCandle[]);
      volumeRef.current.setData(candles.map(colorVolume));
      series.applyOptions({
        priceFormat: inferPerpPriceFormat(candles as PerpCandle[]),
      });
      // Truncation clears OHLC from a previously hovered future bar before paint.
    }
    if (preserveView) {
      if (visibleRange) {
        if (shouldFollow && !sameVisible) {
          // Move both ends together so playback cannot change the zoom level.
          const to = candles.length - 1 + rightOffset(chart.timeScale().width());
          chart.timeScale().setVisibleLogicalRange({ from: to - (visibleRange.to - visibleRange.from), to });
        } else {
          // setData during archive extension must not move a manually panned view.
          chart.timeScale().setVisibleLogicalRange(visibleRange);
        }
      }
      follow.current = shouldFollow;
    } else {
      const span = Math.min(180, Math.max(40, chart.timeScale().width() / 8));
      const to = candles.length - 1 + rightOffset(chart.timeScale().width());
      chart.timeScale().setVisibleLogicalRange({ from: to - span, to });
      follow.current = true;
    }
    previous.current = candles;
  }, [api, candles, session]);

  useEffect(() => {
    if (!api) return;
    volumeRef.current?.applyOptions({ visible: volume });
    api.series.priceScale().applyOptions({
      scaleMargins: { top: 0.13, bottom: volume ? 0.23 : 0.07 },
    });
  }, [api, volume]);

  useLayoutEffect(() => {
    if (!api) return;
    let cancelled = false;
    const runtimes = indicatorSeries.current;
    const oldPrefix = indicatorPrefix.current;
    const oneStep = candles.length === oldPrefix.length + 1 && candles[0] === oldPrefix[0];
    indicatorPrefix.current = candles;
    const classic = indicators.filter(isClassicIndicatorId);
    for (const [id, runtime] of runtimes) {
      if (!classic.includes(id)) {
        const paneIndex = runtime.pane?.paneIndex() ?? -1;
        for (const series of runtime.series.values()) api.chart.removeSeries(series);
        if (paneIndex > 0 && paneIndex < api.chart.panes().length) api.chart.removePane(paneIndex);
        runtimes.delete(id);
      }
    }
    Promise.all(
      classic.map((id) => oneStep && runtimes.has(id)
        ? calculateIndicatorTail(candles as PerpCandle[], id)
        : calculateIndicator(candles as PerpCandle[], id)),
    )
      .then((outputs) => {
        if (cancelled) return;
        setIndicatorError("");
        for (const output of outputs) {
          const id = output.id as ClassicIndicatorId;
          let runtime = runtimes.get(id);
          if (!runtime) {
            const pane = output.placement === "pane" ? api.chart.addPane(true) : null;
            pane?.setStretchFactor(1);
            api.chart.panes()[0]?.setStretchFactor(4);
            runtime = { pane, series: new Map() };
            runtimes.set(id, runtime);
          }
          for (const line of output.series) {
            let series = runtime.series.get(line.key);
            if (!series) {
              const paneIndex = runtime.pane?.paneIndex();
              series = line.kind === "line"
                ? api.chart.addSeries(LineSeries, {
                    color: line.color, lineWidth: line.lineWidth ?? 1,
                    lineStyle: line.lineStyle ?? 0, title: "", priceLineVisible: false,
                    lastValueVisible: true, crosshairMarkerVisible: false,
                  }, paneIndex)
                : api.chart.addSeries(HistogramSeries, {
                    color: line.color, title: "", priceLineVisible: false, lastValueVisible: true,
                  }, paneIndex);
              runtime.series.set(line.key, series);
            }
            if (oneStep && oldPrefix.length && line.data.length === 1) {
              if (line.kind === "line") (series as ISeriesApi<"Line">).update(line.data[0]);
              else (series as ISeriesApi<"Histogram">).update(line.data[0]);
            } else {
              if (line.kind === "line") (series as ISeriesApi<"Line">).setData(line.data);
              else (series as ISeriesApi<"Histogram">).setData(line.data);
            }
          }
        }
      })
      .catch(() => {
        if (!cancelled)
          setIndicatorError(
            "Не удалось рассчитать индикатор. Выключите его и попробуйте снова.",
          );
      });
    return () => {
      cancelled = true;
    };
  }, [api, candles, indicators]);

  useEffect(() => {
    if (!api || !returnToken) return;
    manualView.current = false;
    const scale = api.chart.timeScale();
    const visibleRange = scale.getVisibleLogicalRange();
    const span = visibleRange ? visibleRange.to - visibleRange.from : Math.min(180, Math.max(40, scale.width() / 8));
    const to = stateRef.current.candles.length - 1 + rightOffset(scale.width());
    scale.setVisibleLogicalRange({ from: to - span, to });
    follow.current = true;
  }, [api, returnToken]);
  useEffect(() => {
    if (!api || !position) return;
    const line = api.series.createPriceLine({
      price: position.entry,
      color: position.side === "long" ? "#34d399" : "#fb7185",
      lineWidth: 2,
      lineStyle: 2,
      title: position.side.toUpperCase(),
      axisLabelVisible: true,
    });
    return () => api.series.removePriceLine(line);
  }, [api, position]);

  const done = useCallback(() => setTool("cursor"), [setTool]);
  const selectedHover =
    hover &&
    hover.time <= candles.at(-1)!.time &&
    candles.some((c) => c === hover)
      ? hover
      : candles.at(-1)!;
  const toggleIndicator = (id: IndicatorId) =>
    setIndicators((active) =>
      active.includes(id)
        ? active.filter((i) => i !== id)
        : [...active, id],
    );
  return (
    <div className="rp-chart-region">
      <DrawingToolbar
        tool={selecting ? "cursor" : tool}
        onTool={setTool}
        onUndo={drawings.undo}
        onClear={() => { drawings.clear(); selectDrawing(null); }}
        onDelete={() => { if (selectedIndex !== null) drawings.remove(selectedIndex); selectDrawing(null); }}
        onLock={() => { if (selectedIndex !== null) drawings.update(selectedIndex, (drawing) => ({ ...drawing, locked: !drawing.locked })); }}
        onVisibility={() => { setDrawingsVisible((visible) => !visible); selectDrawing(null); }}
        onMagnet={() => setMagnetEnabled((enabled) => !enabled)}
        selected={selectedDrawing}
        visible={drawingsVisible}
        magnet={magnetEnabled}
        count={drawings.drawings.length}
        disabled={selecting}
      />
      <div className="rp-chart-main">
        <div className="rp-chart-toolbar">
          <button
            className="rp-instrument"
            onClick={onArchive}
            title="Выбрать инструмент и интервал"
          >
            <strong>{title.split(" · ")[0]}</strong>
            <Icon name="chevron" />
            <span>{title.split(" · ").slice(1).join(" · ")}</span>
          </button>
          <IndicatorMenu active={indicators} onToggle={toggleIndicator} onClear={() => setIndicators([])} volumeActive={volume} onVolumeToggle={() => setVolume((value) => !value)} />
          {children}
          <button
            className="rp-icon rp-fullscreen"
            aria-label="Полный экран"
            title="Полный экран"
            onClick={() => {
              if (document.fullscreenElement) void document.exitFullscreen();
              else
                void root.current
                  ?.closest(".replay-layout")
                  ?.requestFullscreen();
            }}
          >
            <Icon name="fullscreen" />
          </button>
        </div>
        <div className="rp-chart-wrap">
          <div className="rp-ohlc" data-testid="ohlc">
            <span className="rp-legend-symbol">{title}</span>
            <span>
              O <b>{price(selectedHover.open)}</b>
            </span>
            <span>
              H <b>{price(selectedHover.high)}</b>
            </span>
            <span>
              L <b>{price(selectedHover.low)}</b>
            </span>
            <span>
              C <b>{price(selectedHover.close)}</b>
            </span>
            <span
              className={
                selectedHover.close >= selectedHover.open
                  ? "rp-positive"
                  : "rp-negative"
              }
            >
              {((selectedHover.close / selectedHover.open - 1) * 100).toFixed(
                2,
              )}
              %
            </span>
          </div>
          <div className="rp-indicator-legend">
            {indicators.map((id) => (
              <span
                key={id}
                style={{ color: id === "ema" ? "#38bdf8" : undefined }}
              >
                {INDICATOR_CATALOG.find((i) => i.id === id)?.shortLabel}
              </span>
            ))}
          </div>
          <div
            ref={root}
            className="rp-chart"
            data-testid="replay-chart"
            data-visible-count={candles.length}
            data-last-time={candles.at(-1)?.time}
            data-drawing-count={drawings.drawings.length}
          />
          <div ref={selectionLine} className="rp-selection-line">
            <span>Начать здесь</span>
          </div>
          {api ? (
            <DrawingCanvas
              key={session}
              api={api}
              tool={selecting ? "cursor" : tool}
              drawings={drawings.drawings}
              onAdd={drawings.add}
              onDone={done}
              lastTime={candles.at(-1)!.time}
              interval={interval}
              candles={candles}
              selectedIndex={selectedIndex}
              onSelect={selectDrawing}
              onUpdate={drawings.update}
              visible={drawingsVisible}
              magnet={magnetEnabled}
              smartMoneyOutput={smartMoneyOutput}
              smartMoneyActive={smartMoneyActive}
            />
          ) : null}
        </div>
        {tool !== "cursor" && !selecting ? (
          <div className="rp-drawing-hint">
            {tool === "horizontal" || tool === "vertical" || tool === "text"
              ? "Нажмите на график, чтобы поставить объект"
              : "Нажмите и перетащите, чтобы нарисовать"}
          </div>
        ) : null}
        {indicatorError ? (
          <p role="alert" className="rp-error">
            {indicatorError}
          </p>
        ) : null}
      </div>
    </div>
  );
}
