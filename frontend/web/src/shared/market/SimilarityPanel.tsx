import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { AnimatedSelect } from "../ui/AnimatedSelect";
import { http } from "../api/http";
import { useAuth } from "../auth/AuthContext";
import type { PrimaryExchange } from "../exchange/primaryExchange";
import { SimilarityChart } from "./SimilarityChart";
import { similarityTimeframes } from "./similarityCandles";
import type { SimilarityCandle, SimilarityTimeframe } from "./similarityCandles";
import { universeFromRequest, universeOptions, universeRequest } from "./similarityUniverse";

const timeframeKey = "similarity:chart-timeframe:v1";
function readTimeframe(): SimilarityTimeframe {
  try {
    const saved = Number(localStorage.getItem(timeframeKey));
    return similarityTimeframes.find((item) => item.seconds === saved)?.seconds ?? 3600;
  } catch {
    return 3600;
  }
}

export type SimilarityMatch = {
  id: string;
  symbol: string;
  market: string;
  start: number;
  end: number;
  score: number;
  breakdown: {
    price: number;
    volume: number;
    volatility: number;
    candles: number;
    statistics: number;
  };
  chart: {
    time: number;
    open: number;
    high: number;
    low: number;
    close: number;
  }[];
  outcomes: {
    horizonBars: number;
    returnPct: number;
    maxUpsidePct: number;
    maxDownsidePct: number;
  }[];
};
type SearchRequest = {
  market: string;
  symbol: string;
  windowBars: number;
  scope: string;
  sector?: string;
  symbols?: string[];
  limit: number;
};
type SearchResult = {
  matches: SimilarityMatch[];
  generatedAt: number;
  query: SearchRequest & { end: number };
  candidates: number;
};
type Options = {
  windows: { bars: number; stride: number }[];
  sectors: string[];
};
const windows = [
  { bars: 12, label: "1 час" },
  { bars: 36, label: "3 часа" },
  { bars: 72, label: "6 часов" },
  { bars: 144, label: "12 часов" },
  { bars: 288, label: "24 часа" },
  { bars: 864, label: "3 дня" },
  { bars: 2016, label: "7 дней" },
];
const control =
  "w-full rounded-lg border border-border bg-background px-3 py-2 text-xs text-foreground outline-none transition-colors focus:border-ring disabled:opacity-50";
const date = new Intl.DateTimeFormat("ru-RU", {
  day: "2-digit",
  month: "2-digit",
  year: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  timeZone: "UTC",
});
function percent(value: number) {
  return `${value > 0 ? "+" : ""}${value.toFixed(2)}%`;
}

function MatchCard({ match, candles, timeframe, loading }: {
  match: SimilarityMatch;
  candles?: SimilarityCandle[];
  timeframe: SimilarityTimeframe;
  loading: boolean;
}) {
  const [details, setDetails] = useState(false);
  const labels = {
    price: "Цена",
    volume: "Объём",
    volatility: "Волатильность",
    candles: "Свечи",
    statistics: "Статистика",
  };
  return (
    <article
      className="min-w-0 overflow-hidden rounded-xl border border-border bg-background"
      data-similarity-match
    >
      <div className="flex items-center justify-between gap-2 px-3 pt-3 text-xs">
        <span className="font-semibold text-foreground">{match.symbol}</span>
        <button
          type="button"
          onClick={() => setDetails(!details)}
          aria-expanded={details}
          className="rounded px-1 text-primary outline-none hover:text-primary focus-visible:ring-2 focus-visible:ring-ring"
          title="Почему ситуации похожи"
        >
          Сходство {match.score.toFixed(1)}%
        </button>
      </div>
      <div className="px-3 pb-2 pt-1 text-[10px] text-muted-foreground">
        {date.format(match.start)} — {date.format(match.end)} UTC
      </div>
      <SimilarityChart match={match} candles={candles} timeframe={timeframe} loading={loading} />
      <div className="grid grid-cols-3 gap-2 border-t border-border px-3 py-2.5">
        {[12, 72, 288].map((horizon) => {
          const outcome = match.outcomes.find((o) => o.horizonBars === horizon);
          return (
            <div key={horizon} className="text-[10px] text-muted-foreground">
              Через {horizon / 12}ч
              <div
                className={`mt-1 text-xs font-semibold ${outcome ? (outcome.returnPct >= 0 ? "text-primary" : "text-destructive") : "text-muted-foreground"}`}
                title={
                  outcome
                    ? `Максимум ${percent(outcome.maxUpsidePct)}, минимум ${percent(outcome.maxDownsidePct)}`
                    : "Исход ещё не рассчитан или свечи неполны"
                }
              >
                {outcome ? percent(outcome.returnPct) : "—"}
              </div>
            </div>
          );
        })}
      </div>
      {details && (
        <div className="space-y-2 border-t border-border px-3 py-3 text-[11px] text-muted-foreground">
          {Object.entries(labels).map(([key, label]) => (
            <div key={key} className="flex justify-between gap-2">
              <span>{label}</span>
              <span className="text-foreground">
                {match.breakdown[key as keyof typeof match.breakdown].toFixed(
                  1,
                )}
                %
              </span>
            </div>
          ))}
          {match.outcomes.map((o) => (
            <div key={o.horizonBars} className="border-t border-border pt-2">
              Через {o.horizonBars / 12}ч: {percent(o.returnPct)}
              <div className="mt-1">
                Макс. {percent(o.maxUpsidePct)} · мин.{" "}
                {percent(o.maxDownsidePct)}
              </div>
            </div>
          ))}
        </div>
      )}
    </article>
  );
}

type SearchJob = {
  id: string;
  status: "queued" | "running" | "completed" | "failed" | "cancelled";
  progress: number;
  phase: string;
  request: SearchRequest;
  result?: SearchResult;
  error?: string;
};
type SavedSearch = { id: string; request: SearchRequest };
function readSearch(key: string): SavedSearch | null {
  try {
    const raw = localStorage.getItem(key);
    if (!raw || raw.length > 16384) return null;
    const value = JSON.parse(raw) as SavedSearch;
    return /^[a-f0-9-]{36}$/.test(value.id) && value.request?.symbol
      ? value
      : null;
  } catch {
    return null;
  }
}
function SimilarityContent({
  symbol,
  exchange,
}: {
  symbol: string;
  exchange: PrimaryExchange;
}) {
  const { user } = useAuth();
  const storageKey = `similarity:requested:v1:${user?.id ?? "guest"}:${symbol}`;
  const [saved] = useState(() => readSearch(storageKey));
  const [market, setMarket] = useState(saved?.request.market ?? "linear");
  const [windowBars, setWindowBars] = useState(
    saved?.request.windowBars ?? 288,
  );
  const [universe, setUniverse] = useState(() => universeFromRequest(saved?.request));
  const reduceMotion = useReducedMotion();
  const [custom, setCustom] = useState(
    saved?.request.symbols?.join(", ") ?? "",
  );
  const [submitted, setSubmitted] = useState<SearchRequest | null>(
    saved?.request ?? null,
  );
  const [jobId, setJobId] = useState<string | null>(saved?.id ?? null);
  const [timeframe, setTimeframe] = useState(readTimeframe);
  const [visible, setVisible] = useState(false);
  const root = useRef<HTMLElement>(null);
  useEffect(() => {
    const node = root.current;
    if (!node) return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setVisible(true);
          observer.disconnect();
        }
      },
      { rootMargin: "100px" },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  const options = useQuery({
    queryKey: ["similarity-options", user?.id],
    queryFn: ({ signal }) => http<Options>("/similarity/options", { signal }),
    enabled: visible,
    staleTime: 60_000,
    retry: false,
  });
  const job = useQuery({
    queryKey: ["similarity-job", user?.id, jobId],
    queryFn: ({ signal }) =>
      http<SearchJob>(`/similarity/jobs/${jobId}`, { signal }),
    enabled: visible && Boolean(jobId),
    retry: false,
    refetchInterval: (q) =>
      q.state.data && ["queued", "running"].includes(q.state.data.status)
        ? 5000
        : false,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: false,
  });
  const create = useMutation({
    mutationFn: (request: SearchRequest) =>
      http<{ jobId: string }>("/similarity/search", {
        method: "POST",
        body: JSON.stringify(request),
      }),
    onSuccess: (result, request) => {
      setSubmitted(request);
      setJobId(result.jobId);
      try {
        localStorage.setItem(
          storageKey,
          JSON.stringify({ id: result.jobId, request }),
        );
      } catch {
        /* search works without storage */
      }
    },
  });
  const cancel = useMutation({
    mutationFn: () =>
      http<{ ok: boolean }>(`/similarity/jobs/${jobId}`, { method: "DELETE" }),
    onSuccess: () => {
      void job.refetch();
    },
  });
  const busy =
    create.isPending ||
    Boolean(
      jobId &&
      (job.isPending ||
        job.data?.status === "queued" ||
        job.data?.status === "running"),
    );
  const activeWindows = options.data?.windows ?? [];
  const selectedWindow =
    activeWindows.length && !activeWindows.some((w) => w.bars === windowBars)
      ? activeWindows[0].bars
      : windowBars;
  const searchOptions = universeOptions(options.data?.sectors ?? []);
  const unavailableUniverse = !searchOptions.some((option) => option.value === universe);
  if (unavailableUniverse) searchOptions.push({ value: universe, label: "Направление недоступно" });
  const draft: SearchRequest = {
    market,
    symbol,
    windowBars: selectedWindow,
    ...universeRequest(universe, custom),
    limit: 6,
  };
  const changed =
    submitted !== null && JSON.stringify(submitted) !== JSON.stringify(draft);
  const invalid =
    !options.data ||
    unavailableUniverse ||
    (draft.scope === "custom" &&
      (!draft.symbols?.length || draft.symbols.length > 100));
  const result = job.data?.status === "completed" ? job.data.result : undefined;
  const error =
    create.error?.message ??
    cancel.error?.message ??
    job.error?.message ??
    (job.data?.status === "failed" ? job.data.error : undefined);
  const charts = useQuery({
    queryKey: ["similarity-charts", user?.id, jobId],
    queryFn: ({ signal }) => http<{ charts: { id: string; candles: SimilarityCandle[] }[] }>(
      `/similarity/jobs/${jobId}/charts`, { signal },
    ),
    enabled: visible && Boolean(result?.matches.length) && !changed && !busy && !error,
    staleTime: Infinity,
    gcTime: 30 * 60_000,
    retry: false,
    refetchOnWindowFocus: false,
  });
  return (
    <section
      ref={root}
      aria-label="Похожие ситуации"
      className="mt-4 rounded-2xl border border-border bg-secondary p-4"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-xs uppercase tracking-wide text-muted-foreground">
          Похожие ситуации
        </h3>
        <span className="text-[10px] text-muted-foreground">Bybit · {symbol}</span>
      </div>
      {exchange !== "bybit" && (
        <p className="mt-2 text-xs text-muted-foreground">
          Для сравнения используется история этой монеты на Bybit.
        </p>
      )}
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (!invalid && !busy) {
            cancel.reset();
            create.mutate(draft);
          }
        }}
      >
        <fieldset
          disabled={busy}
          className="mt-3 flex min-w-0 flex-wrap items-end gap-3"
        >
          <div className="min-w-28 flex-1 text-[10px] text-muted-foreground">
            <span>Рынок</span>
            <AnimatedSelect
              ariaLabel="Рынок похожих ситуаций"
              className={`${control} mt-1`}
              value={market}
              onChange={setMarket}
              disabled={busy}
              options={[{ value: "linear", label: "Futures" }, { value: "spot", label: "Spot" }]}
            />
          </div>
          <div className="min-w-28 flex-1 text-[10px] text-muted-foreground">
            <span>Окно</span>
            <AnimatedSelect
              ariaLabel="Окно похожих ситуаций"
              className={`${control} mt-1`}
              value={String(selectedWindow)}
              onChange={(value) => setWindowBars(Number(value))}
              disabled={busy}
              options={(activeWindows.length ? activeWindows : windows).map((w) => ({ value: String(w.bars), label: windows.find((v) => v.bars === w.bars)?.label ?? `${w.bars * 5} мин` }))}
            />
          </div>
          <div className="min-w-36 flex-1 text-[10px] text-muted-foreground">
            <span>Искать среди</span>
            <AnimatedSelect
              ariaLabel="Область поиска"
              className={`${control} mt-1`}
              value={universe}
              onChange={setUniverse}
              disabled={busy || !options.data}
              options={searchOptions}
            />
          </div>
          <AnimatePresence initial={false}>
          {universe === "custom" && (
            <motion.label
              key="custom-symbols"
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: reduceMotion ? 0 : 0.2 }}
              className="min-w-48 flex-1 overflow-hidden text-[10px] text-muted-foreground"
            >
              До 100 USDT-пар
              <input
                aria-label="Монеты для сравнения"
                placeholder="ETHUSDT, SOLUSDT"
                className={`${control} mt-1`}
                value={custom}
                onChange={(e) => setCustom(e.target.value)}
              />
            </motion.label>
          )}
          </AnimatePresence>
          <button
            type="submit"
            disabled={invalid || busy}
            className="rounded-lg border border-border-strong bg-accent px-4 py-2 text-xs font-semibold text-primary transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
          >
            {busy ? "Поиск…" : "Найти похожие"}
          </button>
        </fieldset>
        <AnimatePresence mode="wait" initial={false}>
          {(universe === "majors" || universe === "alts") && (
            <motion.p
              key={universe}
              initial={{ opacity: 0, y: 4 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -4 }}
              transition={{ duration: reduceMotion ? 0 : 0.18 }}
              className="mt-2 text-[11px] text-muted-foreground"
            >
              {universe === "majors" ? "Основные монеты: BTC, ETH, SOL, BNB и XRP. Поиск по доступным парам выбранного рынка." : "Альткоины — монеты кроме BTC, без стейблкоинов. Мемкоины и DeFi тоже входят в эту группу."}
            </motion.p>
          )}
        </AnimatePresence>
      </form>
      <div aria-live="polite" className="mt-3 text-xs leading-5 text-muted-foreground">
        {options.isError ? (
          <span role="alert">
            {options.error.message}{" "}
            <button
              type="button"
              onClick={() => options.refetch()}
              className="text-primary"
            >
              Повторить
            </button>
          </span>
        ) : error ? (
          <span role="alert">
            {error}{" "}
            {job.isError && (
              <button
                type="button"
                onClick={() => job.refetch()}
                className="text-primary"
              >
                Повторить
              </button>
            )}
          </span>
        ) : busy ? (
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <span>
                {job.data?.phase ?? "Создаём поиск…"} ·{" "}
                {job.data?.progress ?? 0}%
              </span>
              {jobId && (
                <button
                  type="button"
                  onClick={() => cancel.mutate()}
                  disabled={cancel.isPending}
                  className="rounded-lg border border-border px-3 py-1 text-muted-foreground transition-colors hover:bg-card disabled:opacity-50"
                >
                  {cancel.isPending ? "Отменяем…" : "Отменить поиск"}
                </button>
              )}
            </div>
            <div
              role="progressbar"
              aria-label="Подготовка поиска"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={job.data?.progress ?? 0}
              className="h-1.5 overflow-hidden rounded-full bg-secondary"
            >
              <div
                className="h-full rounded-full bg-emerald-400/80 transition-[width] duration-500 motion-reduce:transition-none"
                style={{ width: `${job.data?.progress ?? 0}%` }}
              />
            </div>
            <p>
              Первый поиск готовит выбранную историю. Можно закрыть окно и
              вернуться позже.
            </p>
          </div>
        ) : job.data?.status === "cancelled" ? (
          "Поиск отменён. Можно изменить настройки и запустить снова."
        ) : changed ? (
          "Настройки изменены. Запустите поиск, чтобы обновить графики."
        ) : result ? (
          `Окно сравнения заканчивается ${date.format(result.query.end)} UTC. Отметка «Далее» — начало последующего движения.`
        ) : (
          "Расчёт начинается только по кнопке «Найти похожие». Первый поиск среди всех монет может занять больше времени."
        )}
      </div>
      {result && !changed && !busy && !error && (
        <>
          {result.matches.length ? (
            <>
            <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
              <span className="text-xs text-muted-foreground">Таймфрейм всех графиков</span>
              <div role="group" aria-label="Таймфрейм всех графиков" className="inline-flex items-center gap-1 rounded-xl border border-border bg-background p-1">
                {similarityTimeframes.map((item) => (
                  <button
                    key={item.seconds}
                    type="button"
                    aria-pressed={timeframe === item.seconds}
                    onClick={() => {
                      setTimeframe(item.seconds);
                      try { localStorage.setItem(timeframeKey, String(item.seconds)); } catch { /* optional preference */ }
                    }}
                    className={`rounded-lg border px-3 py-1.5 text-xs font-semibold transition-colors duration-200 motion-reduce:transition-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${timeframe === item.seconds ? "border-border-strong bg-accent text-primary" : "border-transparent text-muted-foreground hover:bg-card hover:text-foreground"}`}
                  >{item.label}</button>
                ))}
              </div>
            </div>
            {charts.isError && (
              <p role="alert" className="mt-3 text-xs text-muted-foreground">
                {charts.error.message}{" "}
                <button type="button" disabled={charts.isFetching} onClick={() => charts.refetch()} className="text-primary disabled:opacity-50">Повторить загрузку свечей</button>
              </p>
            )}
            <div
              className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3"
              data-similarity-grid
            >
              {result.matches.slice(0, 6).map((match) => (
                <MatchCard key={match.id} match={match} candles={charts.data?.charts.find((chart) => chart.id === match.id)?.candles} timeframe={timeframe} loading={charts.isFetching} />
              ))}
            </div>
            </>
          ) : (
            <p className="mt-3 text-xs text-muted-foreground">
              Совпадений в доступной истории не найдено. Попробуйте другое окно
              или область поиска.
            </p>
          )}
          <p className="mt-3 text-[10px] leading-4 text-muted-foreground">
            Сходство не является вероятностью прогноза. Прочерк означает, что
            будущий период ещё не завершён или свечи неполны.
          </p>
        </>
      )}
    </section>
  );
}
export function SimilarityPanel(props: {
  symbol: string;
  exchange: PrimaryExchange;
}) {
  const { user } = useAuth();
  return (
    <SimilarityContent
      key={`${user?.id}:${props.exchange}:${props.symbol}`}
      {...props}
    />
  );
}
