import { useQuery } from "@tanstack/react-query";

export type NewsItem = {
  id: string;
  title: string;
  url: string;
  source?: string;
  publishedAt: string;
  summary?: string | null;
  image?: string | null;
  category: string;
  calendarOnly?: boolean;
  assets?: string[];
};

export type ImportantEvent = {
  id: string;
  eventType: string;
  family: string;
  confidence: number;
  status: "confirmed" | "resolved";
  title: string;
  details: string;
  exchange?: string;
  marketType?: string;
  symbol?: string;
  direction?: string;
  amountUsd?: number;
  changePercent?: number;
  baselineValue?: number;
  baselineRatio?: number;
  percentile?: number;
  windowMinutes: number;
  occurrenceCount: number;
  firstSeenAt: string;
  lastSeenAt: string;
  lastObservedAt: string;
  eventAt: string;
  metrics?: Record<string, unknown>;
};

export type NewsSummary = { summary: string; periodEnd: string };

async function fetchJSON<T>(url: string, signal: AbortSignal): Promise<T> {
  const response = await fetch(url, { signal });
  if (!response.ok) throw new Error(`request_failed:${response.status}`);
  return (await response.json()) as T;
}

export function useNewsItems(options: {
  category: string;
  limit: number;
  tzOffset: number;
  date?: string | null;
  enabled?: boolean;
  refetchInterval?: number | false;
  asset?: string;
  searchName?: string;
}) {
  const {
    category,
    limit,
    tzOffset,
    date = null,
    enabled = true,
    refetchInterval = 30_000,
    asset = "",
    searchName = "",
  } = options;
  return useQuery({
    queryKey: ["news", category, limit, tzOffset, date, asset, searchName] as const,
    queryFn: async ({ signal }) => {
      const params = new URLSearchParams({
        category,
        limit: String(limit),
        tzOffset: String(tzOffset),
      });
      if (date) params.set("date", date);
      if (asset) params.set("asset", asset);
      if (searchName) params.set("searchName", searchName);
      const data = await fetchJSON<{ items?: NewsItem[] }>(`/api/news?${params}`, signal);
      return Array.isArray(data.items) ? data.items : [];
    },
    enabled,
    staleTime: 15_000,
    refetchInterval,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    retry: 1,
  });
}

export function useNewsCalendar(options: {
  category: string;
  from: string;
  to: string;
  tzOffset: number;
  enabled?: boolean;
}) {
  const { category, from, to, tzOffset, enabled = true } = options;
  return useQuery({
    queryKey: ["newsCalendar", category, from, to, tzOffset] as const,
    queryFn: async ({ signal }) => {
      const params = new URLSearchParams({ category, from, to, tzOffset: String(tzOffset) });
      const data = await fetchJSON<{ days?: Array<{ date: string; count: number }> }>(
        `/api/news/calendar?${params}`,
        signal,
      );
      const counts: Record<string, number> = {};
      for (const day of data.days ?? []) counts[day.date] = day.count;
      return counts;
    },
    enabled: enabled && Boolean(from && to),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
    retry: 1,
  });
}

export function useNewsSummary(date?: string | null, enabled = true) {
  return useQuery({
    queryKey: ["newsSummary", date ?? "latest"] as const,
    queryFn: async ({ signal }) => {
      const suffix = date ? `?date=${encodeURIComponent(date)}` : "";
      const data = await fetchJSON<{ summary?: string; periodEnd?: string }>(
        `/api/news/summary${suffix}`,
        signal,
      );
      return {
        summary: String(data.summary ?? "").trim(),
        periodEnd: String(data.periodEnd ?? "").trim(),
      } satisfies NewsSummary;
    },
    enabled,
    staleTime: 15_000,
    refetchInterval: date ? false : 30_000,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    retry: 1,
  });
}

export function useImportantEvents() {
  return useQuery({
    queryKey: ["importantEvents", 6, 24] as const,
    queryFn: async ({ signal }) => {
      const data = await fetchJSON<{ items?: ImportantEvent[] }>(
        "/api/news/important-events?limit=6&hours=24",
        signal,
      );
      return Array.isArray(data.items) ? data.items : [];
    },
    staleTime: 15_000,
    refetchInterval: 30_000,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    retry: 1,
  });
}
