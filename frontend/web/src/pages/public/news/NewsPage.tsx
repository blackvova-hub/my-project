import { useDeferredValue, useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import {
  useImportantEvents,
  useNewsCalendar,
  useNewsItems,
  useNewsSummary,
  type ImportantEvent,
  type NewsItem,
} from "../../../shared/news/newsQueries";

type FilterKey = "crypto";
const EMPTY_NEWS_ITEMS: NewsItem[] = [];

const CHIP_LABELS: Array<{ key: string; label: string; match: RegExp }> = [
  { key: "btc", label: "BTC", match: /\b(btc|bitcoin)\b/i },
  { key: "eth", label: "ETH", match: /\b(eth|ethereum)\b/i },
  { key: "sol", label: "Solana", match: /\b(solana|sol)\b/i },
  { key: "defi", label: "DeFi", match: /\bdefi\b/i },
  { key: "listing", label: "Листинги", match: /\b(listing|листинг|листинги)\b/i },
  { key: "exchange", label: "Биржи", match: /\b(binance|bybit|okx|бирж)\b/i },
  { key: "economy", label: "Экономика", match: /\b(econom|инфляц|ставк|gdp|рынок)\b/i },
  { key: "tech", label: "Технологии", match: /\b(tech|ai|технолог)\b/i },
  { key: "reg", label: "Регуляторы", match: /\b(sec|cftc|регулятор)\b/i },
  { key: "macro", label: "ФРС", match: /\b(fed|fomc|фрс)\b/i },
];

export default function NewsPage() {
  const tzOffset = new Date().getTimezoneOffset();
  const activeFilter: FilterKey = "crypto";
  const [activeChip, setActiveChip] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [visibleCount, setVisibleCount] = useState(10);
  const [monthOffset, setMonthOffset] = useState(0);
  const [selectedDate, setSelectedDate] = useState<string | null>(null);
  const [showScrollTop, setShowScrollTop] = useState(false);
  const deferredSearch = useDeferredValue(search);

  const monthMeta = useMemo(() => {
    const now = new Date();
    const base = new Date(now.getFullYear(), now.getMonth() + monthOffset, 1);
    const start = new Date(base.getFullYear(), base.getMonth(), 1);
    const end = new Date(base.getFullYear(), base.getMonth() + 1, 0);
    return { base, start, end };
  }, [monthOffset]);
  const newsQuery = useNewsItems({ category: "market", limit: 200, tzOffset });
  const calendarQuery = useNewsCalendar({
    category: "market",
    from: formatDateKey(monthMeta.start),
    to: formatDateKey(monthMeta.end),
    tzOffset,
  });
  const dayNewsQuery = useNewsItems({
    category: "market",
    limit: 200,
    date: selectedDate,
    tzOffset,
    enabled: Boolean(selectedDate),
    refetchInterval: false,
  });
  const summaryQuery = useNewsSummary();
  const daySummaryQuery = useNewsSummary(selectedDate, Boolean(selectedDate));
  const importantEventsQuery = useImportantEvents();
  const cryptoItems = newsQuery.data ?? EMPTY_NEWS_ITEMS;
  const calendarCounts = calendarQuery.data ?? {};
  const dayItems = dayNewsQuery.data ?? [];
  const dayLoading = dayNewsQuery.isPending && dayNewsQuery.isFetching;
  const daySummaryText = daySummaryQuery.data?.summary ?? "";
  const daySummaryUpdatedAt = daySummaryQuery.data?.periodEnd ?? "";
  const daySummaryLoading = daySummaryQuery.isPending && daySummaryQuery.isFetching;
  const loading = newsQuery.isPending;
  const error = newsQuery.error instanceof Error ? newsQuery.error.message : null;
  const summaryText = summaryQuery.data?.summary ?? "";
  const summaryUpdatedAt = summaryQuery.data?.periodEnd ?? "";
  const summaryLoading = summaryQuery.isPending;
  const importantEvents = importantEventsQuery.data ?? [];
  const importantEventsLoading = importantEventsQuery.isPending;

  useEffect(() => {
    const onScroll = () => {
      setShowScrollTop(window.scrollY > 600);
    };
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  const filteredItems = useMemo(() => {
    const q = deferredSearch.trim().toLowerCase();
    const items: NewsItem[] = cryptoItems.filter((item) => {
      if (!q) return true;
      const hay = `${item.title} ${item.summary ?? ""} ${item.source}`.toLowerCase();
      return hay.includes(q);
    });

    const withChip = activeChip
      ? items.filter((item) => tagsForItem(item).some((tag) => tag.key === activeChip))
      : items;

    return withChip.sort((a, b) => {
      const at = Date.parse(a.publishedAt || "");
      const bt = Date.parse(b.publishedAt || "");
      return (Number.isNaN(bt) ? 0 : bt) - (Number.isNaN(at) ? 0 : at);
    });
  }, [activeChip, cryptoItems, deferredSearch]);

  const featuredItem = filteredItems[0];
  const secondaryItems = featuredItem ? filteredItems.slice(1, 5) : filteredItems.slice(0, 4);
  const remainingOffset = featuredItem ? 5 : 4;
  const remainingItems = filteredItems.slice(remainingOffset, remainingOffset + visibleCount);
  const remainingTotal = Math.max(filteredItems.length - remainingOffset, 0);
  const hasMoreRemaining = remainingItems.length < remainingTotal;
  const focusChips = useMemo(() => buildFocusChips(filteredItems), [filteredItems]);

  return (
  <div className="mx-auto max-w-[1450px] px-2 py-8 md:px-4 md:py-10">
      {activeFilter === "crypto" ? (
        <div className="mt-5 rounded-3xl border border-border bg-card p-4 md:p-5">
          <div className="text-xs uppercase tracking-[0.2em] text-primary">Сводка от ИИ</div>
          <div className="mt-2 text-lg font-semibold text-foreground">Краткая сводка за последние 12 часов</div>
          <p className="mt-2 text-sm text-muted-foreground whitespace-pre-line">
            {summaryLoading
              ? "Готовим свежую сводку..."
              : summaryText || "Сводка появится после первой генерации."}
          </p>
          {summaryUpdatedAt ? (
            <div className="mt-2 text-[11px] text-muted-foreground">
              Обновлено: {formatDateTime(summaryUpdatedAt)}
            </div>
          ) : null}
        </div>
      ) : null}

      {error ? (
        <div className="mt-6 rounded-2xl border border-rose-400/30 bg-rose-500/10 px-4 py-3 text-sm text-destructive">
          {error}
        </div>
      ) : null}

  <div className="mt-6 grid gap-6 lg:grid-cols-[1fr_360px] xl:grid-cols-[1fr_380px]">
        <div className="space-y-5">
          {loading && filteredItems.length === 0 ? (
            <div className="grid gap-4 md:grid-cols-2">
              {Array.from({ length: 6 }).map((_, idx) => (
                <div key={idx} className="h-44 rounded-2xl border border-border bg-card" />
              ))}
            </div>
          ) : (
            <>
              {featuredItem ? (
                <article className="rounded-[28px] border border-border bg-card p-5">
                  <div className="grid gap-5 md:grid-cols-[1.2fr_1fr]">
                    <div>
                      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                        <span>{featuredItem.source || "Источник"}</span>
                        <span className="h-1 w-1 rounded-full bg-accent" />
                        <span>{formatDateTime(featuredItem.publishedAt)}</span>
                        {isBreaking(featuredItem.publishedAt) ? (
                          <span className="rounded-full border border-rose-400/40 bg-rose-500/15 px-2 py-0.5 text-[10px] text-destructive">
                            Последнее
                          </span>
                        ) : null}
                      </div>
                      <a
                        href={featuredItem.url}
                        target="_blank"
                        rel="noreferrer"
                        className="mt-3 block text-2xl font-semibold text-foreground hover:text-foreground"
                      >
                        {featuredItem.title}
                      </a>
                      {featuredItem.summary ? (
                        <p className="mt-3 text-sm text-muted-foreground line-clamp-4">{featuredItem.summary}</p>
                      ) : null}
                      <div className="mt-4 flex flex-wrap items-center gap-2">
                        {tagsForItem(featuredItem).slice(0, 5).map((tag) => (
                          <span
                            key={tag.key}
                            className="rounded-full border border-border bg-card px-3 py-1 text-[11px] text-muted-foreground"
                          >
                            {tag.label}
                          </span>
                        ))}
                      </div>
                      <div className="mt-5">
                        <a
                          href={featuredItem.url}
                          target="_blank"
                          rel="noreferrer"
                          className="inline-flex items-center gap-2 rounded-full border border-border-strong bg-secondary px-5 py-2 text-xs font-semibold text-secondary-foreground hover:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                        >
                          Читать источник
                          <ExternalIcon />
                        </a>
                      </div>
                    </div>
                    <div>
                      {featuredItem.image ? (
                        <img
                          src={featuredItem.image}
                          alt={featuredItem.title}
                          className="h-full min-h-[220px] w-full rounded-2xl object-cover"
                        />
                      ) : (
                        <div
                          className="flex h-full min-h-[220px] w-full items-center justify-center rounded-2xl bg-secondary"
                          aria-hidden="true"
                        >
                          <span className="select-none text-6xl font-black tracking-[-0.08em] text-muted-foreground md:text-7xl">
                            SL
                          </span>
                        </div>
                      )}
                    </div>
                  </div>
                </article>
              ) : null}

              <div className="grid gap-5 md:grid-cols-2">
                {secondaryItems.map((item) => (
                  <article
                    key={item.id}
                    className="rounded-3xl border border-border bg-card p-4 md:p-5"
                  >
                    <div className="flex flex-col gap-4">
                      <div className="flex items-center gap-3">
                        <div className="h-16 w-16 shrink-0 overflow-hidden rounded-2xl bg-card">
                          {item.image ? (
                            <img
                              src={item.image}
                              alt={item.title}
                              loading="lazy"
                              decoding="async"
                              className="h-full w-full object-cover"
                            />
                          ) : (
                            <div
                              className="flex h-full w-full items-center justify-center bg-secondary"
                              aria-hidden="true"
                            >
                              <span className="select-none text-xl font-black tracking-[-0.08em] text-muted-foreground">
                                SL
                              </span>
                            </div>
                          )}
                        </div>
                        <div>
                          <div className="flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
                            <span>{item.source || "Источник"}</span>
                            <span className="h-1 w-1 rounded-full bg-accent" />
                            <span>{formatDateTime(item.publishedAt)}</span>
                          </div>
                          <a
                            href={item.url}
                            target="_blank"
                            rel="noreferrer"
                            className="mt-1 block text-base font-semibold text-foreground hover:text-foreground"
                          >
                            {item.title}
                          </a>
                        </div>
                      </div>
                      {item.summary ? (
                        <p className="text-sm text-muted-foreground line-clamp-3">{item.summary}</p>
                      ) : null}
                      <div className="flex flex-wrap items-center gap-2">
                        {tagsForItem(item).slice(0, 3).map((tag) => (
                          <span
                            key={tag.key}
                            className="rounded-full border border-border bg-card px-3 py-1 text-[11px] text-muted-foreground"
                          >
                            {tag.label}
                          </span>
                        ))}
                      </div>
                      <div className="flex items-center justify-between">
                        <span className="text-[11px] text-muted-foreground">{formatTime(item.publishedAt)}</span>
                        <a
                          href={item.url}
                          target="_blank"
                          rel="noreferrer"
                          className="inline-flex items-center gap-2 rounded-full border border-border bg-card px-4 py-1.5 text-[11px] font-semibold text-foreground hover:bg-secondary"
                        >
                          Читать
                          <ExternalIcon />
                        </a>
                      </div>
                    </div>
                  </article>
                ))}
              </div>

              {!filteredItems.length && !loading ? (
                <div className="rounded-2xl border border-border bg-card p-6 text-sm text-muted-foreground">
                  Пока новостей нет. Загляни позже.
                </div>
              ) : null}

              {remainingTotal > 0 ? (
                <div className="mt-6 space-y-3">
                  <div className="space-y-3">
                    {remainingItems.map((item) => (
                      <article
                        key={item.id}
                        className="rounded-2xl border border-border bg-card px-4 py-3"
                      >
                        <div className="flex items-center justify-between gap-4">
                          <div>
                            <div className="text-[11px] text-muted-foreground">
                              {item.source || "Источник"} · {formatDateTime(item.publishedAt)}
                            </div>
                            <a
                              href={item.url}
                              target="_blank"
                              rel="noreferrer"
                              className="mt-1 block text-sm font-semibold text-foreground hover:text-foreground"
                            >
                              {item.title}
                            </a>
                          </div>
                          <a
                            href={item.url}
                            target="_blank"
                            rel="noreferrer"
                            className="shrink-0 rounded-full border border-border bg-card px-3 py-1.5 text-[11px] font-semibold text-foreground hover:bg-secondary"
                          >
                            Читать
                          </a>
                        </div>
                      </article>
                    ))}
                  </div>

                  {hasMoreRemaining ? (
                    <div className="flex justify-center pt-2">
                      <button
                        type="button"
                        onClick={() => setVisibleCount((prev) => prev + 10)}
                        className="rounded-full border border-border bg-card px-5 py-2 text-xs font-semibold text-foreground hover:bg-secondary"
                      >
                        Показать ещё 10
                      </button>
                    </div>
                  ) : null}
                </div>
              ) : null}
            </>
          )}
        </div>

        <aside className="self-start space-y-4 rounded-3xl border border-border bg-card p-4">
          <div>
            <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Поиск</div>
            <div className="mt-2 flex items-center gap-2 rounded-2xl border border-border bg-background px-4 py-2 text-sm text-foreground focus-within:border-ring">
              <SearchIcon />
              <input
                value={search}
                onChange={(e) => {
                  setSearch(e.target.value);
                  setVisibleCount(10);
                }}
                placeholder="Поиск по новостям..."
                className="w-full bg-transparent outline-none placeholder:text-muted-foreground"
              />
            </div>
          </div>

          <div>
            <div className="text-xs uppercase tracking-[0.2em] text-muted-foreground">Сегодня в фокусе</div>
            <div className="mt-3 flex flex-wrap items-center gap-2">
              {focusChips.map((chip) => (
                <ChipButton
                  key={chip.key}
                  active={activeChip === chip.key}
                  onClick={() => {
                    setActiveChip(activeChip === chip.key ? null : chip.key);
                    setVisibleCount(10);
                  }}
                  variant="dark"
                >
                  {chip.label}
                </ChipButton>
              ))}
            </div>
          </div>

          <div className="h-px bg-secondary" />

          <div className="flex items-center justify-between">
            <button
              type="button"
              onClick={() => setMonthOffset((prev) => Math.max(prev - 1, -1))}
              className="rounded-full border border-border bg-card px-3 py-1 text-xs text-muted-foreground hover:bg-secondary"
              disabled={monthOffset <= -1}
            >
              ←
            </button>
            <div className="text-sm font-semibold text-foreground">
              {monthMeta.base.toLocaleDateString("ru-RU", { month: "long", year: "numeric" })}
            </div>
            <button
              type="button"
              onClick={() => setMonthOffset((prev) => Math.min(prev + 1, 2))}
              className="rounded-full border border-border bg-card px-3 py-1 text-xs text-muted-foreground hover:bg-secondary"
              disabled={monthOffset >= 2}
            >
              →
            </button>
          </div>

          <div className="mt-2 grid grid-cols-7 gap-1 text-[11px] text-muted-foreground">
            {WEEK_LABELS.map((d) => (
              <div key={d} className="text-center">
                {d}
              </div>
            ))}
          </div>

          <div className="mt-2 grid grid-cols-7 gap-1">
            {buildCalendarGrid(monthMeta.start).map((day, index) => {
              if (!day) {
                return <div key={`empty-${index}`} />;
              }
              const key = formatDateKey(day.date);
              const count = calendarCounts[key] ?? 0;
              const isSelected = selectedDate === key;
              const isToday = isSameDay(day.date, new Date());
              const baseClasses = isSelected ? "" : day.isCurrentMonth ? "text-foreground" : "text-muted-foreground";
              return (
                <button
                  key={key}
                  type="button"
                  onClick={() => setSelectedDate(isSelected ? null : key)}
                  className={
                    "relative h-9 rounded-xl text-xs font-semibold transition " +
                    (isSelected
                      ? "bg-primary text-primary-foreground"
                      : count > 0
                        ? "bg-secondary hover:bg-accent"
                        : "bg-card hover:bg-secondary") +
                    " " +
                    baseClasses
                  }
                >
                  <span>{day.date.getDate()}</span>
                  {isToday ? (
                    <span className="absolute -top-1 -right-1 h-2 w-2 rounded-full bg-emerald-400" />
                  ) : null}
                  {count > 0 ? (
                    <span className="absolute -bottom-1 left-1/2 h-1 w-6 -translate-x-1/2 rounded-full bg-emerald-400/70" />
                  ) : null}
                </button>
              );
            })}
          </div>

          <div className="h-px bg-secondary" />

          <section aria-labelledby="important-events-title">
            <div className="flex items-center justify-between gap-3">
              <div id="important-events-title" className="text-sm font-semibold text-foreground">
                Важные события
              </div>
              <span className="text-[10px] uppercase text-muted-foreground">24 часа</span>
            </div>

            {importantEventsLoading && importantEvents.length === 0 ? (
              <div className="mt-3 space-y-3">
                {Array.from({ length: 3 }).map((_, index) => (
                  <div key={index} className="space-y-2 border-t border-border pt-3 first:border-t-0 first:pt-0">
                    <div className="h-3 w-24 bg-secondary" />
                    <div className="h-4 w-full bg-secondary" />
                    <div className="h-3 w-4/5 bg-card" />
                  </div>
                ))}
              </div>
            ) : null}

            {!importantEventsLoading && importantEvents.length === 0 ? (
              <div className="mt-3 border-t border-border pt-3 text-xs text-muted-foreground">
                За последние сутки значимых рыночных событий не зафиксировано.
              </div>
            ) : null}

            {importantEvents.length > 0 ? (
              <div className="mt-3 max-h-[430px] divide-y divide-border overflow-y-auto pr-1">
                {importantEvents.map((event) => <ImportantEventCard key={event.id} event={event} />)}
              </div>
            ) : null}
          </section>

        </aside>
      </div>

      {selectedDate && typeof document !== "undefined"
        ? createPortal(
            <div className="fixed inset-0 z-50 flex items-center justify-center bg-scrim px-4 py-10">
              <div className="w-full max-w-3xl max-h-[90vh] overflow-y-auto rounded-3xl border border-border bg-popover p-5 shadow-sm">
                <div className="flex items-center justify-between">
                  <div>
                    <div className="text-xs text-muted-foreground">Новости за</div>
                    <div className="text-lg font-semibold text-foreground">{selectedDate}</div>
                  </div>
                  <button
                    type="button"
                    onClick={() => setSelectedDate(null)}
                    className="rounded-full border border-border bg-card px-3 py-1 text-xs text-muted-foreground hover:bg-secondary"
                  >
                    Закрыть
                  </button>
                </div>

                <div className="mt-4 space-y-3">
                  <div className="rounded-2xl border border-border bg-card p-4">
                    <div className="text-xs uppercase tracking-[0.2em] text-primary">Сводка по крипте</div>
                    <div className="mt-2 text-base font-semibold text-foreground">
                      Сводка за выбранный день
                    </div>
                    <p className="mt-2 text-sm text-muted-foreground whitespace-pre-line">
                      {daySummaryLoading
                        ? "Загружаем сводку..."
                        : daySummaryText || "Сводка за этот день пока не готова."}
                    </p>
                    {daySummaryUpdatedAt ? (
                      <div className="mt-2 text-[11px] text-muted-foreground">
                        Обновлено: {formatDateTime(daySummaryUpdatedAt)}
                      </div>
                    ) : null}
                  </div>
                  {dayLoading ? <div className="text-sm text-muted-foreground">Загружаем...</div> : null}
                  {!dayLoading && dayItems.length === 0 ? (
                    <div className="rounded-2xl border border-border bg-card p-4 text-sm text-muted-foreground">
                      За этот день новостей нет.
                    </div>
                  ) : null}
                  {dayItems.map((item) => (
                    <div key={item.id} className="rounded-2xl border border-border bg-card p-4">
                      <div className="text-xs text-muted-foreground">
                        {item.source} · {formatDateTime(item.publishedAt)}
                      </div>
                      {item.calendarOnly ? (
                        <div className="mt-1 text-[10px] uppercase tracking-wide text-primary">
                          Событие биржи
                        </div>
                      ) : null}
                      <a
                        href={item.url}
                        target="_blank"
                        rel="noreferrer"
                        className="mt-2 block text-base font-semibold text-foreground hover:text-foreground"
                      >
                        {item.title}
                      </a>
                      {item.summary ? (
                        <p className="mt-2 text-sm text-muted-foreground line-clamp-3">{item.summary}</p>
                      ) : null}
                    </div>
                  ))}
                </div>
              </div>
            </div>,
            document.body
          )
        : null}

      {showScrollTop ? (
        <button
          type="button"
          onClick={() => window.scrollTo({ top: 0, behavior: "smooth" })}
          className="fixed bottom-6 right-6 z-40 rounded-full border border-border-strong bg-secondary px-4 py-2 text-xs font-semibold text-secondary-foreground hover:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          Подняться вверх
        </button>
      ) : null}
    </div>
  );
}

function ChipButton(props: {
  active: boolean;
  onClick: () => void;
  children: string;
  variant?: "light" | "dark";
}) {
  const variant = props.variant ?? "light";
  return (
    <button
      type="button"
      onClick={props.onClick}
      className={
        "rounded-full border px-4 py-2 text-xs font-semibold transition " +
        (props.active
          ? "border-border-strong bg-secondary text-foreground"
          : variant === "dark"
            ? "border-border bg-background text-muted-foreground hover:text-foreground"
            : "border-border bg-card text-muted-foreground hover:text-foreground")
      }
    >
      {props.children}
    </button>
  );
}

function formatDateTime(value: string) {
  if (!value) return "";
  const dt = new Date(value);
  if (Number.isNaN(dt.getTime())) return value;
  return dt.toLocaleString("ru-RU", {
    day: "2-digit",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function ImportantEventCard({ event }: { event: ImportantEvent }) {
  const technicalRows = importantEventTechnicalRows(event);
  const title = repairImportantEventText(event.title);
  const details = repairImportantEventText(event.details)
    .replace(/\s*Это контекст, а не доказанная причина движения\.?/gi, "")
    .trim();
  return (
    <article className="py-3 first:pt-0 last:pb-0">
      <div className="flex items-center justify-between gap-3 text-[10px] uppercase">
        <span className="inline-flex min-w-0 items-center gap-2 font-semibold text-muted-foreground">
          <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-rose-400" />
          <span className="truncate">{importantEventTypeLabel(event.eventType)}</span>
          {event.status === "confirmed" ? (
            <span className="rounded-full border border-border-strong px-1.5 py-0.5 text-[9px] text-primary">подтверждено</span>
          ) : null}
        </span>
        <span className="shrink-0 text-muted-foreground">{formatTime(event.lastSeenAt)}</span>
      </div>
      <div className="mt-1.5 text-sm font-semibold leading-5 text-foreground">{title}</div>
      {details ? <p className="mt-1 text-xs leading-4 text-muted-foreground">{details}</p> : null}
      <div className="mt-2 text-[10px] text-muted-foreground">
		{event.status === "confirmed"
		  ? `Зафиксировано ${formatTime(event.firstSeenAt)} · обновлено ${formatTime(event.lastSeenAt)}`
          : `Завершено · ${formatTime(event.lastSeenAt)}`}
      </div>
      <div className="mt-1 text-[10px] uppercase text-muted-foreground">
        {[event.exchange, event.symbol, event.occurrenceCount > 1 ? `${event.occurrenceCount} обновл.` : ""]
          .filter(Boolean)
          .join(" · ")}
      </div>
      {technicalRows.length > 0 ? (
        <details className="mt-2 text-[11px] text-muted-foreground">
          <summary className="cursor-pointer select-none text-muted-foreground hover:text-muted-foreground">Технические детали</summary>
          <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 border-l border-border pl-3">
            {technicalRows.map(([label, value]) => (
              <div key={label} className="contents">
                <dt>{label}</dt>
                <dd className="min-w-0 break-words text-muted-foreground">{value}</dd>
              </div>
            ))}
          </dl>
        </details>
      ) : null}
    </article>
  );
}

function repairImportantEventText(value: string) {
  return value
    .replaceAll("РјР»СЂРґ", "млрд")
    .replaceAll("РјР»РЅ", "млн")
    .replaceAll("С‚С‹СЃ.", "тыс.")
    .replaceAll("Binance Рё Bybit", "Binance и Bybit");
}

function importantEventTechnicalRows(event: ImportantEvent): Array<[string, string]> {
  const metrics = event.metrics ?? {};
  const rows: Array<[string, string]> = [
	["Факторы", stringList(metrics.factorNames)],
    ["Биржа / рынок", [event.exchange, event.marketType].filter(Boolean).join(" / ")],
    ["Окно", event.windowMinutes ? `${event.windowMinutes} мин.` : ""],
	["Baseline", formatTechnicalNumber(event.baselineValue)],
	["Отношение к baseline", formatTechnicalNumber(event.baselineRatio, "×")],
	["Цена за 5 минут", formatTechnicalNumber(metrics.change5m, "%")],
	["Цена за 10 минут", formatTechnicalNumber(metrics.change10m, "%")],
	["Цена за 15 минут", formatTechnicalNumber(metrics.change15m, "%")],
	["Объём окна", formatTechnicalUSD(metrics.volumeUsd)],
	["Медианный объём", formatTechnicalUSD(metrics.medianVolumeUsd)],
	["Изменение OI", formatTechnicalUSD(metrics.deltaOiUsd)],
	["ΔOI / объём", formatTechnicalNumber(metrics.oiVolumeRatio, "×")],
	["Агрессивный кластер", formatTechnicalUSD(metrics.clusterUsd)],
	["Сделок в кластере", formatTechnicalNumber(metrics.clusterCount)],
	["Влияние на цену", formatTechnicalNumber(metrics.priceImpactPercent, "%")],
	["Подтверждающие биржи", stringList(metrics.confirmed_venues)],
	["Тип объявления", typeof metrics.announcementType === "string" ? metrics.announcementType : ""],
	["Активы", stringList(metrics.assets)],
	["Тема заявления", typeof metrics.statementTopic === "string" ? metrics.statementTopic : ""],
	["On-chain сценарий", typeof metrics.scenario === "string" ? metrics.scenario : ""],
    ["История обновлений", formatUpdateHistory(metrics.updateHistory)],
  ];
  return rows.filter(([, value]) => Boolean(value));
}

function stringList(value: unknown) {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string").join(", ") : "";
}

function formatTechnicalNumber(value: unknown, suffix = "") {
  return typeof value === "number" && Number.isFinite(value) ? `${value.toLocaleString("ru-RU", { maximumFractionDigits: 2 })}${suffix}` : "";
}

function formatTechnicalUSD(value: unknown) {
  return typeof value === "number" && Number.isFinite(value)
    ? new Intl.NumberFormat("ru-RU", { style: "currency", currency: "USD", maximumFractionDigits: 0 }).format(value)
    : "";
}

function formatUpdateHistory(value: unknown) {
  if (!Array.isArray(value)) return "";
  const times = value
    .map((entry) => (entry && typeof entry === "object" && "at" in entry ? String(entry.at) : ""))
    .filter(Boolean)
    .map(formatTime);
  return times.length > 0 ? times.join(" → ") : `${value.length}`;
}

function formatTime(value: string) {
  if (!value) return "";
  const dt = new Date(value);
  if (Number.isNaN(dt.getTime())) return value;
  return dt.toLocaleTimeString("ru-RU", {
    hour: "2-digit",
    minute: "2-digit",
  });
}

function importantEventTypeLabel(eventType: string) {
  switch (eventType) {
	case "price_shock":
	  return "Сильное движение цены";
	case "open_interest_shock":
	  return "Скачок открытого интереса";
	case "large_aggressive_trade":
	  return "Крупная агрессивная сделка";
	case "exchange_announcement":
	  return "Объявление биржи";
	case "onchain_transfer":
	  return "Крупный on-chain перевод";
	case "major_statement":
	  return "Важное официальное заявление";
	default:
	  return "Событие";
  }
}

const WEEK_LABELS: string[] = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];

function formatDateKey(date: Date) {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

function isSameDay(a: Date, b: Date) {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

function buildCalendarGrid(monthStart: Date): Array<{ date: Date; isCurrentMonth: boolean } | null> {
  const year = monthStart.getFullYear();
  const month = monthStart.getMonth();
  const first = new Date(year, month, 1);
  const last = new Date(year, month + 1, 0);
  const startWeekday = (first.getDay() + 6) % 7; // monday=0
  const start = new Date(first);
  start.setDate(first.getDate() - startWeekday);
  const cells: Array<{ date: Date; isCurrentMonth: boolean } | null> = [];
  const cursor = new Date(start);
  while (cursor <= last || cells.length % 7 !== 0) {
    cells.push({ date: new Date(cursor), isCurrentMonth: cursor.getMonth() === month });
    cursor.setDate(cursor.getDate() + 1);
  }
  return cells;
}

function isBreaking(value: string) {
  if (!value) return false;
  const dt = Date.parse(value);
  if (Number.isNaN(dt)) return false;
  return Date.now() - dt < 1000 * 60 * 60 * 2;
}

function tagsForItem(item: NewsItem) {
  const hay = `${item.title} ${item.summary ?? ""} ${item.source}`;
  const tags = CHIP_LABELS.filter((chip) => chip.match.test(hay)).map((chip) => ({
    key: chip.key,
    label: chip.label,
  }));
  if (!tags.length) {
    return [{ key: item.category, label: item.category === "crypto" ? "Крипта" : "Мир" }];
  }
  return tags;
}

function buildFocusChips(items: NewsItem[]) {
  const counts = new Map<string, { key: string; label: string; count: number }>();
  items.forEach((item) => {
    tagsForItem(item).forEach((tag) => {
      const existing = counts.get(tag.key);
      if (existing) {
        existing.count += 1;
      } else {
        counts.set(tag.key, { key: tag.key, label: tag.label, count: 1 });
      }
    });
  });
  return Array.from(counts.values())
    .sort((a, b) => b.count - a.count)
    .slice(0, 8);
}

function SearchIcon() {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className="text-muted-foreground"
    >
      <path
        d="M21 21L16.65 16.65M19 11C19 15.4183 15.4183 19 11 19C6.58172 19 3 15.4183 3 11C3 6.58172 6.58172 3 11 3C15.4183 3 19 6.58172 19 11Z"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function ExternalIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className="text-muted-foreground"
    >
      <path
        d="M14 5H19V10"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M10 14L19 5"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M19 14V19H5V5H10"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
