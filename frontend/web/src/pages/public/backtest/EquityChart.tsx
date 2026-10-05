import { useEffect, useRef } from "react";
import {
  ColorType,
  createChart,
  LineSeries,
  LineStyle,
  type Time,
} from "lightweight-charts";
import type { BacktestResult } from "./model";
import { useTheme } from "../../../shared/theme/ThemeContext";
import { readChartTheme } from "../../../shared/market/chartTheme";

export default function EquityChart({
  points,
}: {
  points: BacktestResult["equity"];
}) {
  const { theme } = useTheme();
  const container = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!container.current) return;
    const colors = readChartTheme();
    const chart = createChart(container.current, {
      autoSize: true,
      height: 280,
      layout: {
        background: { type: ColorType.Solid, color: "transparent" },
        textColor: colors.foreground,
        fontFamily: "ui-sans-serif, system-ui, sans-serif",
        fontSize: 12,
        attributionLogo: true,
      },
      grid: {
        vertLines: { visible: false },
        horzLines: { color: colors.grid, style: LineStyle.Dashed },
      },
      rightPriceScale: { visible: false },
      leftPriceScale: {
        visible: true,
        borderVisible: false,
        scaleMargins: { top: 0.12, bottom: 0.08 },
      },
      timeScale: {
        borderVisible: false,
        timeVisible: true,
        lockVisibleTimeRangeOnResize: true,
        minBarSpacing: 0.01,
      },
      localization: { locale: "ru-RU" },
      handleScroll: false,
      handleScale: false,
    });
    const series = chart.addSeries(LineSeries, {
      color: colors.up,
      lineWidth: 2,
      priceScaleId: "left",
      priceLineVisible: false,
      crosshairMarkerRadius: 4,
      lastValueVisible: true,
      priceFormat: { type: "price", precision: 2, minMove: 0.01 },
    });
    series.setData(
      points.map((p) => ({ time: (p.time / 1000) as Time, value: p.value })),
    );
    chart.timeScale().fitContent();
    return () => chart.remove();
  }, [points, theme]);
  return (
    <div
      className="bt-equity-chart"
      ref={container}
      role="img"
      aria-label="График капитала портфеля за период теста"
    />
  );
}
