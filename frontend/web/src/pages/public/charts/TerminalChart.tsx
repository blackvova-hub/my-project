import { memo, useEffect, useRef, useState } from "react";
import { X, Settings2, Maximize2 } from "lucide-react";
import { LineSeries, type ISeriesApi } from "lightweight-charts";
import { ChartSurface } from "../../../shared/charts/ChartSurface";
import { useMarketData } from "../../../shared/charts/useMarketData";
import { useDerivativePanels } from "../../../shared/charts/derivatives";
import { useChartWorkspace } from "../../../shared/charts/useChartWorkspace";
import { instrumentKey } from "../../../shared/charts/marketData";
import { comparePrices } from "../../../shared/charts/comparison";
import {
  barCountdown,
  logicalViewport,
  temporalViewport,
  type TimeViewport,
} from "../../../shared/charts/navigation";
import { timestamp, type Candle } from "../../../shared/charts/engine";
import type { DrawingRuntime } from "../../../shared/charts/drawings/DrawingEditor";
import type { ChartWindow, Comparison } from "./workspace";

export type TerminalRuntime = DrawingRuntime & {
  candles: Candle[];
  seconds: number;
  loadAround: (time: number) => Promise<Candle[]>;
  retry: () => void;
};
type Props = {
  window: ChartWindow;
  active: boolean;
  visible: boolean;
  index: number;
  onChange: (index: number, change: Partial<ChartWindow>) => void;
  onRuntime: (index: number, runtime: TerminalRuntime | null) => void;
  onCrosshair: (index: number, time: number | null) => void;
  onViewport: (index: number, range: TimeViewport) => void;
  onMaximize: (index: number) => void;
  onSave: () => void;
};
export const TerminalChart = memo(function TerminalChart({
  window: w,
  active,
  visible,
  index,
  onChange,
  onRuntime,
  onCrosshair,
  onViewport,
  onMaximize,
  onSave,
}: Props) {
  const data = useMarketData(w, w.interval, visible),
    external = useDerivativePanels(w, w.interval, w.derivatives, visible);
  const identity = `${instrumentKey(w)}:${w.interval}`;
  const drawingKey = `${w.id}:${instrumentKey(w)}`;
  const drawings = useChartWorkspace(drawingKey, `${w.id}:${identity}`);
  const [api, setApi] = useState<DrawingRuntime | null>(null);
  const latest = useRef({
    w,
    data,
    onChange,
    onRuntime,
    onCrosshair,
    onViewport,
  });
  useEffect(() => {
    latest.current = { w, data, onChange, onRuntime, onCrosshair, onViewport };
  });
  const restored = useRef<{ identity: string; api: DrawingRuntime } | null>(
    null,
  );
  useEffect(() => {
    if (!api || api.disposed) return;
    onRuntime(
      index,
      Object.assign(api, {
        candles: data.candles,
        seconds: data.seconds,
        loadAround: data.loadAround,
        retry: data.retry,
      }),
    );
  }, [
    api,
    index,
    onRuntime,
    data.candles,
    data.seconds,
    data.loadAround,
    data.retry,
  ]);
  useEffect(() => () => latest.current.onRuntime(index, null), [index]);
  useEffect(() => {
    if (
      !api ||
      api.disposed ||
      !data.candles.length ||
      (restored.current?.identity === identity && restored.current.api === api)
    )
      return;
    const frame = requestAnimationFrame(() => {
      if (api.disposed) return;
      restored.current = { identity, api };
      if (w.view) {
        const range = logicalViewport(data.candles, w.view, data.seconds);
        if (range) api.chart.timeScale().setVisibleLogicalRange(range);
      }
      if (!w.autoScale && w.priceView)
        api.chart.priceScale("right").setVisibleRange(w.priceView);
    });
    return () => cancelAnimationFrame(frame);
  }, [
    api,
    data.candles,
    data.seconds,
    identity,
    w.view,
    w.autoScale,
    w.priceView,
  ]);
  useEffect(() => {
    if (!api || api.disposed) return;
    let timer: ReturnType<typeof setTimeout>;
    const scale = api.chart.timeScale();
    const range: Parameters<
      typeof scale.subscribeVisibleLogicalRangeChange
    >[0] = (r) => {
      if (!r || !latest.current.data.candles.length) return;
      const value = temporalViewport(
        latest.current.data.candles,
        r,
        latest.current.data.seconds,
      );
      if (value) latest.current.onViewport(index, value);
    };
    const cross: Parameters<typeof api.chart.subscribeCrosshairMove>[0] = (p) =>
      latest.current.onCrosshair(index, p.time ? Number(p.time) : null);
    const root = api.chart.chartElement();
    const capture = () => {
      clearTimeout(timer);
      timer = setTimeout(() => {
        if (api.disposed) return;
        const r = scale.getVisibleLogicalRange();
        if (!r) return;
        const value = temporalViewport(
          latest.current.data.candles,
          r,
          latest.current.data.seconds,
        );
        const priceScale = api.chart.priceScale("right"),
          autoScale = priceScale.options().autoScale;
        if (value)
          latest.current.onChange(index, {
            view: value,
            // Negative scroll positions are historical bars. Once the user
            // pauses following, only the explicit live action resumes it.
            follow:
              latest.current.w.follow &&
              scale.scrollPosition() >= -2 &&
              scale.scrollPosition() < 14,
            autoScale,
            priceView: autoScale
              ? undefined
              : (priceScale.getVisibleRange() ?? undefined),
          });
      }, 180);
    };
    const wheel = () => {
      capture();
      if (latest.current.w.follow)
        latest.current.onChange(index, { follow: false });
    };
    scale.subscribeVisibleLogicalRangeChange(range);
    api.chart.subscribeCrosshairMove(cross);
    root.addEventListener("pointerup", capture);
    root.addEventListener("wheel", wheel, { passive: true });
    return () => {
      clearTimeout(timer);
      scale.unsubscribeVisibleLogicalRangeChange(range);
      api.chart.unsubscribeCrosshairMove(cross);
      root.removeEventListener("pointerup", capture);
      root.removeEventListener("wheel", wheel);
    };
  }, [api, index]);
  const patch = (change: Partial<ChartWindow>) => onChange(index, change);
  const remove = (key: string) => {
    if (key === "volume") patch({ volume: false });
    else if (key.startsWith("external:"))
      patch({ derivatives: w.derivatives.filter((id) => id !== key.slice(9)) });
    else
      patch({
        indicators: w.indicators.filter((id) => id !== key),
        instances: w.instances.filter((i) => i.key !== key),
      });
  };
  const duplicate = (key: string) => {
    const instance = w.instances.find((i) => i.key === key),
      id = instance?.id ?? w.indicators.find((id) => id === key);
    if (!id || w.instances.length >= 16) return;
    const isClassic = ![
      "bos",
      "choch",
      "mss",
      "order_blocks",
      "fvg",
      "liquidity",
      "equal_high_low",
      "liquidity_sweeps",
      "swing_points",
    ].includes(id);
    if (!isClassic) return;
    patch({
      instances: [
        ...w.instances,
        {
          ...instance,
          ...w.studySettings[key],
          id: id as ChartWindow["instances"][number]["id"],
          key: `study-${Date.now()}`,
          period:
            w.studySettings[key]?.period ?? instance?.period ?? w.periods[id],
        },
      ],
    });
  };
  return (
    <section
      className="ct-terminal-window"
      data-window-id={w.id}
      data-active={active}
    >
      <div className="ct-window-heading">
        <span>
          {index + 1} · {w.symbol} ·{" "}
          {w.exchange === "bybit" ? "Bybit" : "Binance"} ·{" "}
          {w.market === "spot" ? "Спот" : "Фьючерсы"} · {w.interval}
        </span>
        <button
          aria-label={`Развернуть окно ${index + 1}`}
          onClick={() => onMaximize(index)}
        >
          <Maximize2 size={12} />
        </button>
      </div>
      <ChartSurface
        candles={data.candles}
        interval={data.seconds}
        height="100%"
        title={`${w.symbol} · ${w.interval} · ${w.exchange}`}
        mode={w.mode}
        indicators={w.indicators.filter(
          (id) =>
            !w.studySettings[id]?.hidden ||
            ![
              "bos",
              "choch",
              "mss",
              "order_blocks",
              "fvg",
              "liquidity",
              "equal_high_low",
              "liquidity_sweeps",
              "swing_points",
            ].includes(id),
        )}
        periods={w.periods}
        instances={w.instances}
        externalPanes={external.data}
        volume={w.volume}
        grid={w.grid}
        log={w.log}
        percent={w.percent}
        autoScale={w.autoScale}
        follow={w.follow}
        drawingKey={drawingKey}
        initialDrawings={drawings.initial}
        onDrawingsChange={drawings.update}
        priceFormat={data.format ?? undefined}
        onReady={setApi}
        onNeedHistory={data.loadHistory}
        loading={data.loading}
        error={data.error}
        studySettings={w.studySettings}
        panelLayout={w.panels}
        editableStudies
        onScaleChange={patch}
        onStudySettings={(key, value) =>
          patch({ studySettings: { ...w.studySettings, [key]: value } })
        }
        onStudyRemove={remove}
        onStudyDuplicate={duplicate}
        onPanelLayout={(panels) => patch({ panels })}
        onSave={onSave}
        saveStatus={drawings.status}
      >
        <span className="ct-stream-status">
          {data.status === "live"
            ? "Поток"
            : data.status === "connecting"
              ? "Подключение…"
              : "Переподключение · REST 30с"}
          {data.error && (
            <button aria-label="Повторить загрузку свечей" onClick={data.retry}>
              Повторить
            </button>
          )}
        </span>
        {data.candles.length > 0 && (
          <CandleClock
            time={data.candles.at(-1)!.time}
            interval={data.seconds}
          />
        )}
        {api &&
          w.comparisons.map((c) => (
            <ComparisonSeries
              key={c.id}
              comparison={c}
              window={w}
              primary={data.candles}
              api={api}
              onChange={(next) =>
                patch({
                  comparisons: w.comparisons.map((i) =>
                    i.id === c.id ? { ...i, ...next } : i,
                  ),
                })
              }
              onRemove={() =>
                patch({
                  comparisons: w.comparisons.filter((i) => i.id !== c.id),
                })
              }
            />
          ))}
      </ChartSurface>
    </section>
  );
});
function CandleClock({ time, interval }: { time: number; interval: number }) {
  const [now, setNow] = useState(() => Date.now() / 1000);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now() / 1000), 1000);
    return () => clearInterval(timer);
  }, []);
  return (
    <span className="ct-candle-clock" title="До закрытия текущей свечи">
      {barCountdown(time, interval, now)}
    </span>
  );
}
function ComparisonSeries({
  comparison: c,
  window: w,
  primary,
  api,
  onChange,
  onRemove,
}: {
  comparison: Comparison;
  window: ChartWindow;
  primary: Candle[];
  api: DrawingRuntime;
  onChange: (change: Partial<Comparison>) => void;
  onRemove: () => void;
}) {
  const data = useMarketData(c, w.interval),
    [settings, setSettings] = useState(false);
  const own = c.mode !== "price" || c.ownScale !== false;
  const firstOwn = w.comparisons.find(
    (v) => v.mode !== "price" || v.ownScale !== false,
  );
  const scaleId = own
    ? firstOwn?.id === c.id
      ? "left"
      : `comparison-${c.id}`
    : "right";
  const lines = useRef<{
    main: ISeriesApi<"Line">;
    reference: ISeriesApi<"Line"> | null;
    api: DrawingRuntime;
  } | null>(null);
  useEffect(() => {
    if (api.disposed) return;
    const main = api.chart.addSeries(
      LineSeries,
      { priceScaleId: scaleId, priceLineVisible: false },
      0,
    );
    const reference =
      c.mode === "percent"
        ? api.chart.addSeries(
            LineSeries,
            { priceScaleId: scaleId, priceLineVisible: false },
            0,
          )
        : null;
    if (scaleId === "left")
      api.chart.applyOptions({
        leftPriceScale: { visible: true, autoScale: true },
      });
    lines.current = { main, reference, api };
    return () => {
      if (!api.disposed) {
        api.chart.removeSeries(main);
        if (reference) api.chart.removeSeries(reference);
        if (scaleId === "left")
          api.chart.applyOptions({ leftPriceScale: { visible: false } });
      }
      lines.current = null;
    };
  }, [api, c.mode, scaleId, own]);
  useEffect(() => {
    const runtime = lines.current;
    if (!runtime || runtime.api !== api || api.disposed) return;
    const { main, reference } = runtime;
    main.applyOptions({
      title: `${c.symbol} ${c.mode}`,
      color: c.color,
      lineWidth: 2,
      visible: !c.hidden,
    });
    reference?.applyOptions({
      title: `${w.symbol} %`,
      color: "#fbbf24",
      visible: !c.hidden,
    });
    if (c.mode === "price") {
      const times = new Set(primary.map((p) => p.time));
      main.setData(
        data.candles
          .filter((p) => times.has(p.time))
          .map((p) => ({ time: timestamp(p.time), value: p.close })),
      );
    } else {
      const output = comparePrices(
        primary,
        data.candles,
        c.mode === "percent" ? "returns" : c.mode,
      );
      main.setData(
        (output[c.mode === "percent" ? 1 : 0] ?? []).map((p) => ({
          ...p,
          time: timestamp(p.time),
        })),
      );
      reference?.setData(
        (output[0] ?? []).map((p) => ({ ...p, time: timestamp(p.time) })),
      );
    }
  }, [
    api,
    c.symbol,
    c.mode,
    c.color,
    c.hidden,
    data.candles,
    primary,
    w.symbol,
  ]);
  return (
    <div
      className="ct-comparison-legend"
      style={{ top: 90 + w.comparisons.findIndex((i) => i.id === c.id) * 24 }}
    >
      <span style={{ color: c.color }}>
        {c.symbol} ·{" "}
        {c.mode === "percent"
          ? "% от общей свечи"
          : c.mode === "ratio"
            ? "отношение"
            : c.mode === "spread"
              ? "разница"
              : "цена"}
      </span>
      {data.error && <span title={data.error}>нет данных</span>}
      <button
        aria-label={`Настроить сравнение ${c.symbol}`}
        onClick={() => setSettings(!settings)}
      >
        <Settings2 size={12} />
      </button>
      <button aria-label={`Удалить сравнение ${c.symbol}`} onClick={onRemove}>
        <X size={12} />
      </button>
      {settings && (
        <div className="ct-comparison-settings">
          <select
            value={c.mode}
            onChange={(e) =>
              onChange({ mode: e.target.value as Comparison["mode"] })
            }
          >
            <option value="price">Цена</option>
            <option value="percent">Доходность %</option>
            <option value="ratio">Отношение</option>
            <option value="spread">Разница</option>
          </select>
          <input
            type="color"
            value={c.color}
            onChange={(e) => onChange({ color: e.target.value })}
          />
          <label>
            <input
              type="checkbox"
              checked={c.ownScale !== false}
              onChange={(e) => onChange({ ownScale: e.target.checked })}
            />
            Своя шкала цены
          </label>
          <label>
            <input
              type="checkbox"
              checked={!c.hidden}
              onChange={(e) => onChange({ hidden: !e.target.checked })}
            />
            Показать
          </label>
        </div>
      )}
    </div>
  );
}
