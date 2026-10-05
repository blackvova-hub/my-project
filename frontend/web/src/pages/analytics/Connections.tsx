import { useState } from "react";
import type { Connection, Dataset } from "./types";
import { http } from "../../shared/api/http";
import { isUSD, money, sum, timestamp } from "./model";
import { Empty, Icon, Modal, Panel, Rows, Select } from "./ui";
import { displayLabel, errorMessage, localizeMessage } from "./locale";

function ConnectionForm({
  onClose,
  onConnected,
}: {
  onClose: () => void;
  onConnected: () => void;
}) {
  const [exchange, setExchange] = useState("bybit"),
    [name, setName] = useState("Основной счёт"),
    [key, setKey] = useState(""),
    [secret, setSecret] = useState(""),
    [readOnly, setReadOnly] = useState(false),
    [saving, setSaving] = useState(false),
    [error, setError] = useState("");
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError("");
    try {
      await http("/analytics/connections", {
        method: "POST",
        body: JSON.stringify({ exchange, name, key, secret, readOnly }),
      });
      setKey("");
      setSecret("");
      onConnected();
      onClose();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }
  return (
    <Modal
      title="Подключить биржу"
      onClose={() => {
        if (!saving) onClose();
      }}
    >
      <form className="an-connection-form" onSubmit={submit}>
        <fieldset>
          <legend> Биржа </legend>
          {["bybit", "binance"].map((value) => (
            <label className="an-radio" key={value}>
              <input
                type="radio"
                checked={exchange === value}
                onChange={() => setExchange(value)}
                name="exchange"
                value={value}
              />
              {value === "bybit" ? "Bybit" : "Binance"}
            </label>
          ))}
        </fieldset>
        <label>
          {" "}
          Название счёта{" "}
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={80}
            required
            autoComplete="off"
          />
        </label>
        <label>
          {" "}
          Тип счёта{" "}
          <input
            readOnly
            value={
              exchange === "bybit"
                ? "Единый счёт · линейные контракты USDT"
                : "Фьючерсы USD-M · контракты USDT"
            }
          />
        </label>
        <label>
          {" "}
          API-ключ{" "}
          <input
            aria-label="API-ключ"
            type="password"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            minLength={8}
            maxLength={256}
            required
            autoComplete="off"
            spellCheck={false}
          />
        </label>
        <label>
          {" "}
          Секретный ключ API{" "}
          <input
            aria-label="Секретный ключ API"
            type="password"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            minLength={8}
            maxLength={512}
            required
            autoComplete="new-password"
            spellCheck={false}
          />
        </label>
        <label className="an-checkbox">
          <input
            type="checkbox"
            checked={readOnly}
            onChange={(e) => setReadOnly(e.target.checked)}
            required
          />{" "}
          Я создал API-ключ с доступом только для чтения{" "}
        </label>
        {error ? (
          <div className="an-error" role="alert">
            {error}
          </div>
        ) : null}
        <button className="an-button an-primary" disabled={saving || !readOnly}>
          {saving ? "Проверяем права доступа…" : "Проверить и подключить"}
        </button>
        <p className="an-footnote">
          {" "}
          Ключи шифруются на сервере. Права на торговлю, переводы и вывод
          средств не допускаются.{" "}
          {exchange === "bybit"
            ? "Первая загрузка охватывает доступную историю за два года."
            : "Первая загрузка охватывает последние 90 дней."}{" "}
        </p>
        <p className="an-footnote">
          {exchange === "bybit"
            ? "Сделки восстанавливаются по линейным контрактам USDT. Активы единого кошелька и журнал операций загружаются отдельно."
            : "Подключение охватывает фьючерсы USD-M. Спотовые счета и контракты с обеспечением в криптовалюте не включены."}
        </p>
      </form>
    </Modal>
  );
}
export function Connections({
  data,
  demo,
  onRefresh,
  onExitDemo,
  onFormClosed,
  connectInitially = false,
}: {
  data: Dataset;
  demo: boolean;
  onRefresh: () => void;
  onExitDemo: () => void;
  onFormClosed: () => void;
  connectInitially?: boolean;
}) {
  const [form, setForm] = useState(connectInitially),
    [disconnect, setDisconnect] = useState<Connection | null>(null),
    [working, setWorking] = useState(""),
    [error, setError] = useState("");
  async function sync(c: Connection, fullHistory = false) {
    setWorking(c.id);
    setError("");
    try {
      await http(`/analytics/connections/${c.id}/sync`, {
        method: "POST",
        body: JSON.stringify({ fullHistory }),
      });
      onRefresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setWorking("");
    }
  }
  async function remove() {
    if (!disconnect) return;
    setWorking(disconnect.id);
    setError("");
    try {
      await http(`/analytics/connections/${disconnect.id}`, {
        method: "DELETE",
      });
      setDisconnect(null);
      onRefresh();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setWorking("");
    }
  }
  function openForm() {
    if (demo) {
      onExitDemo();
    }
    setForm(true);
  }
  return (
    <>
      <div className="an-connections-action">
        <button className="an-button an-primary" onClick={openForm}>
          <Icon name="plus" /> Подключить биржу{" "}
        </button>
      </div>
      {error ? (
        <div className="an-error" role="alert">
          {error}
        </div>
      ) : null}
      <Panel>
        {data.connections.length ? (
          <div className="an-connection-list">
            {data.connections.map((c) => (
              <article className="an-connection" key={c.id}>
                <strong className={`an-exchange-logo ${c.exchange}`}>
                  {c.exchange === "bybit" ? (
                    <>
                      BYB<span>I</span>T
                    </>
                  ) : (
                    "BINANCE"
                  )}
                </strong>
                <div>
                  <h3>{displayLabel(c.name)}</h3>
                  <small>
                    {c.accountType === "unified"
                      ? "Единый счёт · контракты USDT"
                      : "Фьючерсы USD-M"}
                  </small>
                </div>
                <span className="an-connection-access">
                  <Icon name="check" size={14} /> Только чтение{" "}
                </span>
                <div>
                  <span
                    className={`an-sync-state ${c.status === "error" ? "an-negative" : ""}`}
                  >
                    <i />
                    {c.status === "ready"
                      ? "Синхронизировано"
                      : c.status === "queued"
                        ? "В очереди"
                        : c.status === "syncing"
                          ? "Синхронизация…"
                          : "Требуется проверка"}
                  </span>
                  <small>
                    {c.lastSync
                      ? `${timestamp(new Date(c.lastSync).getTime())} UTC`
                      : "Ещё не синхронизировано"}
                  </small>
                </div>
                <div className="an-connection-actions">
                  {c.exchange === "bybit" ? (
                    <button
                      className="an-button"
                      disabled={
                        demo ||
                        working === c.id ||
                        c.status === "syncing" ||
                        c.status === "queued"
                      }
                      onClick={() => void sync(c, true)}
                      title="Повторно загрузить доступную историю за два года, сохранив ключи и пометки"
                    >
                      История за 2 года
                    </button>
                  ) : null}
                  <button
                    className="an-button"
                    disabled={
                      demo ||
                      working === c.id ||
                      c.status === "syncing" ||
                      c.status === "queued"
                    }
                    onClick={() => void sync(c)}
                  >
                    <Icon name="sync" size={15} /> Обновить{" "}
                  </button>
                  <button
                    className="an-icon-button"
                    aria-label={`Отключить ${displayLabel(c.name)}`}
                    disabled={demo || working === c.id}
                    onClick={() => setDisconnect(c)}
                  >
                    <Icon name="close" size={16} />
                  </button>
                </div>
                {c.error ? (
                  <p className="an-connection-error" role="alert">
                    {localizeMessage(c.error)}
                  </p>
                ) : null}
              </article>
            ))}
          </div>
        ) : (
          <Empty title="Подключите первую биржу">
            {" "}
            Соберите сделки, расходы и позиции в одном кабинете.{" "}
          </Empty>
        )}
      </Panel>
      <Panel title="Полнота данных и сверка">
        <p className="an-muted">
          {" "}
          Доступная глубина истории зависит от биржи. Отсутствующие данные не
          заменяются оценками.{" "}
        </p>
        <div className="an-table-scroll">
          <table>
            <thead>
              <tr>
                <th> Счёт </th>
                <th> История запрошена с </th>
                <th> Загружено по </th>
                <th> Сверка снимков </th>
              </tr>
            </thead>
            <tbody>
              {data.connections.map((c) => {
                const snapshots = data.snapshots
                  .filter((s) => s.connectionId === c.id)
                  .sort((a, b) => a.at - b.at);
                const first = snapshots.at(-2),
                  last = snapshots.at(-1);
                const delta = first && last ? last.wallet - first.wallet : null;
                const movements =
                  first && last
                    ? sum(
                        data.ledger.filter(
                          (l) =>
                            l.connectionId === c.id &&
                            l.at > first.at &&
                            l.at <= last.at &&
                            isUSD(l),
                        ),
                        (l) => l.amount,
                      )
                    : null;
                const residual =
                  delta != null && movements != null ? delta - movements : null;
                return (
                  <tr key={c.id}>
                    <td>{displayLabel(c.name)}</td>
                    <td>
                      {new Date(c.coverageFrom).toLocaleDateString("ru-RU", {
                        timeZone: "UTC",
                      })}
                    </td>
                    <td>
                      {c.syncedThrough
                        ? timestamp(new Date(c.syncedThrough).getTime())
                        : "Ожидает загрузки"}
                    </td>
                    <td>
                      {residual == null ? (
                        "Ожидаются два снимка"
                      ) : (
                        <span
                          className={
                            Math.abs(residual) < 0.01
                              ? "an-positive"
                              : "an-negative"
                          }
                        >
                          {Math.abs(residual) < 0.01
                            ? "Баланс совпадает"
                            : "Есть расхождение"}{" "}
                          · {money(residual, true)}
                        </span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        <p className="an-footnote">
          {" "}
          Снимки баланса сверяются с загруженными операциями. Переоценка валют и
          задержки биржи могут давать расхождение — оно всегда показано.
          Стейблкоины учитываются по номинальному курсу к доллару 1:1.{" "}
        </p>
        {data.connections.flatMap((c) =>
          c.warnings.map((warning, i) => (
            <p className="an-notice" key={c.id + i}>
              {displayLabel(c.name)}: {localizeMessage(warning)}
            </p>
          )),
        )}
      </Panel>
      <Panel title="Что входит в подключение">
        <Rows
          items={[
            { label: "Автоматическое обновление", value: "Каждые 15 минут" },
            {
              label: "Исходные события и операции",
              value: "Без повторов, с исходными данными",
            },
            {
              label: "Первая загрузка истории",
              value: "Bybit — до 2 лет; Binance — 90 дней",
            },
            {
              label: "История капитала счёта",
              value: "С первой синхронизации",
            },
            {
              label: "История плеча, стопов и целей",
              value: "При наличии исходных данных",
            },
          ]}
        />
      </Panel>
      {form ? (
        <ConnectionForm
          onClose={() => {
            setForm(false);
            onFormClosed();
          }}
          onConnected={onRefresh}
        />
      ) : null}
      {disconnect ? (
        <Modal
          title={`Отключить ${displayLabel(disconnect.name)}?`}
          onClose={() => setDisconnect(null)}
        >
          <p>
            {" "}
            Будут удалены ключи и загруженная история этого подключения.
            Остальные счета останутся доступны.{" "}
          </p>
          <div className="an-two-actions">
            <button className="an-button" onClick={() => setDisconnect(null)}>
              {" "}
              Отмена{" "}
            </button>
            <button
              className="an-button an-danger"
              disabled={Boolean(working)}
              onClick={() => void remove()}
            >
              {working ? "Отключаем…" : "Отключить счёт"}
            </button>
          </div>
        </Modal>
      ) : null}
    </>
  );
}

export function AccountSelector({
  connections,
  value,
  onChange,
}: {
  connections: Connection[];
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <Select
      label="Счёт"
      value={value}
      onChange={onChange}
      options={[
        { value: "", label: "Все счета" },
        ...connections.map((c) => ({
          value: c.id,
          label: `${c.exchange === "bybit" ? "Bybit" : "Binance"} · ${displayLabel(c.name)}`,
        })),
      ]}
    />
  );
}
