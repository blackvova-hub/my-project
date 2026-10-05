import { useEffect, useEffectEvent, useId, type FormEvent } from "react";
import { createPortal } from "react-dom";
import { AnimatedSelect, type AnimatedSelectOption } from "../ui/AnimatedSelect";

export type TradeCloseDraft = {
  tradeId: string;
  signalId: string;
  symbol: string;
  buyAt: string;
  sellAt: string;
  durationMinutes: string;
  profitPercent: string;
  profitUsd: string;
  timeframe: string;
  exchange: string;
  strategy: string;
  comment: string;
  entryBasis: string;
  entryPhotos: string[];
};

type Props = {
  draft: TradeCloseDraft;
  strategyNames: string[];
  busy: boolean;
  onChange: (draft: TradeCloseDraft) => void;
  onDismiss: () => void;
  onSave: () => void;
  onAddPhotos: (files: FileList | null) => void | Promise<void>;
  onRemovePhoto: (index: number) => void;
};

const TIMEFRAME_OPTIONS: AnimatedSelectOption[] = [
  { value: "", label: "Выберите таймфрейм" },
  ...["1m", "3m", "5m", "15m", "30m", "1h", "2h", "4h", "6h", "12h", "1d", "1w", "1M"].map((value) => ({ value, label: value })),
];

const EXCHANGE_OPTIONS: AnimatedSelectOption[] = [
  { value: "", label: "Выберите биржу" },
  ...["Bybit", "Binance", "OKX", "Bitget", "KuCoin", "Gate", "HTX", "MEXC", "BingX"].map((value) => ({ value, label: value })),
];

const fieldClass = "min-h-12 min-w-0 w-full rounded-xl border border-input bg-background px-3.5 py-3 text-sm text-foreground outline-none transition-[border-color,background-color,box-shadow] placeholder:text-muted-foreground hover:border-border-strong focus:border-ring focus:bg-background focus:ring-2 focus:ring-ring/20";
const labelClass = "grid min-w-0 gap-2 text-xs font-medium text-muted-foreground";

const tradeDate = new Intl.DateTimeFormat("ru-RU", {
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});

function formatTradeDate(value: string) {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : tradeDate.format(parsed);
}

export function CloseTradeModal(props: Props) {
  const { draft, strategyNames, busy, onChange, onDismiss, onSave, onAddPhotos, onRemovePhoto } = props;
  const strategyListId = useId();
  const dismissFromEffect = useEffectEvent(onDismiss);

  useEffect(() => {
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") dismissFromEffect();
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, []);

  const update = <K extends keyof TradeCloseDraft>(field: K, value: TradeCloseDraft[K]) => {
    onChange({ ...draft, [field]: value });
  };

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!busy) onSave();
  };

  if (typeof document === "undefined") return null;

  return createPortal(
    <div className="fixed inset-0 z-[110] flex items-end justify-center p-2 sm:items-center sm:p-6">
      <button
        type="button"
        aria-label="Закрыть форму сделки"
        className="absolute inset-0 bg-scrim"
        onClick={onDismiss}
      />

      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="close-trade-title"
        className="relative flex min-w-0 max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden rounded-[22px] border border-border bg-popover text-popover-foreground shadow-xl sm:max-h-[92vh] sm:w-full sm:max-w-3xl sm:rounded-3xl"
      >
        <header className="flex min-w-0 shrink-0 items-center gap-3 border-b border-border bg-card px-4 py-4 sm:px-6">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-primary/30 bg-primary/10 text-primary">
            <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 7h13" />
              <path d="m15 4 3 3-3 3" />
              <path d="M19 17H6" />
              <path d="m9 14-3 3 3 3" />
            </svg>
          </div>
          <div className="min-w-0">
            <h3 id="close-trade-title" className="truncate text-lg font-semibold tracking-tight text-foreground sm:text-xl">
              {draft.symbol}
            </h3>
            <div className="text-xs text-muted-foreground">Закрытие сделки</div>
          </div>
          <button
            type="button"
            onClick={onDismiss}
            aria-label="Закрыть"
            className="ml-auto inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border border-border bg-secondary text-muted-foreground transition-colors hover:border-border-strong hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <svg aria-hidden="true" viewBox="0 0 20 20" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
              <path d="m5 5 10 10M15 5 5 15" />
            </svg>
          </button>
        </header>

        <form onSubmit={submit} className="flex min-h-0 min-w-0 flex-1 flex-col">
          <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain px-4 py-4 [scrollbar-color:var(--muted-foreground)_transparent] sm:px-6 sm:py-5">
            <div className="grid min-w-0 grid-cols-1 overflow-hidden rounded-2xl border border-border bg-card sm:grid-cols-2">
              <div className="min-w-0 border-b border-border px-3 py-3 sm:border-b-0 sm:border-r sm:px-4">
                <div className="flex items-center gap-2 text-[10px] uppercase tracking-[0.12em] text-muted-foreground">
                  <span className="h-1.5 w-1.5 rounded-full bg-primary" />
                  Вход
                </div>
                <div className="mt-1.5 break-words text-xs font-medium text-foreground sm:text-sm">{formatTradeDate(draft.buyAt)}</div>
              </div>
              <div className="min-w-0 px-3 py-3 sm:px-4">
                <div className="flex items-center gap-2 text-[10px] uppercase tracking-[0.12em] text-muted-foreground">
                  <span className="h-1.5 w-1.5 rounded-full bg-rose-400" />
                  Выход
                </div>
                <div className="mt-1.5 break-words text-xs font-medium text-foreground sm:text-sm">{formatTradeDate(draft.sellAt)}</div>
              </div>
            </div>

            <div className="mt-5 grid gap-4 sm:grid-cols-2">
              <label className={labelClass}>
                <span>Таймфрейм</span>
                <AnimatedSelect
                  value={draft.timeframe}
                  onChange={(value) => update("timeframe", value)}
                  options={TIMEFRAME_OPTIONS}
                  className={fieldClass}
                  ariaLabel="Таймфрейм сделки"
                />
              </label>

              <label className={labelClass}>
                <span>Биржа</span>
                <AnimatedSelect
                  value={draft.exchange}
                  onChange={(value) => update("exchange", value)}
                  options={EXCHANGE_OPTIONS}
                  className={fieldClass}
                  ariaLabel="Биржа сделки"
                />
              </label>

              <label className={`${labelClass} sm:col-span-2`}>
                <span>Стратегия</span>
                <input
                  list={strategyListId}
                  value={draft.strategy}
                  onChange={(event) => update("strategy", event.target.value)}
                  placeholder="Например: рост на 1% за 10 минут"
                  className={fieldClass}
                />
                <datalist id={strategyListId}>
                  {strategyNames.map((name) => <option key={name} value={name} />)}
                </datalist>
              </label>

              <label className={labelClass}>
                <span>Комментарий</span>
                <textarea
                  rows={4}
                  value={draft.comment}
                  onChange={(event) => update("comment", event.target.value)}
                  placeholder="Что важно помнить по сделке"
                  className={`${fieldClass} min-h-28 resize-y`}
                />
              </label>

              <label className={labelClass}>
                <span>Основание входа</span>
                <textarea
                  rows={4}
                  value={draft.entryBasis}
                  onChange={(event) => update("entryBasis", event.target.value)}
                  placeholder="На что опирались при входе в сделку"
                  className={`${fieldClass} min-h-28 resize-y`}
                />
              </label>
            </div>

            <div className="mt-5 rounded-2xl border border-dashed border-border bg-secondary p-3.5 sm:p-4">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <div className="text-xs font-medium text-secondary-foreground">Скриншоты сделки</div>
                  <div className="mt-1 text-[10px] text-muted-foreground">До 3 изображений, не больше 5 МБ каждое</div>
                </div>
                <span className="rounded-full border border-border bg-card px-2.5 py-1 text-[10px] text-muted-foreground">{draft.entryPhotos.length}/3</span>
              </div>

              <label className="mt-3 flex min-h-11 cursor-pointer items-center justify-center gap-2 rounded-xl border border-border bg-card px-4 py-2.5 text-xs font-semibold text-card-foreground transition-colors hover:bg-accent focus-within:ring-2 focus-within:ring-ring">
                <svg aria-hidden="true" viewBox="0 0 20 20" className="h-4 w-4" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M10 13V3m0 0L6.5 6.5M10 3l3.5 3.5" />
                  <path d="M4 11v4a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2v-4" />
                </svg>
                Добавить скриншоты
                <input
                  type="file"
                  accept="image/*"
                  multiple
                  disabled={draft.entryPhotos.length >= 3}
                  onChange={(event) => {
                    void onAddPhotos(event.target.files);
                    event.currentTarget.value = "";
                  }}
                  className="sr-only"
                />
              </label>

              {draft.entryPhotos.length > 0 ? (
                <div className="mt-3 grid grid-cols-3 gap-2">
                  {draft.entryPhotos.map((photo, index) => (
                    <div key={`${draft.tradeId}-photo-${index}`} className="group relative overflow-hidden rounded-xl border border-border bg-background">
                      <img src={photo} alt={`Скриншот сделки ${draft.symbol} ${index + 1}`} className="h-20 w-full object-cover sm:h-24" />
                      <button
                        type="button"
                        onClick={() => onRemovePhoto(index)}
                        aria-label={`Удалить скриншот ${index + 1}`}
                        className="absolute right-1.5 top-1.5 inline-flex h-7 w-7 items-center justify-center rounded-lg border border-border bg-background text-foreground transition-colors hover:border-destructive/40 hover:bg-destructive/10 hover:text-destructive focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      >
                        <svg aria-hidden="true" viewBox="0 0 20 20" className="h-3.5 w-3.5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
                          <path d="m5 5 10 10M15 5 5 15" />
                        </svg>
                      </button>
                    </div>
                  ))}
                </div>
              ) : null}
            </div>

            <div className="mt-5 grid gap-4 sm:grid-cols-3">
              <label className={labelClass}>
                <span>Длительность, мин</span>
                <input
                  type="number"
                  min={0}
                  step={1}
                  inputMode="numeric"
                  value={draft.durationMinutes}
                  onChange={(event) => update("durationMinutes", event.target.value)}
                  className={fieldClass}
                />
              </label>
              <label className={labelClass}>
                <span>Результат, %</span>
                <input
                  type="number"
                  step="0.01"
                  inputMode="decimal"
                  value={draft.profitPercent}
                  onChange={(event) => update("profitPercent", event.target.value)}
                  placeholder="0.00"
                  className={fieldClass}
                />
              </label>
              <label className={labelClass}>
                <span>Результат, $</span>
                <input
                  type="number"
                  step="0.01"
                  inputMode="decimal"
                  value={draft.profitUsd}
                  onChange={(event) => update("profitUsd", event.target.value)}
                  placeholder="0.00"
                  className={fieldClass}
                />
              </label>
            </div>
          </div>

          <footer className="flex shrink-0 flex-col-reverse gap-2 border-t border-border bg-popover px-4 py-3.5 sm:flex-row sm:justify-end sm:px-6 sm:py-4">
            <button
              type="button"
              onClick={onDismiss}
              className="min-h-11 rounded-xl border border-border bg-secondary px-5 py-2.5 text-sm font-medium text-secondary-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              Отмена
            </button>
            <button
              type="submit"
              disabled={busy}
              className="min-h-11 rounded-xl border border-primary bg-primary px-5 py-2.5 text-sm font-semibold text-primary-foreground transition-[background-color,transform] hover:bg-primary-hover active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {busy ? "Сохранение…" : "Сохранить сделку"}
            </button>
          </footer>
        </form>
      </section>
    </div>,
    document.body,
  );
}

