import { useState } from "react";
import { useLocation } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import type { PrimaryExchange } from "./primaryExchange";

export function PrimaryExchangeGate() {
  const { isAuth, isLoading, primaryExchange, setPrimaryExchange } = useAuth();
  const location = useLocation();
  const [saving, setSaving] = useState<PrimaryExchange | null>(null);
  const [error, setError] = useState("");

  if (isLoading || !isAuth || primaryExchange || location.pathname.startsWith("/admin")) return null;

  const choose = async (exchange: PrimaryExchange) => {
    setSaving(exchange);
    setError("");
    try {
      await setPrimaryExchange(exchange);
    } catch {
      setError("Не удалось сохранить выбор. Попробуйте ещё раз.");
      setSaving(null);
    }
  };

  return (
    <div className="fixed inset-0 z-[200] flex items-center justify-center bg-scrim px-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="primary-exchange-title"
        className="w-full max-w-lg rounded-3xl border border-border bg-popover p-6 text-popover-foreground shadow-lg md:p-8"
      >
        <div className="text-xs font-semibold uppercase tracking-[0.2em] text-primary">
          Основная биржа
        </div>
        <h2 id="primary-exchange-title" className="mt-3 text-2xl font-semibold text-popover-foreground">
          Пожалуйста, выберите вашу биржу
        </h2>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          Она будет автоматически использоваться в сканерах, watchlist, тикерах, графиках и ссылках на торговые данные.
        </p>

        <div className="mt-6 grid gap-3 sm:grid-cols-2">
          {(["bybit", "binance"] as const).map((exchange) => (
            <button
              key={exchange}
              type="button"
              disabled={saving !== null}
              onClick={() => void choose(exchange)}
              className="rounded-2xl border border-border bg-card px-5 py-5 text-left transition hover:border-border-strong hover:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:cursor-wait disabled:opacity-60"
            >
              <span className="block text-lg font-semibold text-card-foreground">
                {exchange === "bybit" ? "Bybit" : "Binance"}
              </span>
              <span className="mt-1 block text-xs text-muted-foreground">USDT perpetual</span>
              {saving === exchange ? (
                <span className="mt-3 block text-xs text-primary">Сохраняем…</span>
              ) : null}
            </button>
          ))}
        </div>

        {error ? <div className="mt-4 text-sm text-destructive">{error}</div> : null}
      </div>
    </div>
  );
}
