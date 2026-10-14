import { useMemo, useState } from "react";
import { CalendarDays, ChevronLeft, ChevronRight } from "lucide-react";
import { useEconomicCalendar, type EconomicEvent } from "./api";
import {
  addDays,
  calendarRange,
  countdown,
  dateInZone,
  type CalendarPeriod,
} from "./model";
import { Empty, ImportanceMark, Skeleton } from "./ui";
import {
  category,
  country,
  latestTimestamp,
  number,
  observationDate,
  stamp,
} from "./format";

const PERIODS: { id: CalendarPeriod; title: string }[] = [
  { id: "today", title: "Сегодня" },
  { id: "tomorrow", title: "Завтра" },
  { id: "week", title: "Неделя" },
  { id: "month", title: "Месяц" },
];
const TIME_ZONES = [
  "UTC",
  "America/New_York",
  "Europe/London",
  "Europe/Berlin",
  "Europe/Moscow",
  "Asia/Vladivostok",
  "Asia/Tokyo",
  "Asia/Hong_Kong",
];
function eventStatus(event: EconomicEvent, now: number) {
  if (event.status === "released") return "Опубликовано";
  if (event.status === "elapsed") return "Время публикации прошло";
  if (event.status === "postponed") return "Перенесено";
  if (event.status === "cancelled") return "Отменено";
  return event.scheduledAt && Date.parse(event.scheduledAt) <= now
    ? "Ожидаем публикацию"
    : "Предстоящее";
}
export function CalendarTable({
  events,
  zone,
  now,
  onSelect,
  compact = false,
}: {
  events: EconomicEvent[];
  zone: string;
  now: number;
  onSelect: (event: EconomicEvent) => void;
  compact?: boolean;
}) {
  const groups = new Map<string, EconomicEvent[]>();
  for (const event of events) {
    const date = event.scheduledAt
      ? dateInZone(new Date(event.scheduledAt), zone)
      : event.date;
    const rows = groups.get(date) ?? [];
    rows.push(event);
    groups.set(date, rows);
  }
  return (
    <div className="nm-table-scroll">
      <table
        className={`nm-calendar-table ${compact ? "nm-calendar-compact" : ""}`}
      >
        <thead>
          <tr>
            <th>Время</th>
            <th>Событие</th>
            {!compact ? <th>Факт</th> : null}
            <th title="Прогноз может отсутствовать у официального источника">
              Прогноз
            </th>
            <th>Пред.</th>
            {!compact ? <th>До публикации / статус</th> : null}
          </tr>
        </thead>
        <tbody>
          {[...groups]
            .sort(([a], [b]) => a.localeCompare(b))
            .flatMap(([date, rows]) => [
              <tr className="nm-date-row" key={date}>
                <th colSpan={compact ? 4 : 6}>
                  <CalendarDays size={13} aria-hidden="true" />
                  {observationDate(date)}
                </th>
              </tr>,
              ...rows.map((event) => (
                <tr
                  key={event.id}
                  className={
                    event.status === "released" ? "nm-event-released" : ""
                  }
                >
                  <td>
                    <time dateTime={event.scheduledAt ?? event.date}>
                      {event.scheduledAt
                        ? stamp(event.scheduledAt, zone, false)
                        : "—"}
                    </time>
                    {!event.scheduledAt ? (
                      <small>время не объявлено</small>
                    ) : null}
                  </td>
                  <td>
                    <button
                      className="nm-event-title"
                      onClick={() => onSelect(event)}
                    >
                      <ImportanceMark value={event.importance} />
                      <span>{event.title}</span>
                    </button>
                    <small>
                      {country(event.country)} · {event.currency}
                      {!compact && event.unit ? ` · ${event.unit}` : ""}
                    </small>
                    {compact ? (
                      <small className="nm-countdown">
                        {event.scheduledAt && event.status === "upcoming"
                          ? countdown(event.scheduledAt, now)
                          : eventStatus(event, now)}
                      </small>
                    ) : null}
                  </td>
                  {!compact ? (
                    <td
                      className={
                        event.actual != null && event.forecast != null
                          ? event.actual > event.forecast
                            ? "nm-surprise-positive"
                            : event.actual < event.forecast
                              ? "nm-surprise-negative"
                              : ""
                          : ""
                      }
                      title={
                        event.actual != null && event.forecast != null
                          ? `Отклонение от прогноза: ${number(event.actual - event.forecast)} ${event.unit}. Цвет обозначает знак, а не оценку пользы для рынка.`
                          : "Факт не опубликован или не сопоставлен с этой публикацией"
                      }
                    >
                      {number(event.actual)}
                    </td>
                  ) : null}
                  <td
                    title={
                      event.forecast == null
                        ? "Официальный прогноз недоступен"
                        : event.unit
                    }
                  >
                    {number(event.forecast)}
                  </td>
                  <td title={event.unit}>{number(event.previous)}</td>
                  {!compact ? (
                    <td>
                      <span
                        className={
                          event.status === "upcoming" ? "nm-countdown" : ""
                        }
                      >
                        {event.scheduledAt &&
                        Date.parse(event.scheduledAt) > now &&
                        event.status === "upcoming"
                          ? countdown(event.scheduledAt, now)
                          : eventStatus(event, now)}
                      </span>
                    </td>
                  ) : null}
                </tr>
              )),
            ])}
        </tbody>
      </table>
    </div>
  );
}
export function EconomicCalendar({
  zone,
  setZone,
  now,
  onSelect,
}: {
  zone: string;
  setZone: (zone: string) => void;
  now: number;
  onSelect: (event: EconomicEvent) => void;
}) {
  const [anchor, setAnchor] = useState(() => dateInZone(new Date(), zone)),
    [period, setPeriod] = useState<CalendarPeriod>("week");
  const [filters, setFilters] = useState({
    country: "",
    currency: "",
    category: "",
    importance: "",
    status: "",
  });
  const range = calendarRange(anchor, period, zone);
  const query = useEconomicCalendar(range.from, range.to);
  const events = useMemo(
    () =>
      (query.data?.events ?? [])
        .filter((event) => {
          const date = event.scheduledAt
            ? dateInZone(new Date(event.scheduledAt), zone)
            : event.date;
          return (
            date >= range.first &&
            date <= range.last &&
            (!filters.country || event.country === filters.country) &&
            (!filters.currency || event.currency === filters.currency) &&
            (!filters.category || event.category === filters.category) &&
            (!filters.importance || event.importance === filters.importance) &&
            (!filters.status || event.status === filters.status)
          );
        })
        .sort((a, b) =>
          (a.scheduledAt ?? `${a.date}T23:59:59Z`).localeCompare(
            b.scheduledAt ?? `${b.date}T23:59:59Z`,
          ),
        ),
    [query.data, filters, zone, range.first, range.last],
  );
  function shift(direction: number) {
    if (period === "month") {
      const date = new Date(`${anchor}T12:00:00Z`);
      date.setUTCDate(1);
      date.setUTCMonth(date.getUTCMonth() + direction);
      setAnchor(date.toISOString().slice(0, 10));
    } else
      setAnchor((p) => addDays(p, direction * (period === "week" ? 7 : 1)));
  }
  const options = (key: "country" | "currency" | "category") =>
    [...new Set((query.data?.events ?? []).map((event) => event[key]))].sort();
  return (
    <div>
      <div className="nm-toolbar">
        <div className="nm-segmented" aria-label="Период календаря">
          {PERIODS.map((item) => (
            <button
              key={item.id}
              className={period === item.id ? "is-active" : ""}
              aria-pressed={period === item.id}
              onClick={() => {
                setPeriod(item.id);
                if (item.id === "today" || item.id === "tomorrow")
                  setAnchor(dateInZone(new Date(), zone));
              }}
            >
              {item.title}
            </button>
          ))}
        </div>
        <div className="nm-calendar-navigation">
          <button
            className="nm-icon-button"
            aria-label="Предыдущий период"
            onClick={() => shift(-1)}
          >
            <ChevronLeft size={18} />
          </button>
          <input
            aria-label="Опорная дата календаря"
            type="date"
            value={anchor}
            onChange={(e) => {
              if (e.target.value) setAnchor(e.target.value);
            }}
          />
          <button
            className="nm-icon-button"
            aria-label="Следующий период"
            onClick={() => shift(1)}
          >
            <ChevronRight size={18} />
          </button>
        </div>
        <label className="nm-inline-label">
          Часовой пояс
          <select value={zone} onChange={(e) => setZone(e.target.value)}>
            {[...new Set([zone, ...TIME_ZONES])].map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </select>
        </label>
      </div>
      <div className="nm-filters">
        {(["country", "currency", "category"] as const).map((key) => (
          <label key={key}>
            {key === "country"
              ? "Страна"
              : key === "currency"
                ? "Валюта"
                : "Категория"}
            <select
              value={filters[key]}
              onChange={(e) =>
                setFilters((p) => ({ ...p, [key]: e.target.value }))
              }
            >
              <option value="">Все</option>
              {options(key).map((value) => (
                <option key={value} value={value}>
                  {key === "country"
                    ? country(value)
                    : key === "category"
                      ? category(value)
                      : value}
                </option>
              ))}
            </select>
          </label>
        ))}
        <label>
          Важность
          <select
            value={filters.importance}
            onChange={(e) =>
              setFilters((p) => ({ ...p, importance: e.target.value }))
            }
          >
            <option value="">Любая</option>
            <option value="high">Высокая</option>
            <option value="medium">Средняя</option>
            <option value="low">Низкая</option>
          </select>
        </label>
        <label>
          Статус
          <select
            value={filters.status}
            onChange={(e) =>
              setFilters((p) => ({ ...p, status: e.target.value }))
            }
          >
            <option value="">Все события</option>
            <option value="upcoming">Предстоящие</option>
            <option value="released">Опубликованные</option>
            <option value="elapsed">Время прошло</option>
            <option value="postponed">Перенесённые</option>
            <option value="cancelled">Отменённые</option>
          </select>
        </label>
      </div>
      <div className="nm-feed-header">
        <span>
          {observationDate(range.first)}
          {range.first !== range.last
            ? ` — ${observationDate(range.last)}`
            : ""}{" "}
          · {events.length} событий
        </span>
        <span>
          Синхронизация:{" "}
          {stamp(
            latestTimestamp(
              query.data?.sources.map((source) => source.lastSuccess) ?? [],
            ),
            zone,
          )}
        </span>
      </div>
      <section className="nm-panel">
        {query.isPending ? (
          <Skeleton rows={6} />
        ) : query.isError ? (
          <Empty
            title="Календарь не загрузился"
            retry={() => void query.refetch()}
          >
            Официальные источники могут быть временно недоступны.
          </Empty>
        ) : !events.length ? (
          <Empty title="В выбранном периоде нет подтверждённых событий">
            Измените период или фильтры. Расписание не заполняется
            предполагаемыми датами.
          </Empty>
        ) : (
          <CalendarTable
            events={events}
            zone={zone}
            now={now}
            onSelect={onSelect}
          />
        )}
      </section>
      <p className="nm-note">
        Время показано в {zone}; переходы на летнее время учтены. Для событий
        без точного времени используется дата официального расписания. «—»
        означает отсутствие данных. Консенсус-прогнозы не подставляются.
        Источники могут переносить даты и пересматривать значения.
      </p>
    </div>
  );
}
