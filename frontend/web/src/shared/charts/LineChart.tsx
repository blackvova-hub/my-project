import { splitLineData } from "./seriesData";
import { readChartTheme } from "../market/chartTheme";
import { useEffect, useRef, useState } from "react";
import {
  LineSeries,
  LineStyle,
  type IChartApi,
  type ISeriesApi,
} from "lightweight-charts";
import { createUnifiedChart, resolveColor, timestamp } from "./engine";
import { useTheme } from "../theme/ThemeContext";
import "./charts.css";
export type ChartLine = {
  name: string;
  color: string;
  data: { time: number; value: number | null }[];
  dashed?: boolean;
  gapSeconds?: number;
};
export function TimeSeriesChart({
  lines,
  height = 240,
  label,
  percent = false,
  onHover,
}: {
  lines: ChartLine[];
  height?: number;
  label: string;
  percent?: boolean;
  onHover?: (time: number | null) => void;
}) {
  const host = useRef<HTMLDivElement>(null),
    [hover, setHover] = useState<{
      time: number;
      values: { name: string; value: number; color: string }[];
    } | null>(null),
    { theme } = useTheme();
  const runtime = useRef<{
    chart: IChartApi;
    seriesMap: Map<ISeriesApi<"Line">, ChartLine>;
  } | null>(null);
  const hoverRef = useRef(onHover);
  useEffect(() => {
    hoverRef.current = onHover;
  });
  useEffect(() => {
    if (!host.current) return;
    const chart = createUnifiedChart(host.current, {
        height,
        timeScale: { timeVisible: true, minBarSpacing: 0.01 },
        rightPriceScale: { visible: true, borderVisible: false },
      }),
      seriesMap = new Map<ISeriesApi<"Line">, ChartLine>();
    for (const line of lines) {
      const groups = splitLineData(line.data, line.gapSeconds);
      for (const [index, group] of groups.entries()) {
        const series = chart.addSeries(LineSeries, {
          title: line.name,
          color: resolveColor(line.color),
          lineStyle: line.dashed ? LineStyle.Dashed : LineStyle.Solid,
          lineWidth: 2,
          pointMarkersVisible: group.length === 1,
          priceLineVisible: false,
          lastValueVisible: index === groups.length - 1,
          priceFormat: percent
            ? { type: "custom", formatter: (v: number) => v.toFixed(2) + "%" }
            : { type: "price", precision: 2, minMove: 0.01 },
        });
        seriesMap.set(series, line);
        series.setData(group.map((p) => ({ ...p, time: timestamp(p.time) })));
      }
    }
    runtime.current = { chart, seriesMap };
    chart.timeScale().fitContent();
    const cross: Parameters<typeof chart.subscribeCrosshairMove>[0] = (p) => {
      if (!p.time) {
        setHover(null);
        hoverRef.current?.(null);
        return;
      }
      const values: { name: string; value: number; color: string }[] = [];
      for (const [series, line] of seriesMap) {
        const v = p.seriesData.get(series);
        if (v && "value" in v)
          values.push({
            name: line.name,
            value: Number(v.value),
            color: line.color,
          });
      }
      setHover({ time: Number(p.time), values });
      hoverRef.current?.(Number(p.time));
    };
    chart.subscribeCrosshairMove(cross);
    return () => {
      chart.unsubscribeCrosshairMove(cross);
      runtime.current = null;
      chart.remove();
    };
  }, [lines, height, percent]);
  useEffect(() => {
    const api = runtime.current;
    if (!api) return;
    const t = readChartTheme();
    api.chart.applyOptions({
      layout: { background: { color: t.surface }, textColor: t.foreground },
      grid: { vertLines: { color: t.grid }, horzLines: { color: t.grid } },
      timeScale: { borderColor: t.grid },
      crosshair: {
        vertLine: { color: t.crosshair, labelBackgroundColor: t.label },
        horzLine: { color: t.crosshair, labelBackgroundColor: t.label },
      },
    });
    for (const [series, line] of api.seriesMap)
      series.applyOptions({ color: resolveColor(line.color) });
  }, [theme]);
  return (
    <div
      className="ct-line-chart"
      style={{ height }}
      role="img"
      aria-label={label}
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
        e.preventDefault();
        const times = [
          ...new Set(
            lines.flatMap((l) =>
              l.data.filter((p) => p.value !== null).map((p) => p.time),
            ),
          ),
        ].sort((a, b) => a - b);
        const index = hover ? times.indexOf(hover.time) : 0,
          time =
            times[
              Math.max(
                0,
                Math.min(
                  times.length - 1,
                  index + (e.key === "ArrowRight" ? 1 : -1),
                ),
              )
            ];
        if (!time) return;
        const values = lines.flatMap((line) => {
          const p = line.data.find((p) => p.time === time);
          return p?.value != null
            ? [{ name: line.name, color: line.color, value: p.value }]
            : [];
        });
        setHover({ time, values });
        hoverRef.current?.(time);
      }}
    >
      <div ref={host} className="ct-line-chart-host" />
      {hover && (
        <div className="ct-line-tooltip">
          <small>
            {new Date(hover.time * 1000).toLocaleString("ru-RU", {
              timeZone: "UTC",
            })}{" "}
            UTC
          </small>
          {hover.values.map((v) => (
            <div key={v.name} style={{ color: v.color }}>
              {v.name}:{" "}
              {v.value.toLocaleString("ru-RU", { maximumFractionDigits: 4 })}
              {percent ? "%" : ""}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
