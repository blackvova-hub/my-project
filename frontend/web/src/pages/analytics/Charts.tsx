import { useMemo, useState } from "react";
import type { Time } from "lightweight-charts";
import { ChartSurface } from "../../shared/charts/ChartSurface";
import { TimeSeriesChart } from "../../shared/charts/LineChart";
import type { Candle, Trade } from "./types";
import type { EquityPoint } from "./model";
import { displayLabel } from "./locale";
import { Empty } from "./ui";
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
  if (points.length < 2)
    return (
      <Empty title="История капитала накапливается">
        Для графика нужны два снимка счёта. Отсутствующие исторические балансы
        не подставляются.
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

  const visible = lines.filter((l) => !hidden.includes(l.name));
  return (
    <div className="an-line-chart">
      <TimeSeriesChart
        lines={visible.map((l) => ({
          name: l.name,
          color: l.color,
          dashed: !!l.dash,
          data: points.map((p, i) => ({
            time: p.at / 1000,
            value: l.values[i],
          })),
        }))}
        height={240}
        label={portfolio ? "Динамика портфеля" : mode}
        percent={mode === "Доходность" || mode === "Просадка"}
      />
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
  const data = useMemo(
    () =>
      [...new Map(candles.map((c) => [c.time, c])).values()]
        .sort((a, b) => a.time - b.time)
        .map((c) => ({ ...c, volume: 0 })),
    [candles],
  );
  const markers = useMemo(() => {
    if (!data.length) return [];
    const grouped = new Map<number, Trade["fills"]>();
    for (const fill of trade.fills) {
      const t = fill.at / 1000;
      const nearest = data.reduce((a, b) =>
        Math.abs(b.time - t) < Math.abs(a.time - t) ? b : a,
      ).time;
      grouped.set(nearest, [...(grouped.get(nearest) ?? []), fill]);
    }
    return [...grouped].map(([time, fills]) => ({
      time: time as Time,
      position:
        fills[0].action === "Entry" || fills[0].action === "Add"
          ? ("belowBar" as const)
          : ("aboveBar" as const),
      color: fills[0].side === "BUY" ? "#34d399" : "#fb7185",
      shape: "circle" as const,
      text: fills.map((f) => displayLabel(f.action ?? f.side)).join(" / "),
    }));
  }, [trade, data]);
  const lines = useMemo(
    () =>
      [
        { title: "Средняя цена входа", price: trade.entry },
        { title: "Стоп-лосс", price: trade.stopLoss, color: "#fb7185" },
        { title: "Тейк-профит", price: trade.takeProfit, color: "#34d399" },
        { title: "Цена ликвидации", price: trade.liquidation },
      ].filter(
        (p): p is { title: string; price: number; color?: string } =>
          p.price != null && p.price > 0,
      ),
    [trade],
  );
  return (
    <ChartSurface
      candles={data}
      height={315}
      title={`${trade.symbol} · исполнения`}
      markers={markers}
      priceLines={lines}
      drawings={false}
      volume={false}
      fit
    />
  );
}
