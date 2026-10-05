import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import { loadWatchlist, normalizeSymbol, syncWatchlist, WATCHLIST_UPDATED_EVENT } from "../../../../shared/watchlist";
import { useAuth } from "../../../../shared/auth/AuthContext";
import type { PrimaryExchange } from "../../../../shared/exchange/primaryExchange";
import { useTickerQuotes, type MarketQuote } from "../../../../shared/market/tickerQueries";

const CORE_SYMBOLS: Record<PrimaryExchange, string[]> = {
  bybit: ["BTCUSDT", "ETHUSDT", "XAUTUSDT"],
  binance: ["BTCUSDT", "ETHUSDT", "PAXGUSDT"],
};

const TICKER_SPEED_PX_PER_SECOND = 18;

function fmtPrice(value: number | null) {
  if (!Number.isFinite(value)) return "-";
  return (value ?? 0).toLocaleString(undefined, { maximumFractionDigits: 4 });
}

function fmtPct(value: number | null) {
  if (!Number.isFinite(value)) return "-";
  const n = value ?? 0;
  const sign = n >= 0 ? "+" : "";
  return `${sign}${n.toFixed(2)}%`;
}

function prettySymbol(symbol: string) {
  if (symbol === "XAUTUSDT") return "GOLD(XAUT)";
  if (symbol === "PAXGUSDT") return "GOLD(PAXG)";
  if (symbol.endsWith("USDT")) return symbol.slice(0, -4);
  return symbol;
}

type TickerItem = {
  symbol: string;
  label: string;
  quote: MarketQuote;
};

function MarketTickerPill({ item }: { item: TickerItem }) {
  const price = fmtPrice(item.quote.last);
  const percentage = fmtPct(item.quote.changePct);
  const pct = item.quote.changePct ?? 0;
  const pctClass = pct >= 0 ? "text-primary" : "text-destructive";

  const width = Math.min(260, Math.max(184, 164 + item.label.length * 7));

  return (
    <div
      className="market-ticker-pill mr-2 shrink-0 overflow-hidden rounded-lg border border-border bg-card text-[11px]"
      style={{ width }}
    >
      <div className="grid grid-cols-[minmax(0,1fr)_66px_48px] items-center gap-2 px-3 py-1.5">
        <span className="truncate whitespace-nowrap font-semibold text-foreground" title={item.label}>
          {item.label}
        </span>
        <span className="truncate whitespace-nowrap text-right tabular-nums text-foreground" title={price}>
          {price}
        </span>
        <span className={`truncate whitespace-nowrap text-right tabular-nums ${pctClass}`} title={percentage}>
          {percentage}
        </span>
      </div>
    </div>
  );
}
export function MarketTickerMarquee() {
  const { primaryExchange } = useAuth();
  const [watchSymbols, setWatchSymbols] = useState<string[]>(() => loadWatchlist());
  const tickerViewportRef = useRef<HTMLDivElement>(null);
  const tickerUnitRef = useRef<HTMLDivElement>(null);
  const tickerCopyRef = useRef<HTMLDivElement>(null);
  const [repeatCount, setRepeatCount] = useState(1);
  const [isTickerReady, setIsTickerReady] = useState(false);
  const [animationDuration, setAnimationDuration] = useState(36);

  const symbols = useMemo(() => {
    const core = primaryExchange ? CORE_SYMBOLS[primaryExchange] : [];
    const merged = [...core, ...watchSymbols.map((item) => normalizeSymbol(item))].filter(Boolean);
    return Array.from(new Set(merged));
  }, [primaryExchange, watchSymbols]);
  const quotes = useTickerQuotes(primaryExchange, symbols, 20_000);

  useEffect(() => {
    let mounted = true;

    void syncWatchlist().then((list) => {
      if (mounted) setWatchSymbols(list);
    });

    const handler = () => setWatchSymbols(loadWatchlist());
    window.addEventListener(WATCHLIST_UPDATED_EVENT, handler);
    return () => {
      mounted = false;
      window.removeEventListener(WATCHLIST_UPDATED_EVENT, handler);
    };
  }, []);

  const items = useMemo(
    () =>
      symbols.map((symbol) => ({
        symbol,
        label: prettySymbol(symbol),
        quote: quotes[symbol] ?? { last: null, changePct: null },
      })),
    [quotes, symbols]
  );

  const safeItems =
    items.length > 0
      ? items
      : [{ symbol: "BTCUSDT", label: "BTC", quote: { last: null, changePct: null } }];

  useLayoutEffect(() => {
    const viewport = tickerViewportRef.current;
    const unit = tickerUnitRef.current;
    if (!viewport || !unit) return;

    let frame = 0;
    const measure = () => {
      window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        const viewportWidth = viewport.getBoundingClientRect().width;
        const unitWidth = unit.getBoundingClientRect().width;
        if (viewportWidth <= 0 || unitWidth <= 0) return;

        // Each animated copy must cover the viewport on its own. Otherwise the
        // trailing edge of the second copy becomes visible before the loop.
        const nextRepeatCount = Math.max(1, Math.ceil((viewportWidth + 16) / unitWidth));
        setRepeatCount((current) => (current === nextRepeatCount ? current : nextRepeatCount));
        setIsTickerReady(true);
      });
    };

    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(viewport);
    observer.observe(unit);
    return () => {
      window.cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, [symbols]);

  useLayoutEffect(() => {
    const copy = tickerCopyRef.current;
    if (!copy) return;

    let frame = 0;
    const measure = () => {
      window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        const distance = copy.getBoundingClientRect().width;
        if (distance > 0) {
          setAnimationDuration(distance / TICKER_SPEED_PX_PER_SECOND);
        }
      });
    };

    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(copy);
    return () => {
      window.cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, [repeatCount, symbols]);

  const repeatedItems = Array.from({ length: repeatCount }, (_, repetition) =>
    safeItems.map((item) => ({ item, repetition }))
  ).flat();

  return (
    <section className="min-w-0 overflow-hidden rounded-2xl border border-border bg-background">
      <style>{`
        @keyframes tickerMarquee {
          from { transform: translate3d(0, 0, 0); }
          to { transform: translate3d(-50%, 0, 0); }
        }
        @media (prefers-reduced-motion: reduce) {
          .market-ticker-track { animation: none !important; transform: translate3d(0, 0, 0) !important; }
        }
      `}</style>
      <div ref={tickerViewportRef} className="relative overflow-hidden">
        <div
          ref={tickerUnitRef}
          aria-hidden="true"
          className="pointer-events-none invisible absolute flex w-max"
        >
          {safeItems.map((item) => (
            <MarketTickerPill key={`measure-${item.symbol}`} item={item} />
          ))}
        </div>
        <div
          className="market-ticker-track flex w-max py-2 will-change-transform"
          style={{
            animation: `tickerMarquee ${animationDuration}s linear infinite`,
            visibility: isTickerReady ? "visible" : "hidden",
          }}
        >
          {[0, 1].map((copy) => (
            <div
              key={copy}
              ref={copy === 0 ? tickerCopyRef : undefined}
              aria-hidden={copy === 1}
              className="flex shrink-0"
            >
              {repeatedItems.map(({ item, repetition }) => (
                <MarketTickerPill key={`${copy}-${repetition}-${item.symbol}`} item={item} />
              ))}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
