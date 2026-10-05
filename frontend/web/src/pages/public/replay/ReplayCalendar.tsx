import { useRef, useState } from "react";
import { Icon } from "./Icons";

const months = ["Январь", "Февраль", "Март", "Апрель", "Май", "Июнь", "Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"];
const weekdays = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];

export function ReplayCalendar({ firstTime, lastTime, currentTime, onChoose }: {
  firstTime: number;
  lastTime: number;
  currentTime: number;
  onChoose: (time: number) => void;
}) {
  const picker = useRef<HTMLDetailsElement>(null);
  const [month, setMonth] = useState(() => {
    const date = new Date(currentTime * 1000);
    return Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), 1);
  });
  const active = new Date(month);
  const firstDay = Date.UTC(active.getUTCFullYear(), active.getUTCMonth(), 1);
  const offset = (active.getUTCDay() + 6) % 7;
  const days = new Date(Date.UTC(active.getUTCFullYear(), active.getUTCMonth() + 1, 0)).getUTCDate();
  const earliest = Math.floor(firstTime / 86400000) * 86400000;
  const latest = Math.floor(lastTime / 86400000) * 86400000;
  const selected = Math.floor(currentTime * 1000 / 86400000) * 86400000;
  const earliestMonth = Date.UTC(new Date(earliest).getUTCFullYear(), new Date(earliest).getUTCMonth(), 1);
  const latestMonth = Date.UTC(new Date(latest).getUTCFullYear(), new Date(latest).getUTCMonth(), 1);
  return (
    <details ref={picker} className="rp-date-picker rp-calendar" onToggle={(event) => {
      if (event.currentTarget.open) {
        const date = new Date(currentTime * 1000);
        setMonth(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), 1));
      }
    }}>
      <summary aria-label="Выбрать дату начала Replay" title="Выбрать дату начала Replay"><Icon name="calendar" /></summary>
      <div className="rp-calendar-popover" role="group" aria-label="Дата начала Replay (UTC)">
        <div className="rp-calendar-heading">
          <strong>{months[active.getUTCMonth()]} {active.getUTCFullYear()}</strong>
          <div>
            <button type="button" aria-label="Предыдущий месяц" disabled={firstDay <= earliestMonth} onClick={() => setMonth(Date.UTC(active.getUTCFullYear(), active.getUTCMonth() - 1, 1))}>‹</button>
            <button type="button" aria-label="Следующий месяц" disabled={firstDay >= latestMonth} onClick={() => setMonth(Date.UTC(active.getUTCFullYear(), active.getUTCMonth() + 1, 1))}>›</button>
          </div>
        </div>
        <div className="rp-calendar-grid">
          {weekdays.map((day) => <span key={day} className="rp-calendar-weekday">{day}</span>)}
          {Array.from({ length: offset }, (_, index) => <span key={`blank-${index}`} />)}
          {Array.from({ length: days }, (_, index) => {
            const day = index + 1;
            const time = Date.UTC(active.getUTCFullYear(), active.getUTCMonth(), day);
            return <button key={day} type="button" aria-label={`${day} ${months[active.getUTCMonth()]} ${active.getUTCFullYear()} UTC`} aria-current={time === selected ? "date" : undefined} disabled={time < earliest || time > latest} onClick={() => { onChoose(time / 1000); picker.current?.removeAttribute("open"); }}>{day}</button>;
          })}
        </div>
        <small>Нажмите на день: загрузим до 2000 свечей до него и до 5000 после. Время — UTC.</small>
      </div>
    </details>
  );
}
