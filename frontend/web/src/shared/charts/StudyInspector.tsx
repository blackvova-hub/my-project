import { useState } from "react";
import { X, Plus } from "lucide-react";
import { useDialogFocus } from "./useDialogFocus";
import {
  defaultIndicatorPeriod,
  type IndicatorId,
} from "../market/indicatorTypes";
import { PRICE_SOURCE_STUDIES } from "../market/indicatorEngine";
import {
  normalizeStudySettings,
  type StudySettings,
  type PriceSource,
} from "./studySettings";

export function StudyInspector({
  studyKey,
  id,
  title,
  value,
  components,
  onApply,
  onClose,
  onDuplicate,
}: {
  studyKey: string;
  id?: IndicatorId;
  title: string;
  value: StudySettings;
  components: { key: string; title: string; color: string; kind: string }[];
  onApply: (value: StudySettings) => void;
  onClose: () => void;
  onDuplicate?: () => void;
}) {
  const [draft, setDraft] = useState(value),
    [levels, setLevels] = useState(
      (value.levels ?? (id === "rsi" ? [30, 70] : [])).join(", "),
    );
  useDialogFocus(true, onClose);
  const patch = (change: Partial<StudySettings>) =>
    setDraft((v) => ({ ...v, ...change }));
  const period = id ? defaultIndicatorPeriod(id) : undefined;
  return (
    <div className="ct-modal-backdrop" onClick={onClose}>
      <section
        className="ct-dialog ct-study-inspector"
        role="dialog"
        aria-modal="true"
        aria-label={`Настройки ${title}`}
        onClick={(e) => e.stopPropagation()}
      >
        <header>
          <strong>{title}</strong>
          <button aria-label="Закрыть настройки индикатора" onClick={onClose}>
            <X size={16} />
          </button>
        </header>
        <div className="ct-inspector-fields">
          {!id && (
            <label>
              Представление
              <select
                aria-label="Представление рыночного ряда"
                value={draft.representation ?? components[0]?.kind ?? "line"}
                onChange={(e) =>
                  patch({
                    representation: e.target
                      .value as StudySettings["representation"],
                  })
                }
              >
                <option value="line">Линия</option>
                <option value="histogram">Столбцы</option>
                {components.some((c) => c.kind === "candles") && (
                  <option value="candles">Свечи наблюдений</option>
                )}
              </select>
            </label>
          )}
          {period !== undefined && (
            <label>
              Период
              <input
                aria-label={`Период ${studyKey}`}
                type="number"
                min="2"
                max="400"
                value={draft.period ?? period}
                onChange={(e) => patch({ period: Number(e.target.value) })}
              />
            </label>
          )}
          {id && PRICE_SOURCE_STUDIES.has(id) && (
            <label>
              Источник цены
              <select
                value={draft.source ?? "close"}
                onChange={(e) =>
                  patch({ source: e.target.value as PriceSource })
                }
              >
                {["close", "open", "high", "low", "hl2", "hlc3", "ohlc4"].map(
                  (s) => (
                    <option key={s}>{s}</option>
                  ),
                )}
              </select>
            </label>
          )}
          {id === "macd" &&
            (
              [
                ["fast", "Быстрый период", 12],
                ["slow", "Медленный период", 26],
                ["signal", "Сигнальный период", 9],
              ] as const
            ).map(([key, label, fallback]) => (
              <label key={key}>
                {label}
                <input
                  type="number"
                  min="2"
                  max="400"
                  value={draft.parameters?.[key] ?? fallback}
                  onChange={(e) =>
                    patch({
                      parameters: {
                        ...draft.parameters,
                        [key]: Number(e.target.value),
                      },
                    })
                  }
                />
              </label>
            ))}
          {id === "bollinger" && (
            <label>
              Отклонение
              <input
                type="number"
                min="0.1"
                max="10"
                step="0.1"
                value={draft.parameters?.deviation ?? 2}
                onChange={(e) =>
                  patch({
                    parameters: {
                      ...draft.parameters,
                      deviation: Number(e.target.value),
                    },
                  })
                }
              />
            </label>
          )}
        </div>
        <div className="ct-style-components">
          <h3>Оформление компонентов</h3>
          {components.map((c) => {
            const style = draft.components?.[c.key] ?? {};
            const update = (change: Partial<StudySettings>) =>
              patch({
                components: {
                  ...draft.components,
                  [c.key]: { ...style, ...change },
                },
              });
            return (
              <div className="ct-component-row" key={c.key}>
                <label>
                  <input
                    type="checkbox"
                    checked={!style.hidden}
                    onChange={(e) => update({ hidden: !e.target.checked })}
                  />
                  {c.title}
                </label>
                <input
                  type="color"
                  aria-label={`Цвет ${c.title}`}
                  value={style.color ?? draft.color ?? c.color}
                  onChange={(e) => update({ color: e.target.value })}
                />
                {c.kind === "line" && (
                  <>
                    <select
                      aria-label={`Толщина ${c.title}`}
                      value={style.width ?? draft.width ?? 1}
                      onChange={(e) =>
                        update({
                          width: Number(e.target.value) as 1 | 2 | 3 | 4,
                        })
                      }
                    >
                      {[1, 2, 3, 4].map((n) => (
                        <option value={n} key={n}>
                          {n} px
                        </option>
                      ))}
                    </select>
                    <select
                      aria-label={`Штрих ${c.title}`}
                      value={style.dash ?? draft.dash ?? 0}
                      onChange={(e) =>
                        update({ dash: Number(e.target.value) as 0 | 1 | 2 })
                      }
                    >
                      <option value="0">Линия</option>
                      <option value="1">Точки</option>
                      <option value="2">Штрихи</option>
                    </select>
                  </>
                )}
              </div>
            );
          })}
        </div>
        {id && !["volume_profile", "visible_volume_profile"].includes(id) && (
          <label>
            Горизонтальные уровни
            <input
              aria-label="Уровни индикатора"
              placeholder="30, 70"
              value={levels}
              onChange={(e) => setLevels(e.target.value)}
            />
          </label>
        )}
        {id === "rsi" && (
          <label>
            <input
              type="checkbox"
              checked={draft.fill !== false}
              onChange={(e) => patch({ fill: e.target.checked })}
            />
            Заливка RSI
          </label>
        )}
        <footer>
          {onDuplicate && (
            <button onClick={onDuplicate}>
              <Plus size={14} />
              Ещё экземпляр
            </button>
          )}
          <button
            onClick={() => {
              if (
                id === "macd" &&
                (draft.parameters?.fast ?? 12) >= (draft.parameters?.slow ?? 26)
              )
                return;
              onApply(
                normalizeStudySettings({
                  ...draft,
                  levels: levels.trim()
                    ? levels
                        .split(",")
                        .map((s) => Number(s.trim()))
                        .filter(Number.isFinite)
                    : [],
                }),
              );
              onClose();
            }}
          >
            Применить
          </button>
        </footer>
        {id === "macd" &&
          (draft.parameters?.fast ?? 12) >= (draft.parameters?.slow ?? 26) && (
            <p role="alert">Быстрый период должен быть меньше медленного.</p>
          )}
      </section>
    </div>
  );
}
