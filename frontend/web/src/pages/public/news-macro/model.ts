import type { TerminalNewsItem } from "./api";
export type CalendarPeriod = "today" | "tomorrow" | "week" | "month";
export type Observation = { date: string; value: number | null };
export type ReadingState = {
  version: 1;
  saved: string[];
  read: string[];
  reminders: string[];
};

const DAY = 86_400_000;
export function dateInZone(date: Date, timeZone: string): string {
  const p = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(date);
  const part = (type: string) => p.find((x) => x.type === type)?.value ?? "";
  return `${part("year")}-${part("month")}-${part("day")}`;
}
export function addDays(date: string, days: number): string {
  return new Date(Date.parse(`${date}T12:00:00Z`) + days * DAY)
    .toISOString()
    .slice(0, 10);
}
/** Resolve a civil midnight in its IANA zone instead of applying today's offset. */
export function zonedMidnight(date: string, timeZone: string): Date {
  const target = Date.parse(`${date}T00:00:00Z`);
  const formatter = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  });
  let utc = target;
  for (let i = 0; i < 5; i++) {
    const p = formatter.formatToParts(new Date(utc));
    const part = (type: string) =>
      p.find((x) => x.type === type)?.value ?? "00";
    const civil = Date.parse(
      `${part("year")}-${part("month")}-${part("day")}T${part("hour")}:${part("minute")}:${part("second")}Z`,
    );
    const next = utc + target - civil;
    if (next === utc) break;
    utc = next;
  }
  return new Date(utc);
}
export function calendarRange(
  anchor: string,
  period: CalendarPeriod,
  timeZone: string,
) {
  let first = anchor;
  let end: string;
  if (period === "tomorrow") first = addDays(anchor, 1);
  if (period === "week") {
    const weekday = new Date(`${anchor}T12:00:00Z`).getUTCDay();
    first = addDays(anchor, -((weekday + 6) % 7));
    end = addDays(first, 7);
  } else if (period === "month") {
    first = `${anchor.slice(0, 7)}-01`;
    const d = new Date(`${first}T12:00:00Z`);
    d.setUTCMonth(d.getUTCMonth() + 1);
    end = d.toISOString().slice(0, 10);
  } else end = addDays(first, 1);
  return {
    first,
    last: addDays(end, -1),
    from: zonedMidnight(first, timeZone).toISOString(),
    to: zonedMidnight(end, timeZone).toISOString(),
  };
}
export function countdown(timestamp: string, now: number): string {
  const diff = Date.parse(timestamp) - now;
  if (!Number.isFinite(diff)) return "Время не указано";
  if (diff <= 0) return "Время публикации наступило";
  const mins = Math.ceil(diff / 60_000),
    hours = Math.floor(mins / 60);
  if (hours >= 24) return `Через ${Math.floor(hours / 24)} д ${hours % 24} ч`;
  return hours ? `Через ${hours} ч ${mins % 60} мин` : `Через ${mins} мин`;
}
export function normalizeObservations(points: Observation[]) {
  const first = points.find(
    (p) => p.value !== null && Number.isFinite(p.value) && p.value !== 0,
  )?.value;
  if (first == null) return [];
  return points.map((p) => ({
    date: p.date,
    value:
      p.value === null || !Number.isFinite(p.value)
        ? null
        : (p.value / first - 1) * 100,
  }));
}
export function comparisonObservations(a: Observation[], b: Observation[]) {
  const valid = (points: Observation[]) =>
    points
      .filter((p) => p.value != null && Number.isFinite(p.value))
      .sort((x, y) => x.date.localeCompare(y.date));
  const left = valid(a),
    right = valid(b);
  if (!left.length || !right.length) return null;
  const date = left[0].date > right[0].date ? left[0].date : right[0].date;
  const end =
    left.at(-1)!.date < right.at(-1)!.date
      ? left.at(-1)!.date
      : right.at(-1)!.date;
  if (end < date) return null;
  const leftBase = left.filter((p) => p.date <= date).at(-1),
    rightBase = right.filter((p) => p.date <= date).at(-1);
  if (
    !leftBase?.value ||
    !rightBase?.value ||
    leftBase.value <= 0 ||
    rightBase.value <= 0
  )
    return null;
  const normalize = (points: Observation[], base: number) =>
    points
      .filter((p) => p.date >= date && p.date <= end)
      .map((p) => ({
        date: p.date,
        value: p.value == null ? null : (p.value / base - 1) * 100,
      }));
  const normalizedLeft = normalize(a, leftBase.value),
    normalizedRight = normalize(b, rightBase.value);
  if (
    normalizedLeft.filter((p) => p.value != null).length < 2 ||
    normalizedRight.filter((p) => p.value != null).length < 2
  )
    return null;
  return {
    date,
    leftBase,
    rightBase,
    left: normalizedLeft,
    right: normalizedRight,
  };
}
export function macroFetchRange(range: string, yearOnYear: boolean) {
  return yearOnYear ? "all" : range;
}
export function rollingNewsFrom(hours: string, minute: number) {
  return hours
    ? new Date(minute * 60000 - Number(hours) * 3600000).toISOString()
    : "";
}
export function yearOverYear(points: Observation[]): Observation[] {
  const map = new Map(points.map((p) => [p.date.slice(0, 7), p.value]));
  return points.map((p) => {
    const previousDate = `${Number(p.date.slice(0, 4)) - 1}${p.date.slice(4, 7)}`;
    const old = map.get(previousDate);
    return {
      date: p.date,
      value:
        p.value == null || old == null || old === 0
          ? null
          : (p.value / old - 1) * 100,
    };
  });
}
export function readReadingState(raw: string | null): ReadingState {
  const empty: ReadingState = {
    version: 1,
    saved: [],
    read: [],
    reminders: [],
  };
  if (!raw) return empty;
  try {
    const value: unknown = JSON.parse(raw);
    if (
      typeof value !== "object" ||
      !value ||
      !("version" in value) ||
      value.version !== 1
    )
      return empty;
    const obj = value as Record<string, unknown>;
    const ids = (key: string) =>
      Array.isArray(obj[key])
        ? [
            ...new Set(
              (obj[key] as unknown[]).filter(
                (x): x is string => typeof x === "string" && x.length < 200,
              ),
            ),
          ].slice(-1000)
        : [];
    return {
      version: 1,
      saved: ids("saved"),
      read: ids("read"),
      reminders: ids("reminders"),
    };
  } catch {
    return empty;
  }
}
export function isStoredArticle(value: unknown): value is TerminalNewsItem {
  if (typeof value !== "object" || !value) return false;
  const item = value as Record<string, unknown>;
  const text = (key: string, limit: number) =>
    typeof item[key] === "string" && (item[key] as string).length <= limit;
  const list = (key: string) =>
    Array.isArray(item[key]) &&
    (item[key] as unknown[]).length <= 50 &&
    (item[key] as unknown[]).every(
      (entry) => typeof entry === "string" && entry.length <= 500,
    );
  return (
    text("id", 200) &&
    text("title", 500) &&
    text("url", 4000) &&
    text("publishedAt", 80) &&
    Number.isFinite(Date.parse(item.publishedAt as string)) &&
    text("source", 300) &&
    text("sourceId", 200) &&
    text("category", 50) &&
    text("language", 30) &&
    list("assets") &&
    list("tags") &&
    list("importanceReason") &&
    ["high", "medium", "low"].includes(String(item.importance)) &&
    typeof item.importanceScore === "number" &&
    Number.isFinite(item.importanceScore) &&
    typeof item.breaking === "boolean" &&
    (item.summary == null || text("summary", 1000)) &&
    (item.attribution == null || text("attribution", 2000))
  );
}
export function safeExternalURL(url: string): string | null {
  try {
    const value = new URL(url);
    return value.protocol === "https:" || value.protocol === "http:"
      ? value.href
      : null;
  } catch {
    return null;
  }
}
export function assetDestination(asset: string): string {
  if (asset === "EUR") return "/news/macro?id=ECB_EURUSD";
  if (asset === "USD") return "/news/macro?id=DTWEXBGS";
  if (asset === "BTC" || asset === "ETH")
    return `/charts?symbol=${asset}USDT&exchange=bybit&market=spot`;
  return `/news/live?asset=${encodeURIComponent(asset)}`;
}
export function reactionSeries(
  candles: { time: number; close: number }[],
  eventAt: number,
) {
  const ordered = candles
    .filter(
      (p) =>
        Number.isFinite(p.close) &&
        p.close > 0 &&
        p.time >= eventAt - 7200 &&
        p.time <= eventAt + 86400,
    )
    .sort((a, b) => a.time - b.time);
  const baseline = ordered.filter((p) => p.time + 300 <= eventAt).at(-1);
  if (!baseline || eventAt - (baseline.time + 300) > 600) return [];
  return ordered.map((p) => ({
    time: p.time,
    value: (p.close / baseline.close - 1) * 100,
  }));
}
export function calendarICS(event: {
  id: string;
  title: string;
  description: string;
  scheduledAt: string | null;
  sourceUrl: string;
}): string | null {
  if (!event.scheduledAt || !Number.isFinite(Date.parse(event.scheduledAt)))
    return null;
  const esc = (value: string) =>
    value
      .replace(/\\/g, "\\\\")
      .replace(/\r\n|\r|\n/g, "\\n")
      .replace(/,/g, "\\,")
      .replace(/;/g, "\\;");
  const stamp = (date: Date) =>
    date
      .toISOString()
      .replace(/[-:]/g, "")
      .replace(/\.\d{3}/, "");
  const fold = (line: string) => {
    const encoder = new TextEncoder();
    let result = "",
      length = 0;
    for (const character of line) {
      const size = encoder.encode(character).length;
      if (length + size > 75) {
        result += "\r\n ";
        length = 1;
      }
      result += character;
      length += size;
    }
    return result;
  };
  return [
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "PRODID:-//Short & Long//News Macro//RU",
    "BEGIN:VEVENT",
    `UID:${esc(event.id)}@short-long`,
    `DTSTAMP:${stamp(new Date())}`,
    `DTSTART:${stamp(new Date(event.scheduledAt))}`,
    `SUMMARY:${esc(event.title)}`,
    `DESCRIPTION:${esc(event.description)}`,
    `URL:${esc(event.sourceUrl)}`,
    "END:VEVENT",
    "END:VCALENDAR",
    "",
  ]
    .map(fold)
    .join("\r\n");
}
