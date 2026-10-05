import { apiUpdateWatchlist, apiWatchlist } from "./auth/authApi";

const WATCHLIST_KEY = "watchlist_v2";
const WATCHLIST_LEGACY_KEY = "watchlist_v1";
export const WATCHLIST_UPDATED_EVENT = "watchlist:updated";
const DEFAULT_WATCHLIST = ["BTCUSDT", "ETHUSDT"];

export type Watchlists = {
  hot: string[];
  cold: string[];
};

export function normalizeSymbol(symbol: string): string {
  return symbol.replace(/[^a-zA-Z0-9]/g, "").toUpperCase();
}

function emitWatchlistUpdate() {
  try {
    window.dispatchEvent(new Event(WATCHLIST_UPDATED_EVENT));
  } catch {
    // ignore
  }
}

function normalizeList(list: string[]): string[] {
  const normalized = list
    .map((item) => normalizeSymbol(item))
    .filter((item) => item.length > 0);
  return Array.from(new Set(normalized));
}

export function loadWatchlists(): Watchlists {
  if (typeof window === "undefined") {
    return { hot: [...DEFAULT_WATCHLIST], cold: [] };
  }
  try {
    const raw = localStorage.getItem(WATCHLIST_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<Watchlists> | string[];
      if (Array.isArray(parsed)) {
        return { hot: normalizeList(parsed), cold: [] };
      }
      const hot = normalizeList(parsed.hot ?? []);
      const cold = normalizeList(parsed.cold ?? []);
      return { hot, cold };
    }
    const legacy = localStorage.getItem(WATCHLIST_LEGACY_KEY);
    if (legacy) {
      const parsed = JSON.parse(legacy) as string[];
      return { hot: normalizeList(parsed), cold: [] };
    }
  } catch {
    // ignore
  }
  return { hot: [...DEFAULT_WATCHLIST], cold: [] };
}

export function loadWatchlist(): string[] {
  return loadWatchlists().hot;
}

export async function syncWatchlists(): Promise<Watchlists> {
  try {
    const res = await apiWatchlist();
    return saveWatchlists({ hot: res.hot ?? [], cold: res.cold ?? [] });
  } catch {
    return loadWatchlists();
  }
}

export async function syncWatchlist(): Promise<string[]> {
  const res = await syncWatchlists();
  return res.hot;
}

export function saveWatchlists(input: Watchlists): Watchlists {
  if (typeof window === "undefined") return { hot: [], cold: [] };
  const hot = normalizeList(input.hot);
  const cold = normalizeList(input.cold);
  const next = { hot, cold };
  try {
    localStorage.setItem(WATCHLIST_KEY, JSON.stringify(next));
  } catch {
    // ignore
  }
  void apiUpdateWatchlist(hot, cold).catch(() => {
    // ignore
  });
  emitWatchlistUpdate();
  return next;
}

export function saveWatchlist(list: string[]): string[] {
  const res = saveWatchlists({ hot: list, cold: loadWatchlists().cold });
  return res.hot;
}

export function addToWatchlist(symbol: string): string[] {
  const norm = normalizeSymbol(symbol);
  if (!norm) return loadWatchlist();
  const list = loadWatchlist();
  if (list.includes(norm)) return list;
  return saveWatchlist([...list, norm]);
}

export function removeFromWatchlist(symbol: string): string[] {
  const norm = normalizeSymbol(symbol);
  const list = loadWatchlist().filter((item) => item !== norm);
  return saveWatchlist(list);
}

export function loadColdWatchlist(): string[] {
  return loadWatchlists().cold;
}

export function addToColdWatchlist(symbol: string): string[] {
  const norm = normalizeSymbol(symbol);
  if (!norm) return loadColdWatchlist();
  const list = loadColdWatchlist();
  if (list.includes(norm)) return list;
  const updated = saveWatchlists({ hot: loadWatchlist(), cold: [...list, norm] });
  return updated.cold;
}

export function removeFromColdWatchlist(symbol: string): string[] {
  const norm = normalizeSymbol(symbol);
  const list = loadColdWatchlist().filter((item) => item !== norm);
  const updated = saveWatchlists({ hot: loadWatchlist(), cold: list });
  return updated.cold;
}

export function isInWatchlist(symbol: string): boolean {
  const norm = normalizeSymbol(symbol);
  return loadWatchlist().includes(norm);
}
