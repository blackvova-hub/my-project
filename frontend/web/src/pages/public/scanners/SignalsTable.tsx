// src/pages/public/scanners/SignalsTable.tsx
import { Fragment, useMemo, useState, useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import type { ApiCloseTradeInput, ApiTrade, ApiTradeStrategy, SignalRow } from "./types";
import { useSymbolTradeStats } from "./queries";
import { useToast } from "../../../shared/ui/ToastProvider";
import { AnimatedNativeSelect } from "../../../shared/ui/AnimatedSelect";
import {
  coinGlassTVURL,
  tradingViewPerpetualSymbol,
  type PrimaryExchange,
} from "../../../shared/exchange/primaryExchange";
import {
  loadWatchlist,
  normalizeSymbol,
  syncWatchlist,
  WATCHLIST_UPDATED_EVENT,
} from "../../../shared/watchlist";
import { useCoinInfo } from "../../../shared/market/coinInfoQueries";
import { CoinResearchPanel } from "../../../shared/market/CoinResearchPanel";
import { SimilarityPanel } from "../../../shared/market/SimilarityPanel";
import { PerpChart } from "../../../shared/market/PerpChart";
import { CloseTradeModal, type TradeCloseDraft } from "../../../shared/trades/CloseTradeModal";

const PAGE_SIZE = 10;
type SignalSortMode = "default" | "change_desc" | "change_asc";

export default function SignalsTable({
  signals,
  isLoading,
  error,
  trades = [],
  strategies = [],
  tradeMeta,
  onBuy,
  onCloseTrade,
  isTradeBusy,
}: {
  signals: SignalRow[];
  isLoading?: boolean;
  error?: unknown;
  trades?: ApiTrade[];
  strategies?: ApiTradeStrategy[];
  tradeMeta?: { timeframe?: string | null; exchange?: string | null; strategy?: string | null };
  onBuy?: (signalId: string) => Promise<ApiTrade>;
  onCloseTrade?: (tradeId: string, input: ApiCloseTradeInput) => Promise<ApiTrade>;
  isTradeBusy?: boolean;
}) {
  const [visible, setVisible] = useState(PAGE_SIZE);
  const reduceMotion = useReducedMotion();
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [tradeDraft, setTradeDraft] = useState<TradeCloseDraft | null>(null);
  const [tradePending, setTradePending] = useState<string | null>(null);
  const [infoSymbol, setInfoSymbol] = useState<string | null>(null);
  const [infoExchange, setInfoExchange] = useState<PrimaryExchange>("bybit");
  const [watchlist, setWatchlist] = useState<string[]>(() => loadWatchlist());
  const [newSignalIds, setNewSignalIds] = useState<Set<string>>(() => new Set());
  const [newSignalDuration, setNewSignalDuration] = useState(420);
  const [sortMode, setSortMode] = useState<SignalSortMode>("default");
  const [maxAgeMinutes, setMaxAgeMinutes] = useState<string>("0");
  const lastSignalIdsRef = useRef<string[]>([]);
  const lastBurstAtRef = useRef<number>(0);
  const toast = useToast();
  const canBuy = Boolean(onBuy);
  const canSell = Boolean(onCloseTrade);

  useEffect(() => {
    void syncWatchlist();
    const handler = () => setWatchlist(loadWatchlist());
    window.addEventListener(WATCHLIST_UPDATED_EVENT, handler);
    return () => window.removeEventListener(WATCHLIST_UPDATED_EVENT, handler);
  }, []);

  const strategyNames = useMemo(() => {
    const names = strategies.map((s) => s.name).filter(Boolean);
    return Array.from(new Set(names));
  }, [strategies]);

  const MAX_TRADE_PHOTOS = 3;
  const MAX_TRADE_PHOTO_BYTES = 5 * 1024 * 1024;

  const sorted = useMemo(() => {
    const now = Date.now();
    const maxAgeRaw = Number(maxAgeMinutes);
    const hasAgeLimit = Number.isFinite(maxAgeRaw) && maxAgeRaw > 0;
    const maxAgeMs = hasAgeLimit ? maxAgeRaw * 60_000 : 0;

    const parseCreatedAtMs = (value: string): number => {
      const direct = Date.parse(value);
      if (Number.isFinite(direct)) return direct;

      const m = value.match(
        /^(\d{1,2})\.(\d{1,2})\.(\d{4}),\s*(\d{1,2}):(\d{2})(?::(\d{2}))?$/
      );
      if (!m) return Number.NaN;
      const day = Number(m[1]);
      const month = Number(m[2]) - 1;
      const year = Number(m[3]);
      const hour = Number(m[4]);
      const minute = Number(m[5]);
      const second = Number(m[6] ?? "0");
      return new Date(year, month, day, hour, minute, second).getTime();
    };

    const parseChangeValue = (changes: string): number => {
      const raw = String(changes ?? "");
      const normalized = raw.replace(/\u00a0/g, " ");
      const match = normalized.match(/[-+]?\d+(?:[.,]\d+)?/);
      if (!match) return Number.NaN;
      const parsed = Number(match[0].replace(",", "."));
      return Number.isFinite(parsed) ? parsed : Number.NaN;
    };

    const filtered = [...signals].filter((s) => {
      if (!hasAgeLimit) return true;
      const createdMs = parseCreatedAtMs(s.createdAt);
      if (!Number.isFinite(createdMs)) return false;
      return now - createdMs <= maxAgeMs;
    });

    if (sortMode === "default") {
      return filtered;
    }

    return filtered.sort((a, b) => {
      const va = parseChangeValue(a.changes);
      const vb = parseChangeValue(b.changes);
      const aBad = !Number.isFinite(va);
      const bBad = !Number.isFinite(vb);
      if (aBad && bBad) return 0;
      if (aBad) return 1;
      if (bBad) return -1;
      return sortMode === "change_desc" ? vb - va : va - vb;
    });
  }, [signals, sortMode, maxAgeMinutes]);

  useEffect(() => {
    setVisible(PAGE_SIZE);
  }, [sortMode, maxAgeMinutes]);

  const shown = sorted.slice(0, visible);
  const canLoadMore = visible < sorted.length;

  const watchlistSet = useMemo(
    () => new Set(watchlist.map((symbol) => normalizeSymbol(symbol))),
    [watchlist]
  );

  const tradesBySignal = useMemo(() => {
    const map = new Map<string, ApiTrade>();
    for (const trade of trades) {
      if (trade.status === "OPEN") {
        map.set(trade.signal_id, trade);
      }
    }
    return map;
  }, [trades]);

  useEffect(() => {
    const currentIds = signals.map((s) => s.id);
    const prevIds = lastSignalIdsRef.current;
    const prevSet = new Set(prevIds);
    const newIds = currentIds.filter((id) => !prevSet.has(id));
    let timeoutId: number | null = null;

    if (newIds.length > 0) {
      const now = Date.now();
      const sinceLast = now - lastBurstAtRef.current;
      lastBurstAtRef.current = now;
      let duration = 420;
      if (newIds.length >= 8 || (sinceLast < 1200 && newIds.length >= 4)) {
        duration = 180;
      } else if (newIds.length >= 4) {
        duration = 240;
      } else if (newIds.length >= 2) {
        duration = 320;
      }
      setNewSignalDuration(duration);
      setNewSignalIds((prev) => {
        const next = new Set(prev);
        newIds.forEach((id) => next.add(id));
        return next;
      });
      timeoutId = window.setTimeout(() => {
        setNewSignalIds((prev) => {
          const next = new Set(prev);
          newIds.forEach((id) => next.delete(id));
          return next;
        });
      }, duration + 120);
    }

    lastSignalIdsRef.current = currentIds;
    return () => {
      if (timeoutId) window.clearTimeout(timeoutId);
    };
  }, [signals]);

  function calcMinutes(buyAtIso: string, sellAtIso: string) {
    const buyAtMs = Date.parse(buyAtIso);
    const sellAtMs = Date.parse(sellAtIso);
    if (!Number.isFinite(buyAtMs) || !Number.isFinite(sellAtMs)) return 1;
    const minutes = Math.round((sellAtMs - buyAtMs) / 60000);
    return minutes < 1 ? 1 : minutes;
  }

  function readTradePhoto(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result || ""));
      reader.onerror = () => reject(new Error("read_failed"));
      reader.readAsDataURL(file);
    });
  }

  async function handleTradePhotoAdd(files: FileList | null) {
    if (!files || !tradeDraft) return;
    const remaining = MAX_TRADE_PHOTOS - tradeDraft.entryPhotos.length;
    if (remaining <= 0) {
      toast.error("Можно добавить максимум 3 фото.");
      return;
    }

    const selected = Array.from(files).slice(0, remaining);
    if (files.length > remaining) {
      toast.error("Можно добавить максимум 3 фото.");
    }

    const validFiles = selected.filter((file) => {
      if (!file.type.startsWith("image/")) {
        toast.error(`Файл ${file.name} не является изображением.`);
        return false;
      }
      if (file.size > MAX_TRADE_PHOTO_BYTES) {
        toast.error(`Файл ${file.name} больше 5MB.`);
        return false;
      }
      return true;
    });

    if (validFiles.length === 0) return;

    try {
      const payloads = await Promise.all(validFiles.map(readTradePhoto));
      setTradeDraft((prev) =>
        prev ? { ...prev, entryPhotos: [...prev.entryPhotos, ...payloads] } : prev
      );
    } catch {
      toast.error("Не удалось прочитать фото.");
    }
  }

  function removeTradePhoto(index: number) {
    setTradeDraft((prev) => {
      if (!prev) return prev;
      const next = prev.entryPhotos.filter((_, idx) => idx !== index);
      return { ...prev, entryPhotos: next };
    });
  }

  async function handleBuy(signalId: string) {
    if (!onBuy || tradePending) return;
    setTradePending(signalId);
    try {
      await onBuy(signalId);
    } finally {
      setTradePending(null);
    }
  }

  function openSellModal(signal: SignalRow, trade: ApiTrade) {
    const sellAt = new Date().toISOString();
    const duration = calcMinutes(trade.buy_at, sellAt);
    const lastMeta = tradeMeta ?? {};
    setTradeDraft({
      tradeId: trade.id,
      signalId: signal.id,
      symbol: signal.symbol,
      buyAt: trade.buy_at,
      sellAt,
      durationMinutes: String(duration),
      profitPercent: "",
      profitUsd: "",
      timeframe: trade.timeframe ?? lastMeta.timeframe ?? "",
      exchange: trade.exchange ?? signal.exchange ?? lastMeta.exchange ?? "",
      strategy: trade.strategy_name ?? lastMeta.strategy ?? "",
      comment: trade.comment ?? "",
      entryBasis: trade.entry_basis ?? "",
      entryPhotos: trade.entry_photos ?? [],
    });
  }

  async function saveTrade() {
    if (!tradeDraft || !onCloseTrade) return;
    const durationRaw = tradeDraft.durationMinutes.trim();
    const profitPercentRaw = tradeDraft.profitPercent.trim();
    const profitUsdRaw = tradeDraft.profitUsd.trim();
    const timeframeRaw = tradeDraft.timeframe.trim();
    const exchangeRaw = tradeDraft.exchange.trim();
    const strategyRaw = tradeDraft.strategy.trim();
    const commentRaw = tradeDraft.comment.trim();
  const entryBasisRaw = tradeDraft.entryBasis.trim();
  const entryPhotos = tradeDraft.entryPhotos;

    if (!timeframeRaw) {
      toast.error("Выбери таймфрейм сделки.");
      return;
    }
    if (!exchangeRaw) {
      toast.error("Выбери биржу, на которой торговал.");
      return;
    }
    if (!strategyRaw) {
      toast.error("Укажи стратегию сделки.");
      return;
    }

    let duration: number | undefined;
    if (durationRaw !== "") {
      const parsed = Number(durationRaw);
      if (!Number.isFinite(parsed) || parsed < 0) {
        toast.error("Укажи корректную длительность в минутах.");
        return;
      }
      duration = Math.round(parsed);
    }

    let profitPercent: number | undefined;
    if (profitPercentRaw !== "") {
      const parsed = Number(profitPercentRaw);
      if (!Number.isFinite(parsed)) {
        toast.error("Укажи корректный процент прибыли.");
        return;
      }
      profitPercent = parsed;
    }

    let profitUsd: number | undefined;
    if (profitUsdRaw !== "") {
      const parsed = Number(profitUsdRaw);
      if (!Number.isFinite(parsed)) {
        toast.error("Укажи корректную прибыль в $." );
        return;
      }
      profitUsd = parsed;
    }

    const input: ApiCloseTradeInput = {
      sell_at: tradeDraft.sellAt,
      duration_minutes: duration,
      profit_percent: profitPercent,
      profit_usd: profitUsd,
      timeframe: timeframeRaw,
      exchange: exchangeRaw,
      strategy: strategyRaw,
      comment: commentRaw || undefined,
      entry_basis: entryBasisRaw || undefined,
      entry_photos: entryPhotos.length > 0 ? entryPhotos : undefined,
    };

    setTradePending(tradeDraft.tradeId);
    try {
      await onCloseTrade(tradeDraft.tradeId, input);
      setTradeDraft(null);
    } finally {
      setTradePending(null);
    }
  }

  function baseSymbol(symbol: string): string {
    const s = symbol.toUpperCase();
    const quotes = ["USDT", "USDC", "BUSD", "FDUSD", "USD", "BTC", "ETH"];
    for (const q of quotes) {
      if (s.endsWith(q) && s.length > q.length) return s.slice(0, -q.length);
    }
    return s;
  }

  function linksForSymbol(symbol: string, exchange: PrimaryExchange) {
    const sym = symbol.toUpperCase();
    const base = baseSymbol(sym);
    const tvSymbol = tradingViewPerpetualSymbol(exchange, sym);
    return {
      bybit: `https://www.bybit.com/en-US/trade/usdt/${sym}`,
      binance: `https://www.binance.com/en/futures/${sym}`,
      coinglass: coinGlassTVURL(exchange, sym),
      tradingview: `https://www.tradingview.com/chart/?symbol=${encodeURIComponent(tvSymbol)}`,
      base,
    };
  }

  function normalizeInfoSymbol(symbol: string) {
    const upper = symbol.toUpperCase().trim();
    if (upper.endsWith("PERP")) return upper.slice(0, -4);
    return upper;
  }

  const normalizedInfoSymbol = infoSymbol ? normalizeInfoSymbol(infoSymbol) : "";
  const infoBase = infoSymbol ? baseSymbol(normalizedInfoSymbol) : "";
  const infoLinks = infoSymbol ? linksForSymbol(infoSymbol, infoExchange) : null;
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

  return (
  <div className="relative min-h-[320px] overflow-hidden rounded-2xl border border-border bg-card p-6">

      <div className="relative mb-4 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold text-foreground">Таблица сигналов</h2>
        </div>
        <div className="flex flex-wrap items-end gap-2">
          <label className="grid gap-1 text-xs text-muted-foreground">
            <span>Сортировка</span>
            <AnimatedNativeSelect
              value={sortMode}
              onChange={(e) => setSortMode(e.target.value as SignalSortMode)}
              className="rounded-lg border border-border bg-background px-3 py-2 text-xs text-foreground outline-none focus:border-ring"
            >
              <option value="default">Без изменений</option>
              <option value="change_desc">По изменению: больше → меньше</option>
              <option value="change_asc">По изменению: меньше → больше</option>
            </AnimatedNativeSelect>
          </label>

          <label className="grid gap-1 text-xs text-muted-foreground">
            <span>Фильтр по времени</span>
            <AnimatedNativeSelect
              value={maxAgeMinutes}
              onChange={(e) => setMaxAgeMinutes(e.target.value)}
              className="rounded-lg border border-border bg-background px-3 py-2 text-xs text-foreground outline-none focus:border-ring"
            >
              <option value="0">Все</option>
              <option value="1">До 1 минуты</option>
              <option value="3">До 3 минут</option>
              <option value="5">До 5 минут</option>
              <option value="10">До 10 минут</option>
              <option value="15">До 15 минут</option>
              <option value="30">До 30 минут</option>
              <option value="60">До 60 минут</option>
            </AnimatedNativeSelect>
          </label>
        </div>
      </div>

      {!!error && !isLoading && (
        <div className="relative rounded-xl border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-amber-700 dark:text-amber-200">
          Не удалось загрузить сигналы. Проверь авторизацию и доступ к API.
          {error instanceof Error && error.message ? (
            <span className="block mt-2 text-amber-700 dark:text-amber-200">Причина: {error.message}</span>
          ) : null}
        </div>
      )}

      {!isLoading && sorted.length === 0 && !error && null}

      {sorted.length > 0 && (
        <>
          {/* ⬇️ ВАЖНО: тут таблица ТЕПЕРЬ w-full и без min-w */}
          <div className="relative w-full overflow-x-auto rounded-2xl border border-border bg-background">
            <table className="w-full table-fixed text-left text-sm text-foreground md:text-base">
              <thead className="bg-background text-xs uppercase text-muted-foreground">
                <tr>
                  <th className="w-[180px] px-4 py-3">Монета</th>
                  <th className="w-[120px] px-4 py-3">Биржа</th>
                  <th className="px-4 py-3">Критерий</th>
                  <th className="px-4 py-3">Изменения</th>
                  <th className="w-[220px] px-4 py-3">Время</th>
                </tr>
              </thead>

              <tbody>
                {shown.map((s) => {
                  const isOpen = expandedId === s.id;
                const signalExchange = s.exchange ?? "bybit";
                const links = linksForSymbol(s.symbol, signalExchange);
                  const isWatchlisted = watchlistSet.has(normalizeSymbol(s.symbol));
                  const openTrade = tradesBySignal.get(s.id);
                  const isNew = newSignalIds.has(s.id);
                  return (
                    <Fragment key={s.id}>
                      <motion.tr
                        whileTap={reduceMotion ? undefined : { scale: 0.998 }}
                        transition={{ duration: 0.16, ease: [0.22, 1, 0.36, 1] }}
                        className="border-t border-border cursor-pointer"
                        style={
                          isNew
                            ? {
                                animation: `signalSlideIn ${newSignalDuration}ms ease-out`,
                              }
                            : undefined
                        }
                        onClick={() => setExpandedId(isOpen ? null : s.id)}
                        title="Открыть быстрые ссылки"
                      >
                        <td className="px-4 py-3 font-semibold text-foreground">
                          <span className="inline-flex items-center gap-2">
                            <span className="h-2 w-2 rounded-full bg-primary" />
                            {s.symbol}
                            {isWatchlisted ? (
                              <span className="rounded-full border border-border-strong bg-accent px-2 py-0.5 text-[10px] font-semibold text-primary">
                                Wait list
                              </span>
                            ) : null}
                          </span>
                        </td>

                        <td className="px-4 py-3">
                          <span
                            className={
                              "inline-flex rounded-full border px-2.5 py-1 text-xs font-semibold " +
                              (s.combinedExchanges
                                ? "border-border-strong bg-accent text-primary"
                                : s.exchange === "binance"
                                  ? "border-amber-400/30 bg-amber-400/10 text-amber-700 dark:text-amber-200"
                                  : "border-cyan-400/30 bg-cyan-400/10 text-cyan-700 dark:text-cyan-200")
                            }
                          >
                            {s.combinedExchanges
                              ? "5 exchanges"
                              : s.exchange === "binance"
                                ? "Binance"
                                : "Bybit"}
                          </span>
                        </td>

                        <td className="px-4 py-3 text-foreground">
                          <span className="block truncate">{s.criteria}</span>
                        </td>

                        <td className="px-4 py-3 text-foreground">
                          <span className="block truncate">{s.changes}</span>
                        </td>

                        <td className="px-4 py-3 text-sm text-muted-foreground md:text-base">
                          {s.createdAt}
                        </td>
                      </motion.tr>

                      <AnimatePresence initial={false}>
                      {isOpen && (
                        <motion.tr
                          key={"details-" + s.id}
                          initial={reduceMotion ? false : { opacity: 0 }}
                          animate={{ opacity: 1 }}
                          exit={reduceMotion ? undefined : { opacity: 0 }}
                          transition={{ duration: reduceMotion ? 0 : 0.2 }}
                          className="border-t border-border bg-accent"
                        >
                          <td colSpan={5} className="p-0">
                            <motion.div
                              initial={reduceMotion ? false : { height: 0, opacity: 0, y: -8 }}
                              animate={{ height: "auto", opacity: 1, y: 0 }}
                              exit={reduceMotion ? undefined : { height: 0, opacity: 0, y: -6 }}
                              transition={{ duration: reduceMotion ? 0 : 0.26, ease: [0.22, 1, 0.36, 1] }}
                              className="overflow-hidden"
                            >
                              <div className="px-4 py-4">
                            <div className="rounded-xl border border-border-strong bg-background p-4 text-sm text-foreground">
                              <div className="flex flex-wrap items-start justify-between gap-4">
                                <div>
                                  <div className="text-xs uppercase tracking-wide text-muted-foreground">
                                    Перейти на монету {links.base} в
                                  </div>
                                  <div className="mt-3 flex flex-wrap gap-2">
                                    <a
                                      href={links.bybit}
                                      target="_blank"
                                      rel="noreferrer"
                                      onClick={(e) => e.stopPropagation()}
                                      className="rounded-lg border border-border bg-secondary px-3 py-2 text-xs font-semibold text-secondary-foreground transition-all duration-200 hover:-translate-y-0.5 hover:bg-accent hover:text-accent-foreground active:scale-[0.97]"
                                    >
                                      Bybit
                                    </a>
                                    <a
                                      href={links.binance}
                                      target="_blank"
                                      rel="noreferrer"
                                      onClick={(e) => e.stopPropagation()}
                                      className="rounded-lg border border-border bg-secondary px-3 py-2 text-xs font-semibold text-secondary-foreground transition-all duration-200 hover:-translate-y-0.5 hover:bg-accent hover:text-accent-foreground active:scale-[0.97]"
                                    >
                                      Binance
                                    </a>
                                    <a
                                      href={links.coinglass}
                                      target="_blank"
                                      rel="noreferrer"
                                      onClick={(e) => e.stopPropagation()}
                                      className="rounded-lg border border-border bg-secondary px-3 py-2 text-xs font-semibold text-secondary-foreground transition-all duration-200 hover:-translate-y-0.5 hover:bg-accent hover:text-accent-foreground active:scale-[0.97]"
                                    >
                                      Coinglass
                                    </a>
                                    <a
                                      href={links.tradingview}
                                      target="_blank"
                                      rel="noreferrer"
                                      onClick={(e) => e.stopPropagation()}
                                      className="rounded-lg border border-border bg-secondary px-3 py-2 text-xs font-semibold text-secondary-foreground transition-all duration-200 hover:-translate-y-0.5 hover:bg-accent hover:text-accent-foreground active:scale-[0.97]"
                                    >
                                      TradingView
                                    </a>
                                  </div>
                                </div>

                                <div className="ml-auto flex flex-col items-end gap-2">
                                  <div className="text-[11px] uppercase tracking-wide text-muted-foreground">
                                    Trade
                                  </div>
                                  <div className="flex flex-wrap items-center justify-end gap-2">
                                    <button
                                      onClick={() => {
                                        setInfoExchange(signalExchange);
                                        setInfoSymbol(s.symbol);
                                      }}
                                      className="rounded-lg border border-cyan-500/40 bg-cyan-500/15 px-4 py-2 text-xs font-semibold text-cyan-700 dark:text-cyan-200 transition-all duration-200 hover:-translate-y-0.5 hover:bg-cyan-500/25 active:scale-[0.97]"
                                    >
                                      Info
                                    </button>
                                    {openTrade ? (
                                      <button
                                        onClick={() => openSellModal(s, openTrade)}
                                        disabled={!canSell || isTradeBusy || tradePending !== null}
                                        className="rounded-lg border border-rose-500/40 bg-rose-500/15 px-4 py-2 text-xs font-semibold text-destructive transition-all duration-200 hover:-translate-y-0.5 hover:bg-rose-500/25 active:scale-[0.97] disabled:cursor-not-allowed disabled:opacity-60"
                                      >
                                        Sell
                                      </button>
                                    ) : (
                                      <button
                                        onClick={() => handleBuy(s.id)}
                                        disabled={!canBuy || isTradeBusy || tradePending !== null}
                                        className="rounded-lg border border-border-strong bg-accent px-4 py-2 text-xs font-semibold text-primary transition-all duration-200 hover:-translate-y-0.5 hover:bg-accent active:scale-[0.97] disabled:cursor-not-allowed disabled:opacity-60"
                                      >
                                        Buy
                                      </button>
                                    )}
                                  </div>
                                </div>
                              </div>
                            </div>
                            </div>
                            </motion.div>
                          </td>
                        </motion.tr>
                      )}
                      </AnimatePresence>
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          </div>

          {/* Load more */}
          <div className="relative mt-4 flex flex-wrap items-center justify-center gap-2">
            {canLoadMore ? (
              <button
                onClick={() => setVisible((v) => v + PAGE_SIZE)}
                className="rounded-xl border border-border bg-background px-4 py-2 text-sm font-semibold text-foreground hover:bg-secondary"
              >
                Показать ещё (+{PAGE_SIZE})
              </button>
            ) : (
              <div className="text-xs text-muted-foreground">Это все сигналы</div>
            )}
            {visible > PAGE_SIZE ? (
              <button
                onClick={() => setVisible(PAGE_SIZE)}
                className="rounded-xl border border-border bg-background px-4 py-2 text-sm font-semibold text-foreground hover:bg-secondary"
              >
                Скрыть
              </button>
            ) : null}
          </div>
        </>
      )}

      {tradeDraft ? (
        <CloseTradeModal
          draft={tradeDraft}
          strategyNames={strategyNames}
          busy={isTradeBusy || tradePending !== null}
          onChange={setTradeDraft}
          onDismiss={() => setTradeDraft(null)}
          onSave={() => { void saveTrade(); }}
          onAddPhotos={handleTradePhotoAdd}
          onRemovePhoto={removeTradePhoto}
        />
      ) : null}

      {infoSymbol && typeof document !== "undefined"
        ? createPortal(
            <div className="signal-modal-backdrop fixed inset-0 z-[90] flex items-center justify-center px-4 py-6">
              <button
                className="absolute inset-0 bg-scrim"
                onClick={() => setInfoSymbol(null)}
              />
              <div className="signal-modal-panel relative w-full max-w-[96vw] max-h-[94vh] overflow-y-auto rounded-3xl border border-border-strong bg-surface-raised p-6 text-sm text-foreground shadow-2xl">
            <div className="flex items-center gap-3 overflow-x-auto whitespace-nowrap pb-1 [scrollbar-width:thin]">
              <h3 className="shrink-0 text-xl font-semibold text-foreground sm:text-2xl">
                {infoSymbol.toUpperCase()} {coinInfo ? `• ${coinInfo.name}` : ""}
              </h3>
              {infoLinks ? (
                <div className="flex shrink-0 items-center gap-1.5">
                  <a href={infoLinks.bybit} target="_blank" rel="noreferrer" className="rounded-full border border-border bg-secondary px-3 py-1.5 text-[11px] font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground">Bybit</a>
                  <a href={infoLinks.binance} target="_blank" rel="noreferrer" className="rounded-full border border-border bg-secondary px-3 py-1.5 text-[11px] font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground">Binance</a>
                  <a href={infoLinks.coinglass} target="_blank" rel="noreferrer" className="rounded-full border border-border bg-secondary px-3 py-1.5 text-[11px] font-semibold text-secondary-foreground hover:bg-accent hover:text-accent-foreground">Coinglass</a>
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
              <PerpChart exchange={infoExchange} symbol={normalizedInfoSymbol} interval="1m" />
            </div>

            <CoinResearchPanel exchange={infoExchange} base={infoBase} symbol={normalizedInfoSymbol} coinInfo={coinInfo} infoError={infoError} />

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
            <SimilarityPanel symbol={normalizedInfoSymbol} exchange={infoExchange} />
              </div>
            </div>,
            document.body
          )
        : null}

      {isLoading && (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-2xl bg-background">
          <div className="rounded-full border border-border bg-secondary px-4 py-2 text-xs text-foreground shadow">
            Loading...
          </div>
        </div>
      )}

      <style>{`
        @keyframes scan {
          from { transform: translateY(0); }
          to { transform: translateY(220%); }
        }
        @keyframes signalModalBackdropIn { from { opacity: 0; } to { opacity: 1; } }
        @keyframes signalModalPanelIn { from { opacity: 0; transform: translateY(18px) scale(.97); filter: blur(4px); } to { opacity: 1; transform: translateY(0) scale(1); filter: blur(0); } }
        .signal-modal-backdrop { animation: signalModalBackdropIn .22s ease-out both; }
        .signal-modal-panel { animation: signalModalPanelIn .32s cubic-bezier(.22,1,.36,1) both; }
        @media (prefers-reduced-motion: reduce) { .signal-modal-backdrop, .signal-modal-panel { animation: none !important; } }
        @keyframes signalSlideIn {
          from { opacity: 0; transform: translateY(-10px); }
          to { opacity: 1; transform: translateY(0); }
        }
      `}</style>
    </div>
  );
}

