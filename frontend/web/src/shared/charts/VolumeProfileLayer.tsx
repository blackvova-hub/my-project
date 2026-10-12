import { useEffect, useMemo, useState } from "react";
import type { DrawingRuntime } from "./drawings/DrawingEditor";
import type { Candle } from "./engine";
import { calculateVolumeProfile } from "./volumeProfile";
export function VolumeProfileLayer({
  api,
  candles,
  period,
  visible,
  color = "#38bdf8",
}: {
  api: DrawingRuntime;
  candles: readonly Candle[];
  period: number;
  visible: boolean;
  color?: string;
}) {
  const [view, setView] = useState({
    from: 0,
    to: 0,
    width: 0,
    height: 0,
    revision: 0,
  });
  useEffect(() => {
    let frame = 0;
    const redraw = () => {
      if (frame || api.disposed) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        if (api.disposed) return;
        const range = api.chart.timeScale().getVisibleLogicalRange();
        setView((v) => ({
          from: Math.max(0, Math.floor(range?.from ?? 0)),
          to: Math.min(
            candles.length,
            Math.ceil((range?.to ?? candles.length - 1) + 1),
          ),
          width: api.chart.timeScale().width(),
          height: api.chart.panes()[0]?.getHeight() ?? 0,
          revision: v.revision + 1,
        }));
      });
    };
    const element = api.chart.chartElement(),
      pane = api.chart.panes()[0]?.getHTMLElement();
    const observer = new ResizeObserver(redraw);
    observer.observe(element);
    if (pane) observer.observe(pane);
    const drag = (e: PointerEvent) => {
      if (e.buttons) redraw();
    };
    api.chart.timeScale().subscribeVisibleLogicalRangeChange(redraw);
    element.addEventListener("wheel", redraw, { passive: true });
    element.addEventListener("pointermove", drag, { passive: true });
    redraw();
    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      if (!api.disposed)
        api.chart.timeScale().unsubscribeVisibleLogicalRangeChange(redraw);
      element.removeEventListener("wheel", redraw);
      element.removeEventListener("pointermove", drag);
    };
  }, [api, candles]);
  const bins = useMemo(
    () =>
      calculateVolumeProfile(
        visible ? candles.slice(view.from, view.to) : candles.slice(-period),
      ),
    [candles, period, visible, view.from, view.to],
  );
  const max = Math.max(0, ...bins.map((b) => b.volume)),
    poc = bins.find((b) => b.volume === max),
    width = Math.min(140, view.width * 0.22);
  if (api.disposed || !max || !view.width || !view.height) return null;
  const pocY = poc
    ? api.series.priceToCoordinate((poc.low + poc.high) / 2)
    : null;
  return (
    <svg
      className="ct-smart-overlay ct-volume-profile"
      width={view.width}
      height={view.height}
      style={{ height: view.height }}
      aria-label={`${visible ? "Видимый" : "Фиксированный"} профиль объёма: оценка по OHLCV`}
      data-profile={visible ? "visible" : "fixed"}
    >
      <title>
        Объём равномерно распределён по диапазону OHLC каждой свечи. Это оценка,
        не точный volume-at-price.
      </title>
      {bins.map((b, i) => {
        const top = api.series.priceToCoordinate(b.high),
          bottom = api.series.priceToCoordinate(b.low);
        if (top === null || bottom === null) return null;
        const w = (width * b.volume) / max;
        return (
          <rect
            key={i}
            data-profile-bin
            x={view.width - w}
            y={Math.min(top, bottom)}
            width={w}
            height={Math.max(1, Math.abs(bottom - top) - 1)}
            fill={b === poc ? "#fbbf24" : color}
            opacity={b === poc ? 0.65 : 0.3}
          />
        );
      })}
      {pocY !== null && (
        <g>
          <line
            x1={view.width - width}
            x2={view.width}
            y1={pocY}
            y2={pocY}
            stroke="#fbbf24"
            strokeDasharray="3 3"
          />
          <text
            x={view.width - width - 4}
            y={Math.max(12, pocY - 4)}
            fill="#fbbf24"
            textAnchor="end"
            fontSize="10"
          >
            POC · оценка
          </text>
        </g>
      )}
    </svg>
  );
}
