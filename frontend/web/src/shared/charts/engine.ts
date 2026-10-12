import {
  createChart,
  ColorType,
  CrosshairMode,
  LineStyle,
  type DeepPartial,
  type ChartOptions,
  type UTCTimestamp,
} from "lightweight-charts";
import { readChartTheme } from "../market/chartTheme";
export type Candle = {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
};
export function normalizeCandles(data: readonly Candle[]): Candle[] {
  const unique = new Map<number, Candle>();
  for (const c of data)
    if (
      [c.time, c.open, c.high, c.low, c.close, c.volume].every(
        Number.isFinite,
      ) &&
      c.time > 0 &&
      Math.min(c.open, c.high, c.low, c.close) > 0 &&
      c.volume >= 0 &&
      c.high >= Math.max(c.open, c.close, c.low) &&
      c.low <= Math.min(c.open, c.close, c.high)
    )
      unique.set(c.time, c);
  return [...unique.values()].sort((a, b) => a.time - b.time);
}
export const timestamp = (time: number) => time as UTCTimestamp;
export function createUnifiedChart(
  host: HTMLElement,
  options: DeepPartial<ChartOptions> = {},
) {
  const t = readChartTheme();
  return createChart(host, {
    autoSize: true,
    layout: {
      background: { type: ColorType.Solid, color: t.surface },
      textColor: t.foreground,
      fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
      fontSize: 11,
      attributionLogo: true,
      panes: {
        enableResize: true,
        separatorColor: t.grid,
        separatorHoverColor: t.crosshair,
      },
    },
    grid: {
      vertLines: { color: t.grid, style: LineStyle.Dotted },
      horzLines: { color: t.grid, style: LineStyle.Dotted },
    },
    crosshair: {
      mode: CrosshairMode.Normal,
      vertLine: { color: t.crosshair, labelBackgroundColor: t.label },
      horzLine: { color: t.crosshair, labelBackgroundColor: t.label },
    },
    rightPriceScale: {
      borderColor: t.grid,
      scaleMargins: { top: 0.14, bottom: 0.08 },
    },
    timeScale: {
      borderColor: t.grid,
      timeVisible: true,
      secondsVisible: false,
      minBarSpacing: 0.05,
      rightOffset: 12,
      lockVisibleTimeRangeOnResize: true,
    },
    localization: { locale: "ru-RU" },
    handleScroll: {
      mouseWheel: true,
      pressedMouseMove: true,
      horzTouchDrag: true,
      vertTouchDrag: false,
    },
    handleScale: { axisPressedMouseMove: true, mouseWheel: true, pinch: true },
    ...options,
  });
}
export function resolveColor(color: string) {
  if (!color.startsWith("var(")) return color;
  return (
    getComputedStyle(document.documentElement)
      .getPropertyValue(color.slice(4, -1))
      .trim() || "#61d99b"
  );
}
