import { Component, lazy, Suspense, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { backtestApi, csvURL } from "./api";
import { downloadBlob, type Job } from "./model";
import { Icon, Panel } from "./ui";

const EquityChart = lazy(() => import("./EquityChart"));
class ChartBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  render() {
    return this.state.failed ? (
      <div className="bt-chart-loading" role="alert">
        <p>Не удалось загрузить график. Результат теста сохранён.</p>
        <button
          type="button"
          className="bt-button"
          onClick={() => window.location.reload()}
        >
          Обновить страницу
        </button>
      </div>
    ) : (
      this.props.children
    );
  }
}
const money = new Intl.NumberFormat("ru-RU", {
  maximumFractionDigits: 2,
  minimumFractionDigits: 2,
});
const price = new Intl.NumberFormat("ru-RU", { maximumSignificantDigits: 8 });
const date = new Intl.DateTimeFormat("ru-RU", {
  timeZone: "UTC",
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
});
const reasons: Record<string, string> = {
  tp: "TP",
  sl: "SL",
  condition: "Условие",
  end_of_period: "Конец периода",
};
const signed = (n: number) => `${n > 0 ? "+" : ""}${money.format(n)}`;

export function Methodology() {
  return (
    <details className="bt-panel bt-method">
      <summary>
        Как считается результат
        <Icon name="right" size={16} />
      </summary>
      <div>
        <p>
          Условия проверяются на закрытии свечи, сделка исполняется на открытии
          следующей. TP и SL отсчитываются от цены входа с проскальзыванием.
          Если оба уровня задеты в одной свече, первым считается SL; при разрыве
          цены выход происходит по открытию. В конце периода позиции
          закрываются.
        </p>
        <p>
          Капитал общий для всех монет, одновременно открывается не более одной
          позиции на монету. Размер позиции — доля текущего капитала; комиссия
          списывается на входе и выходе. При нехватке средств объём уменьшается,
          при одновременных сигналах приоритет определяется алфавитным порядком
          монет.
        </p>
        <p>
          Futures моделируются без кредитного плеча (1×), без funding,
          ликвидаций, округления лотов и влияния объёма заявки на рынок. История
          — цены сделок Bybit, время UTC. Данные до начала периода используются
          только для прогрева индикаторов. MACD: 12 / 26 / 9.
        </p>
        <p>
          Просадка считается по капиталу на закрытии каждой свечи. Sharpe — по
          дневным доходностям, 365 дней в году и нулевой безрисковой ставке; при
          недостатке данных отображается «—». Profit factor — сумма чистой
          прибыли / сумма чистых убытков; «∞» означает отсутствие убыточных
          сделок.
        </p>
      </div>
    </details>
  );
}

export function BacktestResults({ job, userId }: { job: Job; userId: string }) {
  const [page, setPage] = useState(1),
    [downloading, setDownloading] = useState(false),
    [downloadError, setDownloadError] = useState("");
  const trades = useQuery({
    queryKey: ["backtest", userId, "trades", job.id, page],
    queryFn: ({ signal }) => backtestApi.trades(job.id, page, signal),
    enabled: job.status === "completed",
    staleTime: Infinity,
  });
  if (!job.result) return null;
  const { metrics: m, equity } = job.result;
  const metrics = [
    {
      label: "Доходность",
      value: `${signed(m.profitPct)}%`,
      tone: m.profitPct >= 0 ? "positive" : "negative",
    },
    {
      label: "Макс. просадка",
      value: `${m.maxDrawdownPct ? "−" : ""}${money.format(m.maxDrawdownPct)}%`,
      tone: m.maxDrawdownPct ? "negative" : "",
    },
    { label: "Прибыльных сделок", value: `${money.format(m.winRate)}%` },
    { label: "Сделок", value: m.tradeCount },
    {
      label: "Profit factor",
      value:
        m.profitFactor === null
          ? m.winRate > 0
            ? "∞"
            : "—"
          : money.format(m.profitFactor),
    },
    {
      label: "Sharpe",
      value: m.sharpe === null ? "—" : money.format(m.sharpe),
    },
  ];
  const download = async () => {
    setDownloading(true);
    setDownloadError("");
    try {
      const res = await fetch(csvURL(job.id), { credentials: "include" });
      if (!res.ok)
        throw new Error("Не удалось скачать сделки. Повторите попытку.");
      downloadBlob(await res.blob(), `backtest-${job.id}.csv`);
    } catch (err) {
      setDownloadError(err instanceof Error ? err.message : "Ошибка выгрузки");
    } finally {
      setDownloading(false);
    }
  };
  const end = new Date(
    new Date(job.request.to).getTime() - 1,
  ).toLocaleDateString("ru-RU", { timeZone: "UTC" });
  return (
    <Panel className="bt-results" aria-label="Результаты теста">
      <div className="bt-results-heading">
        <div>
          <h2>Результаты теста</h2>
          <p className="bt-muted">
            Bybit · {job.request.market === "spot" ? "Spot" : "Futures"} ·{" "}
            {job.request.symbols.join(", ")} · {job.request.timeframe}{" "}
            <span className="bt-date-summary">
              {new Date(job.request.from).toLocaleDateString("ru-RU", {
                timeZone: "UTC",
              })}{" "}
              — {end}
            </span>
          </p>
        </div>
        <button
          className="bt-button"
          type="button"
          onClick={() => void download()}
          disabled={downloading}
        >
          <Icon name="download" />
          {downloading ? "Скачиваем…" : "Скачать сделки"}
        </button>
      </div>
      {downloadError ? (
        <p className="bt-error" role="alert">
          {downloadError}
        </p>
      ) : null}
      <dl className="bt-panel bt-metrics">
        {metrics.map((metric) => (
          <div key={metric.label}>
            <dt>{metric.label}</dt>
            <dd className={metric.tone ? `bt-${metric.tone}` : ""}>
              {metric.value}
            </dd>
          </div>
        ))}
      </dl>
      {job.result.signals?.length ? (
        <div className="bt-panel bt-signal-stats">
          <h3>Срабатывания условий входа</h3>
          <p className="bt-caption">
            Число закрытых свечей, на которых совпали правила входа целиком.
            Открытая позиция или нехватка капитала могут уменьшить число сделок.
          </p>
          <div>
            {job.result.signals.map((s) => (
              <p key={s.symbol}>
                <strong>{s.symbol}</strong>
                <span>
                  {s.matchedBars.toLocaleString("ru-RU")} из{" "}
                  {s.evaluatedBars.toLocaleString("ru-RU")} свечей
                </span>
              </p>
            ))}
          </div>
          {job.result.signals.every((s) => s.matchedBars === 0) ? (
            <p>
              За выбранный период условия входа ни разу не совпали. Проверьте
              пороги и связь И / ИЛИ.
            </p>
          ) : null}
        </div>
      ) : null}
      <div className="bt-panel bt-chart-panel">
        <h3>Капитал</h3>
        <div className="bt-capital-value">
          {money.format(m.finalCapital)} <span>USDT</span>
        </div>
        <ChartBoundary>
          <Suspense
            fallback={<div className="bt-chart-loading">Загружаем график…</div>}
          >
            <EquityChart points={equity} />
          </Suspense>
        </ChartBoundary>
        <p className="bt-caption">
          С учётом комиссии и проскальзывания · Комиссии:{" "}
          {money.format(m.totalFees)} USDT
        </p>
      </div>
      <div className="bt-panel bt-trades">
        <h3>Сделки</h3>
        <div className="bt-table-scroll">
          <table>
            <thead>
              <tr>
                {[
                  "Монета",
                  "Направление",
                  "Вход (UTC)",
                  "Выход (UTC)",
                  "Цена входа",
                  "Цена выхода",
                  "PnL",
                  "Причина",
                ].map((h) => (
                  <th key={h}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {trades.isPending ? (
                <tr>
                  <td colSpan={8}>Загружаем сделки…</td>
                </tr>
              ) : trades.isError ? (
                <tr>
                  <td colSpan={8}>
                    <span role="alert">Не удалось загрузить сделки.</span>{" "}
                    <button
                      className="bt-text-button"
                      onClick={() => void trades.refetch()}
                    >
                      Повторить
                    </button>
                  </td>
                </tr>
              ) : trades.data.trades.length ? (
                trades.data.trades.map((trade, i) => (
                  <tr key={`${page}-${i}`}>
                    <td>{trade.symbol}</td>
                    <td
                      className={
                        trade.direction === "long"
                          ? "bt-positive"
                          : "bt-negative"
                      }
                    >
                      {trade.direction === "long" ? "Long" : "Short"}
                    </td>
                    <td>{date.format(trade.entryTime)}</td>
                    <td>{date.format(trade.exitTime)}</td>
                    <td>{price.format(trade.entryPrice)}</td>
                    <td>{price.format(trade.exitPrice)}</td>
                    <td
                      className={trade.pnl >= 0 ? "bt-positive" : "bt-negative"}
                    >
                      {signed(trade.pnl)}
                      <small>{signed(trade.pnlPct)}%</small>
                    </td>
                    <td>{reasons[trade.reason] ?? trade.reason}</td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={8} className="bt-no-trades">
                    Условия входа не сработали. За выбранный период сделок нет.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <div className="bt-pagination">
          <span>
            {m.tradeCount
              ? `${(page - 1) * 20 + 1}–${Math.min(page * 20, m.tradeCount)} из ${m.tradeCount}`
              : "0 сделок"}
          </span>
          <div>
            <button
              type="button"
              className="bt-icon-button"
              aria-label="Предыдущая страница сделок"
              disabled={page === 1}
              onClick={() => setPage((p) => p - 1)}
            >
              <Icon name="left" />
            </button>
            <button
              type="button"
              className="bt-icon-button"
              aria-label="Следующая страница сделок"
              disabled={page * 20 >= m.tradeCount}
              onClick={() => setPage((p) => p + 1)}
            >
              <Icon name="right" />
            </button>
          </div>
        </div>
      </div>
      <Methodology />
    </Panel>
  );
}
