import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import {
  useNewsCalendar,
  useNewsItems,
  useNewsSummary,
  type NewsItem,
} from "../../../../shared/news/newsQueries";

const EMPTY_NEWS_ITEMS: NewsItem[] = [];

function currentDateKey() {
  const date = new Date();
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

export function MiniNewsWidget() {
  const [selectedDay, setSelectedDay] = useState(currentDateKey);
  const [isDayModalOpen, setIsDayModalOpen] = useState(false);
  const tzOffset = new Date().getTimezoneOffset();
  const newsQuery = useNewsItems({ category: "crypto", limit: 6, tzOffset });
  const summaryQuery = useNewsSummary();
  const items = newsQuery.data ?? EMPTY_NEWS_ITEMS;
  const loading = newsQuery.isPending;
  const error = newsQuery.error instanceof Error ? newsQuery.error.message : "";
  const summaryText = summaryQuery.data?.summary ?? "";
  const summaryUpdatedAt = summaryQuery.data?.periodEnd ?? "";
  const summaryLoading = summaryQuery.isPending;

  const calendarDays = useMemo(() => {
    const grouped = new Map<string, { label: string; items: NewsItem[] }>();
    for (const item of items) {
      const d = new Date(item.publishedAt);
      if (Number.isNaN(d.getTime())) continue;
      const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
      const label = d.toLocaleDateString();
      if (!grouped.has(key)) {
        grouped.set(key, { label, items: [] });
      }
      grouped.get(key)?.items.push(item);
    }
    return Array.from(grouped.entries())
      .sort((a, b) => (a[0] < b[0] ? 1 : -1))
      .map(([key, value]) => ({ key, label: value.label, items: value.items }))
      .slice(0, 7);
  }, [items]);

  const daySelector = useMemo(() => {
    const today = new Date();
    const days: Array<{ key: string; dayNum: string; monthShort: string; fullLabel: string; isToday: boolean }> = [];
    for (let offset = -3; offset <= 6; offset += 1) {
      const d = new Date(today);
      d.setDate(today.getDate() + offset);
      const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
      const isToday = offset === 0;
      days.push({
        key,
        dayNum: String(d.getDate()),
        monthShort: d.toLocaleDateString(undefined, { month: "short" }),
        fullLabel: d.toLocaleDateString(),
        isToday,
      });
    }
    return days;
  }, []);
  const calendarQuery = useNewsCalendar({
    category: "crypto",
    from: daySelector[0]?.key ?? "",
    to: daySelector[daySelector.length - 1]?.key ?? "",
    tzOffset,
  });
  const selectedDayNewsQuery = useNewsItems({
    category: "crypto",
    limit: 200,
    date: selectedDay,
    tzOffset,
    enabled: isDayModalOpen && Boolean(selectedDay),
    refetchInterval: false,
  });
  const selectedDaySummaryQuery = useNewsSummary(
    selectedDay,
    isDayModalOpen && Boolean(selectedDay),
  );
  const calendarCounts = calendarQuery.data ?? {};
  const selectedDayItems = selectedDayNewsQuery.data ?? EMPTY_NEWS_ITEMS;
  const selectedDayLoading = selectedDayNewsQuery.isPending && selectedDayNewsQuery.isFetching;
  const selectedDaySummary = selectedDaySummaryQuery.data?.summary ?? "";
  const selectedDaySummaryLoading =
    selectedDaySummaryQuery.isPending && selectedDaySummaryQuery.isFetching;

  const selectedDayEntries = useMemo(() => {
    return [...selectedDayItems].sort((a, b) => {
      const aa = Date.parse(a.publishedAt);
      const bb = Date.parse(b.publishedAt);
      if (!Number.isFinite(aa) || !Number.isFinite(bb)) return 0;
      return bb - aa;
    });
  }, [selectedDayItems]);

  useEffect(() => {
    if (!isDayModalOpen || typeof document === "undefined") return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previousOverflow;
    };
  }, [isDayModalOpen]);

  function formatDateTime(value: string) {
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return value;
    return d.toLocaleString();
  }

  const selectedDayLabel =
    daySelector.find((day) => day.key === selectedDay)?.fullLabel ??
    calendarDays.find((day) => day.key === selectedDay)?.label ??
    selectedDay;

  return (
    <section className="rounded-2xl border border-border bg-card p-5 md:p-6 min-w-0">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold tracking-tight">Свежие новости</h3>
        <span className="text-xs text-muted-foreground">новые за день</span>
      </div>

      {error ? (
        <div className="mt-3 rounded-xl border border-rose-400/30 bg-rose-500/10 px-3 py-2 text-xs text-destructive">
          Не удалось загрузить новости.
        </div>
      ) : null}

      <div className="mt-3 grid gap-3 xl:grid-cols-[minmax(0,1fr)_minmax(260px,31%)]">
        <div className="space-y-3">
          {loading ? (
            <div className="text-xs text-muted-foreground">Загружаем...</div>
          ) : null}
          {!loading && items.length === 0 ? (
            <div className="text-xs text-muted-foreground">Пока новостей нет.</div>
          ) : null}
          {items.map((item) => (
            <a
              key={item.id}
              href={item.url}
              target="_blank"
              rel="noreferrer"
              className="block rounded-xl border border-border bg-background px-3 py-2 text-sm text-foreground hover:bg-card"
            >
              <div className="font-semibold text-foreground line-clamp-1">{item.title}</div>
              {item.summary ? (
                <div className="mt-1 text-xs text-muted-foreground line-clamp-2">{item.summary}</div>
              ) : null}
            </a>
          ))}
        </div>

        <aside className="rounded-2xl border border-border bg-surface-raised p-4 min-h-[220px] flex flex-col gap-3">
          <div className="text-[11px] uppercase tracking-wide text-primary">Сводка от ИИ</div>
          <div className="mt-2 text-sm font-semibold text-foreground">12 часов</div>
          <p className="mt-1 max-h-[220px] overflow-y-auto pr-1 text-xs text-muted-foreground whitespace-pre-line break-words site-scrollbar">
            {summaryLoading
              ? "Готовим свежую сводку..."
              : summaryText || "Сводка появится после первой генерации."}
          </p>
          {summaryUpdatedAt ? (
            <div className="text-[10px] text-muted-foreground">Обновлено: {formatDateTime(summaryUpdatedAt)}</div>
          ) : null}
          
          <div className="rounded-xl border border-border bg-surface-raised p-3">
            <div className="text-[10px] uppercase tracking-wide text-primary">Мини-календарь новостей</div>
            <div className="mt-2 grid grid-cols-5 gap-1.5">
              {daySelector.length > 0 ? (
                daySelector.map((day) => (
                  <button
                    key={day.key}
                    type="button"
                    onClick={() => {
                      setSelectedDay(day.key);
                      setIsDayModalOpen(true);
                    }}
                    className={[
                      "relative rounded-md border px-1 py-1 text-center transition",
                      selectedDay === day.key
                        ? "border-border-strong bg-accent text-primary"
                        : "border-border bg-background text-muted-foreground hover:bg-secondary",
                    ].join(" ")}
                    title={day.fullLabel}
                  >
                    <div className="text-[10px] leading-none opacity-80">{day.monthShort}</div>
                    <div className="mt-0.5 text-[11px] font-semibold leading-none">{day.dayNum}</div>
                    {(calendarCounts[day.key] ?? 0) > 0 ? (
                      <span className="absolute -bottom-px left-1/2 h-0.5 w-5 -translate-x-1/2 rounded-full bg-emerald-300" />
                    ) : null}
                  </button>
                ))
              ) : (
                <div className="text-[11px] text-muted-foreground">События календаря появятся после загрузки новостей.</div>
              )}
            </div>
          </div>
        </aside>
      </div>

      {isDayModalOpen && typeof document !== "undefined"
        ? createPortal(
            <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-scrim px-4 py-6">
              <button
                className="absolute inset-0 cursor-default"
                onClick={() => setIsDayModalOpen(false)}
                aria-label="Закрыть окно новостей"
              />
              <div
                className="site-scrollbar relative max-h-[76vh] w-full max-w-2xl overflow-y-auto overscroll-contain rounded-2xl border border-border bg-surface-raised p-4 [box-shadow:var(--shadow-overlay)] md:p-5"
                onWheel={(event) => event.stopPropagation()}
                onTouchMove={(event) => event.stopPropagation()}
              >
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <div className="text-xs text-primary">Новости за</div>
                    <div className="text-2xl font-semibold text-foreground">{selectedDayLabel}</div>
                  </div>
                  <button
                    type="button"
                    onClick={() => setIsDayModalOpen(false)}
                    className="rounded-xl border border-border bg-card px-3 py-1.5 text-xs text-foreground hover:bg-secondary"
                  >
                    Закрыть
                  </button>
                </div>

                <div className="mt-4 rounded-2xl border border-border bg-background p-3.5">
                  <div className="text-[11px] uppercase tracking-[0.18em] text-primary">Сводка по крипте</div>
                  <div className="mt-2 text-lg font-semibold text-foreground">Сводка за выбранный день</div>
                  <div className="mt-2 text-sm leading-6 text-muted-foreground">
                    {selectedDaySummaryLoading
                      ? "Готовим сводку за выбранный день..."
                      : selectedDaySummary || "Сводка за этот день пока не готова."}
                  </div>
                </div>

                <div className="mt-3 space-y-3">
                  {selectedDayLoading ? (
                    <div className="rounded-2xl border border-border bg-background p-4 text-sm text-muted-foreground">
                      Загружаем новости за выбранный день...
                    </div>
                  ) : selectedDayEntries.length > 0 ? (
                    selectedDayEntries.map((entry) => (
                      <a
                        key={`modal-day-${entry.id}`}
                        href={entry.url}
                        target="_blank"
                        rel="noreferrer"
                        className="block rounded-2xl border border-border bg-background p-3.5 hover:bg-card"
                      >
                        <div className="text-xs text-muted-foreground">{formatDateTime(entry.publishedAt)}</div>
                        {entry.calendarOnly ? (
                          <div className="mt-1 text-[10px] uppercase tracking-wide text-primary">
                            Событие биржи
                          </div>
                        ) : null}
                        <div className="mt-1.5 text-lg font-semibold text-foreground">{entry.title}</div>
                        {entry.summary ? (
                          <div className="mt-2 text-sm leading-6 text-muted-foreground">{entry.summary}</div>
                        ) : null}
                      </a>
                    ))
                  ) : (
                    <div className="rounded-2xl border border-border bg-background p-4 text-sm text-muted-foreground">
                      За выбранный день новостей пока нет.
                    </div>
                  )}
                </div>
              </div>
            </div>,
            document.body,
          )
        : null}
    </section>
  );
}
