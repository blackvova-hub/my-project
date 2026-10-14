import { useDeferredValue, useEffect, useState } from "react";
import { Bookmark, Check, Search } from "lucide-react";
import { useSearchParams } from "react-router-dom";
import {
  DEFAULT_FILTERS,
  useTerminalNews,
  type NewsFilters,
  type TerminalNewsItem,
} from "./api";
import type { useReading } from "./useReading";
import { Empty, ImportanceMark, Skeleton } from "./ui";
import { category, stamp } from "./format";
import { rollingNewsFrom } from "./model";

type Reading = ReturnType<typeof useReading>;
export function NewsRow({
  item,
  reading,
  onSelect,
  featured = false,
  zone,
}: {
  item: TerminalNewsItem;
  reading: Reading;
  onSelect: (item: TerminalNewsItem) => void;
  featured?: boolean;
  zone: string;
}) {
  const read = reading.state.read.includes(item.id),
    saved = reading.state.saved.includes(item.id);
  return (
    <article
      className={`nm-news-row ${featured ? "nm-news-featured" : ""} ${read ? "nm-news-read" : ""}`}
    >
      <div className="nm-news-time">
        <time
          dateTime={item.publishedAt}
          title={`${item.timestampBasis === "indexed" ? "Обнаружено источником" : "Опубликовано"}: ${stamp(item.publishedAt, zone)} (${zone})`}
        >
          {stamp(item.publishedAt, zone, false)}
        </time>
        <small>
          {new Date(item.publishedAt).toLocaleDateString("ru-RU", {
            timeZone: zone,
            day: "numeric",
            month: "short",
          })}
        </small>
        <small>{item.source}</small>
        {item.timestampBasis === "indexed" ? <small>обнаружено</small> : null}
      </div>
      <div className="nm-news-copy">
        <button
          className="nm-headline"
          onClick={() => {
            reading.markRead(item.id);
            onSelect(item);
          }}
        >
          {item.breaking ? <span className="nm-breaking">Срочно</span> : null}
          {item.title}
        </button>
        {item.summary ? <p>{item.summary}</p> : null}
        <div className="nm-news-meta">
          <ImportanceMark value={item.importance} />
          <span>{category(item.category)}</span>
          {item.assets.slice(0, 4).map((asset) => (
            <span key={asset} className="nm-ticker">
              {asset}
            </span>
          ))}
          {read ? <span className="nm-read-label">Прочитано</span> : null}
        </div>
      </div>
      <div className="nm-news-actions">
        <button
          className={`nm-icon-button ${saved ? "is-active" : ""}`}
          onClick={() => reading.toggleSave(item)}
          aria-label={
            saved ? "Убрать новость из избранного" : "Сохранить новость"
          }
          aria-pressed={saved}
        >
          <Bookmark size={16} fill={saved ? "currentColor" : "none"} />
        </button>
        <button
          className="nm-icon-button"
          onClick={() => reading.markRead(item.id)}
          aria-label="Отметить новость прочитанной"
          disabled={read}
        >
          <Check size={16} />
        </button>
      </div>
    </article>
  );
}
export function LiveNews({
  reading,
  onSelect,
  onSources,
  zone,
}: {
  reading: Reading;
  onSelect: (item: TerminalNewsItem) => void;
  onSources: () => void;
  zone: string;
}) {
  const [searchParams] = useSearchParams();
  const [filters, setFilters] = useState<NewsFilters>(() => ({
    ...DEFAULT_FILTERS,
    q: searchParams.get("q") ?? "",
    asset: searchParams.get("asset") ?? "",
  }));
  const [cursorStack, setCursorStack] = useState<string[]>([]),
    [savedOnly, setSavedOnly] = useState(false),
    [auto, setAuto] = useState(true),
    [hours, setHours] = useState("168");
  const [minute, setMinute] = useState(() => Math.floor(Date.now() / 60000));
  useEffect(() => {
    if (!auto) return;
    const update = () => {
      if (!document.hidden) setMinute(Math.floor(Date.now() / 60000));
    };
    const timer = window.setInterval(update, 60_000);
    document.addEventListener("visibilitychange", update);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", update);
    };
  }, [auto]);
  const deferredSearch = useDeferredValue(filters.q);
  const query = useTerminalNews(
    {
      ...filters,
      q: deferredSearch,
      from: rollingNewsFrom(hours, minute),
      cursor: cursorStack.at(-1) ?? "",
    },
    true,
    auto,
  );
  function change(key: keyof NewsFilters, value: string) {
    setFilters((p) => ({ ...p, [key]: value }));
    setCursorStack([]);
  }
  const items = savedOnly
    ? (reading.state.articles ?? [])
        .filter(
          (item) =>
            reading.state.saved.includes(item.id) &&
            (!deferredSearch ||
              `${item.title} ${item.summary ?? ""}`
                .toLowerCase()
                .includes(deferredSearch.toLowerCase())),
        )
        .sort((a, b) => Date.parse(b.publishedAt) - Date.parse(a.publishedAt))
    : (query.data?.items ?? []);
  return (
    <div>
      <div className="nm-toolbar">
        <label className="nm-search">
          <Search size={16} aria-hidden="true" />
          <input
            aria-label="Поиск новостей"
            placeholder="Поиск по событиям, активам, темам…"
            maxLength={200}
            value={filters.q}
            onChange={(e) => change("q", e.target.value)}
          />
        </label>
        <button
          className={`nm-button ${savedOnly ? "is-active" : ""}`}
          aria-pressed={savedOnly}
          onClick={() => {
            setSavedOnly((v) => !v);
            setCursorStack([]);
          }}
        >
          <Bookmark size={14} />
          Избранное ({reading.state.saved.length})
        </button>
        <button
          className="nm-button"
          aria-pressed={auto}
          onClick={() => setAuto((v) => !v)}
        >
          Автообновление: {auto ? "вкл" : "выкл"}
        </button>
      </div>
      {!savedOnly ? (
        <div className="nm-filters">
          <label>
            Категория
            <select
              value={filters.category}
              onChange={(e) => change("category", e.target.value)}
            >
              <option value="all">Все новости</option>
              {["crypto", "macro", "markets", "world"].map((value) => (
                <option key={value} value={value}>
                  {category(value)}
                </option>
              ))}
            </select>
          </label>
          <label>
            Источник
            <select
              value={filters.source}
              onChange={(e) => change("source", e.target.value)}
            >
              <option value="">Все источники</option>
              {query.data?.sources
                .filter((source) => source.enabled)
                .map((source) => (
                  <option value={source.id} key={source.id}>
                    {source.name}
                  </option>
                ))}
            </select>
          </label>
          <label>
            Тикер
            <input
              placeholder="Например, BTC"
              aria-label="Фильтр по тикеру"
              value={filters.asset}
              maxLength={20}
              onChange={(e) => change("asset", e.target.value.toUpperCase())}
            />
          </label>
          <label>
            Важность
            <select
              value={filters.importance}
              onChange={(e) => change("importance", e.target.value)}
            >
              <option value="all">Любая</option>
              <option value="high">Высокая</option>
              <option value="medium">Средняя</option>
              <option value="low">Низкая</option>
            </select>
          </label>
          <label>
            Период
            <select
              value={hours}
              onChange={(e) => {
                const value = e.target.value;
                setHours(value);
                setMinute(Math.floor(Date.now() / 60000));
                setCursorStack([]);
              }}
            >
              <option value="">Доступная история</option>
              <option value="1">Последний час</option>
              <option value="24">24 часа</option>
              <option value="168">7 дней</option>
            </select>
          </label>
          <label>
            Порядок
            <select
              value={filters.sort}
              onChange={(e) => change("sort", e.target.value)}
            >
              <option value="latest">Сначала новые</option>
              <option value="important">По важности</option>
            </select>
          </label>
        </div>
      ) : (
        <p className="nm-note">
          Сохранённые публикации на этом устройстве. Поиск действует по
          избранному; фильтры ленты применяются отдельно.
        </p>
      )}
      <div className="nm-feed-header">
        <span>
          {savedOnly
            ? `${items.length} сохранённых публикаций`
            : query.isFetching
              ? "Синхронизация…"
              : `${query.data?.total ?? 0} публикаций в выборке`}
        </span>
        <button className="nm-text-link" onClick={onSources}>
          Источники и условия →
        </button>
      </div>
      <section className="nm-panel nm-feed">
        {!savedOnly && query.isPending ? (
          <Skeleton rows={7} />
        ) : !savedOnly && query.isError ? (
          <Empty
            title="Не удалось загрузить ленту"
            retry={() => {
              if (cursorStack.length) setCursorStack([]);
              else void query.refetch();
            }}
          >
            Проверьте соединение. При устаревшем указателе повторная загрузка
            вернёт первую страницу. Сохранённые публикации доступны в избранном.
          </Empty>
        ) : !items.length ? (
          <Empty
            title={
              query.data?.syncing && !savedOnly
                ? "Источники синхронизируются"
                : savedOnly
                  ? "Здесь будут сохранённые новости"
                  : "Публикаций по этим условиям нет"
            }
          >
            {savedOnly
              ? "Нажмите значок закладки рядом с новостью."
              : "Измените фильтры или дождитесь следующего обновления. Недоступные источники отмечены в панели качества данных."}
          </Empty>
        ) : (
          items.map((item) => (
            <NewsRow
              key={item.id}
              item={item}
              reading={reading}
              onSelect={onSelect}
              zone={zone}
            />
          ))
        )}
      </section>
      {!savedOnly ? (
        <div className="nm-pagination">
          <button
            className="nm-button"
            disabled={!cursorStack.length}
            onClick={() => setCursorStack((p) => p.slice(0, -1))}
          >
            ← Предыдущая
          </button>
          <span>
            Страница {cursorStack.length + 1} · обновлено{" "}
            {stamp(query.data?.asOf, zone)}
          </span>
          <button
            className="nm-button"
            disabled={!query.data?.nextCursor || query.isFetching}
            onClick={() => {
              const next = query.data?.nextCursor;
              if (next) setCursorStack((p) => [...p, next]);
            }}
          >
            Следующая →
          </button>
        </div>
      ) : null}
    </div>
  );
}
