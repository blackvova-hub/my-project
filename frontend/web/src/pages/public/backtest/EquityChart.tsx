import { useMemo } from "react";
import { TimeSeriesChart } from "../../../shared/charts/LineChart";
import type { BacktestResult } from "./model";
export default function EquityChart({
  points,
}: {
  points: BacktestResult["equity"];
}) {
  const lines = useMemo(
    () => [
      {
        name: "Капитал",
        color: "var(--primary)",
        data: points.map((p) => ({ time: p.time / 1000, value: p.value })),
      },
    ],
    [points],
  );
  return (
    <div className="bt-equity-chart">
      <TimeSeriesChart
        lines={lines}
        height={280}
        label="График капитала портфеля за период теста"
      />
    </div>
  );
}
