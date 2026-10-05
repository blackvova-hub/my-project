import type { SignalRow } from "../../scanners/types";
import { loadScannerConfig } from "./storage";

function sentimentFromMovers(gainers: number, losers: number) {
  if (gainers === losers) return "Neutral";
  return gainers > losers ? "Bullish" : "Bearish";
}

export function OverviewCards(props: {
  signals: SignalRow[];
  gainersCount: number;
  losersCount: number;
}) {
  const cfg = loadScannerConfig();
  const scannersCount = cfg?.rules?.length ? 1 : 0; // пока одна конфигурация, но виджет готов к расширению

  const sentiment = sentimentFromMovers(props.gainersCount, props.losersCount);

  return (
    <section className="mt-8 grid gap-3 md:grid-cols-4">
      <Card title="Active Scanners" value={String(scannersCount)} hint="конфигурации" />
      <Card title="Signals (24h)" value={String(props.signals.length)} hint="последние события" />
  <Card title="РЫНОЧНЫЕ НАСТРОЕНИЯ" value={sentiment} hint="на основе топ-движений" />
    </section>
  );
}

function Card(props: { title: string; value: string; hint?: string }) {
  return (
    <div className="rounded-2xl border border-border bg-card p-4">
      <div className="text-xs text-muted-foreground">{props.title}</div>
      <div className="mt-2 text-2xl font-extrabold tracking-tight text-foreground">
        {props.value}
      </div>
      {props.hint ? <div className="mt-1 text-xs text-muted-foreground">{props.hint}</div> : null}
    </div>
  );
}
