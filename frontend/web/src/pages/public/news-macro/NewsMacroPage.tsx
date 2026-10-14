import { useEffect, useMemo, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { ArrowUpRight, BookOpen, RefreshCw } from "lucide-react";
import { useAuth } from "../../../shared/auth/AuthContext";
import { Sparkline } from "../../../shared/charts/Sparkline";
import {
  DEFAULT_FILTERS,
  useMacroMarkets,
  useMacroOverview,
  useNewsSources,
  useTerminalNews,
  type EconomicEvent,
  type MarketItem,
  type TerminalNewsItem,
} from "./api";
import { CalendarTable, EconomicCalendar } from "./Calendar";
import {
  EventDetail,
  IntelligenceDetail,
  MarketDetail,
  NewsDetail,
  SourcesDetail,
} from "./Details";
import { GLOSSARY } from "./intelligence";
import {
  MacroAnalysis,
  MacroChart,
  SeriesPreview,
  YieldCurve,
} from "./MacroAnalysis";
import { countdown, dateInZone } from "./model";
import { LiveNews, NewsRow } from "./NewsFeed";
import { useReading } from "./useReading";
import { Empty, External, Panel, Skeleton } from "./ui";
import { latestTimestamp, number, stamp } from "./format";
import "./news-macro.css";

const TABS = [
  { id: "", title: "Обзор", path: "/news" },
  { id: "live", title: "Live News", path: "/news/live" },
  { id: "calendar", title: "Экономический календарь", path: "/news/calendar" },
  { id: "macro", title: "Макроанализ", path: "/news/macro" },
];
type Detail =
  | { type: "news"; item: TerminalNewsItem }
  | { type: "event"; event: EconomicEvent }
  | { type: "market"; item: MarketItem }
  | { type: "sources" }
  | { type: "intelligence" }
  | null;
function useNow() {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = window.setInterval(() => {
      if (!document.hidden) setNow(Date.now());
    }, 30_000);
    const update = () => {
      if (!document.hidden) setNow(Date.now());
    };
    document.addEventListener("visibilitychange", update);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", update);
    };
  }, []);
  return now;
}
function MarketStrip({
  items,
  loading,
  error,
  onSelect,
  retry,
  zone,
}: {
  items: MarketItem[];
  loading: boolean;
  error: boolean;
  onSelect: (item: MarketItem) => void;
  retry: () => void;
  zone: string;
}) {
  if (loading)
    return (
      <div className="nm-market-loading">
        <Skeleton rows={1} />
      </div>
    );
  if (error)
    return (
      <div className="nm-market-error">
        <span>Рыночные котировки временно недоступны</span>
        <button className="nm-text-link" onClick={retry}>
          Повторить
        </button>
      </div>
    );
  if (!items.length)
    return (
      <div className="nm-market-error">
        Источник ещё не вернул рыночные котировки
      </div>
    );
  const order = ["BTC", "ETH", "SPX", "NDX", "DTWEXBGS", "XAUUSD"];
  const primary = order.flatMap((symbol) => {
    const item = items.find((value) => value.symbol === symbol);
    return item ? [item] : [];
  });
  const names: Record<string, string> = {
    BTC: "BTC",
    ETH: "ETH",
    SPX: "S&P 500",
    NDX: "NASDAQ 100",
    DTWEXBGS: "Широкий доллар ФРС",
    XAUUSD: "Золото",
  };
  return (
    <div className="nm-market-strip" aria-label="Обзор рынков">
      {primary.map((item) => (
        <button
          className="nm-market-item"
          key={item.symbol}
          onClick={() => onSelect(item)}
          title={`${item.name}. ${item.note} Источник: ${item.source}. Наблюдение: ${stamp(item.observedAt, zone)} (${zone})`}
        >
          <span className="nm-market-name">
            {names[item.symbol] ?? item.name}
            <ArrowUpRight size={11} aria-hidden="true" />
          </span>
          <div>
            <b>
              {number(
                item.price,
                item.price != null && item.price < 10 ? 4 : 2,
              )}
            </b>
            <span
              className={
                item.changePercent != null && item.status === "fresh"
                  ? item.changePercent >= 0
                    ? "nm-positive"
                    : "nm-negative"
                  : "nm-muted"
              }
            >
              {item.changePercent == null
                ? "—"
                : `${item.changePercent > 0 ? "+" : ""}${number(item.changePercent)}%`}
            </span>
          </div>
          <div className="nm-market-bottom">
            <small>
              {item.symbol === "DTWEXBGS" ? "Индекс · не DXY" : item.unit}
            </small>
            {item.points.length >= 2 ? (
              <Sparkline
                values={item.points.map((p) => p.value)}
                height={18}
                positive={(item.changePercent ?? 0) >= 0}
                label={`История ${item.name}`}
              />
            ) : (
              <small>
                {item.status === "unavailable"
                  ? "Недоступно"
                  : item.status === "stale"
                    ? "Задержка"
                    : item.source}
              </small>
            )}
          </div>
          <small
            className={`nm-market-observed ${item.status === "stale" ? "nm-market-stale" : ""}`}
          >
            {item.status === "stale" ? "Задержка · " : ""}
            {item.observedAt ? stamp(item.observedAt, zone) : "Нет наблюдений"}
          </small>
        </button>
      ))}
    </div>
  );
}
function Terminal({ userId }: { userId: string }) {
  const location = useLocation(),
    navigate = useNavigate(),
    queryClient = useQueryClient();
  const section = location.pathname.split("/")[2] ?? "";
  const [detail, setDetail] = useState<Detail>(() =>
    section === "intelligence" ? { type: "intelligence" } : null,
  );
  const [zone, setZone] = useState(
    () => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  );
  const [pulse, setPulse] = useState("CPIAUCSL"),
    [term, setTerm] = useState(0),
    [refreshing, setRefreshing] = useState(false);
  const now = useNow(),
    reading = useReading(userId);
  const overview = useMacroOverview(
      section !== "live" || detail?.type === "sources",
    ),
    markets = useMacroMarkets(),
    sources = useNewsSources();
  const news = useTerminalNews(
    {
      ...DEFAULT_FILTERS,
      limit: 12,
      sort: "important",
      from: new Date(
        Math.floor(now / 3600000) * 3600000 - 86400000,
      ).toISOString(),
    },
    section === "" || section === "intelligence",
  );
  const marketItems = markets.data?.items ?? [],
    catalog = overview.data?.series ?? [],
    macroSources = overview.data?.sources ?? [],
    newsSources = sources.data?.sources ?? news.data?.sources ?? [];
  const upcoming = useMemo(
    () =>
      (overview.data?.events ?? [])
        .filter(
          (event) =>
            event.status === "upcoming" &&
            (event.scheduledAt
              ? Date.parse(event.scheduledAt) > now
              : event.date >= dateInZone(new Date(now), zone)),
        )
        .sort((a, b) =>
          (a.scheduledAt ?? `${a.date}T23:59:59Z`).localeCompare(
            b.scheduledAt ?? `${b.date}T23:59:59Z`,
          ),
        )
        .slice(0, 3),
    [overview.data, now, zone],
  );
  const heatItems = marketItems.filter(
    (item) =>
      item.assetClass === "crypto" &&
      item.changePercent != null &&
      item.status === "fresh",
  );
  const next =
    upcoming.find(
      (event) => event.scheduledAt && event.importance === "high",
    ) ?? upcoming.find((event) => event.scheduledAt);
  const healthy = [...newsSources, ...macroSources].filter(
      (source) => source.status === "ok",
    ).length,
    totalSources = newsSources.length + macroSources.length;
  async function refresh() {
    setRefreshing(true);
    try {
      await Promise.all(
        [
          "newsTerminal",
          "newsTerminalSources",
          "newsMacroOverview",
          "newsMacroMarkets",
          "newsEconomicCalendar",
          "newsMacroSeries",
        ].map((key) => queryClient.invalidateQueries({ queryKey: [key] })),
      );
    } finally {
      setRefreshing(false);
    }
  }
  function selectMarket(item: MarketItem) {
    if (["BTC", "ETH"].includes(item.symbol.replace("USDT", "")))
      navigate(
        `/charts?symbol=${item.symbol.replace("USDT", "")}USDT&exchange=bybit&market=spot`,
      );
    else if (catalog.some((series) => series.id === item.symbol))
      navigate(`/news/macro?id=${encodeURIComponent(item.symbol)}`);
    else setDetail({ type: "market", item });
  }
  function closeDetail() {
    setDetail(null);
    if (section === "intelligence") navigate("/news", { replace: true });
  }
  return (
    <div className="nm-terminal">
      <div className="nm-titlebar">
        <div>
          <h1>News &amp; Macro</h1>
          <p>Новости, события и данные, которые движут рынки.</p>
        </div>
        <div className="nm-title-actions">
          <span>
            Обновлено:{" "}
            {stamp(
              latestTimestamp([
                ...marketItems.map((item) => item.observedAt),
                ...macroSources.map((source) => source.lastSuccess),
                ...newsSources.map((source) => source.lastSuccess),
                news.data?.asOf,
              ]),
              zone,
            )}
          </span>
          <button
            className="nm-button"
            onClick={() => void refresh()}
            disabled={refreshing}
          >
            <RefreshCw size={14} className={refreshing ? "nm-spinning" : ""} />
            {refreshing ? "Обновляем…" : "Обновить"}
          </button>
        </div>
      </div>
      <MarketStrip
        items={marketItems}
        loading={markets.isPending}
        error={markets.isError}
        onSelect={selectMarket}
        retry={() => void markets.refetch()}
        zone={zone}
      />
      <div className="nm-navigation">
        <nav aria-label="Разделы News & Macro">
          {TABS.map((tab) => (
            <Link
              to={tab.path}
              key={tab.id}
              className={
                section === tab.id || (!tab.id && section === "intelligence")
                  ? "is-active"
                  : ""
              }
              aria-current={section === tab.id ? "page" : undefined}
            >
              {tab.title}
            </Link>
          ))}
        </nav>
        <button
          className="nm-reference-link"
          onClick={() => setDetail({ type: "intelligence" })}
        >
          <BookOpen size={15} />
          Что это значит?
        </button>
      </div>
      {reading.error ? (
        <p className="nm-notice" role="status">
          {reading.error}
        </p>
      ) : null}
      {section === "live" ? (
        <LiveNews
          zone={zone}
          reading={reading}
          onSelect={(item) => setDetail({ type: "news", item })}
          onSources={() => setDetail({ type: "sources" })}
        />
      ) : section === "calendar" ? (
        <EconomicCalendar
          zone={zone}
          setZone={setZone}
          now={now}
          onSelect={(event) => setDetail({ type: "event", event })}
        />
      ) : section === "macro" ? (
        <MacroAnalysis
          catalog={catalog}
          loading={overview.isPending}
          error={overview.isError}
          onRetry={() => void overview.refetch()}
          onExplain={() => setDetail({ type: "intelligence" })}
        />
      ) : (
        <>
          {next?.scheduledAt ? (
            <div className="nm-next-event">
              <span className="nm-live-dot" />
              <span>Следующее важное событие</span>
              <button onClick={() => setDetail({ type: "event", event: next })}>
                {next.title}
              </button>
              <strong>{countdown(next.scheduledAt, now)}</strong>
              <span>
                {stamp(next.scheduledAt, zone)} · {zone}
              </span>
            </div>
          ) : null}
          <div className="nm-dashboard-columns">
            <Panel
              title="Главное сегодня"
              action={
                <Link className="nm-text-link" to="/news/live">
                  Вся лента →
                </Link>
              }
              className="nm-dashboard-news"
            >
              {news.isPending ? (
                <Skeleton rows={4} />
              ) : news.isError ? (
                <Empty
                  title="Лента временно недоступна"
                  retry={() => void news.refetch()}
                >
                  Макроданные и календарь обновляются независимо.
                </Empty>
              ) : !news.data?.items.length ? (
                <Empty
                  title={
                    news.data?.syncing
                      ? "Собираем последние публикации"
                      : "За сутки нет доступных публикаций"
                  }
                >
                  Статус источников можно проверить ниже.
                </Empty>
              ) : (
                news.data.items
                  .slice(0, 4)
                  .map((item, index) => (
                    <NewsRow
                      zone={zone}
                      key={item.id}
                      item={item}
                      featured={index === 0}
                      reading={reading}
                      onSelect={(item) => setDetail({ type: "news", item })}
                    />
                  ))
              )}
            </Panel>
            <Panel
              title="Ближайшие события"
              action={
                <Link className="nm-text-link" to="/news/calendar">
                  Весь календарь →
                </Link>
              }
            >
              {overview.isPending ? (
                <Skeleton rows={4} />
              ) : overview.isError ? (
                <Empty
                  title="Расписание недоступно"
                  retry={() => void overview.refetch()}
                />
              ) : !upcoming.length ? (
                <Empty title="Нет подтверждённых ближайших событий">
                  Официальное расписание появится после синхронизации
                  источников.
                </Empty>
              ) : (
                <CalendarTable
                  events={upcoming}
                  compact
                  zone={zone}
                  now={now}
                  onSelect={(event) => setDetail({ type: "event", event })}
                />
              )}
              <p className="nm-note nm-panel-note">
                Время: {zone} · «—» означает отсутствие прогноза или значения.
              </p>
            </Panel>
          </div>
          <div className="nm-dashboard-columns">
            <div>
              <div className="nm-pulse-switch">
                <span>Макропульс</span>
                {[
                  { id: "CPIAUCSL", title: "CPI" },
                  { id: "FEDFUNDS", title: "Ставка ФРС" },
                  { id: "UNRATE", title: "Безработица" },
                ].map((item) => (
                  <button
                    className={pulse === item.id ? "is-active" : ""}
                    aria-pressed={pulse === item.id}
                    key={item.id}
                    onClick={() => setPulse(item.id)}
                  >
                    {item.title}
                  </button>
                ))}
                <Link className="nm-text-link" to={`/news/macro?id=${pulse}`}>
                  Подробнее →
                </Link>
              </div>
              <MacroChart id={pulse} compact />
            </div>
            <Panel
              title="Что это значит?"
              action={
                <button
                  className="nm-text-link"
                  onClick={() => setDetail({ type: "intelligence" })}
                >
                  Справочник →
                </button>
              }
            >
              <div className="nm-glossary-select">
                {GLOSSARY.slice(0, 5).map((entry, i) => (
                  <button
                    key={entry.term}
                    className={term === i ? "is-active" : ""}
                    aria-pressed={term === i}
                    onClick={() => setTerm(i)}
                  >
                    {entry.term}
                  </button>
                ))}
              </div>
              <h3>{GLOSSARY[term].title}</h3>
              <p>{GLOSSARY[term].text}</p>
              <p className="nm-note">
                В карточках событий доступны факты, условные сценарии и реальные
                графики реакции BTC / ETH.
              </p>
              <Link className="nm-text-link" to="/news/calendar">
                Разобрать ближайшую публикацию →
              </Link>
            </Panel>
          </div>
          <div className="nm-series-grid">
            {overview.isPending ? (
              <Skeleton rows={1} />
            ) : (
              catalog
                .filter((series) =>
                  ["CPILFESL", "PCEPILFE", "M2SL", "T10Y2Y"].includes(
                    series.id,
                  ),
                )
                .map((series) => (
                  <SeriesPreview
                    key={series.id}
                    series={series}
                    onSelect={() => navigate(`/news/macro?id=${series.id}`)}
                  />
                ))
            )}
          </div>
          <div className="nm-dashboard-columns">
            <YieldCurve
              series={catalog}
              onExplain={() => setDetail({ type: "intelligence" })}
            />
            <Panel
              title="Рынок и качество данных"
              action={
                <button
                  className="nm-text-link"
                  onClick={() => setDetail({ type: "sources" })}
                >
                  Все источники →
                </button>
              }
            >
              {markets.data?.sentiment?.price != null ? (
                <div className="nm-sentiment">
                  <h3>Настроения Bitcoin</h3>
                  <div className="nm-sentiment-value">
                    <strong>
                      {number(markets.data.sentiment.price, 0)}
                      <small> / 100</small>
                    </strong>
                    <span>
                      {(
                        {
                          "Extreme Fear": "Экстремальный страх",
                          Fear: "Страх",
                          Neutral: "Нейтрально",
                          Greed: "Жадность",
                          "Extreme Greed": "Экстремальная жадность",
                        } as Record<string, string>
                      )[markets.data.sentiment.classification ?? ""] ??
                        markets.data.sentiment.classification ??
                        ""}
                    </span>
                    <External url={markets.data.sentiment.sourceUrl}>
                      Alternative.me
                    </External>
                  </div>
                  <div
                    className="nm-sentiment-meter"
                    role="img"
                    aria-label={`Индекс страха и жадности Bitcoin: ${number(markets.data.sentiment.price, 0)} из 100`}
                  >
                    <i
                      style={{
                        left: `${Math.max(0, Math.min(100, markets.data.sentiment.price))}%`,
                      }}
                    />
                  </div>
                  <div className="nm-sentiment-scale">
                    <span>0 · Страх</span>
                    <span>100 · Жадность</span>
                  </div>
                  <Sparkline
                    values={markets.data.sentiment.points.map(
                      (point) => point.value,
                    )}
                    height={32}
                    label="История индекса страха и жадности Bitcoin"
                  />
                  <p className="nm-note">
                    {markets.data.sentiment.status === "stale"
                      ? "Данные задержаны. "
                      : ""}
                    Наблюдение: {stamp(markets.data.sentiment.observedAt, zone)}
                    . {markets.data.sentiment.note}
                  </p>
                </div>
              ) : (
                <p className="nm-note">
                  Индекс настроений пока недоступен. Статус источника можно
                  проверить ниже.
                </p>
              )}
              <div className="nm-market-heatmap">
                {heatItems.length ? (
                  heatItems.map((item) => (
                    <button
                      className={
                        item.changePercent! >= 0
                          ? "nm-heat-positive"
                          : "nm-heat-negative"
                      }
                      onClick={() => selectMarket(item)}
                      key={item.symbol}
                    >
                      <strong>{item.name}</strong>
                      <b>
                        {item.changePercent! > 0 ? "+" : ""}
                        {number(item.changePercent)}%
                      </b>
                      <small>24 ч · {item.source}</small>
                    </button>
                  ))
                ) : (
                  <p className="nm-note">
                    Тепловая карта появится после получения сопоставимых
                    изменений за 24 часа.
                  </p>
                )}
              </div>
              <p className="nm-note">
                Изменение котировок за 24 часа. Движение цены и индекс
                настроений не являются прогнозами.
              </p>
              <div className="nm-source-summary">
                <span className={healthy ? "nm-live-dot" : "nm-pending-dot"} />
                <span>
                  {sources.isPending
                    ? "Проверяем источники…"
                    : `${healthy} из ${totalSources} источников доступны`}
                </span>
                <button
                  className="nm-text-link"
                  onClick={() => setDetail({ type: "sources" })}
                >
                  Проверить
                </button>
              </div>
            </Panel>
          </div>
        </>
      )}
      <div className="nm-terminal-foot">
        <span>Время интерфейса: {zone}</span>
        <span>
          Факты с источниками · Сценарии с ограничениями · Без вымышленных
          значений
        </span>
        <button
          className="nm-text-link"
          onClick={() => setDetail({ type: "sources" })}
        >
          Качество данных
        </button>
      </div>
      {detail?.type === "news" ? (
        <NewsDetail
          zone={zone}
          item={detail.item}
          saved={reading.state.saved.includes(detail.item.id)}
          toggleSave={() => reading.toggleSave(detail.item)}
          markRead={() => reading.markRead(detail.item.id)}
          onClose={closeDetail}
        />
      ) : detail?.type === "event" ? (
        <EventDetail event={detail.event} onClose={closeDetail} zone={zone} />
      ) : detail?.type === "market" ? (
        <MarketDetail item={detail.item} onClose={closeDetail} zone={zone} />
      ) : detail?.type === "sources" ? (
        <SourcesDetail
          zone={zone}
          news={newsSources}
          macro={macroSources}
          onClose={closeDetail}
        />
      ) : detail?.type === "intelligence" ? (
        <IntelligenceDetail onClose={closeDetail} />
      ) : null}
    </div>
  );
}
export default function NewsMacroPage() {
  const { user } = useAuth();
  return <Terminal key={user?.id ?? "guest"} userId={user?.id ?? "guest"} />;
}
