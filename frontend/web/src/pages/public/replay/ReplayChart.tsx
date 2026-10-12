import { useMemo, useState, type ReactNode } from "react";
import { ChartSurface } from "../../../shared/charts/ChartSurface";
import { IndicatorMenu } from "../../../shared/market/IndicatorMenu";
import type { IndicatorId } from "../../../shared/market/indicatorTypes";
import type { Candle, Position } from "./model";
import { Icon } from "./Icons";
export function ReplayChart({
  candles,
  selecting,
  onSelect,
  title,
  interval,
  returnToken,
  session,
  position,
  onArchive,
  children,
}: {
  candles: Candle[];
  selecting: boolean;
  onSelect: (time: number) => void;
  title: string;
  interval: number;
  returnToken: number;
  session: number;
  position: Position | null;
  onArchive: () => void;
  children: ReactNode;
}) {
  const [indicators, setIndicators] = useState<IndicatorId[]>(["ema"]),
    [volume, setVolume] = useState(true);
  const priceLines = useMemo(
    () =>
      position
        ? [
            {
              title: position.side.toUpperCase(),
              price: position.entry,
              color: position.side === "long" ? "#34d399" : "#fb7185",
            },
          ]
        : [],
    [position],
  );
  return (
    <div className="rp-chart-region ct-replay-region">
      <div className="rp-chart-main">
        <div className="rp-chart-toolbar">
          <button className="rp-instrument" onClick={onArchive}>
            <strong>{title.split(" · ")[0]}</strong>
            <Icon name="chevron" />
            <span>{title.split(" · ").slice(1).join(" · ")}</span>
          </button>
          <IndicatorMenu
            active={indicators}
            onToggle={(id) =>
              setIndicators((ids) =>
                ids.includes(id) ? ids.filter((i) => i !== id) : [...ids, id],
              )
            }
            onClear={() => setIndicators([])}
            volumeActive={volume}
            onVolumeToggle={() => setVolume(!volume)}
          />
          {children}
          <button
            className="rp-icon rp-fullscreen"
            aria-label="Полный экран"
            onClick={(e) => {
              if (document.fullscreenElement) void document.exitFullscreen();
              else
                void e.currentTarget
                  .closest(".replay-layout")
                  ?.requestFullscreen();
            }}
          >
            <Icon name="fullscreen" />
          </button>
        </div>
        <div className="rp-chart-wrap">
          <ChartSurface
            key={session}
            candles={candles}
            title={title}
            height="100%"
            interval={interval}
            selecting={selecting}
            onSelect={onSelect}
            indicators={indicators}
            volume={volume}
            resetToken={returnToken}
            priceLines={priceLines}
            drawingKey={`replay:${title}:${interval}:${session}`}
            saveStatus="Разметка текущей сессии · доступен экспорт JSON"
            follow
            testId="replay-chart"
          />
        </div>
      </div>
    </div>
  );
}
