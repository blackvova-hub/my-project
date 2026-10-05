import { useState, type Dispatch } from "react";
import { floatingPnl, price, utc, type Action, type ReplayState } from "./model";

export function PaperTradePanel({ state, dispatch }: { state: ReplayState; dispatch: Dispatch<Action> }) {
  const [amount, setAmount] = useState("1000");
  const current = state.candles[state.cursor];
  const position = state.position;
  const floating = floatingPnl(position, current?.close ?? 0);
  const enabled = state.mode === "paused" || state.mode === "playing";

  return (
    <aside className="rp-trade-panel" aria-label="Сделка в Replay">
      <div className="rp-trade-panel-heading">
        <h2>{position ? "Открытая позиция" : "Сделка"}</h2>
        <span>Виртуальная торговля</span>
      </div>

      {position ? (
        <div className="rp-position-card rp-open-position" role="status">
          <div className="rp-position-side">
            <strong className={position.side === "long" ? "rp-positive" : "rp-negative"}>
              {position.side === "long" ? "LONG ↑" : "SHORT ↓"}
            </strong>
            <span>{price(position.notional)} USDT</span>
          </div>
          <dl>
            <div><dt>Цена входа</dt><dd>{price(position.entry)}</dd></div>
            <div><dt>Текущая цена</dt><dd>{current ? price(current.close) : "—"}</dd></div>
            <div><dt>PnL позиции</dt><dd className={floating < 0 ? "rp-negative" : "rp-positive"}>{floating >= 0 ? "+" : ""}{floating.toFixed(2)} USDT</dd></div>
          </dl>
          <button className="rp-close-position" disabled={!enabled} onClick={() => dispatch({ type: "close" })}>
            Закрыть позицию
          </button>
        </div>
      ) : (
        <div className="rp-open-trade">
          <label className="rp-size-label" htmlFor="rp-position-size">Размер позиции</label>
          <div className="rp-size-input">
            <input id="rp-position-size" type="number" min="1" step="any" value={amount} onChange={(event) => setAmount(event.target.value)} />
            <span>USDT</span>
          </div>
          <div className="rp-size-presets" aria-label="Быстрый выбор размера позиции">
            {[100, 250, 500, 1000].map((value) => <button key={value} type="button" onClick={() => setAmount(String(value))}>{value}</button>)}
          </div>
          <div className="rp-trade-actions">
            <button className="rp-long" disabled={!enabled} onClick={() => dispatch({ type: "open", side: "long", amount: Number(amount) })}>
              Открыть Long <span aria-hidden="true">↑</span>
            </button>
            <button className="rp-short" disabled={!enabled} onClick={() => dispatch({ type: "open", side: "short", amount: Number(amount) })}>
              Открыть Short <span aria-hidden="true">↓</span>
            </button>
          </div>
        </div>
      )}

      {state.error ? <p className="rp-error" role="alert">{state.error}</p> : null}
      <div className="rp-trade-summary">
        <div><span>Баланс</span><strong data-testid="paper-balance">{state.balance.toFixed(2)} USDT</strong></div>
        <div><span>PnL сессии</span><strong data-testid="paper-pnl" className={state.balance + floating - 10000 < 0 ? "rp-negative" : "rp-positive"}>{state.balance + floating - 10000 >= 0 ? "+" : ""}{(state.balance + floating - 10000).toFixed(2)} USDT</strong></div>
      </div>
    </aside>
  );
}

export function TradeJournal({ state }: { state: ReplayState }) {
  const [tab, setTab] = useState<"trades" | "stats">("trades");
  const winners = state.trades.filter((trade) => trade.pnl > 0).length;
  const total = state.trades.reduce((sum, trade) => sum + trade.pnl, 0);
  const exportCSV = () => {
    const rows = [
      ["side", "entry_time_utc", "exit_time_utc", "entry", "exit", "quantity", "pnl_usdt"],
      ...state.trades.map((trade) => [trade.side, new Date(trade.time * 1000).toISOString(), new Date(trade.exitTime * 1000).toISOString(), trade.entry, trade.exit, trade.quantity, trade.pnl]),
    ];
    const url = URL.createObjectURL(new Blob([rows.map((row) => row.join(",")).join("\r\n")], { type: "text/csv;charset=utf-8" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = "replay-trades.csv";
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  return (
    <section className="rp-journal-dock" aria-label="Результат сессии">
      <div className="rp-dock-tabs" role="tablist" aria-label="Результат сессии">
        <button role="tab" id="rp-journal-tab" aria-controls="rp-journal-panel" aria-selected={tab === "trades"} onClick={() => setTab("trades")}>Журнал сделок ({state.trades.length})</button>
        <button role="tab" id="rp-stats-tab" aria-controls="rp-stats-panel" aria-selected={tab === "stats"} onClick={() => setTab("stats")}>Статистика</button>
        <button className="rp-export" aria-label="Экспорт CSV" disabled={!state.trades.length} onClick={exportCSV}>CSV</button>
      </div>
      <div className="rp-journal" id="rp-journal-panel" role="tabpanel" aria-labelledby="rp-journal-tab" hidden={tab !== "trades"}>
        {state.trades.length ? (
          <div className="rp-table-scroll">
            <table>
              <thead><tr><th>Вход → выход (UTC)</th><th>Тип</th><th>Цена входа</th><th>Цена выхода</th><th>PnL, USDT</th></tr></thead>
              <tbody>{state.trades.slice(-100).reverse().map((trade, index) => <tr key={state.trades.length - index}>
                <td>{utc(trade.time)} → {utc(trade.exitTime)}</td>
                <td>{trade.side.toUpperCase()}</td>
                <td>{price(trade.entry)}</td>
                <td>{price(trade.exit)}</td>
                <td className={trade.pnl < 0 ? "rp-negative" : "rp-positive"}>{trade.pnl.toFixed(2)}</td>
              </tr>)}</tbody>
            </table>
            {state.trades.length > 100 ? <small>Показаны последние 100 сделок. Полная история — в CSV.</small> : null}
          </div>
        ) : <p className="rp-empty-journal">Закройте позицию, чтобы увидеть здесь результат сделки.</p>}
      </div>
      <div className="rp-session-stats" id="rp-stats-panel" role="tabpanel" aria-labelledby="rp-stats-tab" hidden={tab !== "stats"}>
        <div><span>Сделок</span><strong>{state.trades.length}</strong></div>
        <div><span>Прибыльных</span><strong>{winners}</strong></div>
        <div><span>Результат сделок</span><strong className={total < 0 ? "rp-negative" : "rp-positive"}>{total >= 0 ? "+" : ""}{total.toFixed(2)} USDT</strong></div>
      </div>
    </section>
  );
}
