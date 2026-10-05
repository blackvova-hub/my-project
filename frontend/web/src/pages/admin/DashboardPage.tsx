import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { http } from "../../shared/api/http";
import { UserDrawer } from "./UserDrawer";
import type { AdminUser } from "./types";
import { planLabel, subscriptionState } from "./utils";
import "./dashboard.css";

function RefreshIcon() {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M20 11a8 8 0 0 0-14.9-3.9L3 9"/><path d="M3 4v5h5"/><path d="M4 13a8 8 0 0 0 14.9 3.9L21 15"/><path d="M21 20v-5h-5"/></svg>;
}

export default function DashboardPage() {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [plan, setPlan] = useState("all");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const inFlight = useRef(false);
  const loaded = useRef(false);

  const load = useCallback(async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    if (loaded.current) setRefreshing(true);
    else setLoading(true);
    try {
      const response = await http<{ users: AdminUser[] }>("/api/admin/users");
      setUsers(response.users ?? []);
      setError("");
      loaded.current = true;
    } catch (cause) {
      const status = cause && typeof cause === "object" && "status" in cause ? cause.status : null;
      setError(status === 401 || status === 403
        ? "Сессия администратора недоступна. Войдите снова."
        : "Не удалось загрузить пользователей. Проверьте соединение и повторите попытку.");
    } finally {
      inFlight.current = false;
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void load();
    const interval = window.setInterval(() => {
      if (document.visibilityState === "visible") void load();
    }, 60_000);
    return () => window.clearInterval(interval);
  }, [load]);

  const visibleUsers = useMemo(() => {
    const query = search.trim().toLocaleLowerCase();
    return users.filter((user) => {
      if (plan !== "all" && user.plan.toLowerCase() !== plan) return false;
      if (!query) return true;
      return [user.email, user.displayName, String(user.public_id), String(user.num_id)]
        .some((value) => value.toLocaleLowerCase().includes(query));
    });
  }, [users, search, plan]);

  const selectedUser = users.find((user) => user.id === selectedId) ?? null;

  return (
    <div className="admin-page">
      <div className="admin-shell">
        <header className="admin-header">
          <div><h1>Админка</h1><p>Управление пользователями</p></div>
          <button className="admin-button admin-button-outline" type="button" onClick={() => void load()} disabled={refreshing || loading}>
            <RefreshIcon /><span>{refreshing ? "Обновление..." : "Обновить"}</span>
          </button>
        </header>

        {error && <div className="admin-alert" role="alert"><span>{error}</span><button type="button" onClick={() => void load()}>Повторить</button></div>}

        <section className="admin-users" aria-labelledby="admin-users-heading">
          <div className="admin-section-heading"><h2 id="admin-users-heading">Пользователи</h2><span>{!loading ? `${visibleUsers.length} из ${users.length}` : ""}</span></div>
          <div className="admin-filters">
            <label className="admin-search"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true"><circle cx="10.8" cy="10.8" r="6.8"/><path d="m16 16 5 5"/></svg><span className="sr-only">Поиск пользователей</span><input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Поиск по имени, email или ID" /></label>
            <label className="admin-select"><span className="sr-only">Фильтр по тарифу</span><select value={plan} onChange={(event) => setPlan(event.target.value)}><option value="all">Все тарифы</option><option value="free">Free</option><option value="standard">Standard</option><option value="pro">Pro</option></select></label>
          </div>

          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead><tr><th>ID</th><th>Имя и email</th><th>Тариф</th><th>Подписка</th><th><span className="sr-only">Действия</span></th></tr></thead>
              <tbody>
                {loading && !loaded.current ? <tr><td colSpan={5} className="admin-empty">Загрузка пользователей...</td></tr> : null}
                {!loading && !error && users.length === 0 ? <tr><td colSpan={5} className="admin-empty">Пользователей пока нет.</td></tr> : null}
                {!loading && users.length > 0 && visibleUsers.length === 0 ? <tr><td colSpan={5} className="admin-empty">По вашему запросу пользователи не найдены.</td></tr> : null}
                {visibleUsers.map((user) => {
                  const state = subscriptionState(user);
                  return <tr key={user.id} className={selectedId === user.id ? "admin-row-selected" : undefined}>
                    <td data-label="ID" className="admin-id">{user.public_id || user.num_id}</td>
                    <td data-label="Пользователь"><div className="admin-person"><strong>{user.displayName || "Без имени"}</strong><span>{user.email}</span></div></td>
                    <td data-label="Тариф">{planLabel(user.plan)}</td>
                    <td data-label="Подписка"><span className={`admin-status admin-status-${state.tone}`}><i />{state.label}</span></td>
                    <td className="admin-row-action"><button className="admin-button admin-button-outline" type="button" onClick={() => setSelectedId(user.id)} aria-label={`Открыть пользователя ${user.email}`}>Открыть <span aria-hidden="true">↗</span></button></td>
                  </tr>;
                })}
              </tbody>
            </table>
          </div>
        </section>
      </div>
      {selectedUser && <UserDrawer user={selectedUser} onClose={() => setSelectedId(null)} onChanged={load} onDeleted={() => { setSelectedId(null); void load(); }} />}
    </div>
  );
}
