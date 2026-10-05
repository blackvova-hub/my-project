import { useMemo, useState } from "react";
import {
  Link,
  NavLink,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuth } from "../../shared/auth/AuthContext";
import { http } from "../../shared/api/http";
import type { Dataset, Range, TradeFilter } from "./types";
import { csvExport, filterTrades, insights, portfolio, ranges } from "./model";
import { makeDemo } from "./demo";
import { Empty, Icon, Modal, Panel, Select } from "./ui";
import { Overview } from "./Overview";
import { Performance } from "./Performance";
import { Trades } from "./Trades";
import { Costs, Edge, Insights, Risk } from "./Analysis";
import { AccountSelector, Connections } from "./Connections";
import { AnalyticsHeader } from "./AnalyticsHeader";
import { displayLabel, errorMessage, localizeMessage } from "./locale";
import "./analytics.css";

const pages = [
  ["overview", "Обзор", "Весь счёт, каждая сделка и понятный итог."],
  [
    "performance",
    "Результаты",
    "Доходность и качество ваших торговых результатов.",
  ],
  ["trades", "Сделки", "От позиции до каждого исполнения ордера."],
  ["edge", "Эффективность", "Узнайте, какие сделки приносят вам прибыль."],
  ["costs", "Расходы", "Все комиссии и расходы на торговлю."],
  ["risk", "Риски", "Оцените нагрузку на капитал и открытые позиции."],
  [
    "insights",
    "Закономерности",
    "Закономерности вашей торговли с подтверждением в данных.",
  ],
  ["connections", "Подключения", "Ваши биржи и счета в одном месте."],
] as const;

function Profile({ onClose }: { onClose: () => void }) {
  const { user, updateProfile, logout } = useAuth();
  const [name, setName] = useState(user?.displayName ?? ""),
    [busy, setBusy] = useState(false),
    [message, setMessage] = useState("");
  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await updateProfile({ displayName: name });
      setMessage("Профиль сохранён");
    } catch (e) {
      setMessage(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Modal title="Ваш профиль" onClose={onClose}>
      <form className="an-connection-form" onSubmit={save}>
        <label>
          {" "}
          Имя пользователя{" "}
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            maxLength={80}
          />
        </label>
        <label>
          {" "}
          Электронная почта <input readOnly value={user?.email ?? ""} />
        </label>
        <button className="an-button an-primary" disabled={busy}>
          {busy ? "Сохраняем…" : "Сохранить профиль"}
        </button>
        <span role="status">{message}</span>
        {user?.isAdmin ? (
          <Link to="/admin" className="an-button">
            {" "}
            Администрирование <Icon name="arrow" />
          </Link>
        ) : null}
        <button
          type="button"
          className="an-button"
          onClick={() => void logout()}
        >
          <Icon name="logout" /> Выйти{" "}
        </button>
      </form>
    </Modal>
  );
}

export default function AnalyticsPage() {
  const { user } = useAuth();
  const { section = "overview" } = useParams();
  const [search, setSearch] = useSearchParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const demo = search.get("demo") === "1";
  const page = pages.find((p) => p[0] === section) ?? pages[0];
  const [range, setRange] = useState<Range>("ALL"),
    [account, setAccount] = useState(""),
    [filter, setFilter] = useState<TradeFilter>({}),
    [profile, setProfile] = useState(false);
  const [demoData, setDemoData] = useState<Dataset | null>(null);
  const demoSource = useMemo(
    () => demoData ?? (demo ? makeDemo() : null),
    [demo, demoData],
  );
  const query = useQuery({
    queryKey: ["analytics", user?.id],
    queryFn: () => http<Dataset>("/analytics"),
    enabled: !demo,
    refetchInterval: 15000,
    refetchIntervalInBackground: false,
    retry: 1,
  });
  const data = demo ? demoSource : query.data;
  const effectiveAccount = data?.connections.some((c) => c.id === account)
    ? account
    : "";
  const p = useMemo(
    () => (data ? portfolio(data, effectiveAccount, range) : null),
    [data, effectiveAccount, range],
  );
  const patterns = useMemo(
    () => (data && p ? insights(data, p) : []),
    [data, p],
  );
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["analytics", user?.id] });
  };
  function setDemo(value: boolean) {
    setAccount("");
    setFilter({});
    setSearch(value ? { demo: "1" } : {});
  }
  function go(section: string) {
    navigate(`/account/${section}${demo ? "?demo=1" : ""}`);
  }
  function onTrades(f: TradeFilter) {
    setFilter(f);
    go("trades");
  }
  function exportData() {
    if (!p) return;
    const records = filterTrades(p.trades, section === "trades" ? filter : {});
    const url = URL.createObjectURL(
      new Blob([csvExport(records)], { type: "text/csv;charset=utf-8;" }),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = `shortlong-${demo ? "demo-" : ""}trades-${range.toLowerCase()}.csv`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  async function saveNote(id: string, tag: string, strategy: string) {
    if (demo && data) {
      setDemoData({
        ...data,
        trades: data.trades.map((t) =>
          t.id === id ? { ...t, tag, strategy } : t,
        ),
      });
      return;
    }
    await http("/analytics/notes", {
      method: "PUT",
      body: JSON.stringify({ tradeId: id, tag, strategy }),
    });
    refresh();
  }
  return (
    <div className="an-root">
      <AnalyticsHeader onProfile={() => setProfile(true)} />
      <aside className="an-sidebar">
        <nav aria-label="Разделы аналитики">
          {pages.slice(0, 7).map(([id, title]) => (
            <NavLink
              key={id}
              to={`/account/${id}${demo ? "?demo=1" : ""}`}
              className={() => `an-nav-link ${page[0] === id ? "active" : ""}`}
            >
              <Icon name={id} />
              <span>{title}</span>
            </NavLink>
          ))}
        </nav>
        <div className="an-sidebar-bottom">
          <NavLink
            to={`/account/connections${demo ? "?demo=1" : ""}`}
            className={`an-nav-link ${page[0] === "connections" ? "active" : ""}`}
          >
            <Icon name="connections" />
            <span> Подключения </span>
          </NavLink>
          <button className="an-user" onClick={() => setProfile(true)}>
            <span className="an-avatar">
              {(user?.displayName || user?.email || "SL")
                .slice(0, 2)
                .toUpperCase()}
            </span>
            <span>
              <strong>{user?.displayName || "Мой аккаунт"}</strong>
              <small> Личный кабинет </small>
            </span>
            <Icon name="chevron" size={14} />
          </button>
        </div>
      </aside>
      <main className="an-main">
        <header className="an-page-header">
          <div>
            <h1>{page[1]}</h1>
            <p>{page[2]}</p>
          </div>
          <div className="an-header-controls">
            {demo ? (
              <button
                className="an-demo"
                title="Закрыть демо и вернуться к своему счёту"
                onClick={() => setDemo(false)}
              >
                <i /> Демоданные <Icon name="close" size={12} />
              </button>
            ) : null}
            {data && page[0] !== "connections" ? (
              <>
                <AccountSelector
                  connections={data.connections}
                  value={effectiveAccount}
                  onChange={setAccount}
                />
                <Select
                  label="Период"
                  value={range}
                  onChange={(v) => setRange(v as Range)}
                  options={ranges.map((r) => ({
                    value: r,
                    label: displayLabel(r),
                  }))}
                />
                <button className="an-button" onClick={exportData}>
                  <Icon name="export" size={16} />
                  <span> Скачать CSV </span>
                </button>
              </>
            ) : null}
          </div>
        </header>
        {!demo && query.isPending ? (
          <Panel>
            <div className="an-loading" role="status">
              {" "}
              Загружаем вашу аналитику…{" "}
            </div>
          </Panel>
        ) : !demo && query.isError ? (
          <Panel>
            <Empty title="Не удалось загрузить аналитику">
              {errorMessage(query.error)}
            </Empty>
            <button className="an-button an-primary" onClick={refresh}>
              {" "}
              Повторить{" "}
            </button>
          </Panel>
        ) : data && p ? (
          <>
            {!demo &&
            data.connections.some(
              (c) => c.status === "syncing" || c.status === "queued",
            ) ? (
              <div className="an-notice" role="status">
                <Icon name="sync" /> Загружаем историю с биржи. Данные появятся
                по мере готовности каждого счёта.{" "}
              </div>
            ) : null}
            {page[0] === "connections" ? (
              <Connections
                data={data}
                demo={demo}
                onRefresh={refresh}
                onExitDemo={() => {
                  setAccount("");
                  setFilter({});
                  setSearch({ connect: "1" });
                }}
                onFormClosed={() => setSearch({})}
                connectInitially={search.get("connect") === "1"}
              />
            ) : !data.snapshots.length ? (
              <Panel>
                <Empty
                  title="Подключите счёт для анализа торговли"
                  action={
                    <>
                      <button
                        className="an-button an-primary"
                        onClick={() => go("connections")}
                      >
                        <Icon name="plus" /> Подключить биржу{" "}
                      </button>
                      <button
                        className="an-button"
                        onClick={() => setDemo(true)}
                      >
                        {" "}
                        Посмотреть демо <Icon name="arrow" />
                      </button>
                    </>
                  }
                >
                  {" "}
                  Добавьте API-ключ Bybit или Binance с доступом только для
                  чтения, чтобы увидеть сделки, расходы и результат.{" "}
                </Empty>
              </Panel>
            ) : (
              <>
                {page[0] === "overview" ? (
                  <Overview
                    p={p}
                    data={data}
                    range={range}
                    setRange={setRange}
                    patterns={patterns}
                    onTrades={onTrades}
                    onInsights={() => go("insights")}
                  />
                ) : page[0] === "performance" ? (
                  <Performance
                    p={p}
                    range={range}
                    setRange={setRange}
                    now={data.serverTime}
                    onTrades={onTrades}
                  />
                ) : page[0] === "trades" ? (
                  <Trades
                    trades={p.trades}
                    filter={filter}
                    setFilter={setFilter}
                    demo={demo}
                    onSave={saveNote}
                  />
                ) : page[0] === "edge" ? (
                  <Edge p={p} onTrades={onTrades} />
                ) : page[0] === "costs" ? (
                  <Costs p={p} onTrades={onTrades} />
                ) : page[0] === "risk" ? (
                  <Risk p={p} />
                ) : (
                  <Insights
                    patterns={patterns}
                    onTrades={onTrades}
                    demo={demo}
                  />
                )}
              </>
            )}
            {data.warnings.map((w) => (
              <p className="an-notice" key={w}>
                {localizeMessage(w)}
              </p>
            ))}
          </>
        ) : null}
        <footer className="an-footer">
          <span>
            Short&Long <span className="an-footer-divider">/</span> Аналитика
            трейдера{" "}
          </span>
          <span>
            {demo
              ? "Демонстрация · вымышленная история счёта"
              : "Только чтение · ключи хранятся на сервере"}{" "}
            <span className="an-footer-divider">/</span> UTC
          </span>
        </footer>
      </main>
      {profile ? <Profile onClose={() => setProfile(false)} /> : null}
    </div>
  );
}
