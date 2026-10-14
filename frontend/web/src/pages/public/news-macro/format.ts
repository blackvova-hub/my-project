import type { Importance } from "./api";
export function number(value: number | null | undefined, digits = 2): string {
  return value == null || !Number.isFinite(value)
    ? "—"
    : value.toLocaleString("ru-RU", { maximumFractionDigits: digits });
}
export function stamp(
  value: string | null | undefined,
  zone?: string,
  date = true,
) {
  if (!value || !Number.isFinite(Date.parse(value))) return "—";
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) return observationDate(value);
  return new Date(value).toLocaleString("ru-RU", {
    timeZone: zone,
    ...(date ? { day: "2-digit", month: "short" as const } : {}),
    hour: "2-digit",
    minute: "2-digit",
  });
}
export function observationDate(value: string | null | undefined) {
  return value
    ? new Date(`${value.slice(0, 10)}T12:00:00Z`).toLocaleDateString("ru-RU", {
        timeZone: "UTC",
        day: "numeric",
        month: "short",
        year: "numeric",
      })
    : "Дата не указана";
}
export const IMPORTANCE: Record<Importance, string> = {
  high: "Высокая",
  medium: "Средняя",
  low: "Низкая",
};
export const CATEGORY: Record<string, string> = {
  other: "Прочие события",
  crypto: "Криптовалюты",
  macro: "Макроэкономика",
  world: "Мир и политика",
  markets: "Рынки",
  inflation: "Инфляция",
  labor: "Рынок труда",
  employment: "Рынок труда",
  rates: "Ставки",
  growth: "Экономический рост",
  liquidity: "Ликвидность",
  bonds: "Облигации",
  yields: "Доходности облигаций",
  currency: "Валюты",
  currencies: "Валюты",
  activity: "Деловая активность",
  output: "Производство",
};
export const COUNTRY: Record<string, string> = {
  US: "США",
  USA: "США",
  EA: "Еврозона",
  EU: "Еврозона",
  EZ: "Еврозона",
  GB: "Великобритания",
  JP: "Япония",
  CN: "Китай",
  DE: "Германия",
};
export function category(value: string) {
  return CATEGORY[value] ?? value;
}
export function country(value: string) {
  return COUNTRY[value] ?? value;
}
export function latestTimestamp(values: (string | null | undefined)[]) {
  return values
    .filter(
      (value): value is string =>
        Boolean(value) && Number.isFinite(Date.parse(value!)),
    )
    .sort((a, b) => Date.parse(a) - Date.parse(b))
    .at(-1);
}
export function importanceReasonLabel(reason: string): string {
  if (!/^[a-z0-9_:-]+(?::\+?\d+)?$/i.test(reason)) return reason;
  const weight = reason.match(/([+-]?\d+)$/)?.[1];
  const topic: Record<string, string> = {
    "monetary-policy": "Денежно-кредитная политика",
    inflation: "Инфляция",
    employment: "Рынок труда",
    growth: "Экономический рост",
    regulation: "Регулирование",
    "systemic-risk": "Системные риски",
    geopolitics: "Геополитика",
    etf: "ETF",
    liquidity: "Ликвидность",
  };
  if (reason.startsWith("official_source"))
    return `Официальный источник${weight ? `: ${weight} баллов` : ""}.`;
  if (reason.startsWith("base"))
    return `Базовая оценка публикации${weight ? `: ${weight} баллов` : ""}.`;
  const label = Object.entries(topic).find(([key]) =>
    reason.includes(key),
  )?.[1];
  return `${label ? `Ключевые слова темы «${label}»` : "Дополнительный критерий важности"}${weight ? `: ${weight} баллов` : ""}.`;
}
