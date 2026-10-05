import type { Dispatch } from "react";
import {
  speeds,
  utc,
  type Action,
  type ReplayState,
} from "./model";
import { Icon } from "./Icons";
import { ReplayCalendar } from "./ReplayCalendar";

export function ReplayControls({
  state,
  dispatch,
  onChooseDate,
  firstTime,
  lastTime,
  onReturn,
}: {
  state: ReplayState;
  dispatch: Dispatch<Action>;
  onChooseDate: (time: number) => void;
  firstTime: number;
  lastTime: number;
  onReturn: () => void;
}) {
  const ready = state.mode === "paused" || state.mode === "playing";
  const atEnd = state.cursor >= state.candles.length - 1;
  return (
      <div className="rp-controls" aria-label="Управление Replay">
        <button
          aria-pressed={state.mode === "select"}
          onClick={() =>
            dispatch({
              type: state.mode === "select" ? "cancelSelect" : "select",
            })
          }
        >
          <Icon name="target" />
          Выбрать начало Replay
        </button>
        <ReplayCalendar firstTime={firstTime} lastTime={lastTime} currentTime={state.candles[state.cursor].time} onChoose={onChooseDate} />
        <div className="rp-transport">
          <button
            className="rp-icon"
            aria-label="Предыдущая свеча"
            title={state.position || state.trades.length ? "Назад можно перейти до первой сделки" : "Предыдущая свеча (←)"}
            disabled={state.mode !== "paused" || state.cursor <= 0 || !!state.position || state.trades.length > 0}
            onClick={() => dispatch({ type: "prev" })}
          >
            <Icon name="previous" />
          </button>
          <button
            className="rp-primary rp-icon"
            aria-label={state.mode === "playing" ? "Pause" : "Play"}
            disabled={state.mode === "select" || state.candles.length < 2 || atEnd}
            onClick={() => {
              dispatch({ type: "toggle" });
            }}
          >
            <Icon name={state.mode === "playing" ? "pause" : "play"} />
            <span className="rp-control-text">{state.mode === "playing" ? "Пауза" : "Play"}</span>
          </button>
          <button
            className="rp-icon"
            aria-label="Следующая свеча"
            title="Следующая свеча (→)"
            disabled={state.mode !== "paused" || atEnd}
            onClick={() => dispatch({ type: "next" })}
          >
            <Icon name="next" />
          </button>
          <select
            aria-label="Скорость воспроизведения"
            value={state.speed}
            onChange={(e) =>
              dispatch({ type: "speed", value: Number(e.target.value) })
            }
          >
            {speeds.map((v) => (
              <option key={v} value={v}>
                {v}x
              </option>
            ))}
          </select>
        </div>
        <time data-testid="replay-time">
          {utc(state.candles[state.cursor].time)} UTC
        </time>
        <button
          className="rp-return rp-icon"
          aria-label="К текущей свече"
          title="К текущей свече"
          onClick={onReturn}
        >
          <Icon name="return" />
        </button>
        {state.mode === "select" || (ready && atEnd) ? <span className="rp-control-status" role="status">{state.mode === "select" ? "Нажмите на свечу" : "Конец загруженных свечей"}</span> : null}
      </div>
  );
}
