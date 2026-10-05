import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { backtestApi } from "./api";
import { dateOnly, utcDate, type BacktestRequest } from "./model";
import { Icon, Panel, SectionTitle, Segments } from "./ui";
import { type BacktestLimits, limitNotice } from "./limits";

export function MarketForm({
  value,
  onChange,
  userId,
  limits,
  onLimit,
}: {
  value: BacktestRequest;
  onChange: (v: BacktestRequest) => void;
  userId: string;
  limits: BacktestLimits;
  onLimit: (message: string) => void;
}) {
  const [adding, setAdding] = useState(false),
    [search, setSearch] = useState("");
  const listId = useId();
  const pairLimit = () => onLimit(limits.plan === "pro"
    ? "В Pro можно выбрать до 10 торговых пар за один тест. Удалите одну из выбранных пар, чтобы добавить другую."
    : limitNotice(limits, "Дополнительные торговые пары"));
  const symbols = useQuery({
    queryKey: ["backtest", userId, "symbols", value.market],
    queryFn: ({ signal }) => backtestApi.symbols(value.market, signal),
    staleTime: 600_000,
  });
  const available = (symbols.data?.symbols ?? [])
    .filter(
      (s) => !value.symbols.includes(s) && s.includes(search.toUpperCase()),
    )
    .slice(0, 40);
  const add = (symbol: string) => {
    if (value.symbols.length >= limits.maxSymbols) { pairLimit(); return; }
    if (value.symbols.includes(symbol)) return;
    onChange({ ...value, symbols: [...value.symbols, symbol] });
    setSearch("");
    setAdding(false);
  };
  const randomize = () => {
    if (value.symbols.length > limits.maxSymbols) { pairLimit(); return; }
    const all = [...new Set(symbols.data?.symbols ?? [])];
    const count = Math.min(Math.max(1, value.symbols.length), all.length);
    if (!count) return;
    const fresh = all.filter((symbol) => !value.symbols.includes(symbol));
    const previous = all.filter((symbol) => value.symbols.includes(symbol));
    // Shuffle each pool, prioritizing pairs that are not selected already.
    for (const pool of [fresh, previous]) {
      for (let i = pool.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [pool[i], pool[j]] = [pool[j], pool[i]];
      }
    }
    onChange({ ...value, symbols: [...fresh, ...previous].slice(0, count) });
    setSearch("");
    setAdding(false);
  };
  const endDate = new Date(value.to);
  endDate.setUTCDate(endDate.getUTCDate() - 1);
  const latest = new Date();
  latest.setUTCDate(latest.getUTCDate() - 1);
  const earliest = new Date();
  earliest.setUTCFullYear(earliest.getUTCFullYear() - 1);
  const periodEnd = utcDate(dateOnly(new Date().toISOString()));
  const presets = [
    { label: "30 дней", from: utcDate(dateOnly(periodEnd), -30) },
    { label: "90 дней", from: utcDate(dateOnly(periodEnd), -90) },
    { label: "1 год", from: utcDate(dateOnly(earliest.toISOString())) },
  ];
  return (
    <Panel className="bt-panel bt-market">
      <SectionTitle>Рынок и период</SectionTitle>
      <div className="bt-market-grid">
        <div className="bt-field">
          <span className="bt-label">Биржа</span>
          <div className="bt-static-input">Bybit</div>
        </div>
        <div className="bt-field">
          <span className="bt-label">Тип рынка</span>
          <Segments
            label="Тип рынка"
            value={value.market}
            options={[
              { value: "spot", label: "Spot" },
              { value: "linear", label: "Futures" },
            ]}
            onChange={(market) =>
              onChange({
                ...value,
                market,
                symbols: ["BTCUSDT"],
                strategy: {
                  ...value.strategy,
                  direction:
                    market === "spot" ? "long" : value.strategy.direction,
                },
              })
            }
          />
        </div>
        <div className="bt-field bt-symbol-field">
          <span className="bt-label">Торговые пары (до 15)</span>
          <div className="bt-symbols">
            {value.symbols.map((symbol) => (
              <span className="bt-symbol" key={symbol}>
                {symbol}
                <button
                  type="button"
                  aria-label={`Удалить ${symbol}`}
                  onClick={() =>
                    onChange({
                      ...value,
                      symbols: value.symbols.filter((s) => s !== symbol),
                    })
                  }
                >
                  <Icon name="close" size={14} />
                </button>
              </span>
            ))}
            <button
              className="bt-button bt-add-symbol"
              type="button"
              aria-expanded={adding}
              aria-controls={listId}
              onClick={() => {
                if (value.symbols.length >= limits.maxSymbols) { pairLimit(); return; }
                setAdding(!adding);
              }}
            >
              <Icon name="plus" size={14} />
              Добавить
            </button>
            <button
              className="bt-button"
              type="button"
              title="Заменить выбранные пары случайными, без повторов"
              disabled={!symbols.data?.symbols.length || symbols.isError || symbols.isPending}
              onClick={randomize}
            >
              Рандом
            </button>
          </div>
          <span className="bt-caption">
            Выбрано {value.symbols.length} из 15
          </span>
          {adding ? (
            <div className="bt-symbol-picker" id={listId}>
              <input
                autoFocus
                aria-label="Поиск торговой пары"
                placeholder="Поиск, например SOL"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape") setAdding(false);
                  if (e.key === "Enter") {
                    e.preventDefault();
                    if (available[0]) add(available[0]);
                  }
                }}
              />
              <div className="bt-symbol-options">
                {symbols.isPending ? (
                  <span>Загружаем пары Bybit…</span>
                ) : symbols.isError ? (
                  <span role="alert">
                    {symbols.error.message}
                    <button
                      type="button"
                      className="bt-text-button"
                      onClick={() => void symbols.refetch()}
                    >
                      Повторить
                    </button>
                  </span>
                ) : available.length ? (
                  available.map((symbol) => (
                    <button
                      type="button"
                      key={symbol}
                      onClick={() => add(symbol)}
                    >
                      {symbol}
                      <Icon name="plus" size={14} />
                    </button>
                  ))
                ) : (
                  <span>Пары не найдены</span>
                )}
              </div>
            </div>
          ) : null}
        </div>
        <div className="bt-field">
          <span className="bt-label">Таймфрейм</span>
          <Segments
            label="Таймфрейм"
            value={value.timeframe}
            options={(["1h", "4h"] as const).map((v) => ({
              value: v,
              label: v === "1h" ? "1 час" : "4 часа",
            }))}
            onChange={(timeframe) => onChange({ ...value, timeframe })}
          />
        </div>
        <div className="bt-field bt-date-field">
          <span className="bt-label">Период (UTC)</span>
          <div className="bt-period-controls">
            <div className="bt-date-range">
              <label>
                С
                <input
                  type="date"
                  required
                  aria-label="Начало периода"
                  min={dateOnly(earliest.toISOString())}
                  max={dateOnly(endDate.toISOString())}
                  value={dateOnly(value.from)}
                  onChange={(e) => {
                    if (e.target.value)
                      onChange({ ...value, from: utcDate(e.target.value) });
                  }}
                />
              </label>
              <label>
                По
                <input
                  type="date"
                  required
                  aria-label="Конец периода"
                  min={dateOnly(value.from)}
                  max={dateOnly(latest.toISOString())}
                  value={dateOnly(endDate.toISOString())}
                  onChange={(e) => {
                    if (e.target.value)
                      onChange({ ...value, to: utcDate(e.target.value, 1) });
                  }}
                />
              </label>
            </div>
            <div
              className="bt-period-presets"
              role="group"
              aria-label="Быстрый выбор периода"
            >
              {presets.map((preset) => (
                <button
                  type="button"
                  className="bt-button"
                  key={preset.label}
                  aria-pressed={
                    value.from === preset.from && value.to === periodEnd
                  }
                  onClick={() =>
                    onChange({ ...value, from: preset.from, to: periodEnd })
                  }
                >
                  {preset.label}
                </button>
              ))}
            </div>
          </div>
          <span className="bt-caption">
            До 1 года истории · Только закрытые свечи
          </span>
        </div>
      </div>
    </Panel>
  );
}
