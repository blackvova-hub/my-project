import { GAINERS, LOSERS } from "./topMoversData";

export function TopMoversWidget() {
  return (
    <section className="rounded-2xl border border-border bg-card p-5 md:p-6">
      <div>
        <h3 className="text-lg font-semibold tracking-tight">Top Movers</h3>
        <p className="mt-1 text-sm text-muted-foreground">Лидеры роста и падения за 24 часа.</p>
      </div>

      <div className="mt-4 grid gap-3">
        <div className="rounded-2xl border border-border bg-background p-4">
          <div className="text-xs text-muted-foreground">Gainers</div>
          <div className="mt-2 grid gap-2">
            {GAINERS.slice(0, 5).map((x) => (
              <div key={x.symbol} className="flex items-center justify-between text-sm">
                <div className="font-semibold text-foreground">{x.symbol}</div>
                <div className="font-semibold text-primary">+{x.changePct.toFixed(2)}%</div>
              </div>
            ))}
          </div>
        </div>

        <div className="rounded-2xl border border-border bg-background p-4">
          <div className="text-xs text-muted-foreground">Losers</div>
          <div className="mt-2 grid gap-2">
            {LOSERS.slice(0, 5).map((x) => (
              <div key={x.symbol} className="flex items-center justify-between text-sm">
                <div className="font-semibold text-foreground">{x.symbol}</div>
                <div className="font-semibold text-destructive">{x.changePct.toFixed(2)}%</div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
