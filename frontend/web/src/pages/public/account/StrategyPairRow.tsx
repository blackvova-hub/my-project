import { Fragment, useMemo, useState } from "react";
import type { ApiTrade, ApiTradeStrategyPair } from "../scanners/types";

type Props = {
  strategyId: string;
  strategyName: string;
  pair: ApiTradeStrategyPair;
  trades?: ApiTrade[];
  deletePending: boolean;
  onDelete: () => void;
  onSelectTrade: (trade: ApiTrade) => void;
};

function formatDate(value?: string | null) {
  if (!value) return "Открыта";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString();
}

function formatPercent(value?: number | null) {
  if (value === null || value === undefined || !Number.isFinite(value)) return "—";
  return `${value.toFixed(2)}%`;
}

function formatUsd(value?: number | null) {
  if (value === null || value === undefined || !Number.isFinite(value)) return "—";
  return `$${value.toFixed(2)}`;
}

function resultTone(value?: number | null) {
  if (value === null || value === undefined || !Number.isFinite(value) || value === 0) {
    return "text-muted-foreground";
  }
  return value > 0 ? "text-primary" : "text-destructive";
}

function strategyMatches(trade: ApiTrade, strategyId: string, strategyName: string) {
  if (trade.strategy_id) return trade.strategy_id === strategyId;
  return trade.strategy_name?.trim().toLocaleLowerCase() === strategyName.trim().toLocaleLowerCase();
}

export default function StrategyPairRow({
  strategyId,
  strategyName,
  pair,
  trades,
  deletePending,
  onDelete,
  onSelectTrade,
}: Props) {
  const [expanded, setExpanded] = useState(false);
  const pairTrades = useMemo(() => {
    const normalizedSymbol = pair.symbol.trim().toLocaleUpperCase();
    return (trades ?? [])
      .filter(
        (trade) =>
          (trade.symbol ?? "").trim().toLocaleUpperCase() === normalizedSymbol &&
          strategyMatches(trade, strategyId, strategyName),
      )
      .sort((left, right) => {
        const leftTime = Date.parse(left.sell_at ?? left.buy_at);
        const rightTime = Date.parse(right.sell_at ?? right.buy_at);
        return rightTime - leftTime;
      });
  }, [pair.symbol, strategyId, strategyName, trades]);

  return (
    <Fragment>
      <tr className={`border-t border-border transition-colors ${expanded ? "bg-card" : "hover:bg-card"}`}>
        <td className="px-4 py-3 font-semibold text-foreground">
          <button
            type="button"
            aria-expanded={expanded}
            aria-controls={`pair-trades-${strategyId}-${pair.symbol}`}
            onClick={() => setExpanded((current) => !current)}
            className="group inline-flex min-h-9 items-center gap-2 rounded-lg px-2 py-1 text-left transition-colors hover:bg-accent hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <span>{pair.symbol}</span>
            <svg
              aria-hidden="true"
              viewBox="0 0 20 20"
              className={`h-4 w-4 text-muted-foreground transition-transform group-hover:text-primary ${expanded ? "rotate-180" : ""}`}
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="m6 8 4 4 4-4" />
            </svg>
          </button>
        </td>
        <td className="px-4 py-3">{pair.total_trades}</td>
        <td className="px-4 py-3">{formatPercent(pair.win_rate ?? null)}</td>
        <td className="px-4 py-3">{formatPercent(pair.sum_profit_percent ?? null)}</td>
        <td className="px-4 py-3">{formatUsd(pair.sum_profit_usd ?? null)}</td>
        <td className="px-4 py-3">
          {pair.avg_duration_minutes === null || pair.avg_duration_minutes === undefined
            ? "—"
            : pair.avg_duration_minutes.toFixed(0)}
        </td>
        <td className="px-4 py-3 text-right">
          <button
            type="button"
            onClick={onDelete}
            disabled={deletePending}
            className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-1 text-xs font-semibold text-amber-700 dark:text-amber-200 transition hover:bg-amber-500/20 disabled:cursor-not-allowed disabled:opacity-50"
          >
            Удалить пару
          </button>
        </td>
      </tr>

      {expanded ? (
        <tr className="border-t border-border-strong">
          <td colSpan={7} className="p-0">
            <section id={`pair-trades-${strategyId}-${pair.symbol}`} className="bg-card/80 px-4 py-4 sm:px-5">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <div className="text-sm font-semibold text-foreground">Сделки {pair.symbol}</div>
                  <div className="mt-0.5 text-xs text-muted-foreground">Нажмите на сделку, чтобы посмотреть подробности</div>
                </div>
                <span className="shrink-0 rounded-full border border-border bg-background px-2.5 py-1 text-xs text-muted-foreground">
                  {pairTrades.length}
                </span>
              </div>

              {pairTrades.length > 0 ? (
                <div className="mt-3 grid gap-2">
                  {pairTrades.map((trade) => (
                    <button
                      key={trade.id}
                      type="button"
                      onClick={() => onSelectTrade(trade)}
                      className="grid min-w-[720px] grid-cols-[minmax(240px,1.6fr)_110px_110px_110px_24px] items-center gap-3 rounded-xl border border-border bg-background px-4 py-3 text-left text-sm transition hover:border-border-strong hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    >
                      <span className="min-w-0">
                        <span className="block text-[10px] uppercase tracking-wide text-muted-foreground">Buy → Sell</span>
                        <span className="mt-1 block whitespace-nowrap font-medium text-foreground">
                          {formatDate(trade.buy_at)} → {formatDate(trade.sell_at)}
                        </span>
                      </span>
                      <span className={resultTone(trade.profit_percent)}>
                        <span className="block text-[10px] uppercase tracking-wide text-muted-foreground">Профит %</span>
                        <span className="mt-1 block font-semibold">{formatPercent(trade.profit_percent)}</span>
                      </span>
                      <span className={resultTone(trade.profit_usd)}>
                        <span className="block text-[10px] uppercase tracking-wide text-muted-foreground">Профит $</span>
                        <span className="mt-1 block font-semibold">{formatUsd(trade.profit_usd)}</span>
                      </span>
                      <span className="text-muted-foreground">
                        <span className="block text-[10px] uppercase tracking-wide text-muted-foreground">Биржа</span>
                        <span className="mt-1 block font-medium">{trade.exchange ?? "—"}</span>
                      </span>
                      <svg aria-hidden="true" viewBox="0 0 20 20" className="h-4 w-4 text-muted-foreground" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
                        <path d="m8 5 5 5-5 5" />
                      </svg>
                    </button>
                  ))}
                </div>
              ) : (
                <div className="mt-3 rounded-xl border border-dashed border-border bg-background px-4 py-5 text-center text-sm text-muted-foreground">
                  Сделки этой пары не найдены.
                </div>
              )}
            </section>
          </td>
        </tr>
      ) : null}
    </Fragment>
  );
}
