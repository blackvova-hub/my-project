import { useState } from "react";
import type { Dimension, Range, Trade, TradeFilter } from "./types";
import type { Portfolio } from "./model";
import {
  DAY,
  dateKey,
  group,
  money,
  number,
  pct,
  ranges,
  stats,
  weekdays,
} from "./model";
import { Icon, Metrics, Panel, Rows, Segments, Value } from "./ui";
import { LineChart } from "./Charts";
import { displayLabel } from "./locale";

export function Breakdown({
  trades,
  by,
  title,
  onTrades,
}: {
  trades: Trade[];
  by: Dimension;
  title: string;
  onTrades: (f: TradeFilter) => void;
}) {
  const groups = group(trades, by);
  return (
    <Panel title={title}>
      <div className="an-breakdowns">
        {groups.map((g) => (
          <button
            key={g.label}
            onClick={() => onTrades({ ids: g.trades.map((t) => t.id) })}
          >
            <span>{displayLabel(g.label)}</span>
            <span className="an-mini-bar">
              <i
                style={{
                  width: `${Math.max(2, (Math.abs(g.net) / Math.max(...groups.map((x) => Math.abs(x.net)), 1)) * 100)}%`,
                  background: g.net >= 0 ? "#34d399" : "#fb7185",
                }}
              />
            </span>
            <Value value={g.net} />
          </button>
        ))}
      </div>
    </Panel>
  );
}
export function Calendar({
  trades,
  now,
  onTrades,
}: {
  trades: Trade[];
  now: number;
  onTrades: (f: TradeFilter) => void;
}) {
  const [month, setMonth] = useState(() =>
    Date.UTC(new Date(now).getUTCFullYear(), new Date(now).getUTCMonth(), 1),
  );
  const first = new Date(month),
    offset = (first.getUTCDay() + 6) % 7;
  const days = new Date(
    Date.UTC(first.getUTCFullYear(), first.getUTCMonth() + 1, 0),
  ).getUTCDate();
  const cells = Math.ceil((offset + days) / 7) * 7;
  const grouped = new Map<string, Trade[]>();
  trades
    .filter((t) => t.closedAt && t.complete)
    .forEach((t) => {
      const k = dateKey(t.closedAt!);
      const values = grouped.get(k) ?? [];
      values.push(t);
      grouped.set(k, values);
    });
  const move = (by: number) =>
    setMonth(Date.UTC(first.getUTCFullYear(), first.getUTCMonth() + by, 1));
  return (
    <Panel
      title="Календарь результатов"
      action={
        <div className="an-calendar-nav">
          <button
            className="an-icon-button"
            aria-label="Предыдущий месяц"
            onClick={() => move(-1)}
          >
            <Icon name="back" size={15} />
          </button>
          <span>
            {first.toLocaleDateString("ru-RU", {
              month: "long",
              year: "numeric",
              timeZone: "UTC",
            })}
          </span>
          <button
            className="an-icon-button"
            aria-label="Следующий месяц"
            onClick={() => move(1)}
          >
            <Icon name="chevron" size={15} />
          </button>
        </div>
      }
    >
      <div className="an-calendar">
        {weekdays.map((d) => (
          <span className="an-calendar-day" key={d}>
            {d}
          </span>
        ))}
        {Array.from({ length: cells }, (_, i) => {
          const at = month + (i - offset) * DAY,
            date = dateKey(at),
            items = grouped.get(date) ?? [],
            s = stats(items),
            outside = i < offset || i >= offset + days;
          return (
            <button
              disabled={outside || !items.length}
              key={date}
              className={`an-calendar-cell ${outside ? "outside" : items.length ? (s.net >= 0 ? "gain" : "loss") : ""}`}
              onClick={() => onTrades({ date })}
              title={
                items.length
                  ? `До расходов: ${money(s.gross, true)}\nКомиссии: ${money(-s.fees)}\nФинансирование: ${money(s.funding, true)}\nЧистый результат: ${money(s.net, true)}\nДоля прибыльных сделок: ${pct(s.winRate)}\nФактор прибыли: ${number(s.pf)}`
                  : "Нет сделок"
              }
            >
              <span>{new Date(at).getUTCDate()}</span>
              {items.length ? (
                <>
                  <strong>{money(s.net, true, 0)}</strong>
                  <small>{items.length} сделок </small>
                  <small className="an-calendar-fees">
                    {money(-s.fees, false, 0)} комиссии{" "}
                  </small>
                </>
              ) : null}
            </button>
          );
        })}
      </div>
      <p className="an-footnote">
        {" "}
        UTC · закрытые сделки · наведите курсор для просмотра результата,
        комиссий и финансирования{" "}
      </p>
    </Panel>
  );
}
export function Performance({
  p,
  range,
  setRange,
  now,
  onTrades,
}: {
  p: Portfolio;
  range: Range;
  setRange: (r: Range) => void;
  now: number;
  onTrades: (f: TradeFilter) => void;
}) {
  const [mode, setMode] = useState("Капитал");
  const s = p.stats;
  const first = p.series[0],
    last = p.series.at(-1);
  const returns =
    first && last && first.at !== last.at
      ? ((1 + last.returnPct / 100) / (1 + first.returnPct / 100) - 1) * 100
      : null;
  const metrics = [
    { label: "До расходов", value: <Value value={s.gross} /> },
    {
      label: "Доходность без влияния денежных потоков",
      value: <Value value={returns} format="percent" />,
    },
    { label: "Средняя прибыль", value: <Value value={s.avgWin} /> },
    { label: "Средний убыток", value: <Value value={s.avgLoss} /> },
    { label: "Отношение прибыли к убытку", value: number(s.payoff) },
    { label: "Лучшая сделка", value: <Value value={s.best} /> },
    { label: "Худшая сделка", value: <Value value={s.worst} /> },
    { label: "Лучший день", value: <Value value={s.bestDay} /> },
    { label: "Худший день", value: <Value value={s.worstDay} /> },
    {
      label: "Текущая просадка",
      value: <Value value={p.drawdown.current} format="percent" />,
    },
    {
      label: "Срок восстановления",
      value:
        p.drawdown.recovery == null
          ? "Ещё не восстановлено"
          : `${p.drawdown.recovery} дн.`,
    },
  ];
  return (
    <>
      <Metrics
        items={[
          {
            label: "Чистый результат",
            value: <Value value={s.net} />,
            note: `Закрытых сделок: ${s.n}`,
          },
          {
            label: "Доля прибыльных сделок",
            value: pct(s.winRate),
            note: `Прибыльных: ${s.wins} / убыточных: ${s.losses}`,
          },
          {
            label: "Фактор прибыли",
            value: number(s.pf),
            note: "Сумма прибылей / сумма убытков",
          },
          {
            label: "Ожидаемый результат",
            value: <Value value={s.expectancy} />,
            note: "Средний чистый результат сделки",
          },
          {
            label: "Максимальная просадка",
            value: <Value value={p.drawdown.max} format="percent" />,
            note: "По доступной истории капитала",
          },
        ]}
      />
      <Panel>
        <div className="an-chart-controls">
          <Segments
            items={["Капитал", "Результат", "Доходность", "Просадка"]}
            value={mode}
            onChange={setMode}
            label="Показатель графика"
          />
          <Segments
            items={ranges}
            value={range}
            onChange={setRange}
            label="Период графика"
          />
        </div>
        <LineChart points={p.series} mode={mode} />
      </Panel>
      <div className="an-grid-two an-performance-middle">
        <Calendar trades={p.trades} now={now} onTrades={onTrades} />
        <Panel title="Показатели торговли">
          <Rows items={metrics} />
        </Panel>
      </div>
      <div className="an-grid-four">
        {(
          [
            ["symbol", "По инструментам"],
            ["side", "По направлению"],
            ["exchange", "По биржам"],
            ["weekday", "По дням недели"],
          ] as const
        ).map(([by, title]) => (
          <Breakdown
            key={by}
            trades={p.trades}
            by={by}
            title={title}
            onTrades={onTrades}
          />
        ))}
      </div>
    </>
  );
}
