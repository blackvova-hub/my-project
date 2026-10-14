import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  TimeSeriesChart,
  type ChartLine,
} from "../../../shared/charts/LineChart";
import { Sparkline } from "../../../shared/charts/Sparkline";
import { useMacroSeries, type MacroSeries, type MacroPoint } from "./api";
import { comparisonObservations, macroFetchRange, yearOverYear } from "./model";
import { Empty, External, Panel, Skeleton } from "./ui";
import { category, country, number, observationDate } from "./format";

const RANGES = [
  { id: "1y", label: "1 год" },
  { id: "2y", label: "2 года" },
  { id: "5y", label: "5 лет" },
  { id: "all", label: "Всё" },
];
function isInflation(id: string) {
  return (
    id === "CPIAUCSL" ||
    id === "CPILFESL" ||
    id === "PCEPI" ||
    id === "PCEPILFE"
  );
}
function chartPoints(points: { date: string; value: number | null }[]) {
  return points.map((p) => ({
    time: Date.parse(`${p.date}T00:00:00Z`) / 1000,
    value: p.value,
  }));
}
export function SeriesPreview({
  series,
  onSelect,
}: {
  series: MacroSeries;
  onSelect: () => void;
}) {
  return (
    <button className="nm-series-preview" onClick={onSelect}>
      <div>
        <strong>{series.title}</strong>
        <span className={`nm-series-status nm-series-${series.status}`}>
          {series.status === "stale"
            ? "Задержка"
            : series.status === "unavailable"
              ? "Нет данных"
              : observationDate(series.observationDate)}
        </span>
      </div>
      <div>
        <b>{number(series.latest)}</b>
        <small>{series.unit}</small>
      </div>
      {series.points.length >= 2 ? (
        <Sparkline
          values={series.points.map((p) => p.value)}
          height={38}
          label={`Последние наблюдения ${series.title}`}
        />
      ) : (
        <span className="nm-note">Источник пока не вернул наблюдения</span>
      )}
    </button>
  );
}
export function YieldCurve({
  series,
  onExplain,
}: {
  series: MacroSeries[];
  onExplain: () => void;
}) {
  const ids = [
    { id: "DGS2", label: "2Y", years: 2 },
    { id: "DGS10", label: "10Y", years: 10 },
    { id: "DGS30", label: "30Y", years: 30 },
  ];
  const sources = ids.map((item) => ({
    ...item,
    series: series.find((s) => s.id === item.id),
  }));
  const allAvailable = sources.every((item) => item.series?.points.length);
  const commonDates = allAvailable
    ? sources[0]
        .series!.points.map((p) => p.date)
        .filter((date) =>
          sources.every((item) =>
            item.series?.points.some((p) => p.date === date),
          ),
        )
        .sort()
    : [];
  const date = commonDates.at(-1);
  const values = date
    ? sources.map((item) => ({
        label: item.label,
        years: item.years,
        value: item.series!.points.find((p) => p.date === date)!.value,
      }))
    : [];
  const min = values.length
      ? Math.min(...values.map((p) => p.value)) - 0.25
      : 0,
    max = values.length ? Math.max(...values.map((p) => p.value)) + 0.25 : 1;
  const y = (v: number) => 120 - ((v - min) / (max - min || 1)) * 100;
  const x = (years: number) => 70 + ((years - 2) / 28) * 480;
  return (
    <Panel
      title="Кривая доходности UST"
      action={
        <button className="nm-text-link" onClick={onExplain}>
          Что это значит? →
        </button>
      }
    >
      {!date ? (
        <Empty title="Снимок кривой пока недоступен">
          Нужны доходности 2Y, 10Y и 30Y за одну дату. Несовпадающие наблюдения
          не объединяются.
        </Empty>
      ) : (
        <>
          <div className="nm-yield-chart">
            <svg
              viewBox="0 0 600 160"
              role="img"
              aria-label={`Доходности UST на ${date}: ${values.map((p) => `${p.label} ${p.value}%`).join(", ")}`}
            >
              {[0, 1, 2].map((i) => (
                <g key={i}>
                  <line
                    x1="50"
                    x2="580"
                    y1={20 + i * 50}
                    y2={20 + i * 50}
                    stroke="var(--border)"
                  />
                  <text x="0" y={24 + i * 50}>
                    {number(max - (i * (max - min)) / 2, 1)}%
                  </text>
                </g>
              ))}
              <polyline
                fill="none"
                stroke="var(--primary)"
                strokeWidth="2"
                points={values
                  .map((p) => `${x(p.years)},${y(p.value)}`)
                  .join(" ")}
              />
              {values.map((p) => (
                <g key={p.label}>
                  <circle
                    cx={x(p.years)}
                    cy={y(p.value)}
                    r="4"
                    fill="var(--primary)"
                  >
                    <title>
                      {p.label}: {number(p.value)}%
                    </title>
                  </circle>
                  <text x={x(p.years)} y="151" textAnchor="middle">
                    {p.label} · {number(p.value)}%
                  </text>
                </g>
              ))}
            </svg>
          </div>
          <p className="nm-note">
            Один снимок на {observationDate(date)} · US Treasury через FRED.
            Сроки на оси — годы до погашения.{" "}
            <External url="https://fred.stlouisfed.org/series/DGS10">
              Источник
            </External>
          </p>
        </>
      )}
    </Panel>
  );
}
export function MacroChart({
  id,
  compact = false,
}: {
  id: string;
  compact?: boolean;
}) {
  const [range, setRange] = useState("5y");
  const query = useMacroSeries(id, range);
  const series = query.data?.series;
  const lines = useMemo<ChartLine[]>(
    () => [
      {
        name: series?.title ?? id,
        color: "var(--primary)",
        data: chartPoints(series?.points ?? []),
      },
    ],
    [series, id],
  );
  return (
    <Panel
      title={series?.title ?? "Макропульс"}
      action={
        <div className="nm-segmented nm-segmented-small">
          {RANGES.map((item) => (
            <button
              key={item.id}
              className={range === item.id ? "is-active" : ""}
              aria-pressed={range === item.id}
              onClick={() => setRange(item.id)}
            >
              {item.label}
            </button>
          ))}
        </div>
      }
    >
      {query.isPending ? (
        <Skeleton rows={3} />
      ) : query.isError || !series ? (
        <Empty
          title="Показатель не загрузился"
          retry={() => void query.refetch()}
        />
      ) : series.points.length < 2 ? (
        <Empty title="Исторический ряд недоступен">
          Проверить состояние источника можно в панели качества данных.
        </Empty>
      ) : (
        <>
          <div className="nm-chart-meta">
            <span>{series.unit}</span>
            <b>
              {number(series.latest)}{" "}
              <small>· {observationDate(series.observationDate)}</small>
            </b>
          </div>
          <TimeSeriesChart
            lines={lines}
            height={compact ? 210 : 300}
            label={`История ${series.title}, ${series.unit}`}
            percent={series.unit === "%"}
          />
          <p className="nm-note">
            <External url={series.sourceUrl}>{series.source}</External> ·{" "}
            {series.status === "stale" ? "Данные задержаны. " : ""}Дата точки —
            период наблюдения.
          </p>
        </>
      )}
    </Panel>
  );
}
export function MacroAnalysis({
  catalog,
  loading,
  error,
  onExplain,
  onRetry,
}: {
  catalog: MacroSeries[];
  loading: boolean;
  error: boolean;
  onExplain: () => void;
  onRetry: () => void;
}) {
  const [searchParams, setSearchParams] = useSearchParams();
  const selected = searchParams.get("id") ?? "CPIAUCSL";
  const filter = searchParams.get("category") ?? "",
    nation = searchParams.get("country") ?? "";
  const [range, setRange] = useState("5y"),
    [compareChoice, setCompare] = useState(""),
    [mode, setMode] = useState("level");
  const available = catalog.filter(
    (series) =>
      (!filter || series.category === filter) &&
      (!nation || series.country === nation),
  );
  const id = available.some((series) => series.id === selected)
    ? selected
    : (available[0]?.id ?? "");
  const compare = compareChoice === id ? "" : compareChoice;
  const derived = mode === "yoy" && isInflation(id) && !compare;
  const requestRange = macroFetchRange(range, derived);
  const query = useMacroSeries(id, requestRange, Boolean(id));
  const comparison = useMacroSeries(compare, range, Boolean(compare));
  const series = query.data?.series,
    secondary = comparison.data?.series;
  const chart = useMemo(() => {
    const derive = mode === "yoy" && isInflation(series?.id ?? "") && !compare;
    if (!series) return { lines: [], comparison: null };
    if (compare && secondary) {
      const result = comparisonObservations(series.points, secondary.points);
      return {
        comparison: result,
        lines: result
          ? [
              {
                name: series.title,
                color: "var(--primary)",
                data: chartPoints(result.left),
              },
              {
                name: secondary.title,
                color: "var(--chart-3)",
                data: chartPoints(result.right),
              },
            ]
          : [],
      };
    }
    let points: { date: string; value: number | null }[] = derive
      ? yearOverYear(series.points)
      : series.points;
    if (derive && range !== "all") {
      const cutoff = new Date();
      cutoff.setUTCFullYear(
        cutoff.getUTCFullYear() - Number(range.slice(0, 1)),
      );
      points = points.filter(
        (p) => p.date >= cutoff.toISOString().slice(0, 10),
      );
    }
    return {
      comparison: null,
      lines: [
        {
          name: series.title,
          color: "var(--primary)",
          data: chartPoints(points),
        },
      ],
    };
  }, [series, secondary, compare, mode, range]);
  function choose(value: string) {
    setCompare("");
    const next = new URLSearchParams(searchParams);
    next.set("id", value);
    setSearchParams(next, { replace: true });
  }
  function changeFilter(key: "category" | "country", value: string) {
    setCompare("");
    const next = new URLSearchParams(searchParams);
    next.delete("id");
    if (value) next.set(key, value);
    else next.delete(key);
    setSearchParams(next, { replace: true });
  }
  function exportCSV() {
    if (!series) return;
    const cells = (p: MacroPoint) => `${p.date},${p.value}`;
    const url = URL.createObjectURL(
      new Blob([`date,value\r\n${series.points.map(cells).join("\r\n")}\r\n`], {
        type: "text/csv;charset=utf-8",
      }),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = `${series.id}-${requestRange}.csv`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  if (loading) return <Skeleton rows={6} />;
  if (error)
    return <Empty title="Каталог макроданных не загрузился" retry={onRetry} />;
  if (!catalog.length)
    return <Empty title="Показатели пока недоступны" retry={onRetry} />;
  const latest = derived
    ? chart.lines[0]?.data.filter((p) => p.value !== null).at(-1)?.value
    : series?.latest;
  const unit = derived || compare ? "%" : (series?.unit ?? "");
  return (
    <div>
      <div className="nm-filters nm-macro-controls">
        <label>
          Категория
          <select
            value={filter}
            onChange={(e) => {
              changeFilter("category", e.target.value);
            }}
          >
            <option value="">Все показатели</option>
            {[...new Set(catalog.map((s) => s.category))].map((value) => (
              <option key={value} value={value}>
                {category(value)}
              </option>
            ))}
          </select>
        </label>
        <label>
          Страна
          <select
            value={nation}
            onChange={(e) => {
              changeFilter("country", e.target.value);
            }}
          >
            <option value="">Все страны</option>
            {[...new Set(catalog.map((s) => s.country))].map((value) => (
              <option key={value} value={value}>
                {country(value)}
              </option>
            ))}
          </select>
        </label>
        <label className="nm-indicator-select">
          Показатель
          <select value={id} onChange={(e) => choose(e.target.value)}>
            {available.map((s) => (
              <option key={s.id} value={s.id}>
                {s.title}
              </option>
            ))}
          </select>
        </label>
        <label>
          Сравнить с
          <select value={compare} onChange={(e) => setCompare(e.target.value)}>
            <option value="">Без сравнения</option>
            {catalog
              .filter((s) => s.id !== id)
              .map((s) => (
                <option key={s.id} value={s.id}>
                  {s.title}
                </option>
              ))}
          </select>
        </label>
      </div>
      <div className="nm-toolbar">
        <div className="nm-segmented">
          {RANGES.map((item) => (
            <button
              key={item.id}
              className={range === item.id ? "is-active" : ""}
              aria-pressed={range === item.id}
              onClick={() => setRange(item.id)}
            >
              {item.label}
            </button>
          ))}
        </div>
        {isInflation(id) && !compare ? (
          <div className="nm-segmented">
            <button
              className={mode === "level" ? "is-active" : ""}
              aria-pressed={mode === "level"}
              onClick={() => setMode("level")}
            >
              Индекс
            </button>
            <button
              className={mode === "yoy" ? "is-active" : ""}
              aria-pressed={mode === "yoy"}
              onClick={() => setMode("yoy")}
            >
              Инфляция г/г
            </button>
          </div>
        ) : null}
        <button className="nm-text-link" onClick={onExplain}>
          Справочник показателей →
        </button>
      </div>
      <Panel
        title={
          series?.title ??
          available.find((s) => s.id === id)?.title ??
          "Исторические данные"
        }
        action={
          <button
            className="nm-button"
            title="Исходные наблюдения источника. Нормализация и расчёт инфляции г/г в этот файл не входят."
            disabled={!series?.points.length}
            onClick={exportCSV}
          >
            Экспорт CSV
          </button>
        }
      >
        <div className="nm-macro-chart-layout">
          <div className="nm-macro-chart-main">
            {!available.length ? (
              <Empty title="Показателей по этим фильтрам нет">
                <button
                  className="nm-button"
                  onClick={() => {
                    setCompare("");
                    setSearchParams({}, { replace: true });
                  }}
                >
                  Сбросить фильтры
                </button>
              </Empty>
            ) : query.isPending || (compare && comparison.isPending) ? (
              <Skeleton rows={5} />
            ) : query.isError || (compare && comparison.isError) ? (
              <Empty
                title="История не загрузилась"
                retry={() => {
                  void query.refetch();
                  if (compare) void comparison.refetch();
                }}
              />
            ) : !chart.lines.length ||
              chart.lines.every(
                (line) => line.data.filter((p) => p.value != null).length < 2,
              ) ? (
              <Empty
                title={
                  compare
                    ? "Сопоставление недоступно"
                    : "Недостаточно наблюдений"
                }
              >
                {compare
                  ? "Для сравнения нужны пересекающиеся периоды и положительная база обоих рядов. Нулевые и отрицательные уровни не нормализуются в проценты."
                  : "Источник ещё не вернул исторический ряд."}
              </Empty>
            ) : (
              <>
                <p className="nm-note">
                  {compare
                    ? `Изменение от общей базы ${observationDate(chart.comparison?.date)}; разные единицы приведены к процентам.`
                    : derived
                      ? "Годовое изменение индекса: (текущий / тот же месяц год назад − 1) × 100."
                      : series?.unit}
                </p>
                <TimeSeriesChart
                  lines={chart.lines}
                  height={360}
                  label={`${series?.title}, ${unit}`}
                  percent={derived || Boolean(compare) || series?.unit === "%"}
                />
                <div className="nm-chart-legend">
                  {chart.lines.map((line) => (
                    <span key={line.name}>
                      <i style={{ background: line.color }} />
                      {line.name}
                    </span>
                  ))}
                </div>
              </>
            )}
          </div>
          <aside className="nm-macro-reading">
            <small>
              {compare
                ? "Текущий исходный уровень"
                : derived
                  ? "Инфляция г/г"
                  : "Последнее наблюдение"}
            </small>
            <strong>{number(compare ? series?.latest : latest)}</strong>
            <span>{compare ? series?.unit : unit}</span>
            <p>{observationDate(series?.observationDate)}</p>
            <hr />
            <small>Предыдущий исходный уровень</small>
            <b>
              {number(series?.previous)} {series?.unit}
            </b>
            <small>Частота наблюдений</small>
            <b>
              {(
                {
                  monthly: "Ежемесячно",
                  daily: "По рабочим дням",
                  quarterly: "Ежеквартально",
                  weekly: "Еженедельно",
                  "on change": "При изменении",
                } as Record<string, string>
              )[series?.frequency ?? ""] ?? series?.frequency}
            </b>
            <small>Источник</small>
            {series ? (
              <External url={series.sourceUrl}>{series.source}</External>
            ) : (
              "—"
            )}
            <span className={`nm-series-status nm-series-${series?.status}`}>
              {series?.status === "stale"
                ? "Данные задержаны"
                : series?.status === "unavailable"
                  ? "Нет данных источника"
                  : ""}
            </span>
          </aside>
        </div>
        {chart.comparison && secondary ? (
          <p className="nm-note">
            База {series?.title}: {number(chart.comparison.leftBase.value)} на{" "}
            {observationDate(chart.comparison.leftBase.date)}. База{" "}
            {secondary.title}: {number(chart.comparison.rightBase.value)} на{" "}
            {observationDate(chart.comparison.rightBase.date)}. Используется
            последнее известное наблюдение не позднее общей даты; точки не
            интерполируются.
          </p>
        ) : null}
        <p className="nm-note">
          {series?.description} {series?.revisionNote}
        </p>
      </Panel>
      <div className="nm-dashboard-columns nm-macro-bottom">
        <YieldCurve series={catalog} onExplain={onExplain} />
        <Panel title="Экономический смысл">
          <p>
            {series?.explanation ||
              "Выберите показатель, чтобы увидеть объяснение."}
          </p>
          {series?.scenarios.map((s) => (
            <p className="nm-note" key={s}>
              {s}
            </p>
          ))}
        </Panel>
      </div>
      <div className="nm-series-grid">
        {available.slice(0, 8).map((s) => (
          <SeriesPreview key={s.id} series={s} onSelect={() => choose(s.id)} />
        ))}
      </div>
    </div>
  );
}
