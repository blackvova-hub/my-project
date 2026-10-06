import { lazy, Suspense, useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Star,
  Pause,
  Play,
  Search,
  Check,
  ChevronRight,
  ChevronDown,
  RefreshCw,
} from "lucide-react";
import { SegmentedControl } from "../../../shared/ui/SegmentedControl";
import { http } from "../../../shared/api/http";
import { useAuth } from "../../../shared/auth/AuthContext";
import {
  duration,
  exchangeNames,
  exchanges,
  fmt,
  hydrateDataset,
  identityLabel,
  isStale,
  simpleQuery,
  simpleSettings,
  validSearch,
  type WireDataset,
  type Opportunity,
  type Preferences,
  type Settings,
} from "./model";
import "./arbitrage.css";
const RouteDetails = lazy(() => import("./RouteDetails"));
const strategies = [
  { value: "spot-perpetual", label: "Спот–фьючерс" },
  { value: "perpetual-perpetual", label: "Фьючерс–фьючерс" },
  { value: "spot-spot", label: "Спот–спот" },
];
const descriptions: Record<string, string> = {
  "spot-perpetual": "Покупаем спот на одной бирже, продаём фьючерс на другой.",
  "perpetual-perpetual":
    "Покупаем фьючерс на одной бирже, продаём фьючерс на другой.",
  "spot-spot":
    "Покупаем монету на одной бирже, продаём на другой. Средства нужны на обеих биржах.",
};
const tabs = [
  { value: "all", label: "Все" },
  { value: "favorites", label: "Избранное" },
];
export default function ArbitragePage() {
  const { user } = useAuth();
  const prefs = useQuery({
    queryKey: ["arbitrage-preferences", user?.id],
    queryFn: ({ signal }) =>
      http<Preferences>("/arbitrage/preferences", { signal }),
    staleTime: Infinity,
    retry: 1,
  });
  return (
    <main className="arb-page mx-auto min-h-screen max-w-7xl px-4 py-8 text-foreground md:px-6">
      {prefs.isPending ? (
        <div role="status" className="arb-panel">
          Загрузка…
        </div>
      ) : prefs.isError ? (
        <div role="alert" className="arb-panel">
          Не удалось загрузить страницу.{" "}
          <button className="arb-button" onClick={() => void prefs.refetch()}>
            Повторить
          </button>
        </div>
      ) : (
        <Workspace
          key={user?.id}
          initial={prefs.data}
          userKey={user?.id ?? ""}
        />
      )}
    </main>
  );
}
function Workspace({
  initial,
  userKey,
}: {
  initial: Preferences;
  userKey: string;
}) {
  const queryClient = useQueryClient();
  const [settings, setSettings] = useState(() =>
    simpleSettings(initial.settings),
  );
  const [favorites, setFavorites] = useState(initial.favorites);
  const [paused, setPaused] = useState(false);
  const [tab, setTab] = useState("all");
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<Opportunity | null>(null);
  const [notice, setNotice] = useState("");
  const [now, setNow] = useState(Date.now);
  const serialized = simpleQuery(
    settings,
    offset,
    tab === "favorites" ? favorites : undefined,
  );
  const [query, setQuery] = useState(serialized);
  const valid =
    settings.exchanges.length >= 2 && validSearch(settings.search ?? "");
  useEffect(() => {
    const t = window.setTimeout(() => setQuery(serialized), 300);
    return () => window.clearTimeout(t);
  }, [serialized]);
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, []);
  const dataset = useQuery({
    queryKey: ["arbitrage", userKey, query],
    queryFn: ({ signal }) =>
      http<WireDataset>(`/arbitrage/opportunities?${query}`, { signal }).then(
        hydrateDataset,
      ),
    enabled: valid && !paused,
    refetchInterval: paused ? false : 2000,
    staleTime: 1000,
    retry: 1,
  });
  const save = useMutation({
    scope: { id: `arbitrage-preferences-${userKey}` },
    mutationFn: (p: Preferences) =>
      http<Preferences>("/arbitrage/preferences", {
        method: "PUT",
        body: JSON.stringify(p),
      }),
    onSuccess: (p) =>
      queryClient.setQueryData(["arbitrage-preferences", userKey], p),
  });
  const persist = async (next: Settings, stars = favorites) => {
    try {
      await save.mutateAsync({ settings: next, favorites: stars });
      setNotice("");
    } catch {
      setNotice("Не удалось сохранить выбор. Повторите изменение.");
    }
  };
  const change = (next: Settings) => {
    setSettings(next);
    setOffset(0);
    if (next.exchanges.length >= 2 && validSearch(next.search ?? ""))
      void persist(next);
  };
  const favorite = async (id: string) => {
    const next = favorites.includes(id)
      ? favorites.filter((v) => v !== id)
      : [...favorites, id];
    if (next.length > 200) {
      setNotice("Можно сохранить до 200 связок.");
      return;
    }
    try {
      await save.mutateAsync({ settings, favorites: next });
      setFavorites(next);
      setNotice("");
    } catch {
      setNotice("Не удалось сохранить избранное.");
    }
  };
  const data = dataset.data;
  const pageOffset = data?.offset ?? offset;
  const quoteNow = data
    ? data.at + Math.max(0, now - dataset.dataUpdatedAt)
    : now;
  const unhealthy =
    !!dataset.error ||
    data?.transport !== "live" ||
    query !== serialized ||
    !valid;
  const rows =
    tab === "favorites" && !favorites.length ? [] : (data?.rows ?? []);
  const total =
    tab === "favorites" && !favorites.length ? 0 : (data?.total ?? 0);
  const detail = selected
    ? (rows.find((o) => o.id === selected.id) ?? selected)
    : null;
  const needed =
    settings.strategy === "spot-perpetual"
      ? ["spot", "perpetual"]
      : [settings.strategy === "spot-spot" ? "spot" : "perpetual"];
  const sourceStates = exchanges.map((e) => {
    const sources =
      data?.sources.filter(
        (s) => s.exchange === e && needed.includes(s.market),
      ) ?? [];
    const live =
      !unhealthy &&
      needed.every((k) =>
        sources.some(
          (s) =>
            s.market === k &&
            s.state === "live" &&
            quoteNow - s.updatedAt <= 10000 &&
            s.books > 0 &&
            quoteNow - s.lastBookAt <= 10000,
        ),
      );
    const complete =
      live &&
      sources.every((s) => !s.connections || s.connected === s.connections);
    return { exchange: e, sources, live, complete };
  });
  const liveCount = sourceStates.filter((s) => s.live).length;
  return (
    <>
      <div className="arb-heading">
        <div className="flex flex-wrap items-baseline gap-x-6 gap-y-1">
          <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">
            Арбитраж
          </h1>
          <span className="text-sm text-muted-foreground">
            {data?.universeMode === "selected"
              ? "Выбранные монеты"
              : "Все монеты"}{" "}
            · 6 бирж
          </span>
        </div>
        <button
          className="arb-button"
          aria-pressed={paused}
          onClick={() => setPaused((v) => !v)}
        >
          {paused ? <Play size={16} /> : <Pause size={16} />}{" "}
          {paused ? "Продолжить" : "Пауза"}
        </button>
      </div>
      <section className="arb-panel" aria-label="Арбитражные связки">
        <div className="arb-controls">
          <SegmentedControl
            wrap
            value={settings.strategy}
            options={strategies}
            label="Режим арбитража"
            onChange={(strategy) => change({ ...settings, strategy })}
          />
          <label className="arb-search">
            <Search size={18} aria-hidden="true" />
            <input
              aria-label="Найти монету"
              placeholder="Найти монету"
              maxLength={30}
              value={settings.search ?? ""}
              onChange={(e) => {
                setSettings((s) => ({
                  ...s,
                  search: e.target.value.toUpperCase().trim(),
                }));
                setOffset(0);
              }}
              onBlur={() => {
                if (valid) void persist(settings);
              }}
            />
          </label>
        </div>
        <div className="arb-exchanges" role="group" aria-label="Биржи">
          {sourceStates.map(({ exchange: e, live, complete }) => {
            const checked = settings.exchanges.includes(e);
            return (
              <button
                key={e}
                type="button"
                className="arb-exchange"
                aria-pressed={checked}
                disabled={save.isPending}
                onClick={() =>
                  change({
                    ...settings,
                    exchanges: checked
                      ? settings.exchanges.filter((v) => v !== e)
                      : [...settings.exchanges, e],
                  })
                }
              >
                <span
                  className={`arb-check ${checked ? "arb-checked" : ""}`}
                  aria-hidden="true"
                >
                  {checked ? <Check size={13} /> : null}
                </span>
                <span
                  className={`arb-dot ${live ? (complete ? "arb-live" : "arb-partial") : ""}`}
                  aria-hidden="true"
                />
                <span>{exchangeNames[e]}</span>
              </button>
            );
          })}
        </div>
        <p className="arb-description">{descriptions[settings.strategy]}</p>
        {notice ? (
          <p role="status" className="mb-3 text-sm text-destructive">
            {notice}
          </p>
        ) : null}
        {!valid ? (
          <p role="alert" className="mb-3 text-sm text-destructive">
            {settings.exchanges.length < 2
              ? "Выберите минимум две биржи."
              : "Введите тикер монеты: например BTC или 1INCH."}
          </p>
        ) : null}
        <div className="arb-results-heading">
          <h2 className="text-lg font-semibold">
            Связки{" "}
            <span className="ml-2 text-sm font-normal text-muted-foreground">
              {total}
            </span>
          </h2>
          <SegmentedControl
            value={tab}
            options={tabs}
            label="Список связок"
            onChange={(v) => {
              setTab(v);
              setOffset(0);
            }}
          />
        </div>
        {dataset.isError ? (
          <div
            role="alert"
            className="mb-3 flex flex-wrap items-center gap-3 text-sm text-destructive"
          >
            Котировки временно недоступны.
            <button
              className="arb-button"
              onClick={() => void dataset.refetch()}
            >
              <RefreshCw size={14} />
              Повторить
            </button>
          </div>
        ) : null}
        <div className="arb-table-scroll">
          <table className="arb-table w-full text-left text-sm">
            <thead>
              <tr>
                {[
                  "Монета",
                  "Купить",
                  "Продать",
                  "Разница",
                  "Доступно, USDT",
                  "Длительность",
                ].map((t) => (
                  <th key={t} scope="col">
                    {t}
                  </th>
                ))}
                <th scope="col">
                  <span className="sr-only">Подробности</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((o) => {
                const stale = unhealthy || isStale(o, quoteNow, 10000);
                return (
                  <tr key={o.id}>
                    <td>
                      <div className="flex items-center gap-3">
                        <button
                          className="arb-star"
                          aria-label={`${favorites.includes(o.id) ? "Убрать из избранного" : "В избранное"} ${o.asset} ${exchangeNames[o.buy.instrument.exchange]} ${exchangeNames[o.sell.instrument.exchange]}`}
                          aria-pressed={favorites.includes(o.id)}
                          disabled={save.isPending || !valid}
                          onClick={() => void favorite(o.id)}
                        >
                          <Star
                            size={16}
                            fill={
                              favorites.includes(o.id) ? "currentColor" : "none"
                            }
                          />
                        </button>
                        <button
                          className="font-semibold underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring"
                          onClick={() => setSelected(o)}
                        >
                          {o.asset}
                        </button>
                      </div>
                      {identityLabel(o.buy.instrument.assetIdentity) ? (
                        <span className="ml-9 text-xs text-muted-foreground">
                          {identityLabel(o.buy.instrument.assetIdentity)}
                        </span>
                      ) : null}
                      {stale ? (
                        <span className="text-xs text-muted-foreground">
                          Устарело
                        </span>
                      ) : null}
                    </td>
                    {[o.buy, o.sell].map((leg, i) => (
                      <td key={i}>
                        <div className="font-semibold">
                          {exchangeNames[leg.instrument.exchange]}
                        </div>
                        <div className="arb-price">
                          {leg.instrument.market === "spot"
                            ? "Спот"
                            : "Фьючерс"}{" "}
                          ·{" "}
                          {leg.price.toLocaleString("ru-RU", {
                            maximumSignificantDigits: 8,
                          })}
                        </div>
                      </td>
                    ))}
                    <td
                      className={
                        stale ? "text-muted-foreground" : "arb-positive"
                      }
                    >
                      {stale ? "—" : `+${fmt(o.entrySpreadPct)} %`}
                    </td>
                    <td>{stale ? "—" : fmt(o.depthUsdt)}</td>
                    <td>{stale ? "—" : duration(o.since, quoteNow)}</td>
                    <td>
                      <button
                        className="arb-star"
                        aria-label={`Подробности ${o.asset} ${exchangeNames[o.buy.instrument.exchange]} ${exchangeNames[o.sell.instrument.exchange]}`}
                        onClick={() => setSelected(o)}
                      >
                        <ChevronRight size={18} />
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {!rows.length ? (
            <div role="status" className="arb-empty">
              <p className="font-semibold">
                {!valid
                  ? "Проверьте выбор"
                  : tab === "favorites" && !favorites.length
                    ? "Пока нет избранных связок"
                    : dataset.isPending && !paused
                      ? "Получение котировок…"
                      : unhealthy
                        ? "Нет актуальных данных"
                        : "Связок не найдено"}
              </p>
              <p className="mt-2 text-sm text-muted-foreground">
                {tab === "favorites"
                  ? "Нажмите звёздочку у связки. Здесь появятся сохранённые связки, когда разница цен положительная."
                  : "Попробуйте другую монету или тип связки. Биржи сканируются автоматически."}
              </p>
            </div>
          ) : null}
        </div>
        {total > 100 ? (
          <div className="arb-pagination">
            <span>
              {pageOffset + 1}–{Math.min(pageOffset + rows.length, total)} из{" "}
              {total}
            </span>
            <button
              className="arb-button"
              disabled={pageOffset === 0 || paused}
              onClick={() => setOffset(Math.max(0, pageOffset - 100))}
            >
              Назад
            </button>
            <button
              className="arb-button"
              disabled={pageOffset + 100 >= total || paused}
              onClick={() => setOffset(pageOffset + 100)}
            >
              Далее
            </button>
          </div>
        ) : null}
        <p className="arb-footnote">
          Разница цен — до расходов на сделки. Доступно — объём по указанным
          ценам.
        </p>
        <details className="arb-sources">
          <summary>
            <span className="flex items-center gap-3">
              <ChevronDown size={16} />
              Состояние бирж
            </span>
            <span className="arb-source-summary">
              <span
                className={`arb-dot ${liveCount === 6 ? "arb-live" : liveCount ? "arb-partial" : ""}`}
              />
              {paused ? "Пауза" : `${liveCount} из 6`} · Обновление каждые 2 с
            </span>
          </summary>
          <div className="arb-source-list">
            {sourceStates.map(({ exchange, sources, live, complete }) => (
              <div key={exchange}>
                <div className="font-semibold">
                  {exchangeNames[exchange]} ·{" "}
                  {paused
                    ? "Пауза"
                    : complete
                      ? "Подключено"
                      : live
                        ? "Частично подключено"
                        : "Нет свежих данных"}
                </div>
                {sources.map((s) => (
                  <p
                    key={s.market}
                    className="mt-1 text-xs text-muted-foreground"
                  >
                    {s.market === "spot" ? "Спот" : "Фьючерс"}: свежих стаканов{" "}
                    {s.books} / рынков {s.instruments}
                    {s.connections
                      ? ` · подключений ${s.connected}/${s.connections}`
                      : ""}
                  </p>
                ))}
              </div>
            ))}
          </div>
          <p className="mt-4 text-xs text-muted-foreground">
            {data?.assets.length ?? 0} монет с котировками · активные спот-пары
            USDT и бессрочные USDT-фьючерсы. Устаревшие котировки не
            используются.
          </p>
        </details>
      </section>
      {detail ? (
        <Suspense fallback={<div role="status">Загрузка подробностей…</div>}>
          <RouteDetails
            row={detail}
            available={!unhealthy && rows.some((o) => o.id === detail.id)}
            stale={isStale(detail, quoteNow, 10000)}
            onClose={() => setSelected(null)}
          />
        </Suspense>
      ) : null}
    </>
  );
}
