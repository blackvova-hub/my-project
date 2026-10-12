import { useDerivativePanels, type DerivativeId } from "./derivatives";
import { useCallback, useState } from "react";
import { RefreshCw, Maximize2, Save } from "lucide-react";
import { ChartSurface, type ChartSurfaceProps } from "./ChartSurface";
import { useMarketData } from "./useMarketData";
import { useChartWorkspace } from "./useChartWorkspace";
import {
  CHART_INTERVALS,
  instrumentKey,
  type ChartInterval,
  type Instrument,
} from "./marketData";
import { IndicatorMenu } from "../market/IndicatorMenu";
import type { IndicatorId } from "../market/indicatorTypes";
import type { PerpInterval } from "../market/perpMarketData";
const EMPTY_DERIVATIVES: DerivativeId[] = [];
const EMPTY_INDICATORS: IndicatorId[] = [];
export function MarketChart({
  exchange,
  symbol,
  market = "linear",
  interval: initial = "1m",
  embedded = false,
  derivatives = EMPTY_DERIVATIVES,
  height = 560,
  ...props
}: Instrument & {
  market?: Instrument["market"];
  interval?: ChartInterval;
  embedded?: boolean;
  derivatives?: DerivativeId[];
} & Omit<ChartSurfaceProps, "candles" | "interval">) {
  const [interval, setInterval] = useState<ChartInterval | null>(null),
    [indicatorSelection, setIndicators] = useState<IndicatorId[] | null>(null),
    [reset, setReset] = useState(0);
  const instrument = { exchange, symbol, market };
  const saved = useChartWorkspace(`${instrumentKey(instrument)}:${initial}`);
  const selected = embedded
    ? (interval ?? saved.config?.selected_interval ?? initial)
    : initial;
  const indicators =
    indicatorSelection ??
    saved.config?.active_indicator_ids ??
    EMPTY_INDICATORS;
  const data = useMarketData(instrument, selected);
  const external = useDerivativePanels(instrument, selected, derivatives);
  const workspace = useChartWorkspace(
    `${instrumentKey(instrument)}:${selected}`,
  );
  const save = useCallback(
    () =>
      void workspace.save(
        selected as PerpInterval,
        props.indicators ?? indicators,
      ),
    [workspace, selected, props.indicators, indicators],
  );
  return (
    <div className="ct-market-chart">
      {embedded && (
        <div className="ct-toolbar">
          <strong>{symbol}</strong>
          {CHART_INTERVALS.map((t) => (
            <button
              key={t.value}
              aria-pressed={selected === t.value}
              onClick={() => setInterval(t.value)}
            >
              {t.label}
            </button>
          ))}
          <IndicatorMenu
            active={indicators}
            onToggle={(id) =>
              setIndicators((ids) =>
                (ids ?? indicators).includes(id)
                  ? (ids ?? indicators).filter((i) => i !== id)
                  : [...(ids ?? indicators), id],
              )
            }
            onClear={() => setIndicators([])}
          />
          <button
            aria-label="Сбросить масштаб"
            onClick={() => setReset((n) => n + 1)}
          >
            <RefreshCw size={15} />
          </button>
          <button aria-label="Сохранить конфигурацию графика" onClick={save}>
            <Save size={15} />
          </button>
          <button
            aria-label="Полный экран"
            onClick={(e) => {
              const root = e.currentTarget.closest(".ct-market-chart");
              if (document.fullscreenElement) void document.exitFullscreen();
              else void root?.requestFullscreen();
            }}
          >
            <Maximize2 size={15} />
          </button>
        </div>
      )}
      <ChartSurface
        {...props}
        externalPanes={external.data}
        candles={data.candles}
        interval={data.seconds}
        title={
          props.title ??
          `${symbol} · ${CHART_INTERVALS.find((t) => t.value === selected)?.label ?? selected} · ${exchange === "bybit" ? "Bybit" : "Binance"} · ${market === "spot" ? "Спот" : "Фьючерсы"}`
        }
        height={height}
        indicators={props.indicators ?? indicators}
        priceFormat={data.format ?? undefined}
        initialDrawings={workspace.initial}
        drawingKey={`${instrumentKey(instrument)}:${selected}`}
        onDrawingsChange={workspace.update}
        onSave={save}
        saveStatus={workspace.status}
        onNeedHistory={data.loadHistory}
        loading={data.loading}
        error={data.error}
        resetToken={props.resetToken ?? reset}
        follow
      />
      {!embedded && (
        <span
          className="ct-stream-status"
          aria-label="Состояние рыночных данных"
        >
          {data.status === "live"
            ? "Прямой поток"
            : data.status === "connecting"
              ? "Подключаемся…"
              : "Поток переподключается · REST 30с"}
          {data.updated
            ? ` · ${new Date(data.updated).toLocaleTimeString("ru-RU", { timeZone: "UTC" })} UTC`
            : ""}
        </span>
      )}
      {embedded && (
        <div className="ct-status">
          <span className={data.status === "live" ? "ct-up" : "ct-muted"}>
            {data.status === "live"
              ? "● Прямой поток"
              : data.status === "connecting"
                ? "Подключаемся…"
                : data.status === "reconnecting"
                  ? "Переподключение…"
                  : "Поток недоступен"}
          </span>
          <span>{workspace.status}</span>
          {data.error && <button onClick={data.retry}>Повторить</button>}
        </div>
      )}
    </div>
  );
}
