import { fmt, type Opportunity } from "./model";
import { executionLabels, classic } from "./strategyModel";
export default function StrategyDetails({ row }: { row: Opportunity }) {
  const e = row.execution,
    m = row.metrics;
  return (
    <section className="mt-4 rounded-xl border border-border p-4 text-sm">
      <h3 className="font-semibold">
        {classic(row.strategy) || row.strategy === "cex-cex"
          ? "Сопоставимая глубина стаканов"
          : "Расчёт позиции"}
      </h3>
      {e ? (
        <div className="mt-3 grid gap-2 sm:grid-cols-2">
          <p>
            {classic(row.strategy) || row.strategy === "cex-cex"
              ? "Доступно"
              : "Расчётная позиция"}
            : {fmt(e.requestedUsdt)} USDT
          </p>
          <p>По известной глубине: {fmt(e.filledUsdt)} USDT</p>
          <p>{executionLabels[e.status] ?? e.status}</p>
          <p>Покрытие суммы: {fmt(e.fillPct)} %</p>
          <p>
            Уровней покупки / продажи: {e.buyLevels} / {e.sellLevels}
          </p>
          <p>
            Проскальзывание: {fmt(e.buySlippagePct)} / {fmt(e.sellSlippagePct)}{" "}
            %
          </p>
        </div>
      ) : null}
      {m ? (
        <div className="mt-4 space-y-2">
          {m.kind === "cex-cex" || m.kind === "xemm" ? (
            <>
              <p>Разница до расходов: {fmt(m.grossUsdt)} USDT</p>
              <p>Комиссии, оценка: {fmt(m.costsUsdt)} USDT</p>
              <p>После расходов, оценка: {fmt(m.cashUsdt)} USDT</p>
            </>
          ) : null}
          {m.kind === "funding" || m.kind === "hedged-lp" ? (
            <>
              <p>
                Funding за {m.horizonHours} ч: {fmt(m.fundingUsdt)} USDT ·{" "}
                {m.fundingEvents} начисл.
              </p>
              <p>
                Вход и выход хеджа, оценка комиссий: {fmt(m.costsUsdt)} USDT
              </p>
              {m.nextAt ? (
                <p>
                  Ближайшее начисление:{" "}
                  {new Date(m.nextAt).toLocaleString("ru-RU")}
                </p>
              ) : null}
            </>
          ) : null}
          {m.kind === "funding" ? (
            <>
              <p>Funding после расходов: {fmt(m.cashUsdt)} USDT</p>
              <p>
                Нормализованная годовая ставка: {fmt(m.annualFundingPct)} %
                (текущие ставки)
              </p>
            </>
          ) : null}
          {m.kind === "redemption" ? (
            <>
              <p>Номинальный ориентир: ${fmt(m.referenceUsd)}</p>
              <p>Доступ к погашению: не подтверждён</p>
              <p>Дисконт к номиналу: {fmt(m.discountPct)} %</p>
              <p>
                Оценка разницы после покупки: {fmt(m.cashUsdt)} USDT, до
                расходов на погашение
              </p>
            </>
          ) : null}
          {m.kind === "hedged-lp" ? (
            <>
              <p>
                Сеть: {m.network} · TVL: ${fmt(m.tvlUsd)}
              </p>
              <p>
                Пул: <span className="break-all">{m.poolAddress}</span>
              </p>
              <p>Исторический APY всего пула: {fmt(m.historicalApy)} %</p>
              <p>
                Диапазон позиции: {fmt(m.priceLower)} — {fmt(m.priceUpper)} USDC
              </p>
              <p>
                Short: {fmt(m.hedgeQuantity, 8)} {row.asset} · остаточная delta:{" "}
                {fmt(m.residualQuantity, 8)}
              </p>
            </>
          ) : null}
          <p className="pt-2 text-muted-foreground">{m.condition}</p>
        </div>
      ) : (
        <p className="mt-3 text-muted-foreground">
          Цены — VWAP известного объёма. Комиссии не вычитаются. Нехватка
          глубины не означает, что объёма нет во всём стакане.
        </p>
      )}
    </section>
  );
}
