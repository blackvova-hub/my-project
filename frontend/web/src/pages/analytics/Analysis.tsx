import { useState } from "react";
import type { Dimension, Insight, Trade, TradeFilter } from "./types";
import type { Portfolio } from "./model";
import {
  dimension,
  group,
  money,
  number,
  pct,
  stats,
  sum,
  weekdays,
} from "./model";
import {
  Empty,
  Icon,
  Metrics,
  Modal,
  Panel,
  Rows,
  Segments,
  Value,
} from "./ui";
import { Allocation } from "./Overview";
import { LineChart } from "./Charts";
import { Explanation } from "./Explanation";
import { displayLabel } from "./locale";

function GroupTable({
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
  const rows = group(trades, by);
  return (
    <Panel title={title}>
      <div className="an-table-scroll">
        <table>
          <thead>
            <tr>
              <th>Группа</th>
              <th> Сделки </th>
              <th> Чистый результат </th>
              <th> Доля прибыльных сделок </th>
              <th> Фактор прибыли </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.label}>
                <td>
                  <button
                    className="an-text-button"
                    onClick={() => onTrades({ ids: r.trades.map((t) => t.id) })}
                  >
                    {displayLabel(r.label)} <Icon name="chevron" size={12} />
                  </button>
                </td>
                <td>{r.n}</td>
                <td>
                  <Value value={r.net} />
                </td>
                <td>{pct(r.winRate)}</td>
                <td>{number(r.pf)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Panel>
  );
}
export function Edge({
  p,
  onTrades,
}: {
  p: Portfolio;
  onTrades: (f: TradeFilter) => void;
}) {
  const [by, setBy] = useState("День недели");
  const hours = ["00–04", "04–08", "08–12", "12–16", "16–20", "20–24"];
  const closed = p.trades.filter((t) => t.closedAt && t.complete);
  const long = stats(closed.filter((t) => t.side === "LONG")),
    short = stats(closed.filter((t) => t.side === "SHORT"));
  return (
    <>
      <div className="an-edge-top">
        <Panel
          title="День и время"
          action={<span className="an-muted"> Чистый результат · UTC </span>}
        >
          <div className="an-heatmap">
            <span />
            {weekdays.map((d) => (
              <span key={d}>{d}</span>
            ))}
            {hours.flatMap((hour) => [
              <span className="an-hour" key={hour}>
                {hour}
              </span>,
              ...weekdays.map((day) => {
                const trades = closed.filter(
                    (t) =>
                      dimension(t, "hour") === hour &&
                      dimension(t, "weekday") === day,
                  ),
                  s = stats(trades);
                return (
                  <button
                    key={day + hour}
                    disabled={!trades.length}
                    aria-label={`${day} ${hour} UTC, сделок: ${trades.length}`}
                    title={`Сделок: ${trades.length} · ${money(s.net, true)} · Фактор прибыли: ${number(s.pf)}`}
                    style={{
                      background: trades.length
                        ? `color-mix(in srgb, ${s.net >= 0 ? "#34d399" : "#fb7185"} ${Math.min(50, 12 + Math.abs(s.net) / 100)}%, #18231c)`
                        : undefined,
                    }}
                    onClick={() => onTrades({ weekday: day, hour })}
                  >
                    {trades.length ? money(s.net, true, 0) : "—"}
                  </button>
                );
              }),
            ])}
          </div>
          <p className="an-footnote">
            {" "}
            Время открытия · нажмите на ячейку, чтобы увидеть сделки{" "}
          </p>
        </Panel>
        <Panel title="Лонги и шорты">
          <div className="an-table-scroll">
            <table className="an-comparison">
              <thead>
                <tr>
                  <th />
                  <th> Лонг </th>
                  <th> Шорт </th>
                </tr>
              </thead>
              <tbody>
                {[
                  ["Сделки", long.n, short.n],
                  [
                    "Чистый результат",
                    money(long.net, true),
                    money(short.net, true),
                  ],
                  [
                    "Доля прибыльных сделок",
                    pct(long.winRate),
                    pct(short.winRate),
                  ],
                  ["Фактор прибыли", number(long.pf), number(short.pf)],
                ].map(([label, l, s]) => (
                  <tr key={label}>
                    <td>{label}</td>
                    <td>{l}</td>
                    <td>{s}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="an-two-actions">
            <button
              className="an-button"
              onClick={() => onTrades({ side: "LONG" })}
            >
              {" "}
              Сделки в лонг{" "}
            </button>
            <button
              className="an-button"
              onClick={() => onTrades({ side: "SHORT" })}
            >
              {" "}
              Сделки в шорт{" "}
            </button>
          </div>
        </Panel>
      </div>
      <GroupTable
        trades={closed}
        by="symbol"
        title="По инструментам"
        onTrades={onTrades}
      />
      <div className="an-grid-two">
        <GroupTable
          trades={closed}
          by="leverage"
          title="Влияние плеча"
          onTrades={onTrades}
        />
        <GroupTable
          trades={closed}
          by="duration"
          title="Влияние длительности"
          onTrades={onTrades}
        />
      </div>
      <Segments
        items={["День недели", "Время", "Размер позиции", "Биржа"]}
        value={by}
        onChange={setBy}
        label="Разбивка эффективности"
      />
      <GroupTable
        trades={closed}
        by={
          (
            {
              "День недели": "weekday",
              Время: "hour",
              "Размер позиции": "size",
              Биржа: "exchange",
            } as const
          )[by as "День недели"]
        }
        title={
          {
            "День недели": "По дням недели",
            Время: "По времени открытия",
            "Размер позиции": "По размеру позиции",
            Биржа: "По биржам",
          }[by] ?? "По группам"
        }
        onTrades={onTrades}
      />
    </>
  );
}
export function Costs({
  p,
  onTrades,
}: {
  p: Portfolio;
  onTrades: (f: TradeFilter) => void;
}) {
  const fees = -sum(
      p.ledger.filter((l) => l.category === "fees"),
      (l) => l.amount,
    ),
    paid = -sum(
      p.ledger.filter((l) => l.category === "funding" && l.amount < 0),
      (l) => l.amount,
    ),
    received = sum(
      p.ledger.filter((l) => l.category === "funding" && l.amount > 0),
      (l) => l.amount,
    );
  const fills = p.trades.flatMap((t) => t.fills).filter((f) => f.at >= p.start),
    maker = fills.filter((f) => f.maker),
    taker = fills.filter((f) => !f.maker);
  const makerVolume = sum(maker, (f) => f.quantity * f.price),
    takerVolume = sum(taker, (f) => f.quantity * f.price),
    volume = makerVolume + takerVolume;
  const ratio = p.trading > 0 ? p.costs / p.trading : null;
  const symbols = [
    ...new Set(
      p.ledger
        .filter((l) => l.category === "funding")
        .map((l) => l.symbol || "Счёт"),
    ),
  ];
  const costsBy = (by: "symbol" | "exchange") => {
    const m = new Map<
      string,
      { fees: number; funding: number; other: number; total: number }
    >();
    p.ledger
      .filter((l) =>
        ["fees", "funding", "interest", "withdrawal_fee"].includes(l.category),
      )
      .forEach((l) => {
        const key = l[by] || "Счёт",
          row = m.get(key) ?? { fees: 0, funding: 0, other: 0, total: 0 };
        if (l.category === "fees") row.fees -= l.amount;
        else if (l.category === "funding") row.funding -= l.amount;
        else row.other -= l.amount;
        row.total -= l.amount;
        m.set(key, row);
      });
    return [...m];
  };
  return (
    <>
      <Metrics
        items={[
          {
            label: "Всего уплачено",
            value: money(p.costs, false, 0),
            note: "Расходы за выбранный период",
          },
          {
            label: "Расходы на 1 $ прибыли",
            value: money(ratio),
            note: "Если прибыль до расходов положительна",
          },
          {
            label: "Доля расходов в прибыли",
            value: pct(ratio == null ? null : ratio * 100),
            note: "Полученное финансирование показано отдельно",
          },
        ]}
      />
      <div className="an-grid-three">
        <Panel title="Состав расходов">
          <Rows
            items={[
              { label: "Торговые комиссии", value: money(fees) },
              { label: "Уплачено за финансирование", value: money(paid) },
              {
                label: "Проценты по займу",
                value: money(
                  -sum(
                    p.ledger.filter((l) => l.category === "interest"),
                    (l) => l.amount,
                  ),
                ),
              },
              {
                label: "Комиссии за вывод",
                value: money(
                  -sum(
                    p.ledger.filter((l) => l.category === "withdrawal_fee"),
                    (l) => l.amount,
                  ),
                ),
              },
              { label: "Оценка проскальзывания", value: "Нет данных" },
            ]}
          />
          <p className="an-footnote">
            {" "}
            Проскальзывание уже учтено в ценах исполнения и не вычитается
            повторно. Для отдельной оценки нужна история котировок.{" "}
          </p>
        </Panel>
        <Panel title="Мейкер и тейкер">
          <table className="an-comparison">
            <thead>
              <tr>
                <th />
                <th> Мейкер </th>
                <th> Тейкер </th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td> Объём </td>
                <td>{money(makerVolume, false, 0)}</td>
                <td>{money(takerVolume, false, 0)}</td>
              </tr>
              <tr>
                <td> Комиссии </td>
                <td>
                  {money(
                    sum(
                      maker.filter((f) => f.feeKnown),
                      (f) => f.fee,
                    ),
                  )}
                </td>
                <td>
                  {money(
                    sum(
                      taker.filter((f) => f.feeKnown),
                      (f) => f.fee,
                    ),
                  )}
                </td>
              </tr>
              <tr>
                <td> Исполнения </td>
                <td>{maker.length}</td>
                <td>{taker.length}</td>
              </tr>
            </tbody>
          </table>
          <div className="an-allocation-bar">
            <span
              style={{
                width: `${volume ? (makerVolume / volume) * 100 : 0}%`,
                background: "#34d399",
              }}
            />
            <span style={{ flex: 1, background: "#72817a" }} />
          </div>
          <p className="an-muted">
            {" "}
            Доля исполнений тейкером ·{" "}
            {pct(fills.length ? (taker.length / fills.length) * 100 : null)}
          </p>
        </Panel>
        <Panel title="Финансирование">
          <div className="an-funding-summary">
            <div>
              <small> Уплачено </small>
              <Value value={-paid} />
            </div>
            <div>
              <small> Получено </small>
              <Value value={received} />
            </div>
            <div>
              <small> Итого </small>
              <Value value={received - paid} />
            </div>
          </div>
          <Rows
            items={symbols.map((symbol) => ({
              label: symbol,
              value: (
                <Value
                  value={sum(
                    p.ledger.filter(
                      (l) =>
                        l.category === "funding" &&
                        (l.symbol || "Счёт") === symbol,
                    ),
                    (l) => l.amount,
                  )}
                />
              ),
              onClick:
                symbol === "Счёт" ? undefined : () => onTrades({ symbol }),
            }))}
          />
        </Panel>
      </div>
      <div className="an-grid-two">
        {(["symbol", "exchange"] as const).map((by) => (
          <Panel
            key={by}
            title={
              by === "symbol" ? "Расходы по инструментам" : "Расходы по биржам"
            }
          >
            <div className="an-table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>{by === "symbol" ? "Инструмент" : "Биржа"}</th>
                    <th> Торговые комиссии </th>
                    <th> Финансирование, итог </th>
                    <th> Прочее </th>
                    <th> Всего </th>
                  </tr>
                </thead>
                <tbody>
                  {costsBy(by).map(([label, c]) => (
                    <tr key={label}>
                      <td>
                        {by === "symbol" && label !== "Счёт" ? (
                          <button
                            className="an-text-button"
                            onClick={() => onTrades({ symbol: label })}
                          >
                            {displayLabel(label)}
                          </button>
                        ) : (
                          displayLabel(label)
                        )}
                      </td>
                      <td>{money(c.fees)}</td>
                      <td>{money(c.funding)}</td>
                      <td>{money(c.other)}</td>
                      <td>{money(c.total)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Panel>
        ))}
      </div>
    </>
  );
}
export function Risk({ p }: { p: Portfolio }) {
  const gross = sum(p.positions, (t) => t.notional),
    long = sum(
      p.positions.filter((t) => t.side === "LONG"),
      (t) => t.notional,
    ),
    short = gross - long;
  const margin = sum(p.now, (s) => s.margin);
  const map = new Map<string, { long: number; short: number }>();
  p.positions.forEach((t) => {
    const row = map.get(t.symbol) ?? { long: 0, short: 0 };
    row[t.side === "LONG" ? "long" : "short"] += t.notional;
    map.set(t.symbol, row);
  });
  const dd = p.drawdown;
  return (
    <>
      <Metrics
        items={[
          { label: "Общий капитал", value: money(p.equity, false, 0) },
          { label: "Общий объём позиций", value: money(gross, false, 0) },
          {
            label: "Фактическое плечо",
            value: p.equity ? `${number(gross / p.equity)}×` : "—",
          },
          {
            label: "Использованная маржа",
            value: pct(p.equity ? (margin / p.equity) * 100 : null),
          },
        ]}
      />
      <Panel title="Позиции по активам">
        <div className="an-exposure-header">
          <span> Актив </span>
          <span> Объём лонгов и шортов </span>
          <span> Лонг </span>
          <span> Шорт </span>
          <span> Итого </span>
        </div>
        {[...map].map(([symbol, row]) => (
          <div className="an-exposure-row" key={symbol}>
            <strong>{symbol}</strong>
            <div className="an-exposure-bars">
              <div
                style={{
                  width: `${gross ? (row.long / gross) * 100 : 0}%`,
                  background: "#34d399",
                }}
              />
              <div
                style={{
                  width: `${gross ? (row.short / gross) * 100 : 0}%`,
                  background: "#fb7185",
                }}
              />
            </div>
            <span className="an-positive">{money(row.long, false, 0)}</span>
            <span className="an-negative">{money(row.short, false, 0)}</span>
            <Value value={row.long - row.short} />
          </div>
        ))}
        <Rows
          items={[
            { label: "Объём лонгов", value: money(long) },
            { label: "Объём шортов", value: money(short) },
            {
              label: "Разница лонгов и шортов",
              value: <Value value={long - short} />,
            },
          ]}
        />
      </Panel>
      <Panel title="Концентрация позиций">
        <Allocation
          assets={[...map].map(([symbol, v]) => ({
            symbol,
            value: v.long + v.short,
          }))}
        />
      </Panel>
      <div className="an-grid-two">
        <Panel title="Текущая просадка">
          <div className="an-large-value">
            <Value value={dd.current} format="percent" />
          </div>
          <Rows
            items={[
              { label: "Капитал на наблюдаемом пике", value: money(dd.peak) },
              { label: "Текущий капитал", value: money(p.equity) },
              {
                label: "Изменение от пика в долларах",
                value: <Value value={dd.loss} />,
              },
              { label: "Дней в просадке", value: number(dd.days) },
            ]}
          />
        </Panel>
        <Panel title="Наибольшая просадка за историю">
          <div className="an-large-value">
            <Value value={dd.max} format="percent" />
          </div>
          <Rows
            items={[
              { label: "Капитал на пике", value: money(dd.maxPeak) },
              { label: "Капитал в нижней точке", value: money(dd.bottom) },
              {
                label: "Восстановление",
                value:
                  dd.recovery == null
                    ? "Ещё не восстановлено"
                    : `${dd.recovery} дн.`,
              },
            ]}
          />
          <p className="an-footnote">
            {" "}
            Просадка в процентах не зависит от пополнений и выводов. Изменение
            капитала в долларах может включать денежные потоки. История
            начинается с первого снимка.{" "}
          </p>
        </Panel>
      </div>
      <Panel title="Просадка от максимума">
        <LineChart points={p.series} mode="Просадка" />
      </Panel>
    </>
  );
}
export function Insights({
  patterns,
  onTrades,
  demo,
}: {
  patterns: Insight[];
  onTrades: (f: TradeFilter) => void;
  demo: boolean;
}) {
  const [category, setCategory] = useState("Все закономерности"),
    [selected, setSelected] = useState<Insight | null>(null);
  const filtered = patterns.filter(
    (i) => category === "Все закономерности" || i.category === category,
  );
  return (
    <>
      <Segments
        items={["Все закономерности", "Расходы", "Торговля", "Риски"]}
        value={category}
        onChange={setCategory}
        label="Категория закономерностей"
      />
      <div className="an-insights-layout">
        <div className="an-insight-list">
          {filtered.map((i) => (
            <article
              className={`an-insight ${i.positive ? "positive" : ""}`}
              key={i.id}
            >
              <div className="an-insight-top">
                <div>
                  <h2>{i.title}</h2>
                  <p>{i.text}</p>
                </div>
                <div className="an-insight-actions">
                  <button
                    className="an-button"
                    disabled={!i.trades.length}
                    onClick={() => onTrades({ ids: i.trades })}
                  >
                    {" "}
                    Сделки: {i.sample} сделок{" "}
                  </button>
                  <button
                    className="an-button an-primary"
                    onClick={() => setSelected(i)}
                  >
                    {" "}
                    Разобрать закономерность{" "}
                  </button>
                </div>
              </div>
              <div className="an-insight-evidence">
                <span>
                  {" "}
                  Размер выборки <strong>{i.sample} сделок </strong>
                </span>
                <span>
                  {" "}
                  Полнота данных <strong>{i.confidence}</strong>
                </span>
                <span>
                  {" "}
                  Категория <strong>{i.category}</strong>
                </span>
                <span>
                  {" "}
                  Расчёт <strong> По правилам </strong>
                </span>
              </div>
            </article>
          ))}
          {!filtered.length ? (
            <Panel>
              <Empty title="Недостаточно данных для закономерностей">
                {" "}
                Для анализа группы нужно не менее 30 сделок с полной историей.
                Попробуйте увеличить период.{" "}
              </Empty>
            </Panel>
          ) : null}
        </div>
        <Panel title="Методика" className="an-methodology">
          <h3> Основано на данных </h3>
          <p>
            {" "}
            Каждая закономерность рассчитана по выбранным счетам и загруженной
            истории.{" "}
          </p>
          <h3> Размер выборки </h3>
          <p>
            {" "}
            Для анализа группы требуется не менее 30 закрытых сделок. Позиции с
            неполной историей исключаются.{" "}
          </p>
          <h3> Оценка полноты данных </h3>
          <p>
            {" "}
            Высокая: от 100 сделок. <br /> Средняя: 30–99 сделок. <br /> Низкая:
            менее 30 сделок.{" "}
          </p>
          <p>
            {" "}
            Это оценка объёма данных, а не вероятность или прогноз.
            Закономерности риска описывают текущее состояние счёта.{" "}
          </p>
          <h3> Пояснения </h3>
          <p>
            {" "}
            Пояснения опираются на рассчитанные показатели. Модель не
            придумывает и не вычисляет финансовые значения.{" "}
          </p>
        </Panel>
      </div>
      {selected ? (
        <Modal title={selected.title} onClose={() => setSelected(null)}>
          <p>{selected.text}</p>
          <Panel title="Основание расчёта">
            <p>{selected.evidence}</p>
            <Rows
              items={[
                {
                  label: "Размер выборки",
                  value: `Сделок: ${selected.sample}`,
                },
                { label: "Полнота данных", value: selected.confidence },
                {
                  label: "Источник пояснения",
                  value: "Правила анализа данных",
                },
              ]}
            />
          </Panel>
          <Explanation key={selected.id} insight={selected} demo={demo} />
          <button
            className="an-button an-primary"
            disabled={!selected.trades.length}
            onClick={() => {
              onTrades({ ids: selected.trades });
              setSelected(null);
            }}
          >
            {" "}
            Посмотреть связанные сделки <Icon name="arrow" />
          </button>
        </Modal>
      ) : null}
    </>
  );
}
