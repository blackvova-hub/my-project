import { classic, strategyOptions } from "./strategyModel";
import { AnimatedSelect } from "../../../shared/ui/AnimatedSelect";
import { fmt, type Opportunity, type Settings } from "./model";

export function StrategyControls({
  settings,
  amountText,
  availabilityText,
  onAvailability,
  onAmount,
  onAmountBlur,
  onChange,
  disabled,
}: {
  settings: Settings;
  amountText: string;
  availabilityText: string;
  onAvailability: (text: string) => void;
  onAmount: (text: string) => void;
  onAmountBlur: () => void;
  onChange: (settings: Settings) => void;
  disabled: boolean;
}) {
  return (
    <div className="arb-strategy-controls">
      <label>
        <span>Стратегия</span>
        <AnimatedSelect
          value={classic(settings.strategy) ? "classic" : settings.strategy}
          options={strategyOptions}
          ariaLabel="Стратегия"
          disabled={disabled}
          onChange={(strategy) =>
            onChange({
              ...settings,
              strategy: strategy === "classic" ? "spot-perpetual" : strategy,
            })
          }
        />
      </label>
      <label>
        <span>Мин. доступно, USDT</span>
        <input
          aria-label="Мин. доступно, USDT"
          inputMode="decimal"
          value={availabilityText}
          disabled={disabled}
          onChange={(e) => onAvailability(e.target.value)}
          onBlur={onAmountBlur}
        />
      </label>
      {!classic(settings.strategy) && settings.strategy !== "cex-cex" ? (
        <label>
          <span>Расчётная позиция, USDT</span>
          <input
            aria-label="Расчётная позиция, USDT"
            inputMode="decimal"
            value={amountText}
            disabled={disabled}
            onChange={(e) => onAmount(e.target.value)}
            onBlur={onAmountBlur}
          />
        </label>
      ) : null}
      {settings.strategy === "funding" || settings.strategy === "hedged-lp" ? (
        <label>
          <span>Горизонт</span>
          <AnimatedSelect
            ariaLabel="Горизонт"
            value={String(settings.horizonHours)}
            options={[6, 24, 72, 168].map((n) => ({
              value: String(n),
              label: `${n} ч`,
            }))}
            disabled={disabled}
            onChange={(value) =>
              onChange({ ...settings, horizonHours: Number(value) })
            }
          />
        </label>
      ) : null}
      {settings.strategy === "hedged-lp" ? (
        <>
          <label>
            <span>Сеть</span>
            <AnimatedSelect
              ariaLabel="Сеть LP"
              value={settings.network ?? "all"}
              options={[
              { value: "all", label: "Обе сети" },
                { value: "Ethereum", label: "Ethereum" },
                { value: "Arbitrum", label: "Arbitrum" },
              ]}
              disabled={disabled}
              onChange={(network) => onChange({ ...settings, network })}
            />
          </label>
          <label>
            <span>Диапазон LP</span>
            <AnimatedSelect
              ariaLabel="Диапазон LP"
              value={String(settings.lpRangePct ?? 10)}
              options={[5, 10, 20, 50].map((n) => ({
                value: String(n),
                label: `± ${n} %`,
              }))}
              disabled={disabled}
              onChange={(value) =>
                onChange({ ...settings, lpRangePct: Number(value) })
              }
            />
          </label>
        </>
      ) : null}
    </div>
  );
}
export function MetricCell({
  row,
  stale,
}: {
  row: Opportunity;
  stale: boolean;
}) {
  if (stale) return <>—</>;
  const m = row.metrics;
  const value = m?.valuePct ?? row.entrySpreadPct;
  return (
    <>
      <span className={value > 0 ? "arb-positive" : "text-muted-foreground"}>
        {value > 0 ? "+" : ""}
        {fmt(value)} %
      </span>
      {m ? (
        <div className="arb-price">
          {m.kind === "hedged-lp"
            ? "APY пула, история"
            : m.kind === "funding"
              ? `${fmt(m.cashUsdt)} USDT / ${m.horizonHours} ч`
              : m.kind === "redemption"
                ? "Дисконт к $1, до погашения"
                : `${fmt(m.cashUsdt)} USDT, оценка`}
        </div>
      ) : null}
    </>
  );
}
export function VolumeCell({
  row,
  stale,
}: {
  row: Opportunity;
  stale: boolean;
}) {
  if (stale) return <>—</>;
  return <>{fmt(row.depthUsdt)}</>;
}
