import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchMarketCandles } from "../../../shared/charts/marketData";
import { useEconomicCalendar, type EconomicEvent } from "./api";
import { releaseReaction } from "./releaseReaction";
import { number, stamp } from "./format";
import { Empty, External, Panel, Skeleton } from "./ui";

function ReleaseRow({
  event,
  asset,
  zone,
  now,
}: {
  event: EconomicEvent;
  asset: string;
  zone: string;
  now: number;
}) {
  const at = Date.parse(event.scheduledAt!) / 1000;
  const query = useQuery({
    queryKey: ["previousReleaseReaction", asset, event.scheduledAt],
    queryFn: ({ signal }) =>
      fetchMarketCandles(
        { exchange: "bybit", market: "spot", symbol: asset },
        "5m",
        signal,
        Math.min(now, at + 86400) + 300,
        400,
      ),
    staleTime: 300_000,
    retry: 1,
  });
  return (
    <tr>
      <td>
        <time dateTime={event.scheduledAt!}>
          {stamp(event.scheduledAt, zone)}
        </time>
        <br />
        <External url={event.sourceUrl}>{event.source}</External>
      </td>
      {[1800, 7200, 86400].map((horizon) => {
        const result = releaseReaction(query.data ?? [], at, horizon, now);
        return (
          <td
            key={horizon}
            title={
              query.isError
                ? "Биржа не вернула исторические котировки"
                : result.reason
            }
            className={
              result.value == null
                ? "nm-muted"
                : result.value >= 0
                  ? "nm-positive"
                  : "nm-negative"
            }
          >
            {query.isPending
              ? "…"
              : query.isError || result.value == null
                ? "—"
                : `${result.value > 0 ? "+" : ""}${number(result.value)}%`}
          </td>
        );
      })}
    </tr>
  );
}

export function HistoricalReleases({
  event,
  zone,
}: {
  event: EconomicEvent;
  zone: string;
}) {
  const [enabled, setEnabled] = useState(false);
  const [asset, setAsset] = useState("BTCUSDT");
  const [now] = useState(() => Date.now() / 1000);
  const eventAt =
    Date.parse(event.scheduledAt ?? `${event.date}T00:00:00Z`) / 1000;
  const end = Math.min(now, eventAt);
  const from = new Date((end - 365 * 86400) * 1000).toISOString();
  const to = new Date(end * 1000).toISOString();
  const query = useEconomicCalendar(from, to, enabled);
  const previous = useMemo(
    () =>
      (query.data?.events ?? [])
        .filter(
          (item) =>
            item.seriesId === event.seriesId &&
            item.id !== event.id &&
            item.scheduledAt &&
            item.timePrecision === "exact" &&
            !["cancelled", "postponed"].includes(item.status) &&
            Date.parse(item.scheduledAt) / 1000 < end,
        )
        .sort((a, b) => Date.parse(b.scheduledAt!) - Date.parse(a.scheduledAt!))
        .slice(0, 3),
    [query.data, event.seriesId, event.id, end],
  );
  return (
    <Panel
      title="Предыдущие выпуски и рынок"
      action={
        enabled ? (
          <select
            aria-label="Актив для прошлых выпусков"
            value={asset}
            onChange={(e) => setAsset(e.target.value)}
          >
            <option value="BTCUSDT">BTC</option>
            <option value="ETHUSDT">ETH</option>
          </select>
        ) : null
      }
    >
      <p className="nm-note">
        До трёх предыдущих выпусков того же показателя из официального
        расписания. База — последняя полная свеча перед плановым временем;
        последующие изменения — реальные закрытия Bybit Spot, 5 минут.
      </p>
      {!enabled ? (
        <button className="nm-button" onClick={() => setEnabled(true)}>
          Загрузить историческую реакцию
        </button>
      ) : query.isPending ? (
        <Skeleton rows={2} />
      ) : query.isError ? (
        <Empty
          title="Расписание прошлых выпусков не загрузилось"
          retry={() => void query.refetch()}
        />
      ) : !previous.length ? (
        <Empty title="Нет сопоставимых прошлых выпусков">
          В доступном расписании за год нет точных времён предыдущих публикаций
          этого показателя.
        </Empty>
      ) : (
        <div className="nm-table-scroll">
          <table className="nm-calendar-table">
            <thead>
              <tr>
                <th>Плановое время ({zone})</th>
                <th>+30 мин</th>
                <th>+2 часа</th>
                <th>+24 часа</th>
              </tr>
            </thead>
            <tbody>
              {previous.map((item) => (
                <ReleaseRow
                  key={`${item.id}:${asset}`}
                  event={item}
                  asset={asset}
                  zone={zone}
                  now={now}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p className="nm-note">
        Фактическое время выхода не подтверждено. Совпадение с расписанием не
        доказывает влияние события и не предсказывает реакцию следующего
        выпуска. Пропуски свечей и ещё не наступившие горизонты показаны как
        «—».
      </p>
    </Panel>
  );
}
