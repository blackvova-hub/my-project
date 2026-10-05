import { useEffect } from "react";
import { createPortal } from "react-dom";
import type { ApiTrade } from "../scanners/types";

type Props = {
  trade: ApiTrade;
  onDismiss: () => void;
};

function formatDate(value?: string | null) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString();
}

function formatNumber(value?: number | null, digits = 2) {
  if (value === null || value === undefined || !Number.isFinite(value)) return "—";
  return value.toFixed(digits);
}

function formatPercent(value?: number | null) {
  const formatted = formatNumber(value);
  return formatted === "—" ? formatted : `${formatted}%`;
}

function formatUsd(value?: number | null) {
  const formatted = formatNumber(value);
  return formatted === "—" ? formatted : `$${formatted}`;
}

function resultTone(value?: number | null) {
  if (value === null || value === undefined || !Number.isFinite(value) || value === 0) {
    return "text-foreground";
  }
  return value > 0 ? "text-primary" : "text-destructive";
}

function DetailTile({ label, value, valueClass = "text-foreground" }: { label: string; value: string; valueClass?: string }) {
  return (
    <div className="min-w-0 rounded-xl border border-border bg-card px-3.5 py-3 sm:px-4">
      <div className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">{label}</div>
      <div className={`mt-1.5 break-words text-sm font-semibold ${valueClass}`}>{value}</div>
    </div>
  );
}

export default function TradeDetailsModal({ trade, onDismiss }: Props) {
  useEffect(() => {
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") onDismiss();
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [onDismiss]);

  if (typeof document === "undefined") return null;

  const symbol = trade.symbol ?? trade.signal_id;

  return createPortal(
    <div className="fixed inset-0 z-[130] flex items-end justify-center p-2 sm:items-center sm:p-6">
      <button
        type="button"
        aria-label="Закрыть подробности сделки"
        className="absolute inset-0 bg-scrim"
        onClick={onDismiss}
      />

      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="trade-details-title"
        className="relative flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-4xl flex-col overflow-hidden rounded-[22px] border border-border bg-surface-raised text-foreground [box-shadow:var(--shadow-overlay)] sm:max-h-[92vh] sm:w-full sm:rounded-3xl"
      >
        <header className="flex shrink-0 items-center gap-3 border-b border-border bg-surface-raised px-4 py-4 sm:px-6">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-border-strong bg-accent text-primary">
            <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 7h13" />
              <path d="m15 4 3 3-3 3" />
              <path d="M19 17H6" />
              <path d="m9 14-3 3 3 3" />
            </svg>
          </div>
          <div className="min-w-0">
            <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-primary">Сделка</div>
            <h2 id="trade-details-title" className="truncate text-lg font-semibold tracking-tight sm:text-xl">{symbol}</h2>
          </div>
          <button
            type="button"
            onClick={onDismiss}
            aria-label="Закрыть"
            className="ml-auto inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-border bg-card text-muted-foreground transition-colors hover:border-border-strong hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <svg aria-hidden="true" viewBox="0 0 20 20" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
              <path d="m5 5 10 10M15 5 5 15" />
            </svg>
          </button>
        </header>

        <div className="site-scrollbar min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-4 sm:px-6 sm:py-5">
          <div className="grid gap-3 sm:grid-cols-2">
            <DetailTile label="Buy" value={formatDate(trade.buy_at)} />
            <DetailTile label="Sell" value={formatDate(trade.sell_at)} />
          </div>

          <div className="mt-3 grid grid-cols-2 gap-3 lg:grid-cols-4">
            <DetailTile label="Прибыль (%)" value={formatPercent(trade.profit_percent)} valueClass={resultTone(trade.profit_percent)} />
            <DetailTile label="Прибыль ($)" value={formatUsd(trade.profit_usd)} valueClass={resultTone(trade.profit_usd)} />
            <DetailTile label="Биржа" value={trade.exchange ?? "—"} />
            <DetailTile label="Таймфрейм" value={trade.timeframe ?? "—"} />
            <DetailTile label="Стратегия" value={trade.strategy_name ?? "—"} />
            <DetailTile label="Длительность" value={`${formatNumber(trade.duration_minutes, 0)} мин`} />
          </div>

          <div className="mt-4 grid gap-3 md:grid-cols-2">
            <div className="rounded-2xl border border-border bg-card p-4">
              <div className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">Комментарий</div>
              <div className="mt-2 whitespace-pre-wrap break-words text-sm leading-6 text-foreground">{trade.comment ?? "—"}</div>
            </div>
            <div className="rounded-2xl border border-border bg-card p-4">
              <div className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">Основание входа</div>
              <div className="mt-2 whitespace-pre-wrap break-words text-sm leading-6 text-foreground">{trade.entry_basis ?? "—"}</div>
            </div>
          </div>

          <div className="mt-4 rounded-2xl border border-border bg-card p-4">
            <div className="text-[10px] font-medium uppercase tracking-[0.12em] text-muted-foreground">Фото</div>
            {trade.entry_photos && trade.entry_photos.length > 0 ? (
              <div className="mt-3 grid gap-3 sm:grid-cols-3">
                {trade.entry_photos.map((photo, index) => (
                  <a key={`${trade.id}-photo-${index}`} href={photo} target="_blank" rel="noreferrer" className="overflow-hidden rounded-xl border border-border bg-background transition hover:border-border-strong">
                    <img src={photo} alt={`Скриншот сделки ${symbol} ${index + 1}`} loading="lazy" decoding="async" className="h-32 w-full object-cover" />
                  </a>
                ))}
              </div>
            ) : (
              <div className="mt-2 text-sm text-muted-foreground">Фото не добавлены.</div>
            )}
          </div>
        </div>

        <footer className="flex shrink-0 justify-end border-t border-border bg-card px-4 py-3 sm:px-6">
          <button type="button" onClick={onDismiss} className="min-h-10 rounded-xl border border-border bg-card px-5 py-2 text-sm font-medium text-muted-foreground transition hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            Закрыть
          </button>
        </footer>
      </section>
    </div>,
    document.body
  );
}
