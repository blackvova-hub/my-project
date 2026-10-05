import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useSymbolTradeStats } from "../../scanners/queries";
import { useAuth } from "../../../../shared/auth/AuthContext";
import { AnimatedNativeSelect } from "../../../../shared/ui/AnimatedSelect";
import { useTickerQuotes } from "../../../../shared/market/tickerQueries";
import {
  coinGlassTVURL,
  type PrimaryExchange,
} from "../../../../shared/exchange/primaryExchange";
import { PerpChart } from "../../../../shared/market/PerpChart";
import { useCoinInfo } from "../../../../shared/market/coinInfoQueries";
import { CoinResearchPanel } from "../../../../shared/market/CoinResearchPanel";
import { SimilarityPanel } from "../../../../shared/market/SimilarityPanel";
import {
  addToColdWatchlist,
  addToWatchlist,
  loadColdWatchlist,
  loadWatchlist,
  normalizeSymbol,
  removeFromColdWatchlist,
  removeFromWatchlist,
  syncWatchlist,
  WATCHLIST_UPDATED_EVENT,
} from "../../../../shared/watchlist";

function fmt(n: number) {
  return Number.isFinite(n) ? n.toLocaleString() : "—";
}

type ExchangeInstrument = {
  symbol: string;
  baseCoin: string;
  quoteCoin: string;
  status?: string;
};

const COLD_PLAN_KEY = "cold_watchlist_plans_v1";

function loadColdPlans(): Record<string, string> {
  if (typeof window === "undefined") return {};
  try {
    const raw = localStorage.getItem(COLD_PLAN_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, string>;
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

function saveColdPlans(plans: Record<string, string>) {
  if (typeof window === "undefined") return;
  try {
    localStorage.setItem(COLD_PLAN_KEY, JSON.stringify(plans));
  } catch {
    // ignore
  }
}

async function fetchInstruments(
  exchange: PrimaryExchange,
  signal?: AbortSignal
): Promise<ExchangeInstrument[]> {
  if (exchange === "binance") {
    const res = await fetch("https://fapi.binance.com/fapi/v1/exchangeInfo", { signal });
    if (!res.ok) throw new Error("binance_instruments_failed");
    const data = (await res.json()) as {
      symbols?: Array<{
        symbol?: string;
        baseAsset?: string;
        quoteAsset?: string;
        status?: string;
        contractType?: string;
      }>;
    };
    return (data.symbols ?? [])
      .filter((item) => item.contractType === "PERPETUAL" && item.status === "TRADING")
      .map((item) => ({
        symbol: item.symbol ?? "",
        baseCoin: item.baseAsset ?? "",
        quoteCoin: item.quoteAsset ?? "",
        status: item.status,
      }))
      .filter((item) => item.symbol !== "");
  }

  const res = await fetch(
    "https://api.bybit.com/v5/market/instruments-info?category=linear&limit=1000",
    { signal }
  );
  if (!res.ok) throw new Error("bybit_instruments_failed");
  const data = (await res.json()) as { result?: { list?: ExchangeInstrument[] } };
  return data?.result?.list ?? [];
}

function fmtPct(value: number | null) {
  if (!Number.isFinite(value)) return "—";
  const sign = (value ?? 0) >= 0 ? "+" : "";
  return `${sign}${value?.toFixed(2)}%`;
}

export function WatchlistWidget() {
  const { primaryExchange } = useAuth();
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<"change" | "price">("change");
  const [mode, setMode] = useState<"hot" | "cold">("hot");
  const [symbols, setSymbols] = useState<string[]>(() => loadWatchlist());
  const [coldSymbols, setColdSymbols] = useState<string[]>(() => loadColdWatchlist());
  const [coldPlans, setColdPlans] = useState<Record<string, string>>(() => loadColdPlans());
  const [coldPlanInput, setColdPlanInput] = useState("");
  const quotes = useTickerQuotes(primaryExchange, symbols, 60_000);
  const coldQuotes = useTickerQuotes(primaryExchange, coldSymbols, 30_000);
  const [results, setResults] = useState<ExchangeInstrument[]>([]);
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState("");
  const [infoSymbol, setInfoSymbol] = useState<string | null>(null);
  const instrumentsRef = useRef<{
    exchange: PrimaryExchange;
    ts: number;
    list: ExchangeInstrument[];
  } | null>(null);

  useEffect(() => {
    void syncWatchlist();
    const handler = () => {
      setSymbols(loadWatchlist());
      setColdSymbols(loadColdWatchlist());
      setColdPlans(loadColdPlans());
    };
    window.addEventListener(WATCHLIST_UPDATED_EVENT, handler);
    return () => window.removeEventListener(WATCHLIST_UPDATED_EVENT, handler);
  }, []);

  function baseSymbol(symbol: string): string {
    const s = symbol.toUpperCase();
    const quotes = ["USDT", "USDC", "BUSD", "FDUSD", "USD", "BTC", "ETH"];
    for (const q of quotes) {
      if (s.endsWith(q) && s.length > q.length) return s.slice(0, -q.length);
    }
    return s;
  }

  function normalizeInfoSymbol(symbol: string) {
    const upper = symbol.toUpperCase().trim();
    if (upper.endsWith("PERP")) return upper.slice(0, -4);
    return upper;
  }

  function linksForSymbol(symbol: string) {
    const sym = symbol.toUpperCase();
    const base = baseSymbol(sym);
    return {
      bybit: `https://www.bybit.com/en-US/trade/usdt/${sym}`,
      binance: `https://www.binance.com/en/futures/${sym}`,
      coinglass: primaryExchange ? coinGlassTVURL(primaryExchange, sym) : "",
      tradingview: `https://www.tradingview.com/chart/?symbol=BINANCE:${sym}`,
      base,
    };
  }

  const normalizedInfoSymbol = infoSymbol ? normalizeInfoSymbol(infoSymbol) : "";
  const infoBase = infoSymbol ? baseSymbol(normalizedInfoSymbol) : "";
  const infoLinks = infoSymbol ? linksForSymbol(infoSymbol) : null;
  const coinInfoQuery = useCoinInfo(infoBase, Boolean(infoSymbol));
  const symbolStatsQuery = useSymbolTradeStats(normalizedInfoSymbol, Boolean(infoSymbol));
  const coinInfo = coinInfoQuery.data ?? null;
  const symbolStats = symbolStatsQuery.data ?? null;
  const statsLoading = symbolStatsQuery.isPending && symbolStatsQuery.isFetching;
  const infoError = coinInfoQuery.error
    ? coinInfoQuery.error.message === "coin_not_found"
      ? "Монета не найдена на CoinGecko."
      : "Не удалось загрузить данные CoinGecko."
    : "";

  useEffect(() => {
    const raw = query.trim();
    if (!primaryExchange || raw.length < 2) {
      setResults([]);
      setSearchError("");
      return;
    }
    const controller = new AbortController();
    const timer = window.setTimeout(async () => {
      setSearching(true);
      setSearchError("");
      try {
        const now = Date.now();
        let list = instrumentsRef.current?.exchange === primaryExchange
          ? instrumentsRef.current.list
          : [];
        if (
          !instrumentsRef.current ||
          instrumentsRef.current.exchange !== primaryExchange ||
          now - instrumentsRef.current.ts > 10 * 60 * 1000
        ) {
          list = await fetchInstruments(primaryExchange, controller.signal);
          instrumentsRef.current = { exchange: primaryExchange, ts: Date.now(), list };
        }
        const upper = normalizeSymbol(raw);
        const filtered = list
          .filter((item) => {
            const symbol = item.symbol.toUpperCase();
            const base = item.baseCoin.toUpperCase();
            const quote = item.quoteCoin.toUpperCase();
            return (
              symbol.includes(upper) ||
              base.includes(upper) ||
              quote.includes(upper)
            );
          })
          .slice(0, 10);
        setResults(filtered);
      } catch (err) {
        if ((err as Error).name !== "AbortError") {
          setSearchError(`Не удалось загрузить поиск ${primaryExchange === "binance" ? "Binance" : "Bybit"}.`);
        }
      } finally {
        setSearching(false);
      }
    }, 300);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [primaryExchange, query]);

  function handleAddSymbol(symbol: string) {
    const norm = normalizeSymbol(symbol);
    const instruments = instrumentsRef.current;
    const isValid =
      primaryExchange &&
      instruments?.exchange === primaryExchange &&
      instruments.list.some((item) => item.symbol.toUpperCase() === norm);
    if (!isValid) {
      setSearchError(
        `Такого инструмента нет на ${primaryExchange === 'binance' ? 'Binance' : 'Bybit'}.`
      );
      return;
    }
    if (mode === "hot") {
      const list = addToWatchlist(norm);
      setSymbols(list);
    } else {
      const list = addToColdWatchlist(norm);
      setColdSymbols(list);
      const plan = coldPlanInput.trim();
      if (norm) {
        setColdPlans((prev) => {
          const next = { ...prev };
          if (plan) {
            next[norm] = plan;
          } else {
            delete next[norm];
          }
          saveColdPlans(next);
          return next;
        });
      }
      setColdPlanInput("");
    }
    setQuery("");
    setResults([]);
  }

  function handleRemoveSymbol(symbol: string, kind: "hot" | "cold") {
    if (kind === "hot") {
      const list = removeFromWatchlist(symbol);
      setSymbols(list);
    } else {
      const list = removeFromColdWatchlist(symbol);
      setColdSymbols(list);
      const norm = normalizeSymbol(symbol);
      setColdPlans((prev) => {
        if (!prev[norm]) return prev;
        const next = { ...prev };
        delete next[norm];
        saveColdPlans(next);
        return next;
      });
    }
  }

  const filtered = useMemo(() => {
    const list = symbols
      .map((symbol) => {
        const quote = quotes[symbol];
        return {
          symbol,
          last: quote?.last ?? 0,
          changePct: quote?.changePct ?? 0,
        };
      })
      .filter((item) => item.symbol.length > 0);
    return [...list].sort((a, b) => {
      if (sort === "price") return b.last - a.last;
      return Math.abs(b.changePct) - Math.abs(a.changePct);
    });
  }, [quotes, sort, symbols]);

  const coldList = useMemo(() => {
    return coldSymbols.filter((item) => item.length > 0);
  }, [coldSymbols]);

  const visibleTitle = mode === "hot" ? "Горячие" : "Холодные";
  const visibleListCount = mode === "hot" ? filtered.length : coldList.length;

  const normalizedQuery = normalizeSymbol(query);
  const canAdd =
    results.find((item) => item.symbol.toUpperCase() === normalizedQuery)?.symbol ??
    results.find(
      (item) =>
        item.baseCoin.toUpperCase() === normalizedQuery &&
        item.quoteCoin.toUpperCase() === 'USDT'
    )?.symbol ??
    '';

  return (
    <section className="rounded-2xl border border-border bg-card p-5 md:p-6 min-w-0 h-[720px] flex flex-col">
      <div>
        <h3 className="text-lg font-semibold tracking-tight">Лист ожидания</h3>
      </div>

      <div className="mt-3 flex flex-wrap gap-2">
        <button
          type="button"
          onClick={() => setMode("hot")}
          className={
            mode === "hot"
              ? "rounded-xl bg-amber-400/20 px-3 py-2 text-xs font-semibold text-amber-700 dark:text-amber-200"
              : "rounded-xl border border-border px-3 py-2 text-xs text-muted-foreground hover:bg-card"
          }
        >
          Горячие
        </button>
        <button
          type="button"
          onClick={() => setMode("cold")}
          className={
            mode === "cold"
              ? "rounded-xl bg-sky-400/20 px-3 py-2 text-xs font-semibold text-sky-700 dark:text-sky-200"
              : "rounded-xl border border-border px-3 py-2 text-xs text-muted-foreground hover:bg-card"
          }
        >
          Холодные
        </button>
      </div>

      <div className="mt-4 grid gap-3">
        <div className="grid gap-3 md:grid-cols-[1fr_auto]">
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={`Поиск монеты (${primaryExchange === "binance" ? "Binance" : "Bybit"})`}
            className="w-full rounded-xl border border-border bg-background px-3 py-2 text-xs text-foreground outline-none focus:border-border-strong"
          />
          <button
            type="button"
            disabled={!canAdd}
            onClick={() => handleAddSymbol(canAdd)}
            className="rounded-xl border border-border-strong bg-accent px-3 py-2 text-xs font-semibold text-primary hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
          >
            Добавить
          </button>
        </div>

        {mode === "cold" && (
          <input
            value={coldPlanInput}
            onChange={(event) => setColdPlanInput(event.target.value)}
            placeholder="План для холодного списка"
            className="w-full rounded-xl border border-border bg-background px-3 py-2 text-xs text-foreground outline-none focus:border-border-strong"
          />
        )}

        {(searching || searchError || results.length > 0) && (
          <div className="rounded-xl border border-border bg-background p-2">
            {searching && (
              <div className="px-3 py-2 text-xs text-muted-foreground">
                Идёт поиск на {primaryExchange === "binance" ? "Binance" : "Bybit"}…
              </div>
            )}
            {searchError && !searching && (
              <div className="px-3 py-2 text-xs text-destructive">{searchError}</div>
            )}
            {!searching && !searchError && results.length === 0 ? (
              <div className="px-3 py-2 text-xs text-muted-foreground">Нет совпадений.</div>
            ) : null}
            {!searching && !searchError && results.length > 0 && (
              <div className="grid gap-1">
                {results.map((item) => {
                  const label = `${item.baseCoin}/${item.quoteCoin}`;
                  return (
                    <button
                      key={item.symbol}
                      type="button"
                      onClick={() => handleAddSymbol(item.symbol)}
                      className="flex items-center justify-between rounded-lg px-3 py-2 text-left text-xs text-foreground hover:bg-card"
                    >
                      <span className="font-semibold text-foreground">{item.symbol}</span>
                      <span className="text-[11px] text-muted-foreground">{label}</span>
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        )}

        <AnimatedNativeSelect
          value={sort}
          onChange={(event) => setSort(event.target.value as "change" | "price")}
          className="rounded-xl border border-border bg-background px-3 py-2 text-xs text-foreground"
        >
          <option value="change">Сортировка: волатильность</option>
          <option value="price">Сортировка: цена</option>
        </AnimatedNativeSelect>
      </div>

      <div className="mt-4 min-h-0 flex-1 overflow-hidden rounded-2xl border border-border flex flex-col">
        <div className="bg-background px-4 py-3 text-xs text-muted-foreground flex items-center justify-between">
          <div>{visibleTitle}</div>
          <div>{mode === "hot" ? "24h" : "Планы"}</div>
        </div>
        <div className="site-scrollbar min-h-0 flex-1 overflow-y-auto bg-background pb-2">
          {mode === "hot" &&
            filtered.map((x) => (
              <div key={x.symbol} className="flex items-center justify-between border-t border-border px-4 py-3 text-sm">
                <div className="flex flex-col">
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={() => setInfoSymbol(x.symbol)}
                      className="font-semibold text-foreground hover:text-foreground"
                    >
                      {x.symbol}
                    </button>
                    <button
                      type="button"
                      onClick={() => handleRemoveSymbol(x.symbol, "hot")}
                      className="rounded-full border border-border px-2 py-0.5 text-[10px] text-muted-foreground hover:text-foreground"
                    >
                      Удалить
                    </button>
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">{fmt(x.last)}</div>
                </div>
                <div
                  className={[
                    "text-right text-sm font-semibold",
                    (quotes[x.symbol]?.changePct ?? 0) >= 0
                      ? "text-primary"
                      : "text-destructive",
                  ].join(" ")}
                >
                  {fmtPct(quotes[x.symbol]?.changePct ?? null)}
                </div>
              </div>
            ))}

          {mode === "cold" &&
            coldList.map((symbol) => (
              <div key={symbol} className="flex items-center justify-between border-t border-border px-4 py-3 text-sm">
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setInfoSymbol(symbol)}
                    className="font-semibold text-foreground hover:text-foreground"
                  >
                    {symbol}
                  </button>
                  <button
                    type="button"
                    onClick={() => handleRemoveSymbol(symbol, "cold")}
                    className="rounded-full border border-border px-2 py-0.5 text-[10px] text-muted-foreground hover:text-foreground"
                  >
                    Удалить
                  </button>
                </div>
                <div className="text-right text-xs text-muted-foreground">
                  <div>{coldPlans[symbol] || "Планы на будущее"}</div>
                  <div className="mt-1 text-[11px] text-muted-foreground">
                    {coldQuotes[symbol]?.last !== null && coldQuotes[symbol]?.last !== undefined
                      ? fmt(coldQuotes[symbol]?.last ?? 0)
                      : "—"}
                  </div>
                </div>
              </div>
            ))}

          {visibleListCount === 0 && (
            <div className="px-4 py-3 text-xs text-muted-foreground">
              {mode === "hot" ? "Ничего не найдено." : "Пусто."}
            </div>
          )}
          <div className="h-2" aria-hidden="true" />
        </div>
      </div>
      <div className="mt-3 text-xs text-muted-foreground">
      </div>

      {infoSymbol && typeof document !== "undefined"
        ? createPortal(
            <div className="fixed inset-0 z-[90] flex items-center justify-center px-4 py-6">
              <button className="absolute inset-0 bg-scrim" onClick={() => setInfoSymbol(null)} />
              <div className="relative w-full max-w-[96vw] max-h-[94vh] overflow-y-auto rounded-3xl border border-border-strong bg-surface-raised p-6 text-sm text-foreground shadow-2xl">
            <div className="flex items-center gap-3 overflow-x-auto whitespace-nowrap pb-1 [scrollbar-width:thin]">
              <h3 className="shrink-0 text-xl font-semibold text-foreground sm:text-2xl">
                {infoSymbol.toUpperCase()} {coinInfo ? `• ${coinInfo.name}` : ""}
              </h3>
              {infoLinks ? (
                <div className="flex shrink-0 items-center gap-1.5">
                  <a href={infoLinks.bybit} target="_blank" rel="noreferrer" className="rounded-full border border-border bg-secondary px-3 py-1.5 text-[11px] font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground">Bybit</a>
                  <a href={infoLinks.binance} target="_blank" rel="noreferrer" className="rounded-full border border-border bg-secondary px-3 py-1.5 text-[11px] font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground">Binance</a>
                  {infoLinks.coinglass ? <a href={infoLinks.coinglass} target="_blank" rel="noreferrer" className="rounded-full border border-border bg-secondary px-3 py-1.5 text-[11px] font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground">Coinglass</a> : null}
                  <a href={infoLinks.tradingview} target="_blank" rel="noreferrer" className="rounded-full border border-border bg-secondary px-3 py-1.5 text-[11px] font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground">TradingView</a>
                </div>
              ) : null}
              <button
                onClick={() => setInfoSymbol(null)}
                className="ml-auto shrink-0 rounded-lg border border-border-strong bg-accent px-3 py-2 text-xs text-foreground hover:bg-accent"
              >
                Закрыть
              </button>
            </div>

            <div className="mt-4 grid gap-4">
              <PerpChart
                exchange={primaryExchange ?? "bybit"}
                symbol={normalizedInfoSymbol}
                interval="1m"
              />
            </div>

            <CoinResearchPanel exchange={primaryExchange ?? "bybit"} base={infoBase} symbol={normalizedInfoSymbol} coinInfo={coinInfo} infoError={infoError} />

            <div className="mt-4 rounded-2xl border border-border bg-secondary p-4">
                  <div className="text-xs uppercase tracking-wide text-muted-foreground">Статистика</div>
                  {statsLoading ? (
                    <div className="mt-3 text-xs text-muted-foreground">Загрузка...</div>
                  ) : symbolStats ? (
                    <div className="mt-3 space-y-2 text-xs text-foreground">
                      <div className="flex items-center justify-between">
                        <span>Сделок всего</span>
                        <span className="font-semibold text-foreground">{symbolStats.total_trades}</span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span>Закрыто</span>
                        <span className="font-semibold text-foreground">{symbolStats.closed_trades}</span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span>Открыто</span>
                        <span className="font-semibold text-foreground">{symbolStats.open_trades}</span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span>Win rate</span>
                        <span className="font-semibold text-primary">
                          {symbolStats.win_rate !== null && symbolStats.win_rate !== undefined
                            ? `${symbolStats.win_rate.toFixed(1)}%`
                            : "—"}
                        </span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span>Avg %</span>
                        <span className="font-semibold text-foreground">
                          {symbolStats.avg_profit_percent !== null && symbolStats.avg_profit_percent !== undefined
                            ? `${symbolStats.avg_profit_percent.toFixed(2)}%`
                            : "—"}
                        </span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span>Sum %</span>
                        <span className="font-semibold text-foreground">
                          {symbolStats.sum_profit_percent !== null && symbolStats.sum_profit_percent !== undefined
                            ? `${symbolStats.sum_profit_percent.toFixed(2)}%`
                            : "—"}
                        </span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span>Avg $</span>
                        <span className="font-semibold text-foreground">
                          {symbolStats.avg_profit_usd !== null && symbolStats.avg_profit_usd !== undefined
                            ? `$${symbolStats.avg_profit_usd.toFixed(2)}`
                            : "—"}
                        </span>
                      </div>
                    </div>
                  ) : (
                    <div className="mt-3 text-xs text-muted-foreground">Нет данных</div>
                  )}
            </div>
            <SimilarityPanel symbol={normalizedInfoSymbol} exchange={primaryExchange ?? "bybit"} />
              </div>
            </div>,
            document.body
          )
        : null}
    </section>
  );
}
