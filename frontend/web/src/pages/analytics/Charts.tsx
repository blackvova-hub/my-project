import { useEffect, useRef, useState } from "react";
import {
  CandlestickSeries,
  ColorType,
  createChart,
  createSeriesMarkers,
  LineStyle,
} from "lightweight-charts";
import type { Time } from "lightweight-charts";
import type { Candle, Trade } from "./types";
import type { EquityPoint } from "./model";
import { money, number, pct, timestamp } from "./model";
import { displayLabel } from "./locale";
import { Empty } from "./ui";
import { useTheme } from "../../shared/theme/ThemeContext";
import { readChartTheme } from "../../shared/market/chartTheme";

type Line = {
  name: string;
  color: string;
  values: (number | null)[];
  dash?: string;
};
export function LineChart({
  points,
  mode = "Капитал",
  portfolio = false,
}: {
  points: EquityPoint[];
  mode?: string;
  portfolio?: boolean;
}) {
  const [hidden, setHidden] = useState<string[]>([]);
  const [hover, setHover] = useState<number | null>(null);
  const host = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(950);
  useEffect(() => {
    const node = host.current;
    if (!node) return;
    const observer = new ResizeObserver(([e]) =>
      setWidth(Math.max(260, e.contentRect.width)),
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  if (points.length < 2)
    return (
      <Empty title="История капитала накапливается">
        {" "}
        Для графика нужны два снимка счёта. Отсутствующие исторические балансы
        не подставляются.{" "}
      </Empty>
    );
  const first = points[0];
  const lines: Line[] = portfolio
    ? [
        {
          name: "Стоимость портфеля",
          color: "var(--chart-1)",
          values: points.map((p) => p.equity),
        },
        {
          name: "Вложенный капитал",
          color: "var(--muted-foreground)",
          values: points.map((p) => p.invested),
          dash: "6 5",
        },
        {
          name: "Сравнение с биткоином",
          color: "var(--chart-3)",
          values: points.map((p) => p.benchmark),
          dash: "2 5",
        },
      ]
    : [
        {
          name: mode,
          color: mode === "Просадка" ? "var(--destructive)" : "var(--chart-1)",
          values: points.map((p) =>
            mode === "Результат"
              ? p.net - first.net
              : mode === "Доходность"
                ? ((1 + p.returnPct / 100) / (1 + first.returnPct / 100) - 1) *
                  100
                : mode === "Просадка"
                  ? p.drawdown
                  : p.equity,
          ),
        },
      ];
  const visible = lines.filter((l) => !hidden.includes(l.name)),
    values = visible.flatMap((l) =>
      l.values.filter((v): v is number => v != null),
    );
  const low = Math.min(...values),
    high = Math.max(...values),
    spread = Math.max(high - low, Math.abs(high) * 0.005, 1);
  const min = low - spread * 0.15,
    max = high + spread * 0.15;
  const height = 240,
    left = 70,
    right = 14,
    top = 14,
    bottom = 30;
  const plotWidth = width - left - right,
    plotHeight = height - top - bottom;
  const x = (i: number) => left + (i / (points.length - 1)) * plotWidth,
    y = (v: number) => top + ((max - v) / (max - min)) * plotHeight;
  const percent = mode === "Доходность" || mode === "Просадка";
  const label = (v: number) =>
    percent
      ? pct(v)
      : Math.abs(v) >= 1000
        ? `${number(v / 1000, 1)} тыс.`
        : number(v, 0);
  const selected = hover == null ? null : points[hover];
  return (
    <div ref={host} className="an-line-chart">
      <div
        className="an-chart-canvas"
        tabIndex={0}
        role="img"
        aria-label={`График «${portfolio ? "Динамика портфеля" : mode}», снимков: ${points.length}. Для просмотра точек используйте стрелки клавиатуры.`}
        onKeyDown={(e) => {
          if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
            e.preventDefault();
            setHover((i) =>
              Math.min(
                points.length - 1,
                Math.max(0, (i ?? 0) + (e.key === "ArrowRight" ? 1 : -1)),
              ),
            );
          }
        }}
        onPointerMove={(e) => {
          const rect = e.currentTarget.getBoundingClientRect();
          setHover(
            Math.min(
              points.length - 1,
              Math.max(
                0,
                Math.round(
                  ((e.clientX - rect.left - left) / plotWidth) *
                    (points.length - 1),
                ),
              ),
            ),
          );
        }}
        onPointerLeave={() => setHover(null)}
      >
        <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`}>
          {Array.from({ length: 5 }, (_, i) => {
            const v = min + ((max - min) * i) / 4;
            return (
              <g key={i}>
                <line
                  x1={left}
                  x2={width - right}
                  y1={y(v)}
                  y2={y(v)}
                  stroke="var(--border)"
                  strokeWidth=".7"
                />
                <text
                  x={left - 10}
                  y={y(v) + 4}
                  textAnchor="end"
                  fill="var(--muted-foreground)"
                  fontSize="11"
                >
                  {label(v)}
                </text>
              </g>
            );
          })}
          {Array.from(
            { length: Math.min(width < 500 ? 4 : 7, points.length) },
            (_, i) => {
              const index = Math.round(
                (i / (Math.min(width < 500 ? 4 : 7, points.length) - 1)) *
                  (points.length - 1),
              );
              return (
                <text
                  key={i}
                  x={x(index)}
                  y={height - 6}
                  textAnchor={
                    i === 0
                      ? "start"
                      : i === Math.min(width < 500 ? 4 : 7, points.length) - 1
                        ? "end"
                        : "middle"
                  }
                  fill="var(--muted-foreground)"
                  fontSize="11"
                >
                  {new Date(points[index].at).toLocaleDateString("ru-RU", {
                    day: "numeric",
                    month: "short",
                    timeZone: "UTC",
                  })}
                </text>
              );
            },
          )}
          {visible.map((l) => (
            <path
              key={l.name}
              d={l.values
                .map((v, i) =>
                  v == null
                    ? ""
                    : `${i === 0 || l.values[i - 1] == null ? "M" : "L"}${x(i).toFixed(2)},${y(v).toFixed(2)}`,
                )
                .join(" ")}
              fill="none"
              stroke={l.color}
              strokeWidth="2"
              strokeDasharray={l.dash}
              strokeLinejoin="round"
              strokeLinecap="round"
            />
          ))}
          {hover != null ? (
            <g>
              <line
                x1={x(hover)}
                x2={x(hover)}
                y1={top}
                y2={height - bottom}
                stroke="var(--muted-foreground)"
                strokeDasharray="3 3"
              />
              {visible.map((l) =>
                l.values[hover] != null ? (
                  <circle
                    key={l.name}
                    cx={x(hover)}
                    cy={y(l.values[hover]!)}
                    r="4"
                    fill={l.color}
                    stroke="var(--card)"
                    strokeWidth="2"
                  />
                ) : null,
              )}
            </g>
          ) : null}
        </svg>
        {selected && hover != null ? (
          <div
            className="an-chart-tooltip"
            style={{ left: Math.min(Math.max(x(hover) - 100, 4), width - 210) }}
          >
            <small>{timestamp(selected.at)} UTC</small>
            {visible.map((l) => (
              <div key={l.name}>
                <span style={{ color: l.color }}>{l.name}</span>
                <strong>
                  {l.values[hover] == null
                    ? "—"
                    : percent
                      ? `${number(l.values[hover], 2)}%`
                      : money(l.values[hover])}
                </strong>
              </div>
            ))}
          </div>
        ) : null}
      </div>
      <div className="an-legend">
        {lines.map((l) => (
          <button
            key={l.name}
            aria-pressed={!hidden.includes(l.name)}
            style={{ opacity: hidden.includes(l.name) ? 0.4 : 1 }}
            onClick={() =>
              setHidden((old) =>
                old.includes(l.name)
                  ? old.filter((x) => x !== l.name)
                  : old.length < lines.length - 1
                    ? [...old, l.name]
                    : old,
              )
            }
          >
            <span style={{ background: l.color }} />
            {l.name}
          </button>
        ))}
      </div>
    </div>
  );
}

export function TradeCandles({
  trade,
  candles,
}: {
  trade: Trade;
  candles: Candle[];
}) {
  const { theme } = useTheme();
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!ref.current || !candles.length) return;
    const colors = readChartTheme();
    const chart = createChart(ref.current, {
      autoSize: true,
      height: 315,
      localization: {
        locale: "ru-RU",
        dateFormat: "dd.MM.yyyy",
        priceFormatter: (price: number) =>
          price.toLocaleString("ru-RU", {
            minimumFractionDigits: trade.entry < 1 ? 5 : 2,
            maximumFractionDigits: trade.entry < 1 ? 5 : 2,
          }),
      },
      layout: {
        background: { type: ColorType.Solid, color: colors.surface },
        textColor: colors.foreground,
        fontFamily: "ui-sans-serif, system-ui, sans-serif",
        fontSize: 11,
        attributionLogo: false,
      },
      grid: {
        vertLines: { color: colors.grid },
        horzLines: { color: colors.grid },
      },
      rightPriceScale: { borderColor: colors.grid },
      timeScale: {
        borderColor: colors.grid,
        timeVisible: true,
        secondsVisible: false,
      },
      crosshair: {
        horzLine: { color: colors.crosshair },
        vertLine: { color: colors.crosshair },
      },
    });
    const series = chart.addSeries(CandlestickSeries, {
      upColor: colors.up,
      downColor: colors.down,
      borderVisible: false,
      wickUpColor: colors.up,
      wickDownColor: colors.down,
      priceFormat: {
        type: "price",
        precision: trade.entry < 1 ? 5 : 2,
        minMove: trade.entry < 1 ? 0.00001 : 0.01,
      },
    });
    const sorted = [...new Map(candles.map((c) => [c.time, c])).values()].sort(
      (a, b) => a.time - b.time,
    );
    series.setData(sorted.map((c) => ({ ...c, time: c.time as Time })));
    for (const [title, price, color] of [
      ["Средняя цена входа", trade.entry, colors.foreground],
      ["Стоп-лосс", trade.stopLoss, colors.down],
      ["Тейк-профит", trade.takeProfit, colors.up],
      ["Цена ликвидации", trade.liquidation, colors.crosshair],
    ] as const) {
      if (price != null && price > 0)
        series.createPriceLine({
          price,
          color,
          lineWidth: 1,
          lineStyle: LineStyle.Dashed,
          axisLabelVisible: true,
          title,
        });
    }
    const markerMap = new Map<number, Trade["fills"]>();
    for (const f of trade.fills) {
      const time = f.at / 1000;
      const closest = sorted.reduce((a, b) =>
        Math.abs(b.time - time) < Math.abs(a.time - time) ? b : a,
      ).time;
      const list = markerMap.get(closest) ?? [];
      list.push(f);
      markerMap.set(closest, list);
    }
    createSeriesMarkers(
      series,
      [...markerMap]
        .sort((a, b) => a[0] - b[0])
        .map(([time, fills]) => ({
          time: time as Time,
          position:
            fills[0].action === "Entry" || fills[0].action === "Add"
              ? ("belowBar" as const)
              : ("aboveBar" as const),
          color: fills[0].side === "BUY" ? colors.up : colors.down,
          shape: "circle" as const,
          text: fills.map((f) => displayLabel(f.action ?? f.side)).join(" / "),
        })),
    );
    chart.timeScale().fitContent();
    return () => chart.remove();
  }, [trade, candles, theme]);
  return (
    <div
      className="an-candles"
      ref={ref}
      role="img"
      aria-label={`Свечной график ${trade.symbol} с отметками исполнений`}
    />
  );
}
