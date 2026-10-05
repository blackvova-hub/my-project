import type { FormEvent } from "react";
import { intervals, type ArchiveQuery, type ArchiveSymbol } from "./model";

export function ArchiveForm({
  query,
  onChange,
  symbols,
  loading,
  catalogLoading,
  onSubmit,
}: {
  query: ArchiveQuery;
  onChange: (q: ArchiveQuery) => void;
  symbols: ArchiveSymbol[];
  loading: boolean;
  catalogLoading: boolean;
  onSubmit: () => void;
}) {
  const change = (key: keyof ArchiveQuery, value: string) =>
    onChange({ ...query, [key]: value });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    onSubmit();
  };
  const selected = symbols.find((s) => s.symbol === query.symbol);
  return (
    <form className="rp-archive" onSubmit={submit} aria-label="Выбор архива">
      <label>
        Биржа
        <select
          aria-label="Биржа"
          value={query.exchange}
          onChange={(e) => change("exchange", e.target.value)}
        >
          <option value="bybit">Bybit</option>
          <option value="binance">Binance</option>
        </select>
      </label>
      <label>
        Рынок
        <select
          aria-label="Рынок"
          value={query.market}
          onChange={(e) => change("market", e.target.value)}
        >
          <option value="linear">Futures</option>
          <option value="spot">Spot</option>
        </select>
      </label>
      <label>
        Пара
        <input
          list="replay-symbols"
          aria-label="Пара"
          value={query.symbol}
          maxLength={30}
          onChange={(e) => change("symbol", e.target.value.toUpperCase())}
          autoComplete="off"
          placeholder={catalogLoading ? "Загрузка…" : "Поиск пары"}
        />
        <datalist id="replay-symbols">
          {symbols.map((s) => (
            <option key={s.symbol} value={s.symbol} />
          ))}
        </datalist>
      </label>
      <label>
        Интервал
        <select
          aria-label="Интервал"
          value={query.timeframe}
          onChange={(e) => change("timeframe", e.target.value)}
        >
          {Object.keys(intervals).map((tf) => (
            <option key={tf} value={tf}>
              {tf.replace("m", "м").replace("h", "ч")}
            </option>
          ))}
        </select>
      </label>
      <button
        className="rp-primary"
        type="submit"
        disabled={loading || catalogLoading || !selected}
      >
        {loading ? "Загрузка…" : "Открыть инструмент"}
      </button>
      {selected ? (
        <span className="rp-archive-hint">
          В архиве:{" "}
          {new Date(selected.first).toLocaleDateString("ru-RU", {
            timeZone: "UTC",
          })}{" "}
          —{" "}
          {new Date(selected.last).toLocaleDateString("ru-RU", {
            timeZone: "UTC",
          })}{" "}
          · Выберите день в календаре под графиком
        </span>
      ) : (
        <span className="rp-archive-hint">
          {catalogLoading
            ? "Читаем каталог архива…"
            : "Выберите пару из скачанного архива"}
        </span>
      )}
    </form>
  );
}
