import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AnimatedSelect } from "../../../shared/ui/AnimatedSelect";
import { http } from "../../../shared/api/http";
import { fmt, type Opportunity } from "./model";
import { validAmount } from "./strategyModel";
import { verifiedNetProfit, verificationLabels, verificationSnapshotFresh, type VerificationReport } from "./verificationModel";

export default function VerificationDetails({ row, outdated }: { row: Opportunity; outdated: boolean }) {
  const [amount, setAmount] = useState(String(row.execution?.requestedUsdt && !["spot-spot", "spot-perpetual", "perpetual-perpetual", "cex-cex"].includes(row.strategy) ? row.execution.requestedUsdt : 100));
  const [debounced, setDebounced] = useState(amount);
  const [mode, setMode] = useState("prefunded");
  const [network, setNetwork] = useState("");
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => { const timer = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, []);
  useEffect(() => { const timer = setTimeout(() => setDebounced(amount), 350); return () => clearTimeout(timer); }, [amount]);
  const params = new URLSearchParams({ id: row.id, strategy: row.strategy, amount: debounced.replace(",", "."), horizonHours: String(row.metrics?.horizonHours || 24), network: row.metrics?.network || "all", lpRangePct: String(row.metrics?.lpRangePct || 10) });
  const result = useQuery({
    queryKey: ["arbitrage-verification", params.toString()],
    queryFn: ({ signal }) => http<VerificationReport>(`/arbitrage/verification?${params}`, { signal }),
    enabled: !outdated && validAmount(debounced), refetchInterval: 5000, staleTime: 0, retry: 0,
  });
  const valid = validAmount(amount);
  const current = valid && amount.replace(",", ".") === debounced.replace(",", ".") && !outdated && !result.isError && result.data;
  const paths = current ? result.data.paths.filter((p) => p.mode === mode) : [];
  const rawPath = paths.find((p) => p.id === network) ?? paths[0];
  const freshSnapshot = verificationSnapshotFresh(result.data?.at ?? 0, rawPath, now);
  const path = rawPath ? { ...rawPath, checks: rawPath.checks.map((c) => {
    if (c.state !== "pass") return c;
    if (c.key === "freshness" && !freshSnapshot) return { ...c, state: "fail" as const, detail: "Котировки устарели. Ожидаем новый снимок." };
    if (c.key === "vwap" && !freshSnapshot) return { ...c, state: "unknown" as const, detail: "Стакан устарел. VWAP на сумму требует нового снимка." };
    if (["identity", "trading", "wallet", "network", "delisting"].includes(c.key) && (!c.at || now - c.at > 60000)) return { ...c, state: "unknown" as const, detail: "Метаданные устарели. Ожидаем повторную проверку." };
    return c;
  }) } : undefined;
  const profit = freshSnapshot ? verifiedNetProfit(path) : null;
  const value = (n: number | null | undefined) => freshSnapshot && typeof n === "number" && Number.isFinite(n) ? fmt(n, 8) : "—";
  const spot = row.buy.instrument.market === "spot" && row.sell.instrument.market === "spot";
  return (
    <section className="mt-4 rounded-xl border border-border p-4" aria-labelledby="arb-verification-title">
      <h3 id="arb-verification-title" className="font-semibold">Проверка связки</h3>
      <p className="mt-1 text-xs text-muted-foreground">Котировка — кандидат. Чистая прибыль требует всех подтверждений.</p>
      <div className="arb-strategy-controls mt-4">
        <label><span>Сумма проверки, USDT</span><input aria-label="Сумма проверки, USDT" inputMode="decimal" maxLength={16} value={amount} onChange={(e) => setAmount(e.target.value)} /></label>
        {spot ? <label><span>Способ исполнения</span><AnimatedSelect ariaLabel="Способ исполнения" value={mode} options={[{ value: "prefunded", label: "Средства на двух биржах" }, { value: "transfer", label: "Покупка → перевод → продажа" }]} onChange={(v) => { setMode(v); setNetwork(""); }} /></label> : null}
        {mode === "transfer" && paths.some((p) => p.network) ? <label><span>Общая сеть</span><AnimatedSelect ariaLabel="Общая сеть" value={path?.id || ""} options={paths.filter((p) => p.network && p.id).map((p) => ({ value: p.id!, label: p.networkLabel || p.network! }))} onChange={setNetwork} /></label> : null}
      </div>
      {!valid ? <p role="alert" className="mt-3 text-sm text-destructive">Сумма проверки: от 10 до 1 000 000 USDT</p> : outdated ? <p role="status" className="mt-3 text-sm text-muted-foreground">Снимок устарел. Проверка исполнения недоступна.</p> : result.isError ? <p role="alert" className="mt-3 text-sm text-destructive">Проверка недоступна или связка уже исчезла. Подтверждённых результатов нет.</p> : !current ? <p role="status" className="mt-3 text-sm text-muted-foreground">Проверяем свежие данные…</p> : path ? <>
        <ol className="mt-4 space-y-3 text-sm">
          {path.checks.map((c) => <li key={c.key}><div className="flex flex-wrap justify-between gap-2"><span>{c.label}</span><span className={c.state === "fail" ? "text-destructive" : "text-muted-foreground"}>{verificationLabels[c.state]}</span></div><p className="mt-1 text-xs text-muted-foreground">{c.detail}</p></li>)}
        </ol>
        <div className="mt-4 space-y-2 border-t border-border pt-3 text-sm">
          <p>VWAP покупки / продажи: {value(path.calculation.buyVwap)} / {value(path.calculation.sellVwap)}</p>
          <p>Количество покупки / продажи: {value(path.calculation.buyQuantity)} / {value(path.calculation.sellQuantity)} {row.asset}</p>
          {path.calculation.execution ? <><p>Запрошено / покрыто стаканом: {value(path.calculation.execution.requestedUsdt)} / {value(path.calculation.execution.filledUsdt)} USDT</p><p>Уровней покупки / продажи: {freshSnapshot ? `${path.calculation.execution.buyLevels} / ${path.calculation.execution.sellLevels}` : "—"}</p></> : null}
          <p>Разница до торговых комиссий: {value(path.calculation.grossUsdt)} USDT</p>
          <p>Торговые комиссии, публичная оценка: {value(path.calculation.tradingFeesEstimate)} USDT</p>
          {mode === "transfer" ? <p>Комиссия вывода, оценка в USDT: {value(path.calculation.withdrawalFeeEstimate)}</p> : null}
          <p className="font-semibold">Чистая прибыль: {profit === null ? "не подтверждена" : `${fmt(profit)} USDT`}</p>
          {mode === "transfer" ? <p className="text-xs text-muted-foreground">Цена продажи за время перевода изменится. Показан расчёт по текущему стакану.</p> : null}
        </div>
      </> : null}
    </section>
  );
}
