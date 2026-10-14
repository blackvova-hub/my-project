import { useQuery } from "@tanstack/react-query";
import { http } from "../../../shared/api/http";

export type Importance = "high" | "medium" | "low";
export type NewsSource = {
  id: string;
  name: string;
  url: string;
  termsUrl: string;
  status: "ok" | "stale" | "error" | "pending" | "disabled";
  enabled: boolean;
  lastAttempt: string | null;
  lastSuccess: string | null;
  lastError: string | null;
  itemCount: number;
  attribution: string;
  reason?: string;
  intervalSeconds: number;
};
export type TerminalNewsItem = {
  id: string;
  title: string;
  url: string;
  source: string;
  sourceId: string;
  category: string;
  publishedAt: string;
  summary?: string;
  assets: string[];
  tags: string[];
  importance: Importance;
  importanceScore: number;
  importanceReason: string[];
  breaking: boolean;
  language: string;
  timestampBasis?: "published" | "indexed";
  attribution?: string;
};
export type NewsResponse = {
  items: TerminalNewsItem[];
  sources: NewsSource[];
  asOf: string | null;
  nextCursor: string | null;
  total: number;
  storage: string;
  syncing: boolean;
};
export type NewsFilters = {
  category: string;
  source: string;
  asset: string;
  q: string;
  importance: string;
  from: string;
  to: string;
  sort: string;
  limit: number;
  cursor?: string;
};
export const DEFAULT_FILTERS: NewsFilters = {
  category: "all",
  source: "",
  asset: "",
  q: "",
  importance: "all",
  from: "",
  to: "",
  sort: "latest",
  limit: 50,
};
export type MacroPoint = { date: string; value: number };
export type MacroSeries = {
  id: string;
  title: string;
  description: string;
  country: string;
  currency: string;
  category: string;
  unit: string;
  frequency: string;
  source: string;
  sourceUrl: string;
  observationDate: string | null;
  latest: number | null;
  previous: number | null;
  change: number | null;
  points: MacroPoint[];
  status: "fresh" | "stale" | "unavailable";
  lastUpdated: string | null;
  revisionNote: string;
  explanation: string;
  scenarios: string[];
};
export type EconomicEvent = {
  id: string;
  title: string;
  description: string;
  country: string;
  currency: string;
  category: string;
  importance: Importance;
  importanceReason: string;
  scheduledAt: string | null;
  date: string;
  timePrecision: "exact" | "date";
  status: "upcoming" | "released" | "elapsed" | "postponed" | "cancelled";
  actual: number | null;
  forecast: number | null;
  previous: number | null;
  unit: string;
  seriesId: string | null;
  source: string;
  sourceUrl: string;
  referencePeriod: string | null;
  previousScheduledAt: string | null;
};
export type MacroSource = {
  id: string;
  name: string;
  url: string;
  status: "ok" | "stale" | "unavailable" | "pending" | "standby";
  lastSuccess: string | null;
  lastAttempt: string | null;
  message: string;
};
export type MarketItem = {
  symbol: string;
  name: string;
  assetClass: string;
  price: number | null;
  changePercent: number | null;
  unit: string;
  source: string;
  sourceUrl: string;
  observedAt: string | null;
  points: MacroPoint[];
  status: "fresh" | "stale" | "unavailable";
  note: string;
  classification?: string;
};
export type Overview = {
  series: MacroSeries[];
  events: EconomicEvent[];
  sources: MacroSource[];
  asOf: string;
};
export type CalendarResponse = {
  events: EconomicEvent[];
  sources: MacroSource[];
  asOf: string;
};
export type MarketResponse = {
  items: MarketItem[];
  sources: MacroSource[];
  asOf: string;
  sentiment?: MarketItem | null;
};
export type SeriesResponse = {
  series: MacroSeries;
  sources: MacroSource[];
  asOf: string;
};

export function newsParams(filters: NewsFilters): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters))
    if (value !== "" && value !== undefined) params.set(key, String(value));
  return params.toString();
}
export function useTerminalNews(
  filters: NewsFilters,
  enabled = true,
  auto = true,
) {
  const params = newsParams(filters);
  return useQuery({
    queryKey: ["newsTerminal", params],
    queryFn: ({ signal }) =>
      http<NewsResponse>(`/news/terminal?${params}`, { signal }),
    enabled,
    staleTime: 20_000,
    refetchInterval: auto ? 30_000 : false,
    refetchIntervalInBackground: false,
    retry: 1,
  });
}
export function useNewsSources() {
  return useQuery({
    queryKey: ["newsTerminalSources"],
    queryFn: ({ signal }) =>
      http<{ sources: NewsSource[]; asOf: string | null; syncing: boolean }>(
        "/news/terminal/sources",
        { signal },
      ),
    staleTime: 30_000,
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
    retry: 1,
  });
}
export function useMacroOverview(enabled = true) {
  return useQuery({
    queryKey: ["newsMacroOverview"],
    queryFn: ({ signal }) => http<Overview>("/macro/overview", { signal }),
    enabled,
    staleTime: 300_000,
    refetchInterval: (query) =>
      query.state.data?.sources.some((s) => s.status === "pending")
        ? 10_000
        : 300_000,
    refetchIntervalInBackground: false,
    retry: 1,
  });
}
export function useMacroMarkets(enabled = true) {
  return useQuery({
    queryKey: ["newsMacroMarkets"],
    queryFn: ({ signal }) => http<MarketResponse>("/macro/markets", { signal }),
    enabled,
    staleTime: 60_000,
    refetchInterval: (query) =>
      query.state.data?.sources.some((s) => s.status === "pending")
        ? 10_000
        : 120_000,
    refetchIntervalInBackground: false,
    retry: 1,
  });
}
export function useEconomicCalendar(from: string, to: string, enabled = true) {
  return useQuery({
    queryKey: ["newsEconomicCalendar", from, to],
    queryFn: ({ signal }) =>
      http<CalendarResponse>(
        `/macro/calendar?${new URLSearchParams({ from, to })}`,
        { signal },
      ),
    enabled,
    staleTime: 120_000,
    refetchInterval: (query) =>
      query.state.data?.sources.some((s) => s.status === "pending")
        ? 10_000
        : 300_000,
    refetchIntervalInBackground: false,
    retry: 1,
  });
}
export function useMacroSeries(id: string, range: string, enabled = true) {
  return useQuery({
    queryKey: ["newsMacroSeries", id, range],
    queryFn: ({ signal }) =>
      http<SeriesResponse>(
        `/macro/series?${new URLSearchParams({ id, range })}`,
        { signal },
      ),
    enabled: enabled && Boolean(id),
    staleTime: 300_000,
    refetchInterval: (query) =>
      query.state.data?.sources.some((s) => s.status === "pending")
        ? 10_000
        : 300_000,
    retry: 1,
  });
}
