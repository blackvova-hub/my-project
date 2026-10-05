import { useEffect, useRef, useState } from "react";
import { http } from "../../shared/api/http";
import type { AdminScannerRule, AdminUser } from "./types";
import { formatDate, planLabel, subscriptionState } from "./utils";

type Tab = "overview" | "subscription" | "details";
type Props = { user: AdminUser; onClose: () => void; onChanged: () => Promise<void>; onDeleted: () => void };

const tabs: { id: Tab; label: string }[] = [
  { id: "overview", label: "Обзор" },
  { id: "subscription", label: "Подписка" },
  { id: "details", label: "Данные аккаунта" },
];

function NavIcon({ tab }: { tab: Tab | "delete" }) {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {tab === "overview" && <><circle cx="12" cy="8" r="3.5"/><path d="M5 20v-2a7 7 0 0 1 14 0v2"/></>}
    {tab === "subscription" && <><rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 10h18M7 15h5"/></>}
    {tab === "details" && <><circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/></>}
    {tab === "delete" && <><path d="M4 7h16M9 7V4h6v3m3 0-1 13H7L6 7M10 11v5m4-5v5"/></>}
  </svg>;
}

function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="admin-detail"><dt>{label}</dt><dd>{children}</dd></div>;
}

function yesNo(value: boolean | undefined) {
  return value ? "Да" : "Нет";
}

export function UserDrawer({ user, onClose, onChanged, onDeleted }: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const [tab, setTab] = useState<Tab>("overview");
  const [plan, setPlan] = useState(user.plan);
  const [days, setDays] = useState("30");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [rules, setRules] = useState<AdminScannerRule[] | null>(null);
  const [rulesError, setRulesError] = useState(false);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (!dialog.open) dialog.showModal();
  }, []);

  useEffect(() => {
    if (tab !== "details" || rules !== null || rulesError) return;
    let active = true;
    void http<{ rules: AdminScannerRule[] }>(`/api/admin/users/${user.id}/scanner-rules`)
      .then((response) => { if (active) setRules(response.rules ?? []); })
      .catch(() => { if (active) setRulesError(true); });
    return () => { active = false; };
  }, [tab, rules, rulesError, user.id]);

  async function run(label: string, request: () => Promise<unknown>, success: string) {
    if (busy) return;
    setBusy(label);
    setError("");
    setMessage("");
    try {
      await request();
      await onChanged();
      setMessage(success);
    } catch {
      setError("Не удалось выполнить действие. Попробуйте ещё раз.");
    } finally {
      setBusy("");
    }
  }

  function savePlan() {
    if (plan === user.plan) return;
    void run("plan", () => http(`/api/admin/users/${user.id}/plan`, {
      method: "PATCH", body: JSON.stringify({ plan }),
    }), "Тариф обновлён.");
  }

  function changeSubscription(action: "add_days" | "subtract_days" | "freeze" | "unfreeze") {
    const count = Number(days);
    if ((action === "add_days" || action === "subtract_days") && (!Number.isSafeInteger(count) || count <= 0)) {
      setError("Введите целое число дней больше нуля.");
      return;
    }
    void run(action, () => http(`/api/admin/users/${user.id}/subscription`, {
      method: "PATCH", body: JSON.stringify({ action, days: Number.isFinite(count) ? count : 0 }),
    }), "Подписка обновлена.");
  }

  async function deleteUser() {
    if (busy || !window.confirm(`Удалить аккаунт ${user.email}? Это действие необратимо.`)) return;
    setBusy("delete");
    setError("");
    try {
      await http(`/api/admin/users/${user.id}`, { method: "DELETE" });
      onDeleted();
    } catch {
      setError("Не удалось удалить аккаунт. Попробуйте ещё раз.");
      setBusy("");
    }
  }

  const status = subscriptionState(user);

  return <dialog className="admin-drawer" ref={dialogRef} onClose={onClose} onClick={(event) => {
    if (event.target === dialogRef.current) dialogRef.current?.close();
  }} aria-labelledby="admin-drawer-title">
    <div className="admin-drawer-inner">
      <header className="admin-drawer-header">
        <div>
          <h2 id="admin-drawer-title">Пользователь #{user.public_id || user.num_id}</h2>
          <strong>{user.displayName || "Без имени"}</strong>
          <span>{user.email}</span>
          <span className={`admin-status admin-status-${status.tone}`}><i />{status.label}</span>
          {user.isAdmin && <small>Права администратора</small>}
        </div>
        <button className="admin-close" type="button" onClick={() => dialogRef.current?.close()} aria-label="Закрыть карточку пользователя">×</button>
      </header>

      <nav className="admin-drawer-nav" aria-label="Разделы пользователя">
        {tabs.map((item) => <button type="button" key={item.id} className={tab === item.id ? "active" : ""} aria-current={tab === item.id ? "page" : undefined} onClick={() => { setTab(item.id); setError(""); setMessage(""); }}><NavIcon tab={item.id} />{item.label}</button>)}
        <button className="admin-drawer-delete-link" type="button" disabled={Boolean(busy)} onClick={() => void deleteUser()}><NavIcon tab="delete" />Удалить пользователя</button>
      </nav>

      <div className="admin-drawer-content">
        {message && <div className="admin-success" role="status">{message}</div>}
        {error && <div className="admin-alert" role="alert">{error}</div>}

        {tab === "overview" && <section>
          <h3>Обзор</h3>
          <dl className="admin-details">
            <Detail label="ID">{user.public_id || user.num_id}</Detail>
            <Detail label="Имя">{user.displayName || "—"}</Detail>
            <Detail label="Email">{user.email}</Detail>
            <Detail label="Тариф">{planLabel(user.plan)}</Detail>
            <Detail label="Подписка"><span className={`admin-status admin-status-${status.tone}`}><i />{status.label}</span></Detail>
            <Detail label="Создан">{formatDate(user.createdAt)}</Detail>
            <Detail label="Последний вход">{formatDate(user.lastSeenAt)}</Detail>
          </dl>
        </section>}

        {tab === "subscription" && <section>
          <h3>Подписка</h3>
          <dl className="admin-details">
            <Detail label="Состояние"><span className={`admin-status admin-status-${status.tone}`}><i />{status.label}</span></Detail>
            <Detail label="Осталось">{user.subscriptionDaysRemaining ?? 0} дн.</Detail>
            <Detail label="Действует до">{formatDate(user.subscriptionExpiresAt)}</Detail>
          </dl>
          <div className="admin-form-block">
            <label htmlFor="admin-plan">Тариф</label>
            <div className="admin-inline-form"><select id="admin-plan" value={plan} onChange={(event) => setPlan(event.target.value)}><option value="free">Free</option><option value="standard">Standard</option><option value="pro">Pro</option></select><button className="admin-button admin-button-primary" type="button" onClick={savePlan} disabled={Boolean(busy) || plan === user.plan}>{busy === "plan" ? "Сохранение..." : "Сохранить"}</button></div>
          </div>
          <div className="admin-form-block">
            <label htmlFor="admin-days">Изменить срок, дней</label>
            <input id="admin-days" type="number" min="1" step="1" inputMode="numeric" value={days} onChange={(event) => setDays(event.target.value)} />
            <div className="admin-action-row"><button className="admin-button admin-button-outline" type="button" disabled={Boolean(busy)} onClick={() => changeSubscription("add_days")}>Добавить дни</button><button className="admin-button admin-button-outline" type="button" disabled={Boolean(busy)} onClick={() => changeSubscription("subtract_days")}>Убрать дни</button></div>
          </div>
          <div className="admin-form-block admin-last-action">
            <p>{user.subscriptionFrozenAt ? "Подписка сейчас заморожена." : "При заморозке оставшиеся дни сохраняются."}</p>
            <button className="admin-button admin-button-outline" type="button" disabled={Boolean(busy) || user.plan === "free"} onClick={() => changeSubscription(user.subscriptionFrozenAt ? "unfreeze" : "freeze")}>{user.subscriptionFrozenAt ? "Разморозить подписку" : "Заморозить подписку"}</button>
          </div>
        </section>}

        {tab === "details" && <section>
          <h3>Данные аккаунта</h3>
          <dl className="admin-details">
            <Detail label="IP">{user.lastIp || "—"}</Detail>
            <Detail label="2FA">{yesNo(user.twoFAEnabled)}</Detail>
            <Detail label="Email подтверждён">{yesNo(user.emailVerified)}</Detail>
            <Detail label="Telegram">{yesNo(user.telegramLinked)}</Detail>
            <Detail label="Администратор">{yesNo(user.isAdmin)}</Detail>
            <Detail label="Сканеры">{user.scannerSlots?.length ? user.scannerSlots.join(", ").replaceAll("SLOT_", "") : "—"}</Detail>
            <Detail label="Обновлён">{formatDate(user.updatedAt)}</Detail>
          </dl>
          <div className="admin-rules">
            <h4>Правила сканеров</h4>
            {rulesError ? <p>Не удалось загрузить правила.</p> : rules === null ? <p>Загрузка...</p> : rules.length === 0 ? <p>Правил нет.</p> : <ul>{rules.map((rule) => <li key={rule.id}><strong>{rule.scanner_slot.replace("SLOT_", "Слот ")}</strong><span>{rule.symbol} · {rule.window_minutes} мин · {rule.enabled ? "Включено" : "Выключено"}</span></li>)}</ul>}
          </div>
          <div className="admin-danger-zone"><h4>Удаление аккаунта</h4><p>Пользователь и связанные данные будут удалены без возможности восстановления.</p><button className="admin-button admin-button-danger" type="button" disabled={Boolean(busy)} onClick={() => void deleteUser()}>{busy === "delete" ? "Удаление..." : "Удалить пользователя"}</button></div>
        </section>}
      </div>
    </div>
  </dialog>;
}
