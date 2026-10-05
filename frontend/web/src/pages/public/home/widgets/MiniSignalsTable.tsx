import { Fragment, useEffect, useMemo, useState, useRef } from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import type {
  ApiCloseTradeInput,
  ApiTrade,
  ApiTradeStrategy,
  SignalRow,
} from "../../scanners/types";
import { useSymbolTradeStats } from "../../scanners/queries";
import { useToast } from "../../../../shared/ui/ToastProvider";
import {
  coinGlassTVURL,
  tradingViewPerpetualSymbol,
  type PrimaryExchange,
} from "../../../../shared/exchange/primaryExchange";
import {
  loadWatchlist,
  normalizeSymbol,
  syncWatchlist,
  WATCHLIST_UPDATED_EVENT,
} from "../../../../shared/watchlist";
import { useCoinInfo } from "../../../../shared/market/coinInfoQueries";
import { CoinResearchPanel } from "../../../../shared/market/CoinResearchPanel";
import { SimilarityPanel } from "../../../../shared/market/SimilarityPanel";
import { PerpChart } from "../../../../shared/market/PerpChart";
import { CloseTradeModal, type TradeCloseDraft } from "../../../../shared/trades/CloseTradeModal";

const EMPTY_TRADES: ApiTrade[] = [];
const EMPTY_STRATEGIES: ApiTradeStrategy[] = [];

export function MiniSignalsTable(props: {
  rows: SignalRow[];
  trades?: ApiTrade[];
  strategies?: ApiTradeStrategy[];
  tradeMeta?: { timeframe?: string | null; exchange?: string | null; strategy?: string | null };
  onBuy?: (signalId: string) => Promise<ApiTrade>;
  onCloseTrade?: (tradeId: string, input: ApiCloseTradeInput) => Promise<ApiTrade>;
  isTradeBusy?: boolean;
}) {
  const allRows = props.rows;
  const rows = useMemo(() => allRows.slice(0, 15), [allRows]);
  const trades = props.trades ?? EMPTY_TRADES;
  const strategies = props.strategies ?? EMPTY_STRATEGIES;
  const tradeMeta = props.tradeMeta;
  const onBuy = props.onBuy;
  const onCloseTrade = props.onCloseTrade;
  const isTradeBusy = props.isTradeBusy ?? false;
  const reduceMotion = useReducedMotion();
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [watchlist, setWatchlist] = useState<string[]>(() => loadWatchlist());
  const [tradeDraft, setTradeDraft] = useState<TradeCloseDraft | null>(null);
  const [tradePending, setTradePending] = useState<string | null>(null);
  const [infoSymbol, setInfoSymbol] = useState<string | null>(null);
  const [infoExchange, setInfoExchange] = useState<PrimaryExchange>("bybit");
  const [newSignalIds, setNewSignalIds] = useState<Set<string>>(() => new Set());
  const [newSignalDuration, setNewSignalDuration] = useState(420);
  const lastSignalIdsRef = useRef<string[]>([]);
  const lastBurstAtRef = useRef<number>(0);
  const toast = useToast();
  const canBuy = Boolean(onBuy);
  const canSell = Boolean(onCloseTrade);
  const MAX_TRADE_PHOTOS = 3;
  const MAX_TRADE_PHOTO_BYTES = 5 * 1024 * 1024;

  useEffect(() => {
    void syncWatchlist();
    const handler = () => setWatchlist(loadWatchlist());
    window.addEventListener(WATCHLIST_UPDATED_EVENT, handler);
    return () => window.removeEventListener(WATCHLIST_UPDATED_EVENT, handler);
  }, []);

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
    const currentIds = allRows.map((s) => s.id);
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
  }, [allRows]);

  const strategyNames = useMemo(() => {
    const names = strategies.map((s) => s.name).filter(Boolean);
    return Array.from(new Set(names));
  }, [strategies]);


  function slotLabel(slot?: string) {
    switch ((slot ?? "SLOT_1").toUpperCase()) {
      case "SLOT_2":
        return "Сканер 2";
      case "SLOT_3":
        return "Сканер 3";
      default:
        return "Сканер 1";
    }
  }

  function fmtTime(value: string) {
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return value;
    return d.toLocaleTimeString();
  }

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
        toast.error("Укажи корректную прибыль в $.");
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

  function formatChangeValue(raw: string) {
    return raw.trim() || "—";
  }

  function linksForSymbol(symbol: string, exchange: PrimaryExchange) {
    const sym = normalizeSymbol(symbol);
    const base = baseSymbol(sym);
    const tvSymbol = tradingViewPerpetualSymbol(exchange, sym);
    return {
      base: base || sym,
      bybit: `https://www.bybit.com/en-US/trade/usdt/${sym}`,
      binance: `https://www.binance.com/en/futures/${sym}`,
      coinglass: coinGlassTVURL(exchange, sym),
      tradingview: `https://www.tradingview.com/chart/?symbol=${encodeURIComponent(tvSymbol)}`,
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
    <>
      <style>{`@keyframes signalSlideIn { from { opacity: 0; transform: translateY(-10px); } to { opacity: 1; transform: translateY(0); } }`}</style>
  <div className="h-full overflow-hidden rounded-2xl border border-border bg-card min-w-0">
      <div className="site-scrollbar h-full overflow-auto">
        <table className="w-full border-collapse table-fixed">
          <thead className="bg-background sticky top-0 z-10">
            <tr className="text-left text-xs text-muted-foreground">
              <th className="px-4 py-3 w-[60px]">#</th>
              <th className="px-4 py-3 w-[140px]">Монета</th>
              <th className="px-4 py-3 w-[110px]">Биржа</th>
              <th className="px-4 py-3">Критерии</th>
              <th className="px-4 py-3 w-[220px]">% по индикаторам</th>
              <th className="px-4 py-3 w-[140px]">Сканер</th>
              <th className="px-4 py-3 w-[130px]">Время</th>
            </tr>
          </thead>
          <tbody className="bg-card">
            {rows.length ? (
              rows.map((r, idx) => {
                const isOpen = expandedId === r.id;
                const isWatchlisted = watchlistSet.has(normalizeSymbol(r.symbol));
              const signalExchange = r.exchange ?? "bybit";
              const links = linksForSymbol(r.symbol, signalExchange);
                const openTrade = tradesBySignal.get(r.id);
                const isNew = newSignalIds.has(r.id);
                return (
                  <Fragment key={r.id}>
                    <motion.tr
                      whileTap={reduceMotion ? undefined : { scale: 0.998 }}
                      transition={{ duration: 0.16, ease: [0.22, 1, 0.36, 1] }}
                      className="border-t border-border text-sm text-foreground cursor-pointer"
                      style={
                        isNew
                          ? {
                              animation: `signalSlideIn ${newSignalDuration}ms ease-out`,
                            }
                          : undefined
                      }
                      onClick={() => setExpandedId(isOpen ? null : r.id)}
                      title="Открыть быстрые ссылки"
                    >
                      <td className="px-4 py-3 text-muted-foreground">{idx + 1}</td>
                      <td className="px-4 py-3">
                        <div className="font-semibold flex flex-wrap items-center gap-2">
                          {r.symbol}
                          {isWatchlisted ? (
                              <span className="rounded-full border border-border-strong bg-accent px-2 py-0.5 text-[10px] font-semibold text-primary">
                                Wait list
                              </span>
                          ) : null}
                        </div>
                      </td>
                      <td className="px-4 py-3 text-xs font-semibold">
                        <span
                          className={
                            "inline-flex rounded-full border px-2 py-1 " +
                            (r.combinedExchanges
                              ? "border-border-strong bg-accent text-primary"
                              : r.exchange === "binance"
                                ? "border-amber-400/30 bg-amber-400/10 text-amber-700 dark:text-amber-200"
                                : "border-cyan-400/30 bg-cyan-400/10 text-cyan-700 dark:text-cyan-200")
                          }
                        >
                          {r.combinedExchanges
                            ? "Bybit + Binance"
                            : r.exchange === "binance"
                              ? "Binance"
                              : "Bybit"}
                        </span>
                      </td>
                      <td className="px-4 py-3">
                        <div className="text-foreground line-clamp-2" title={r.criteria}>
                          {r.criteria}
                        </div>
                      </td>
                      <td className="px-4 py-3 text-xs text-muted-foreground" title={r.changes}>
                        {formatChangeValue(r.changes)}
                      </td>
                      <td className="px-4 py-3 text-xs text-muted-foreground">
                        {slotLabel(r.scannerSlot)}
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">{fmtTime(r.createdAt)}</td>
                    </motion.tr>

                    <AnimatePresence initial={false}>
                    {isOpen && (
                      <motion.tr
                        key={"details-" + r.id}
                        initial={reduceMotion ? false : { opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={reduceMotion ? undefined : { opacity: 0 }}
                        transition={{ duration: reduceMotion ? 0 : 0.2 }}
                        className="border-t border-border bg-card"
                      >
                        <td colSpan={7} className="p-0">
                          <motion.div
                            initial={reduceMotion ? false : { height: 0, opacity: 0, y: -8 }}
                            animate={{ height: "auto", opacity: 1, y: 0 }}
                            exit={reduceMotion ? undefined : { height: 0, opacity: 0, y: -6 }}
                            transition={{ duration: reduceMotion ? 0 : 0.26, ease: [0.22, 1, 0.36, 1] }}
                            className="overflow-hidden"
                          >
                            <div className="px-4 py-4">
                          <div className="rounded-xl border border-border bg-background p-4 text-sm text-foreground">
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
                                      setInfoSymbol(r.symbol);
                                    }}
                                    className="rounded-lg border border-cyan-500/40 bg-cyan-500/15 px-4 py-2 text-xs font-semibold text-cyan-700 dark:text-cyan-200 transition-all duration-200 hover:-translate-y-0.5 hover:bg-cyan-500/25 active:scale-[0.97]"
                                  >
                                    Info
                                  </button>
                                  {openTrade ? (
                                    <button
                                      onClick={() => openSellModal(r, openTrade)}
                                      disabled={!canSell || isTradeBusy || tradePending !== null}
                                      className="rounded-lg border border-rose-500/40 bg-rose-500/15 px-4 py-2 text-xs font-semibold text-destructive transition-all duration-200 hover:-translate-y-0.5 hover:bg-rose-500/25 active:scale-[0.97] disabled:cursor-not-allowed disabled:opacity-60"
                                    >
                                      Sell
                                    </button>
                                  ) : (
                                    <button
                                      onClick={() => handleBuy(r.id)}
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
              })
            ) : (
              <tr className="border-t border-border">
                <td className="px-4 py-6 text-sm text-muted-foreground" colSpan={6}>
                  Пока нет сигналов.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      </div>
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
              <button className="absolute inset-0 bg-scrim" onClick={() => setInfoSymbol(null)} />
              <div className="signal-modal-panel relative max-h-[94vh] w-full max-w-[96vw] overflow-y-auto rounded-3xl border border-border-strong bg-surface-raised p-4 text-sm text-foreground shadow-2xl sm:p-5">
            <div className="mb-3 flex items-center gap-3 overflow-x-auto whitespace-nowrap pb-1 [scrollbar-width:thin]">
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

            <div className="grid gap-4">
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
    </>
  );
}
