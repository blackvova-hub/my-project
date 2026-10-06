import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { X, ExternalLink } from "lucide-react";
import { http } from "../../../shared/api/http";
import StrategyDetails from "./StrategyDetails";
import VerificationDetails from "./VerificationDetails";
import { marketLabel, classic } from "./strategyModel";
import {
  exchangeNames,
  fmt,
  identityLabel,
  type History,
  type Leg,
  type Opportunity,
} from "./model";

function Book({
  leg,
  buy,
  maker = false,
}: {
  leg: Leg;
  buy: boolean;
  maker?: boolean;
}) {
  return (
    <section className="min-w-0 rounded-xl border border-border p-4">
      <h3 className="font-semibold">
        {maker
          ? buy
            ? "Maker-покупка"
            : "Maker-продажа"
          : buy
            ? "Купить / Long"
            : "Продать / Short"}{" "}
        · {exchangeNames[leg.instrument.exchange]}
      </h3>
      <div className="mt-1 text-xs text-muted-foreground">
        {leg.instrument.nativeSymbol}
        {identityLabel(leg.instrument.assetIdentity)
          ? ` · ${identityLabel(leg.instrument.assetIdentity)}`
          : ""}{" "}
        · {marketLabel(leg.instrument.market)}
      </div>
      <div className="my-3 space-y-1 text-sm">
        <p>
          Цена:{" "}
          {leg.price.toLocaleString("ru-RU", { maximumSignificantDigits: 8 })}{" "}
          USDT
        </p>
        <p>
          {maker ||
          leg.instrument.market === "lp" ||
          leg.instrument.market === "redemption"
            ? "Размер расчёта"
            : "По известной глубине"}
          : {fmt(leg.notional)} USDT
        </p>
        <p>
          Время котировки: {new Date(leg.bookAt).toLocaleTimeString("ru-RU")}
        </p>
      </div>
      {leg.instrument.market === "spot" ||
      leg.instrument.market === "perpetual" ? (
        <table className="w-full text-sm">
          <caption className="mb-2 text-left text-xs text-muted-foreground">
            {maker
              ? buy
                ? "Текущие bid, очередь исполнения неизвестна"
                : "Текущие ask, очередь исполнения неизвестна"
              : buy
                ? "Предложения продажи"
                : "Предложения покупки"}
          </caption>
          <thead>
            <tr>
              <th className="text-left font-medium">Цена</th>
              <th className="text-right font-medium">Количество</th>
            </tr>
          </thead>
          <tbody>
            {(buy !== maker ? leg.asks : leg.bids)?.map((l) => (
              <tr key={l.price}>
                <td className="py-1">{fmt(l.price, 8)}</td>
                <td className="text-right">{fmt(l.quantity, 8)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <p className="text-xs text-muted-foreground">
          {leg.instrument.market === "lp"
            ? "Позиция LP: цена из контракта пула, не CEX-ордер."
            : "Номинальный ориентир эмитента, не исполнимый ордер продажи."}
        </p>
      )}
      <a
        className="mt-4 inline-flex items-center gap-2 text-sm text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring"
        href={leg.instrument.tradeUrl}
        target="_blank"
        rel="noreferrer"
      >
        Открыть инструмент <ExternalLink size={13} />
      </a>
    </section>
  );
}
function HistoryGraph({ history }: { history: History }) {
  const points = history.points;
  if (points.length < 2)
    return (
      <p className="py-6 text-sm text-muted-foreground">
        История ещё не накоплена. Точки сохраняются каждые 30 с для выбранных
        сервером связок.
      </p>
    );
  const lo = Math.min(...points.map((p) => p.spread)),
    hi = Math.max(...points.map((p) => p.spread));
  const start = points[0].at,
    end = points[points.length - 1].at;
  const coords = points.map((p) => ({
    at: p.at,
    xy: `${48 + ((p.at - start) / Math.max(end - start, 1)) * 692},${20 + ((hi - p.spread) / Math.max(hi - lo, 0.02)) * 135}`,
  }));
  const groups: string[][] = [[]];
  coords.forEach((p, i) => {
    if (i && p.at - coords[i - 1].at > 90000) groups.push([]);
    groups[groups.length - 1].push(p.xy);
  });
  return (
    <>
      <svg
        role="img"
        aria-label={`История спреда: от ${fmt(lo)} до ${fmt(hi)} процентов, ${points.length} наблюдений`}
        viewBox="0 0 760 190"
        className="mt-3 w-full text-primary"
      >
        <line x1="48" y1="160" x2="740" y2="160" stroke="var(--border)" />
        <text x="0" y="25" fill="var(--muted-foreground)" fontSize="12">
          {fmt(hi)}%
        </text>
        <text x="0" y="155" fill="var(--muted-foreground)" fontSize="12">
          {fmt(lo)}%
        </text>
        {groups.map((g, i) => (
          <polyline
            key={i}
            points={g.join(" ")}
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          />
        ))}
        <text x="48" y="182" fill="var(--muted-foreground)" fontSize="12">
          {new Date(start).toLocaleTimeString("ru-RU")}
        </text>
        <text
          x="740"
          y="182"
          textAnchor="end"
          fill="var(--muted-foreground)"
          fontSize="12"
        >
          {new Date(end).toLocaleTimeString("ru-RU")}
        </text>
      </svg>
      <details className="mt-2 text-xs text-muted-foreground">
        <summary className="cursor-pointer">
          Данные графика ({points.length})
        </summary>
        <div className="mt-2 max-h-40 overflow-auto">
          <table className="w-full">
            <thead>
              <tr>
                <th className="text-left">Время</th>
                <th className="text-right">Спред, %</th>
              </tr>
            </thead>
            <tbody>
              {points.map((p) => (
                <tr key={p.at}>
                  <td>{new Date(p.at).toLocaleString("ru-RU")}</td>
                  <td className="text-right">{fmt(p.spread, 4)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </>
  );
}
export default function RouteDetails({
  row,
  available,
  stale,
  onClose,
}: {
  row: Opportunity;
  available: boolean;
  stale: boolean;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const history = useQuery({
    queryKey: ["arbitrage-history", row.id],
    queryFn: ({ signal }) =>
      http<History>(`/arbitrage/history?id=${row.id}`, { signal }),
    refetchInterval: 30000,
    retry: 1,
    enabled: classic(row.strategy),
  });
  useEffect(() => {
    const dialog = ref.current;
    const previous = document.activeElement as HTMLElement | null;
    dialog?.showModal();
    const old = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      dialog?.close();
      document.body.style.overflow = old;
      previous?.focus();
    };
  }, []);
  const outdated = !available || stale;
  return (
    <dialog
      ref={ref}
      className="arb-dialog"
      aria-labelledby="arb-detail-title"
      onCancel={onClose}
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          const r = e.currentTarget.getBoundingClientRect();
          if (
            e.clientX < r.left ||
            e.clientX > r.right ||
            e.clientY < r.top ||
            e.clientY > r.bottom
          )
            onClose();
        }
      }}
    >
      <div className="flex items-center justify-between gap-4">
        <h2 id="arb-detail-title" className="text-xl font-semibold">
          {row.asset} · подробности связки
        </h2>
        <button
          autoFocus
          aria-label="Закрыть подробности"
          className="rounded-lg p-2 hover:bg-secondary focus-visible:outline-2 focus-visible:outline-ring"
          onClick={onClose}
        >
          <X size={20} />
        </button>
      </div>
      {outdated && (
        <p role="status" className="mt-3 text-sm text-destructive">
          Связка больше не входит в актуальные результаты. Ниже сохранён
          последний снимок.
        </p>
      )}
      <div className="mt-5 grid gap-4 md:grid-cols-2">
        <Book leg={row.buy} buy maker={row.metrics?.makerSide === "buy"} />
        <Book
          leg={row.sell}
          buy={false}
          maker={row.metrics?.makerSide === "sell"}
        />
      </div>
      <StrategyDetails row={row} />
      <VerificationDetails row={row} outdated={outdated} />
      {classic(row.strategy) ? (
        <section className="mt-4 rounded-xl border border-border p-4">
          <h3 className="font-semibold">История разницы цен</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            До 6 ч · выборочные наблюдения раз в 30 с. Пропуски данных не
            соединяются.
          </p>
          {history.isPending ? (
            <p role="status" className="py-6 text-sm text-muted-foreground">
              Загрузка истории…
            </p>
          ) : history.isError ? (
            <p role="alert" className="py-6 text-sm text-destructive">
              История временно недоступна.
            </p>
          ) : (
            <HistoryGraph history={history.data} />
          )}
        </section>
      ) : null}
    </dialog>
  );
}
