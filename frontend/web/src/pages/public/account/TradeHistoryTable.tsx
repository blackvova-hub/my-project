import type { ApiTrade } from "../scanners/types";

type Props = {
  trades?: ApiTrade[];
  isLoading: boolean;
  onSelect: (trade: ApiTrade) => void;
};

function formatDate(value?: string | null) {
  if (!value) return "—";
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
  if (value === null || value === undefined || !Number.isFinite(value) || value === 0) return "text-muted-foreground";
  return value > 0 ? "text-primary" : "text-destructive";
}

export default function TradeHistoryTable({ trades, isLoading, onSelect }: Props) {
  return (
    <section className="mt-8 border-t border-border pt-7">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold tracking-tight text-foreground">История сделок</h2>
          <p className="mt-1.5 text-sm leading-6 text-muted-foreground">
            Нажми на сделку, чтобы открыть подробности и приложенные фото.
          </p>
        </div>
        {trades && trades.length > 0 ? (
          <div className="rounded-full border border-border bg-background px-3 py-1 text-xs text-muted-foreground">
            Сделок: {trades.length}
          </div>
        ) : null}
      </div>

      {isLoading ? (
        <div className="mt-5 rounded-2xl border border-border bg-background px-5 py-8 text-center text-sm text-muted-foreground">Загрузка сделок...</div>
      ) : trades && trades.length > 0 ? (
        <div className="mt-5 overflow-x-auto rounded-2xl border border-border bg-background">
          <table className="min-w-[920px] w-full text-left text-sm text-foreground">
            <thead className="bg-background text-[11px] uppercase tracking-wide text-muted-foreground">
              <tr>
                <th className="px-4 py-3">Сигнал</th>
                <th className="px-4 py-3">Buy</th>
                <th className="px-4 py-3">Sell</th>
                <th className="px-4 py-3">Профит %</th>
                <th className="px-4 py-3">Профит $</th>
                <th className="px-4 py-3">Стратегия</th>
                <th className="px-4 py-3">Комментарий</th>
              </tr>
            </thead>
            <tbody>
              {trades.map((trade) => (
                <tr
                  key={trade.id}
                  role="button"
                  tabIndex={0}
                  className="cursor-pointer border-t border-border transition hover:bg-secondary focus:bg-secondary focus:outline-none"
                  onClick={() => onSelect(trade)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      onSelect(trade);
                    }
                  }}
                >
                  <td className="px-4 py-3.5 font-semibold text-foreground">{trade.symbol ?? trade.signal_id}</td>
                  <td className="whitespace-nowrap px-4 py-3.5">{formatDate(trade.buy_at)}</td>
                  <td className="whitespace-nowrap px-4 py-3.5">{formatDate(trade.sell_at)}</td>
                  <td className={`whitespace-nowrap px-4 py-3.5 font-medium ${resultTone(trade.profit_percent)}`}>{formatPercent(trade.profit_percent)}</td>
                  <td className={`whitespace-nowrap px-4 py-3.5 font-medium ${resultTone(trade.profit_usd)}`}>{formatUsd(trade.profit_usd)}</td>
                  <td className="px-4 py-3.5">{trade.strategy_name ?? "—"}</td>
                  <td className="max-w-[260px] truncate px-4 py-3.5 text-muted-foreground">{trade.comment ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="mt-5 rounded-2xl border border-dashed border-border bg-background px-5 py-8 text-center text-sm text-muted-foreground">Пока нет закрытых сделок.</div>
      )}
    </section>
  );
}
