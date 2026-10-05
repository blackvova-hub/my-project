import { useEffect, useRef, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { useAuth } from "../../../shared/auth/AuthContext";
import { backtestApi } from "./api";
import { StrategyForm, ExecutionForm } from "./StrategyForm";
import { MarketForm } from "./MarketForm";
import { BacktestResults, Methodology } from "./BacktestResults";
import { Icon, Panel } from "./ui";
import { backtestLimits, planLimitError } from "./limits";
import { LimitDialog } from "./LimitDialog";
import {
  isActive,
  readDraft,
  saveDraft,
  statuses,
  validateRequest,
  normalizeRequest,
  defaultRequest,
  type BacktestRequest,
  type Job,
} from "./model";
import "./backtest.css";

export default function BacktestPage() {
  const { user } = useAuth();
  return <BacktestWorkspace key={user?.id} userId={user?.id ?? "guest"} plan={user?.plan} />;
}

function BacktestWorkspace({ userId, plan }: { userId: string; plan?: string }) {
  const limits = backtestLimits(plan);
  const [request, setRequest] = useState(() => readDraft(userId));
  const [limitMessage, setLimitMessage] = useState("");
  const [historyOpen, setHistoryOpen] = useState(false),
    [error, setError] = useState("");
  const [params, setParams] = useSearchParams();
  const queryClient = useQueryClient();
  const requestKey = useRef<string | null>(null);
  const submitted = useRef<BacktestRequest | null>(null);
  const resultAnchor = useRef<HTMLDivElement>(null);
  const errorAnchor = useRef<HTMLDivElement>(null);
  const scrollRequested = useRef(false);
  const reduceMotion = useReducedMotion();
  const historyKey = ["backtest", userId, "jobs"] as const;
  const history = useQuery({
    queryKey: historyKey,
    queryFn: ({ signal }) => backtestApi.list(signal),
    refetchInterval: (q) => (q.state.data?.jobs.some(isActive) ? 5000 : false),
  });
  const activeJob = history.data?.jobs.find(isActive);
  const selected = params.get("job");
  const selectedId =
    selected && /^[a-f0-9-]{36}$/i.test(selected) ? selected : undefined;
  const jobId = selectedId ?? activeJob?.id;
  const job = useQuery({
    queryKey: ["backtest", userId, "job", jobId],
    queryFn: ({ signal }) => backtestApi.get(jobId!, signal),
    enabled: !!jobId,
    refetchInterval: (q) =>
      q.state.status === "error"
        ? false
        : !q.state.data || isActive(q.state.data)
          ? 3000
          : false,
  });
  const chooseJob = (id: string) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.set("job", id);
        return next;
      },
      { replace: true },
    );
  const create = useMutation({
    mutationFn: ({ payload, key }: { payload: BacktestRequest; key: string }) =>
      backtestApi.create(payload, key),
    onSuccess: ({ jobId }) => {
      scrollRequested.current = true;
      chooseJob(jobId);
      requestKey.current = null;
      submitted.current = null;
      void queryClient.invalidateQueries({ queryKey: historyKey });
    },
    onError: (e) => {
      setError(e.message);
      void queryClient.invalidateQueries({ queryKey: historyKey });
    },
  });
  const cancel = useMutation({
    mutationFn: backtestApi.cancel,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["backtest", userId] });
    },
    onError: (e) => setError(e.message),
  });
  const busy = create.isPending || !!activeJob || isActive(job.data);
  useEffect(() => {
    if (error)
      errorAnchor.current?.scrollIntoView({
        behavior: reduceMotion ? "instant" : "smooth",
        block: "center",
      });
  }, [error, reduceMotion]);
  useEffect(() => {
    if (!scrollRequested.current || !jobId) return;
    scrollRequested.current = false;
    resultAnchor.current?.scrollIntoView({
      behavior: reduceMotion ? "instant" : "smooth",
      block: "start",
    });
  }, [jobId, historyOpen, reduceMotion]);
  useEffect(() => {
    const timer = setTimeout(() => saveDraft(userId, request), 400);
    return () => clearTimeout(timer);
  }, [userId, request]);
  const jobStatus = job.data?.status;
  useEffect(() => {
    if (jobStatus && jobStatus !== "queued" && jobStatus !== "running")
      void queryClient.invalidateQueries({
        queryKey: ["backtest", userId, "jobs"],
      });
  }, [jobStatus, jobId, queryClient, userId]);
  const update = (next: BacktestRequest) => {
    setRequest(normalizeRequest(next));
    setError("");
    requestKey.current = null;
    submitted.current = null;
  };
  const run = (event: FormEvent) => {
    event.preventDefault();
    if (busy) return;
    const planError = planLimitError(request, limits);
    if (planError) {
      setLimitMessage(planError);
      return;
    }
    const invalid = validateRequest(request, plan);
    if (invalid) {
      setError(invalid);
      return;
    }
    setError("");
    requestKey.current ??= crypto.randomUUID();
    submitted.current ??= request;
    create.mutate({ payload: submitted.current, key: requestKey.current });
  };
  const selectHistory = (j: Job) => {
    scrollRequested.current = true;
    chooseJob(j.id);
    if (!activeJob) update(j.request);
    setHistoryOpen(false);
  };
  return (
    <div className="bt-page" data-reveal-skip>
      <motion.div
        className="bt-page-heading"
        initial={reduceMotion ? false : { opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.4 }}
      >
        <div>
          <h1>Тест стратегии</h1>
          <p>Проверьте правила на истории рынка — до первой реальной сделки.</p>
        </div>
        <div className="bt-heading-actions">
          <button
            type="button"
            className="bt-button"
            disabled={busy}
            onClick={() => update(defaultRequest())}
          >
            Сбросить настройки
          </button>
          <button
            className="bt-button bt-history-toggle"
            type="button"
            aria-expanded={historyOpen}
            aria-controls="bt-history"
            onClick={() => setHistoryOpen(!historyOpen)}
          >
            <Icon name="history" />
            История тестов
          </button>
        </div>
      </motion.div>
      <AnimatePresence initial={false}>
        {historyOpen ? (
          <motion.div
            key="history"
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: reduceMotion ? 0 : 0.3 }}
            style={{ overflow: "hidden" }}
          >
            <Panel id="bt-history" className="bt-panel bt-history">
              <h2>История тестов</h2>
              {history.isPending ? (
                <p>Загружаем историю…</p>
              ) : history.isError ? (
                <p role="alert">{history.error.message}</p>
              ) : history.data.jobs.length ? (
                <div className="bt-history-list">
                  {history.data.jobs.map((j) => (
                    <button
                      type="button"
                      key={j.id}
                      onClick={() => selectHistory(j)}
                      aria-current={jobId === j.id ? "true" : undefined}
                    >
                      <span>
                        {j.request.symbols.join(", ")}
                        <small>
                          {j.request.market === "spot" ? "Spot" : "Futures"} ·{" "}
                          {j.request.timeframe} ·{" "}
                          {new Date(j.createdAt).toLocaleString("ru-RU")}
                        </small>
                      </span>
                      <span>{statuses[j.status]}</span>
                    </button>
                  ))}
                </div>
              ) : (
                <p className="bt-muted">
                  Здесь сохраняются последние 30 тестов.
                </p>
              )}
            </Panel>
          </motion.div>
        ) : null}
      </AnimatePresence>
      <form onSubmit={run}>
        <fieldset disabled={busy} className="bt-form-fields">
          <MarketForm value={request} onChange={update} userId={userId} limits={limits} onLimit={setLimitMessage} />
          <div className="bt-config-grid">
            <StrategyForm
              request={request}
              onChange={update}
              onError={setError}
              limits={limits}
              onLimit={setLimitMessage}
            />
            <ExecutionForm
              value={request.strategy}
              onChange={(strategy) => update({ ...request, strategy })}
              busy={busy}
              disabled={history.isPending || history.isError}
            />
          </div>
        </fieldset>
      </form>
      <LimitDialog message={limitMessage} onClose={() => setLimitMessage("")} />
      <div ref={resultAnchor} className="bt-result-anchor" />
      {error ? (
        <div ref={errorAnchor} className="bt-error" role="alert">
          {error}
        </div>
      ) : null}
      {history.isError ? (
        <div className="bt-error" role="alert">
          {history.error.message}{" "}
          <button
            type="button"
            className="bt-text-button"
            onClick={() => void history.refetch()}
          >
            Повторить подключение
          </button>
        </div>
      ) : null}
      {job.isError ? (
        <div className="bt-error" role="alert">
          {job.error.message}{" "}
          <button
            type="button"
            className="bt-text-button"
            onClick={() => void job.refetch()}
          >
            Повторить
          </button>
        </div>
      ) : null}
      {activeJob && activeJob.id !== jobId ? (
        <div className="bt-panel bt-active-notice">
          У вас выполняется тест.{" "}
          <button
            className="bt-text-button"
            type="button"
            onClick={() => chooseJob(activeJob.id)}
          >
            Показать прогресс
          </button>
          <button
            className="bt-text-button"
            type="button"
            disabled={cancel.isPending}
            onClick={() => cancel.mutate(activeJob.id)}
          >
            Отменить тест
          </button>
        </div>
      ) : null}
      {isActive(job.data) ? (
        <Panel className="bt-panel bt-progress" aria-live="polite">
          <div>
            <div>
              <h2>
                {job.data?.status === "queued"
                  ? "Тест в очереди"
                  : job.data?.phase === "calculating"
                    ? "Рассчитываем стратегию"
                    : job.data?.phase === "loading_trades"
                      ? "Загружаем историю сделок Bybit"
                      : job.data?.phase === "loading_oi"
                        ? "Загружаем историю открытого интереса"
                        : "Подготавливаем историю Bybit"}
              </h2>
              <p className="bt-muted">
                {job.data?.status === "queued"
                  ? "Расчёт начнётся автоматически, когда подойдёт очередь."
                  : job.data?.phase === "calculating"
                    ? "Проверяем условия и рассчитываем сделки."
                    : job.data?.phase === "loading_trades"
                      ? "Считаем объёмы покупок и продаж из дневных архивов. Первая загрузка длинного периода может занять заметное время; готовые дни сохраняются для повторных тестов."
                      : "Сохранённая история используется повторно. Загружаем только недостающие данные."}
              </p>
            </div>
            <button
              className="bt-button"
              type="button"
              disabled={cancel.isPending}
              onClick={() => cancel.mutate(jobId!)}
            >
              {cancel.isPending ? "Отменяем…" : "Отменить тест"}
            </button>
          </div>
          <div className="bt-progress-line">
            <progress
              max={100}
              value={job.data?.progress ?? 0}
              aria-label="Прогресс теста"
            />
            <span>{job.data?.progress ?? 0}%</span>
          </div>
          <p className="bt-caption">
            Можно закрыть страницу — тест сохранится в истории.
          </p>
        </Panel>
      ) : null}
      {job.data?.status === "failed" || job.data?.status === "cancelled" ? (
        <Panel className="bt-panel bt-terminal" aria-live="polite">
          <h2>
            {job.data.status === "failed"
              ? "Не удалось завершить тест"
              : "Тест отменён"}
          </h2>
          <p>
            {job.data.error ||
              "Настройки сохранены. Можно изменить стратегию и запустить новый тест."}
          </p>
          <button
            className="bt-button"
            type="button"
            disabled={!!activeJob}
            onClick={() => update(job.data!.request)}
          >
            Восстановить настройки теста
          </button>
        </Panel>
      ) : null}
      {job.data?.status === "completed" && job.data.result ? (
        <BacktestResults key={job.data.id} job={job.data} userId={userId} />
      ) : !jobId ? (
        <>
          <Panel className="bt-panel bt-empty">
            <Icon name="chart" size={43} />
            <div>
              <h2>Результат появится здесь</h2>
              <p>Настройте стратегию и запустите тест.</p>
            </div>
          </Panel>
          <Methodology />
        </>
      ) : null}
    </div>
  );
}
