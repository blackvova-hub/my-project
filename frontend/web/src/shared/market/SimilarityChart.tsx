import { useEffect, useMemo, useRef, useState } from "react";
import type { IChartApi, ISeriesApi, ISeriesMarkersPluginApi, Time, UTCTimestamp } from "lightweight-charts";
import type { SimilarityMatch } from "./SimilarityPanel";
import { aggregateSimilarityCandles, similarityTimeframes } from "./similarityCandles";
import type { SimilarityCandle, SimilarityTimeframe } from "./similarityCandles";
import { useTheme } from "../theme/ThemeContext";
import { readChartTheme } from "./chartTheme";

type ChartApi = {
  chart: IChartApi;
  series: ISeriesApi<"Candlestick">;
  markers: ISeriesMarkersPluginApi<Time>;
};
const emptyCandles: SimilarityCandle[] = [];

// Same chart library, candle palette, grid, scales and type as PerpChart.
export function SimilarityChart({ match, candles, timeframe, loading }: {
  match: SimilarityMatch;
  candles?: SimilarityCandle[];
  timeframe: SimilarityTimeframe;
  loading: boolean;
}) {
  const { theme } = useTheme();
  const container = useRef<HTMLDivElement>(null);
  const [failed, setFailed] = useState(false);
  const [api, setApi] = useState<ChartApi | null>(null);
  const bars = useMemo(
    () => aggregateSimilarityCandles(candles ?? emptyCandles, timeframe, match.end),
    [candles, timeframe, match.end],
  );
  const label = similarityTimeframes.find((item) => item.seconds === timeframe)!.label;

  useEffect(() => {
    const node = container.current;
    if (!node) return;
    let disposed = false;
    let cleanup = () => {};
    import("lightweight-charts").then((lib) => {
      if (disposed) return;
      const colors = readChartTheme();
      const chart = lib.createChart(node, {
        width: node.clientWidth, height: 175,
        layout: { background: { type: lib.ColorType.Solid, color: colors.surface }, textColor: colors.foreground, fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif", fontSize: 10 },
        localization: { locale: "ru-RU" },
        grid: { vertLines: { color: colors.grid, style: lib.LineStyle.Dotted }, horzLines: { color: colors.grid, style: lib.LineStyle.Dotted } },
        crosshair: { mode: lib.CrosshairMode.Normal, vertLine: { color: colors.crosshair, labelBackgroundColor: colors.label }, horzLine: { color: colors.crosshair, labelBackgroundColor: colors.label } },
        rightPriceScale: { borderColor: colors.grid, scaleMargins: { top: 0.18, bottom: 0.08 } },
        timeScale: { borderColor: colors.grid, timeVisible: true, secondsVisible: false, minBarSpacing: 0.05 },
        handleScroll: { mouseWheel: true, pressedMouseMove: true, horzTouchDrag: true, vertTouchDrag: false },
        handleScale: { axisPressedMouseMove: true, mouseWheel: true, pinch: true },
      });
      const series = chart.addSeries(lib.CandlestickSeries, { upColor: "#34d399", downColor: "#fb7185", borderVisible: false, wickUpColor: "#6ee7b7", wickDownColor: "#fda4af", priceLineVisible: false, lastValueVisible: false });
      const markers = lib.createSeriesMarkers(series, []);
      const resize = new ResizeObserver(() => { if (!disposed) chart.applyOptions({ width: node.clientWidth }); });
      resize.observe(node);
      cleanup = () => { resize.disconnect(); chart.remove(); };
      setApi({ chart, series, markers });
    }).catch(() => { if (!disposed) setFailed(true); });
    return () => { disposed = true; cleanup(); };
  }, [theme]);

  useEffect(() => {
    if (!api) return;
    if (bars.length) {
      const smallest = Math.min(...bars.map((c) => c.low));
      const precision = smallest >= 10 ? 2 : Math.min(8, Math.max(2, 3 - Math.floor(Math.log10(Math.max(smallest, 1e-8)))));
      api.series.applyOptions({ priceFormat: { type: "price", precision, minMove: 10 ** -precision } });
    }
    api.series.setData(bars.map((c) => ({ ...c, time: c.time as UTCTimestamp })));
    const future = bars.find((c) => c.time * 1000 >= match.end);
    api.markers.setMarkers(future ? [{ time: future.time as UTCTimestamp, position: "aboveBar", color: "rgba(226, 232, 240, 0.72)", shape: "circle", text: "Далее" }] : []);
    api.chart.timeScale().fitContent();
  }, [api, bars, match.end]);

  return (
    <div
      aria-busy={loading}
      data-chart-timeframe={timeframe}
      data-chart-bars={bars.length}
      className="group/chart relative h-[175px] w-full"
    >
      <div ref={container} role="img" aria-label={`Исторический график ${match.symbol}, таймфрейм ${label}, сходство ${match.score.toFixed(1)}%`} className="h-full w-full" />
      {!loading && !failed && bars.length > 0 && (
        <button
          type="button"
          aria-label={`Сбросить масштаб графика ${match.symbol}`}
          title="Сбросить масштаб этого графика"
          onClick={() => {
            api?.chart.priceScale("right").applyOptions({ autoScale: true });
            api?.chart.timeScale().fitContent();
          }}
          className="absolute left-2 top-2 z-10 rounded-md border border-border bg-background/90 p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring motion-reduce:transition-none"
        >
          <svg aria-hidden="true" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M3 10a9 9 0 1 1 2.4 8.1M3 4v6h6" /></svg>
        </button>
      )}
      {(loading || failed || !bars.length) && (
        <p className="absolute inset-0 flex items-center justify-center bg-background p-3 text-xs text-muted-foreground">
          {loading ? "Загружаем свечи…" : "График недоступен"}
        </p>
      )}
    </div>
  );
}
