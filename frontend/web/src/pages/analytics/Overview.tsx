import { useState } from "react";
import type { Dataset, Insight, Range, TradeFilter } from "./types";
import type { Portfolio } from "./model";
import {
  categories,
  isCashflow,
  isUSD,
  money,
  pct,
  ranges,
  sum,
  timestamp,
} from "./model";
import { Icon, Metrics, Modal, Panel, Rows, Segments, Value } from "./ui";
import { LineChart } from "./Charts";

const assetColors = ["#fbbf24", "#22d3ee", "#a78bfa", "#34d399", "#9ca3af"];
export function Allocation({
  assets,
}: {
  assets: { symbol: string; value: number }[];
}) {
  const positive = assets.filter((a) => a.value > 0);
  const total = sum(positive, (a) => a.value);
  if (!positive.length)
    return (
      <p className="an-muted">
        {" "}
        В последнем снимке счёта нет активов с положительной стоимостью.{" "}
      </p>
    );
  return (
    <>
      <div className="an-allocation-bar">
        {positive.map((a, i) => (
          <span
            key={a.symbol}
            title={`${a.symbol}: ${money(a.value)}`}
            style={{
              width: `${(a.value / total) * 100}%`,
              background: assetColors[i % assetColors.length],
            }}
          />
        ))}
      </div>
      <div className="an-allocation-list">
        {positive.map((a, i) => (
          <div key={a.symbol}>
            <span
              className="an-asset-dot"
              style={{ background: assetColors[i % assetColors.length] }}
            />
            <span>{a.symbol}</span>
            <strong>{pct((a.value / total) * 100)}</strong>
            <small>{money(a.value, false, 0)}</small>
          </div>
        ))}
      </div>
    </>
  );
}
export function Overview({
  p,
  range,
  setRange,
  patterns,
  onTrades,
  onInsights,
}: {
  p: Portfolio;
  data: Dataset;
  range: Range;
  setRange: (r: Range) => void;
  patterns: Insight[];
  onTrades: (f: TradeFilter) => void;
  onInsights: () => void;
}) {
  const [category, setCategory] = useState<string | null>(null);
  const first = p.series[0];
  const last = p.series.at(-1);
  const changes = first && last && first.at !== last.at;
  const observedLedger = changes
    ? p.allLedger.filter(
        (l) =>
          l.at > first.observedAt[l.connectionId] &&
          l.at <= last.observedAt[l.connectionId] &&
          isUSD(l),
      )
    : [];
  const byCategory = new Map<string, number>();
  observedLedger.forEach((l) => {
    const key =
      l.category === "funding"
        ? l.amount < 0
          ? "funding_paid"
          : "funding_received"
        : l.category;
    byCategory.set(key, (byCategory.get(key) ?? 0) + l.amount);
  });
  const unrealizedChange = changes
    ? last.equity - last.wallet - (first.equity - first.wallet)
    : null;
  const residual = changes
    ? last.wallet - first.wallet - sum(observedLedger, (l) => l.amount)
    : null;
  const moneyRows = [
    {
      label: "Капитал в начале периода",
      value: money(changes ? first.equity : null),
      onClick: () => setCategory("starting"),
    },
    ...Object.entries(categories)
      .filter(([key]) => byCategory.has(key))
      .map(([key, label]) => ({
        label,
        value: <Value value={byCategory.get(key)} />,
        onClick: () => setCategory(key),
      })),
    {
      label: "Изменение незакрытой прибыли",
      value: <Value value={unrealizedChange} />,
      onClick: () => setCategory("unrealized"),
    },
    ...(residual != null && Math.abs(residual) > 0.01
      ? [
          {
            label: "Необъяснённое изменение баланса",
            value: <Value value={residual} />,
            onClick: () => setCategory("reconciliation"),
          },
        ]
      : []),
    {
      label: "Текущий капитал",
      value: money(last?.equity ?? p.equity),
      onClick: () => setCategory("current"),
    },
  ];
  const selected = observedLedger.filter(
    (l) =>
      l.category === category ||
      (l.category === "funding" &&
        (category === "funding_paid"
          ? l.amount < 0
          : category === "funding_received"
            ? l.amount > 0
            : false)),
  );
  const bySymbol = new Map<string, number>();
  selected.forEach((l) =>
    bySymbol.set(
      l.symbol || "Операции по счёту",
      (bySymbol.get(l.symbol || "Операции по счёту") ?? 0) + l.amount,
    ),
  );
  return (
    <>
      <Metrics
        items={[
          {
            label: "Стоимость портфеля",
            value: money(p.equity, false, 0),
            note: "Текущий капитал на бирже",
          },
          {
            label: "Чистый результат счёта",
            value: <Value value={p.net} />,
            note: "С учётом пополнений и выводов",
          },
          {
            label: "Торговый результат",
            value: <Value value={p.trading} />,
            note: "Закрытые позиции · до расходов",
          },
          {
            label: "Все расходы",
            value: <Value value={-p.costs} />,
            note: "Комиссии, финансирование, проценты",
          },
          {
            label: "Текущая просадка",
            value: <Value value={p.drawdown.current} format="percent" />,
            note: "Без влияния пополнений и выводов",
          },
        ]}
      />
      <Panel
        title="Динамика портфеля"
        action={
          <Segments
            items={ranges}
            value={range}
            onChange={setRange}
            label="Период графика"
          />
        }
      >
        <LineChart points={p.series} portfolio />
      </Panel>
      <div className="an-grid-two">
        <Panel title="Движение средств">
          <Rows items={moneyRows} />
          <p className="an-footnote">
            {" "}
            Пополнения и выводы меняют капитал, но не торговую прибыль.{" "}
            {changes
              ? "Результат открытых позиций входит в капитал."
              : "История капитала начинается с первой синхронизации."}
          </p>
        </Panel>
        <Panel title="Состав портфеля">
          <Allocation assets={p.assets} />
        </Panel>
      </div>
      <Panel
        title="Последние закономерности"
        action={
          <button className="an-text-button" onClick={onInsights}>
            {" "}
            Показать все <Icon name="arrow" size={15} />
          </button>
        }
      >
        {patterns.length ? (
          <div className="an-insight-rows">
            {patterns.slice(0, 3).map((i) => (
              <button key={i.id} onClick={() => onTrades({ ids: i.trades })}>
                <span className={i.positive ? "an-positive" : "an-negative"}>
                  <Icon name={i.positive ? "performance" : "edge"} size={23} />
                </span>
                <strong>{i.title}</strong>
                <span>{i.text}</span>
                <Icon name="chevron" size={15} />
              </button>
            ))}
          </div>
        ) : (
          <p className="an-muted">
            {" "}
            За этот период недостаточно данных для подтверждённых
            закономерностей.{" "}
          </p>
        )}
      </Panel>
      {category ? (
        <Modal
          title={
            categories[category] ??
            {
              starting: "Капитал в начале периода",
              current: "Текущий капитал",
              unrealized: "Изменение незакрытой прибыли",
              reconciliation: "Необъяснённое изменение баланса",
            }[category] ??
            category
          }
          onClose={() => setCategory(null)}
        >
          <p className="an-muted">
            {category === "reconciliation"
              ? "Разница между изменением баланса и загруженными операциями в долларах. Проверьте полноту истории и оценку других валют в разделе «Подключения»."
              : category === "unrealized"
                ? "Изменение разницы между капиталом и балансом кошелька по первому и последнему снимкам периода."
                : isCashflow(category)
                  ? "Эти операции меняют вложенный капитал и не входят в торговый результат."
                  : "Счёт → категория → инструмент → сделка → исполнение"}
          </p>
          {selected.length ? (
            <>
              <Rows
                items={[...bySymbol].map(([symbol, value]) => ({
                  label: symbol,
                  value: <Value value={value} />,
                  onClick:
                    symbol === "Операции по счёту"
                      ? undefined
                      : () => {
                          const events = selected.filter(
                            (l) => l.symbol === symbol,
                          );
                          const matches = p.trades.filter((t) =>
                            events.some(
                              (l) =>
                                l.connectionId === t.connectionId &&
                                (l.tradeId === t.id ||
                                  (t.symbol === symbol &&
                                    t.openedAt <= l.at &&
                                    (t.closedAt == null ||
                                      t.closedAt >= l.at))),
                            ),
                          );
                          onTrades({ symbol, ids: matches.map((t) => t.id) });
                          setCategory(null);
                        },
                }))}
              />
              <div className="an-table-scroll an-ledger-table">
                <table>
                  <thead>
                    <tr>
                      <th> Дата (UTC) </th>
                      <th> Инструмент </th>
                      <th> Сумма </th>
                      <th> Исходная операция </th>
                    </tr>
                  </thead>
                  <tbody>
                    {selected.map((l) => (
                      <tr key={l.id}>
                        <td>{timestamp(l.at)}</td>
                        <td>{l.symbol || "Счёт"}</td>
                        <td>
                          <Value value={l.amount} />
                        </td>
                        <td title={l.id}>{l.id.slice(0, 20)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          ) : (
            <Rows
              items={[
                {
                  label:
                    category === "unrealized"
                      ? "Изменение за доступный период"
                      : category === "reconciliation"
                        ? "Расхождение"
                        : "Выбранные счета",
                  value: money(
                    category === "current"
                      ? (last?.equity ?? p.equity)
                      : category === "starting"
                        ? changes
                          ? first.equity
                          : null
                        : category === "unrealized"
                          ? unrealizedChange
                          : residual,
                  ),
                },
              ]}
            />
          )}
          <p className="an-footnote">
            {" "}
            Все суммы относятся к выбранным счетам и периоду. USD, USDT и USDC
            учитываются по номинальному курсу 1:1.{" "}
          </p>
        </Modal>
      ) : null}
    </>
  );
}
