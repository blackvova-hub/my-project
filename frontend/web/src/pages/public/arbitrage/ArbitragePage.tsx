import { lazy, Suspense, useEffect, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { SegmentedControl as Segments } from "../../../shared/ui/SegmentedControl";
import { Star, Pause, Play, RefreshCw } from "lucide-react";
import { http } from "../../../shared/api/http";
import { useAuth } from "../../../shared/auth/AuthContext";
import { AnimatedSelect } from "../../../shared/ui/AnimatedSelect";
import {
  defaults,
  duration,
  exchangeNames,
  exchanges,
  fmt,
  hydrateDataset,
  isStale,
  parseDraft,
  queryString,
  toDraft,
  type WireDataset,
  type Draft,
  type Opportunity,
  type Preferences,
  type Settings,
} from "./model";
import "./arbitrage.css";

const RouteDetails = lazy(() => import("./RouteDetails"));
const inputClass =
  "w-full rounded-xl border border-border bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground focus:border-ring focus:ring-2 focus:ring-ring outline-none";
const buttonClass =
  "rounded-xl border border-border bg-secondary px-4 py-2 text-sm font-semibold text-secondary-foreground hover:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:opacity-50";
const strategies = [
  { value: "spot-perpetual", label: "Спот–фьючерс" },
  { value: "perpetual-perpetual", label: "Фьючерс–фьючерс" },
  { value: "spot-spot", label: "Спот–спот" },
];
const sorts = [
  { value: "spread", label: "По спреду" },
  { value: "net", label: "После комиссий входа" },
  { value: "funding", label: "По funding" },
  { value: "duration", label: "По длительности" },
];
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block min-w-0">
      <span className="mb-1 block text-xs uppercase text-muted-foreground">
        {label}
      </span>
      {children}
    </label>
  );
}
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
    <div className="arb-page relative mx-auto min-h-screen max-w-7xl px-4 py-8 text-foreground md:px-6">
      <div className="relative mb-6">
        <h1 className="mt-3 text-3xl font-semibold tracking-tight md:text-4xl">
          Арбитраж
        </h1>
      </div>
      {prefs.isPending ? (
        <div role="status" className="arb-panel">
          Загрузка настроек…
        </div>
      ) : prefs.isError ? (
        <div role="alert" className="arb-panel">
          Не удалось загрузить настройки.{" "}
          <button className={buttonClass} onClick={() => void prefs.refetch()}>
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
    </div>
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
  const [draft, setDraft] = useState<Draft>(() =>
    toDraft(initial.settings ?? defaults),
  );
  const [favorites, setFavorites] = useState(initial.favorites);
  const [savedSettings, setSavedSettings] = useState<Settings>(
    initial.settings ?? defaults,
  );
  const [paused, setPaused] = useState(false);
  const [tab, setTab] = useState("all");
  const [selected, setSelected] = useState<Opportunity | null>(null);
  const [notice, setNotice] = useState("");
  const [now, setNow] = useState(Date.now);
  const parsed = parseDraft(draft);
  const serialized = parsed.settings ? queryString(parsed.settings) : "";
  const [query, setQuery] = useState(serialized);
  useEffect(() => {
    const t = window.setTimeout(() => setQuery(serialized), 350);
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
    enabled: !!query && !!serialized && !paused,
    refetchInterval: paused ? false : 2000,
    staleTime: 1000,
    retry: 1,
  });
  const save = useMutation({
    mutationFn: (p: Preferences) =>
      http<Preferences>("/arbitrage/preferences", {
        method: "PUT",
        body: JSON.stringify(p),
      }),
    onSuccess: (p) =>
      queryClient.setQueryData(["arbitrage-preferences", userKey], p),
  });
  const change = <K extends keyof Draft>(key: K, value: Draft[K]) => {
    setDraft((d) => ({ ...d, [key]: value }));
    setNotice("");
  };
  const saveFilters = async () => {
    if (!parsed.settings || save.isPending) return;
    try {
      await save.mutateAsync({ settings: parsed.settings, favorites });
      setSavedSettings(parsed.settings);
      setNotice("Настройки сохранены.");
    } catch (e) {
      setNotice(
        e instanceof Error ? e.message : "Не удалось сохранить настройки.",
      );
    }
  };
  const favorite = async (id: string) => {
    if (save.isPending) return;
    const next = favorites.includes(id)
      ? favorites.filter((v) => v !== id)
      : [...favorites, id];
    if (next.length > 200) {
      setNotice("В избранном можно сохранить до 200 связок.");
      return;
    }
    try {
      await save.mutateAsync({ settings: savedSettings, favorites: next });
      setFavorites(next);
    } catch (e) {
      setNotice(
        e instanceof Error ? e.message : "Не удалось сохранить избранное.",
      );
    }
  };
  const data = dataset.data;
  const quoteNow = data
    ? data.at + Math.max(0, now - dataset.dataUpdatedAt)
    : now;
  const changed = serialized !== query;
  const allRows = data?.rows ?? [];
  const rows =
    tab === "favorites"
      ? allRows.filter((o) => favorites.includes(o.id))
      : allRows;
  const unhealthy =
    !!dataset.error || data?.transport !== "live" || changed || !serialized;
  const detail = selected
    ? (allRows.find((o) => o.id === selected.id) ?? selected)
    : null;
  const detailAvailable =
    !!selected && !unhealthy && allRows.some((o) => o.id === selected.id);
  return (
    <div className="flex flex-col gap-4">
      <section className="arb-panel" aria-labelledby="arb-filters">
        <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
          <h2 id="arb-filters" className="text-lg font-semibold">
            Настройки поиска
          </h2>
          <div className="flex gap-2">
            <button
              className={buttonClass}
              disabled={!parsed.settings || save.isPending}
              onClick={() => void saveFilters()}
            >
              Сохранить
            </button>
            <button
              className={buttonClass}
              onClick={() => {
                setDraft(toDraft(defaults));
                setNotice(
                  "Параметры сброшены. Нажмите «Сохранить», чтобы запомнить их.",
                );
              }}
            >
              Сбросить
            </button>
          </div>
        </div>
        <div className="grid grid-cols-1 gap-4 rounded-2xl border border-border bg-background p-4 md:grid-cols-2">
          <div className="md:col-span-2">
            <div className="mb-1 text-xs uppercase text-muted-foreground">
              Режим
            </div>
            <Segments
              wrap
              value={draft.strategy}
              options={strategies}
              onChange={(v) => change("strategy", v)}
              label="Режим арбитража"
            />
          </div>
          <div className="md:col-span-2">
            <div className="mb-1 text-xs uppercase text-muted-foreground">
              Биржи
            </div>
            <div
              className="flex flex-wrap gap-2"
              role="group"
              aria-label="Биржи"
            >
              {exchanges.map((e) => (
                <button
                  type="button"
                  key={e}
                  aria-pressed={draft.exchanges.includes(e)}
                  onClick={() =>
                    change(
                      "exchanges",
                      draft.exchanges.includes(e)
                        ? draft.exchanges.filter((v) => v !== e)
                        : [...draft.exchanges, e],
                    )
                  }
                  className={`rounded-xl border border-border px-3 py-2 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-ring ${draft.exchanges.includes(e) ? "bg-primary text-primary-foreground" : "bg-background text-muted-foreground hover:bg-secondary"}`}
                >
                  {exchangeNames[e]}
                </button>
              ))}
            </div>
          </div>
          <Field label="Размер покупки, USDT">
            <input
              aria-label="Размер покупки, USDT"
              className={inputClass}
              inputMode="decimal"
              value={draft.amount}
              onChange={(e) => change("amount", e.target.value)}
            />
          </Field>
          <Field label="Минимальный спред, %">
            <input
              aria-label="Минимальный спред, %"
              className={inputClass}
              inputMode="decimal"
              value={draft.minSpread}
              onChange={(e) => change("minSpread", e.target.value)}
            />
            <input
              aria-label="Ползунок минимального спреда"
              className="arb-range mt-3 w-full"
              type="range"
              min="0"
              max="5"
              step="0.05"
              value={Math.max(
                0,
                Math.min(5, Number(draft.minSpread.replace(",", ".")) || 0),
              )}
              onChange={(e) => change("minSpread", e.target.value)}
            />
            <span className="mt-1 flex justify-between text-xs text-muted-foreground">
              <span>0</span>
              <span>5</span>
            </span>
          </Field>
          <Field label="Монеты">
            <input
              aria-label="Монеты"
              className={inputClass}
              placeholder="Все доступные · BTC, ETH, SOL"
              value={draft.assets}
              onChange={(e) => change("assets", e.target.value)}
            />
          </Field>
          <Field label="Сортировка">
            <AnimatedSelect
              value={draft.sort}
              options={sorts}
              onChange={(v) => change("sort", v)}
              className={inputClass}
              ariaLabel="Сортировка"
            />
          </Field>
          <details className="rounded-xl border border-border p-3 md:col-span-2">
            <summary className="cursor-pointer text-sm font-semibold focus-visible:outline-2 focus-visible:outline-ring">
              Комиссии и сценарий выхода
            </summary>
            <div className="mt-4 grid gap-4 md:grid-cols-2">
              <Field label="Горизонт funding">
                <AnimatedSelect
                  value={String(draft.horizonHours)}
                  options={[1, 8, 24, 72, 168].map((v) => ({
                    value: String(v),
                    label: `${v} ч`,
                  }))}
                  onChange={(v) => change("horizonHours", Number(v))}
                  className={inputClass}
                  ariaLabel="Горизонт funding"
                />
              </Field>
              <Field label="Спред при закрытии, %">
                <input
                  aria-label="Спред при закрытии, %"
                  className={inputClass}
                  inputMode="decimal"
                  value={draft.exitSpread}
                  onChange={(e) => change("exitSpread", e.target.value)}
                />
              </Field>
              <Field label="Максимальный возраст котировки">
                <AnimatedSelect
                  value={String(draft.maxAgeMs)}
                  options={[1000, 3000, 5000, 10000, 30000].map((v) => ({
                    value: String(v),
                    label: `${v / 1000} с`,
                  }))}
                  onChange={(v) => change("maxAgeMs", Number(v))}
                  className={inputClass}
                  ariaLabel="Максимальный возраст котировки"
                />
              </Field>
              <Field label="Число результатов">
                <AnimatedSelect
                  value={String(draft.limit)}
                  options={[50, 100, 300].map((v) => ({
                    value: String(v),
                    label: String(v),
                  }))}
                  onChange={(v) => change("limit", Number(v))}
                  className={inputClass}
                  ariaLabel="Число результатов"
                />
              </Field>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={draft.interExchange}
                  onChange={(e) => change("interExchange", e.target.checked)}
                  className="accent-primary"
                />
                Только между разными биржами
              </label>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={draft.manualFees}
                  onChange={(e) => change("manualFees", e.target.checked)}
                  className="accent-primary"
                />
                Задать комиссии вручную
              </label>
              {draft.manualFees && (
                <>
                  <Field label="Taker спот, %">
                    <input
                      aria-label="Taker спот, %"
                      className={inputClass}
                      inputMode="decimal"
                      value={draft.feeSpot}
                      onChange={(e) => change("feeSpot", e.target.value)}
                    />
                  </Field>
                  <Field label="Taker фьючерс, %">
                    <input
                      aria-label="Taker фьючерс, %"
                      className={inputClass}
                      inputMode="decimal"
                      value={draft.feePerpetual}
                      onChange={(e) => change("feePerpetual", e.target.value)}
                    />
                  </Field>
                </>
              )}
              <p className="text-xs leading-relaxed text-muted-foreground md:col-span-2">
                По умолчанию используются публичные базовые taker-тарифы; ваш
                VIP-уровень и скидки не учитываются. Сценарий предполагает
                неизменную цену первой ноги и текущую ставку funding до конца
                горизонта. Funding может измениться.
              </p>
            </div>
          </details>
          {parsed.error && (
            <p role="alert" className="text-sm text-destructive md:col-span-2">
              {parsed.error}
            </p>
          )}
        </div>
        {notice && (
          <p role="status" className="mt-3 text-sm">
            {notice}
          </p>
        )}
      </section>
      <section className="arb-panel !py-4" aria-label="Статус бирж">
        <div className="flex flex-col items-start gap-4 lg:flex-row lg:items-center">
          <h2 className="text-sm font-semibold">Источники котировок</h2>
          <div className="grid w-full flex-1 grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
            {exchanges.map((e) => {
              const ss = data?.sources.filter((s) => s.exchange === e) ?? [];
              const needed =
                draft.strategy === "spot-perpetual"
                  ? ["spot", "perpetual"]
                  : [draft.strategy === "spot-spot" ? "spot" : "perpetual"];
              const live =
                !unhealthy &&
                needed.every((k) =>
                  ss.some(
                    (s) =>
                      s.market === k &&
                      s.state === "live" &&
                      quoteNow - s.updatedAt < 10000 &&
                      quoteNow - s.lastBookAt <= draft.maxAgeMs,
                  ),
                );
              return (
                <div
                  key={e}
                  className="min-w-0 border-l border-border pl-3"
                  title={ss
                    .map(
                      (s) =>
                        `${s.market === "spot" ? "Спот" : "Фьючерс"}: ${s.books}/${s.instruments} · ${s.message || s.state}`,
                    )
                    .join("\n")}
                >
                  <div className="flex items-center gap-2 text-sm">
                    <span
                      aria-hidden="true"
                      className={`h-2 w-2 shrink-0 rounded-full ${live ? "bg-primary" : "bg-muted-foreground"}`}
                    />
                    {exchangeNames[e]}
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {live
                      ? `Стаканы: ${ss.filter((s) => needed.includes(s.market)).reduce((n, s) => n + s.books, 0)}`
                      : paused
                        ? "Пауза"
                        : dataset.isPending
                          ? "Подключение…"
                          : "Нет свежих данных"}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
        <p className="mt-3 text-xs text-muted-foreground">
          Покрытие: {data?.assets.length ?? 0} из{" "}
          {data?.configuredAssets.length ?? 30} настроенных монет · обновление
          страницы каждые 2 с · Bybit: первый уровень, остальные: до 5 уровней
          стакана.
        </p>
      </section>
      <section className="arb-panel" aria-labelledby="arb-results">
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <h2 id="arb-results" className="text-lg font-semibold">
            Арбитражные связки{" "}
            <span className="text-sm font-normal text-muted-foreground">
              {rows.length}
            </span>
          </h2>
          <div className="flex flex-wrap items-center gap-3">
            <Segments
              wrap
              value={tab}
              options={[
                { value: "all", label: "Все" },
                {
                  value: "favorites",
                  label: `Избранное${favorites.length ? ` (${favorites.length})` : ""}`,
                },
              ]}
              label="Список связок"
              onChange={setTab}
            />
            <button
              className={`${buttonClass} flex items-center gap-2`}
              aria-pressed={paused}
              onClick={() => setPaused((v) => !v)}
            >
              {paused ? <Play size={14} /> : <Pause size={14} />}
              {paused ? "Продолжить" : "Пауза"}
            </button>
          </div>
        </div>
        {paused && (
          <p role="status" className="mb-3 text-sm text-muted-foreground">
            Обновление приостановлено. Возраст котировок продолжает
            отсчитываться.
          </p>
        )}
        {(dataset.error ||
          data?.transport === "error" ||
          data?.transport === "stale") && (
          <div
            role="alert"
            className="mb-3 flex flex-wrap items-center justify-between gap-2 text-sm text-destructive"
          >
            <span>
              {dataset.error instanceof Error
                ? dataset.error.message
                : "Нет связи с хранилищем котировок."}
            </span>
            <button
              disabled={paused || !serialized}
              className={`${buttonClass} flex items-center gap-2`}
              onClick={() => void dataset.refetch()}
            >
              <RefreshCw size={14} />
              Повторить
            </button>
          </div>
        )}
        <div className="overflow-hidden rounded-2xl border border-border">
          <div className="overflow-x-auto">
            <table className="arb-table w-full text-left text-sm">
              <caption className="sr-only">
                Покупка первой ноги и продажа второй на равный объём монеты.
                Сортировка: {sorts.find((s) => s.value === draft.sort)?.label}.
              </caption>
              <thead>
                <tr>
                  {[
                    "Монета",
                    "Купить · Long",
                    "Продать · Short",
                    "Спред входа",
                    "После комиссий входа",
                    `Funding за ${draft.horizonHours} ч`,
                    "Глубина, USDT",
                    "Длительность",
                  ].map((t) => (
                    <th
                      key={t}
                      scope="col"
                      className="whitespace-nowrap border-b border-border px-4 py-3 text-xs font-medium text-muted-foreground"
                    >
                      {t}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((o) => {
                  const stale =
                    unhealthy || isStale(o, quoteNow, draft.maxAgeMs);
                  return (
                    <tr
                      key={o.id}
                      className="border-b border-border last:border-b-0 hover:bg-secondary/50"
                    >
                      <td className="px-4 py-3">
                        <div className="flex items-center gap-2">
                          <button
                            className="rounded p-1 text-muted-foreground hover:text-primary focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50"
                            aria-label={`${favorites.includes(o.id) ? "Убрать из избранного" : "В избранное"} ${o.asset} ${exchangeNames[o.buy.instrument.exchange]} ${exchangeNames[o.sell.instrument.exchange]}`}
                            aria-pressed={favorites.includes(o.id)}
                            disabled={save.isPending}
                            onClick={() => void favorite(o.id)}
                          >
                            <Star
                              size={16}
                              fill={
                                favorites.includes(o.id)
                                  ? "currentColor"
                                  : "none"
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
                        {stale && (
                          <span className="text-xs text-muted-foreground">
                            Устарело
                          </span>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-4 py-3">
                        {exchangeNames[o.buy.instrument.exchange]}
                        <div className="text-xs text-muted-foreground">
                          {o.buy.instrument.market === "spot"
                            ? "Спот"
                            : "Фьючерс"}{" "}
                          · {fmt(o.buy.price, 6)}
                        </div>
                      </td>
                      <td className="whitespace-nowrap px-4 py-3">
                        {exchangeNames[o.sell.instrument.exchange]}
                        <div className="text-xs text-muted-foreground">
                          {o.sell.instrument.market === "spot"
                            ? "Спот"
                            : "Фьючерс"}{" "}
                          · {fmt(o.sell.price, 6)}
                        </div>
                      </td>
                      <td className="whitespace-nowrap px-4 py-3 font-medium">
                        {stale ? "—" : `${fmt(o.entrySpreadPct)} %`}
                      </td>
                      <td className="whitespace-nowrap px-4 py-3">
                        {stale ? "—" : `${fmt(o.netEntryPct)} %`}
                      </td>
                      <td className="whitespace-nowrap px-4 py-3">
                        {stale || o.fundingEstimate == null
                          ? "—"
                          : `${fmt((o.fundingEstimate / o.buy.notional) * 100)} %`}
                      </td>
                      <td className="px-4 py-3">{fmt(o.depthUsdt, 0)}</td>
                      <td className="whitespace-nowrap px-4 py-3">
                        {stale ? "—" : duration(o.since, quoteNow)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          {!rows.length && (
            <div role="status" className="px-4 py-10 text-center">
              <div className="font-semibold">
                {dataset.isPending && !paused && serialized
                  ? "Получение котировок…"
                  : parsed.error
                    ? "Проверьте параметры поиска"
                    : unhealthy
                      ? "Нет актуальных данных"
                      : "Связок по выбранным условиям нет"}
              </div>
              <p className="mt-2 text-sm text-muted-foreground">
                {tab === "favorites"
                  ? "Избранные связки отображаются, когда попадают в текущие результаты поиска."
                  : "Измените фильтры или дождитесь обновления котировок."}
              </p>
            </div>
          )}
        </div>
      </section>
      <div className="space-y-2 text-xs leading-relaxed text-muted-foreground">
        <p>
          Входной спред не равен прибыли после закрытия. Глубина — объём только
          полученных уровней стакана. Длительность — непрерывный положительный
          спред лучшего bid/ask, измеренный сервером раз в секунду.
        </p>
        <p>
          {draft.strategy === "spot-spot"
            ? "Спот–спот предполагает заранее размещённые средства на обеих биржах. Перевод, вывод и восстановление балансов в результат не включены."
            : "Обе ноги открываются на одинаковое количество монеты. В сценарии учитываются комиссии входа и выхода; результат доступен при наличии свежего funding."}{" "}
          Исполнение ордеров, заём, ликвидация и гарантия одновременного
          исполнения не входят в сканер.
        </p>
      </div>
      {detail && (
        <Suspense fallback={<div role="status">Загрузка подробностей…</div>}>
          <RouteDetails
            row={detail}
            available={detailAvailable}
            stale={isStale(detail, quoteNow, draft.maxAgeMs)}
            onClose={() => setSelected(null)}
          />
        </Suspense>
      )}
    </div>
  );
}
