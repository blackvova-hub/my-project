import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { http } from "../../shared/api/http";
import type { Candle, Trade, TradeFilter } from "./types";
import {
  duration,
  filterTrades,
  money,
  number,
  pct,
  stats,
  timestamp,
} from "./model";
import { demoCandles } from "./demo";
import { Empty, Icon, Metrics, Panel, Rows, Select, Value } from "./ui";
import { TradeCandles } from "./Charts";
import { displayLabel, errorMessage, localizeMessage } from "./locale";

type ChartResponse = {
  candles: Candle[];
  mfe: number | null;
  mae: number | null;
  captured: number | null;
  method: string;
};
function TradeDetail({
  trade,
  demo,
  onClose,
  onSave,
}: {
  trade: Trade;
  demo: boolean;
  onClose: () => void;
  onSave: (id: string, tag: string, strategy: string) => Promise<void>;
}) {
  const [tag, setTag] = useState(trade.tag),
    [strategy, setStrategy] = useState(trade.strategy),
    [saving, setSaving] = useState(false),
    [message, setMessage] = useState("");
  const chart = useQuery({
    queryKey: ["analytics-trade-chart", demo, trade.id],
    queryFn: () =>
      demo
        ? Promise.resolve({
            candles: demoCandles(trade),
            mfe: trade.mfe,
            mae: trade.mae,
            captured: trade.captured,
            method: "Демонстрационные свечи: условное движение цены.",
          })
        : http<ChartResponse>(
            `/analytics/trade-chart?trade=${encodeURIComponent(trade.id)}`,
          ),
    staleTime: 300000,
    retry: 1,
  });
  const candles = chart.data?.candles ?? [];
  const mfe = chart.data?.mfe ?? trade.mfe,
    mae = chart.data?.mae ?? trade.mae,
    captured = chart.data?.captured ?? trade.captured;
  async function save() {
    setSaving(true);
    setMessage("");
    try {
      await onSave(trade.id, tag, strategy);
      setMessage("Сохранено");
    } catch (e) {
      setMessage(errorMessage(e));
    } finally {
      setSaving(false);
    }
  }
  let running = 0,
    held = 0,
    basis = 0;
  const runningRows = trade.fills.map((f) => {
    if (f.action === "Entry" || f.action === "Add") {
      basis = (basis * held + f.price * f.quantity) / (held + f.quantity);
      held += f.quantity;
    } else {
      running +=
        (f.price - basis) * f.quantity * (trade.side === "LONG" ? 1 : -1);
      held -= f.quantity;
    }
    running -= f.fee;
    return { ...f, running };
  });
  return (
    <Panel className="an-trade-detail">
      <div className="an-trade-heading">
        <div>
          <h2>
            {trade.symbol}{" "}
            <span
              className={trade.side === "LONG" ? "an-positive" : "an-negative"}
            >
              {displayLabel(trade.side)}
            </span>
          </h2>
          <p>
            {" "}
            Открыта {timestamp(trade.openedAt)} <Icon name="arrow" size={14} />{" "}
            {trade.closedAt ? timestamp(trade.closedAt) : "Позиция открыта"} UTC
          </p>
        </div>
        <div>
          <strong>
            <Value value={trade.complete ? trade.net : null} />
          </strong>
          <small> Чистый закрытый результат </small>
        </div>
        <button
          className="an-icon-button"
          aria-label="Закрыть подробности сделки"
          onClick={onClose}
        >
          <Icon name="close" />
        </button>
      </div>
      {!trade.complete ? (
        <div className="an-notice">
          {" "}
          История неполна или комиссия не переведена в доллары. Сделка исключена
          из статистики результатов.{" "}
        </div>
      ) : null}
      <div className="an-trade-grid">
        <div>
          <div className="an-chart-label">
            {trade.symbol} · цена маркировки, 1 мин ·{" "}
            {displayLabel(trade.exchange)}
          </div>
          {chart.isPending ? (
            <div className="an-loading">
              {" "}
              Загружаем историю цены маркировки…{" "}
            </div>
          ) : chart.isError ? (
            <Empty title="История цены недоступна">
              {errorMessage(chart.error)}
            </Empty>
          ) : candles.length ? (
            <TradeCandles trade={trade} candles={candles} />
          ) : (
            <Empty title="Нет доступных свечей" />
          )}
          <Metrics
            items={[
              {
                label: "Макс. прибыль внутри сделки",
                value: <Value value={mfe} />,
              },
              {
                label: "Макс. убыток внутри сделки",
                value: <Value value={mae} />,
              },
              { label: "Получено от возможной прибыли", value: pct(captured) },
              {
                label: "Время в позиции",
                value: trade.closedAt ? duration(trade.duration) : "Открыта",
              },
            ]}
          />
          <p className="an-footnote">
            {chart.data?.method
              ? localizeMessage(chart.data.method)
              : "Для оценки движения внутри сделки нужна история цены маркировки."}
          </p>
        </div>
        <div className="an-trade-anatomy">
          <h3> Подробности сделки </h3>
          <Rows
            items={[
              {
                label: "Средняя цена входа",
                value: money(trade.entry || null),
              },
              {
                label: "Средняя цена выхода",
                value: trade.closedAt ? money(trade.exit) : "—",
              },
              { label: "Размер позиции", value: money(trade.size) },
              {
                label: "До расходов",
                value: <Value value={trade.complete ? trade.gross : null} />,
              },
              { label: "Комиссии", value: <Value value={-trade.fees} /> },
              {
                label: "Финансирование",
                value: <Value value={trade.funding} />,
              },
              {
                label: "Чистый результат",
                value: <Value value={trade.complete ? trade.net : null} />,
              },
              {
                label: "Плечо при входе",
                value:
                  trade.leverage == null
                    ? "Нет данных"
                    : `${number(trade.leverage)}×`,
              },
              { label: "Стоп-лосс", value: money(trade.stopLoss) },
              { label: "Тейк-профит", value: money(trade.takeProfit) },
              { label: "Цена ликвидации", value: money(trade.liquidation) },
            ]}
          />
        </div>
      </div>
      <h3 className="an-section-label"> История исполнений </h3>
      <div className="an-table-scroll">
        <table>
          <thead>
            <tr>
              {[
                "Время (UTC)",
                "Действие",
                "Сторона",
                "Количество",
                "Цена",
                "Комиссия",
                "Ликвидность",
                "Накопленный результат",
              ].map((h) => (
                <th key={h}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {runningRows.map((f) => (
              <tr key={f.id + f.action}>
                <td>{timestamp(f.at)}</td>
                <td>{displayLabel(f.action)}</td>
                <td
                  className={f.side === "BUY" ? "an-positive" : "an-negative"}
                >
                  {displayLabel(f.side)}
                </td>
                <td>{number(f.quantity, 6)}</td>
                <td>{money(f.price, false, f.price < 1 ? 5 : 2)}</td>
                <td>
                  {number(f.fee, 6)} {f.feeCurrency}
                </td>
                <td>{f.maker ? "Мейкер" : "Тейкер"}</td>
                <td>
                  <Value value={f.running} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="an-footnote">
        {" "}
        Накопленный результат — закрытая прибыль за вычетом комиссий за
        исполнение. Финансирование указано отдельно выше.{" "}
      </p>
      <form
        className="an-annotation"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <label>
          {" "}
          Метка{" "}
          <input
            value={tag}
            onChange={(e) => setTag(e.target.value)}
            maxLength={80}
            placeholder="Например, пробой"
          />
        </label>
        <label>
          {" "}
          Стратегия{" "}
          <input
            value={strategy}
            onChange={(e) => setStrategy(e.target.value)}
            maxLength={80}
            placeholder="Например, по тренду"
          />
        </label>
        <button className="an-button" disabled={saving}>
          {saving ? "Сохраняем…" : "Сохранить пометку"}
        </button>
        <span role="status">{message}</span>
      </form>
    </Panel>
  );
}
export function Trades({
  trades,
  filter,
  setFilter,
  demo,
  onSave,
}: {
  trades: Trade[];
  filter: TradeFilter;
  setFilter: (f: TradeFilter) => void;
  demo: boolean;
  onSave: (id: string, tag: string, strategy: string) => Promise<void>;
}) {
  const [more, setMore] = useState(false),
    [selected, setSelected] = useState<string | null>(null),
    [page, setPage] = useState(0),
    [sort, setSort] = useState("Сначала новые");
  const filtered = useMemo(() => {
    const rows = filterTrades(trades, filter);
    return [...rows].sort((a, b) =>
      sort === "Сначала прибыльные"
        ? b.net - a.net
        : sort === "Сначала убыточные"
          ? a.net - b.net
          : b.openedAt - a.openedAt,
    );
  }, [trades, filter, sort]);
  const actualPage = Math.min(
    page,
    Math.max(0, Math.ceil(filtered.length / 15) - 1),
  );
  const shown = filtered.slice(actualPage * 15, actualPage * 15 + 15);
  const selectedTrade = filtered.find((t) => t.id === selected);
  const summary = stats(filtered);
  const update = (key: keyof TradeFilter, value: string) => {
    setFilter({ ...filter, [key]: value || undefined });
    setPage(0);
  };
  return (
    <>
      <div className="an-trade-filters">
        <Select
          label="Биржа"
          value={filter.exchange ?? ""}
          onChange={(v) => update("exchange", v)}
          options={[
            { value: "", label: "Все биржи" },
            ...[...new Set(trades.map((t) => t.exchange))].sort().map((v) => ({
              value: v,
              label: v === "bybit" ? "Bybit" : "Binance",
            })),
          ]}
        />
        <Select
          label="Инструмент"
          value={filter.symbol ?? ""}
          onChange={(v) => update("symbol", v)}
          options={[
            { value: "", label: "Все инструменты" },
            ...[...new Set(trades.map((t) => t.symbol))]
              .sort()
              .map((s) => ({ value: s, label: s })),
          ]}
        />
        <Select
          label="Направление"
          value={filter.side ?? ""}
          onChange={(v) => update("side", v)}
          options={[
            { value: "", label: "Все направления" },
            { value: "LONG", label: "Лонг" },
            { value: "SHORT", label: "Шорт" },
          ]}
        />
        <Select
          label="Результат"
          value={filter.result ?? ""}
          onChange={(v) => update("result", v)}
          options={[
            { value: "", label: "Любой результат" },
            { value: "profit", label: "Прибыль" },
            { value: "loss", label: "Убыток" },
          ]}
        />
        <button
          className="an-button"
          aria-expanded={more}
          onClick={() => setMore((v) => !v)}
        >
          <Icon name="filter" /> Ещё фильтры{" "}
        </button>
        <Select
          label="Сортировка сделок"
          value={sort}
          onChange={setSort}
          options={[
            "Сначала новые",
            "Сначала прибыльные",
            "Сначала убыточные",
          ].map((s) => ({
            value: s,
            label: s,
          }))}
        />
        {Object.values(filter).some(Boolean) ? (
          <button
            className="an-text-button"
            onClick={() => {
              setFilter({});
              setPage(0);
            }}
          >
            {" "}
            Сбросить фильтры <Icon name="close" size={14} />
          </button>
        ) : null}
      </div>
      {more ? (
        <div className="an-filter-extra">
          <Select
            label="Плечо"
            value={filter.leverage ?? ""}
            onChange={(v) => update("leverage", v)}
            options={["", "1–3x", "3–5x", "5–10x", "10x+", "Неизвестно"].map(
              (v) => ({ value: v, label: v || "Любое плечо" }),
            )}
          />
          <Select
            label="Длительность сделки"
            value={filter.duration ?? ""}
            onChange={(v) => update("duration", v)}
            options={[
              "",
              "< 5 мин",
              "5–30 мин",
              "30 мин–2 ч",
              "2–8 ч",
              "8 ч и более",
            ].map((v) => ({ value: v, label: v || "Любая длительность" }))}
          />
          <label>
            {" "}
            Дата (UTC){" "}
            <input
              type="date"
              aria-label="Дата сделки"
              value={filter.date ?? ""}
              onChange={(e) => update("date", e.target.value)}
            />
          </label>
          <label>
            {" "}
            Метка{" "}
            <input
              aria-label="Поиск по метке"
              value={filter.tag ?? ""}
              onChange={(e) => update("tag", e.target.value)}
              placeholder="Любая метка"
            />
          </label>
          <label>
            {" "}
            Стратегия{" "}
            <input
              aria-label="Поиск по стратегии"
              value={filter.strategy ?? ""}
              onChange={(e) => update("strategy", e.target.value)}
              placeholder="Любая стратегия"
            />
          </label>
        </div>
      ) : null}
      <Panel className={`an-trades-panel ${selectedTrade ? "has-detail" : ""}`}>
        <div className="an-table-scroll">
          <table className="an-trades-table">
            <thead>
              <tr>
                {[
                  "Инструмент",
                  "Сторона",
                  "Вход",
                  "Выход",
                  "Объём",
                  "Плечо",
                  "До расходов",
                  "Комиссии",
                  "Финансирование",
                  "Чистый результат",
                  "Длительность",
                  "Дата (UTC)",
                ].map((h) => (
                  <th key={h}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {shown.map((t) => (
                <tr
                  className={selected === t.id ? "selected" : ""}
                  key={t.id}
                  onClick={() => setSelected(t.id)}
                >
                  <td>
                    <button
                      className="an-symbol-button"
                      onClick={(e) => {
                        e.stopPropagation();
                        setSelected(t.id);
                      }}
                    >
                      {t.symbol}
                    </button>
                    <small>
                      {displayLabel(t.exchange)}
                      {!t.complete
                        ? " · Неполная история"
                        : !t.closedAt
                          ? " · Открыта"
                          : ""}
                    </small>
                  </td>
                  <td
                    className={
                      t.side === "LONG" ? "an-positive" : "an-negative"
                    }
                  >
                    {displayLabel(t.side)}
                  </td>
                  <td>{money(t.entry, false, t.entry < 1 ? 4 : 2)}</td>
                  <td>
                    {t.closedAt
                      ? money(t.exit, false, t.exit < 1 ? 4 : 2)
                      : "—"}
                  </td>
                  <td>{money(t.size, false, 0)}</td>
                  <td>{t.leverage == null ? "—" : `${number(t.leverage)}×`}</td>
                  <td>
                    <Value value={t.complete ? t.gross : null} />
                  </td>
                  <td>{money(-t.fees)}</td>
                  <td>{money(t.funding, true)}</td>
                  <td>
                    <Value value={t.complete ? t.net : null} />
                  </td>
                  <td>{t.closedAt ? duration(t.duration) : "Открыта"}</td>
                  <td>{timestamp(t.closedAt ?? t.openedAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {!filtered.length ? (
          <Empty title="Нет сделок по выбранным фильтрам">
            {" "}
            Измените период или сбросьте фильтры, чтобы увидеть другие
            сделки.{" "}
          </Empty>
        ) : null}
        <div className="an-table-footer">
          <span>
            {filtered.length} сделок · Чистый результат{" "}
            <Value value={summary.net} />
          </span>
          <div>
            <button
              className="an-icon-button"
              aria-label="Предыдущая страница сделок"
              disabled={actualPage === 0}
              onClick={() => setPage(actualPage - 1)}
            >
              <Icon name="back" size={16} />
            </button>
            <span>
              {actualPage + 1} / {Math.max(1, Math.ceil(filtered.length / 15))}
            </span>
            <button
              className="an-icon-button"
              aria-label="Следующая страница сделок"
              disabled={(actualPage + 1) * 15 >= filtered.length}
              onClick={() => setPage(actualPage + 1)}
            >
              <Icon name="chevron" size={16} />
            </button>
          </div>
        </div>
      </Panel>
      {selectedTrade ? (
        <TradeDetail
          key={selectedTrade.id}
          trade={selectedTrade}
          demo={demo}
          onClose={() => setSelected(null)}
          onSave={onSave}
        />
      ) : (
        <div className="an-trade-hint">
          <Icon name="trades" /> Выберите сделку, чтобы увидеть график,
          подробности и каждое исполнение.{" "}
        </div>
      )}
    </>
  );
}
