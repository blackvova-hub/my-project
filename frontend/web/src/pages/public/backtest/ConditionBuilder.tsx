import { useId, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { AnimatedSelect } from "../../../shared/ui/AnimatedSelect";
import { type Condition, type IndicatorRule } from "./model";
import { type BacktestLimits, type ConditionUsage, canAdd, limitNotice } from "./limits";
import {
  describeRule,
  hasWindow,
  indicators,
  makeRule,
  ruleError,
  thresholdBounds,
  type RuleOperator,
} from "./rules";
import { Icon } from "./ui";

const operators: { value: RuleOperator; label: string }[] = [
  { value: "gte", label: "≥ Больше или равно" },
  { value: "gt", label: "> Больше" },
  { value: "lte", label: "≤ Меньше или равно" },
  { value: "lt", label: "< Меньше" },
  { value: "crosses_above", label: "Пересекает вверх" },
  { value: "crosses_below", label: "Пересекает вниз" },
];
function SelectField({
  label,
  title,
  value,
  options,
  onChange,
}: {
  label: string;
  title: string;
  value: string;
  options: { value: string; label: string; disabled?: boolean }[];
  onChange: (v: string) => void;
}) {
  return (
    <div className="bt-control">
      <span>{title}</span>
      <AnimatedSelect
        className="bt-select"
        ariaLabel={label}
        value={value}
        options={options}
        onChange={onChange}
      />
    </div>
  );
}
function Numeric({
  label,
  title,
  value,
  onChange,
  min,
  max,
  step = "any",
  unit,
}: {
  label: string;
  title: string;
  value: number | undefined;
  onChange: (v: number) => void;
  min: number;
  max: number;
  step?: number | "any";
  unit?: string;
}) {
  return (
    <label className="bt-control">
      <span>{title}</span>
      <div className="bt-input-wrap">
        <input
          aria-label={label}
          type="number"
          required
          inputMode="decimal"
          value={Number.isFinite(value) ? value : ""}
          min={min}
          max={max}
          step={step}
          onChange={(e) => onChange(e.currentTarget.valueAsNumber)}
        />
        {unit ? <span>{unit}</span> : null}
      </div>
    </label>
  );
}
function RuleFields({
  value,
  onChange,
  label,
  timeframe,
  market,
}: {
  value: Condition;
  onChange: (v: IndicatorRule) => void;
  label: string;
  timeframe: string;
  market: string;
}) {
  const reduced = useReducedMotion();
  const rule = value.kind === "indicator" ? value : null;
  const definition = rule ? indicators[rule.indicator] : null;
  const measure = definition?.measures.find((m) => m.value === rule?.measure);
  const step = timeframe === "4h" ? 4 : 1;
  const [min, max] = rule ? thresholdBounds(rule) : [0, 1e12];
  const problem = rule
    ? (ruleError(rule) ??
      (hasWindow(rule) && rule.windowHours! % step
        ? `Окно должно быть кратно ${step} ч — выбран таймфрейм ${step} ч.`
        : definition?.futures && market !== "linear"
          ? "Открытый интерес доступен только на Futures."
          : null))
    : "Это условие сохранено в прежнем формате. Выберите индикатор и задайте параметры заново.";
  return (
    <div className="bt-rule" aria-label={label}>
      <div className="bt-rule-heading">
        <span>{label}</span>
      </div>
      <SelectField
        title="Индикатор"
        label={`${label}: индикатор`}
        value={rule?.indicator ?? ""}
        options={[
          ...(!rule
            ? [{ value: "", label: "Выберите индикатор", disabled: true }]
            : []),
          ...Object.entries(indicators).map(([key, def]) => ({
            value: key,
            label:
              def.name +
              (def.futures && market === "spot" ? " · только Futures" : ""),
            disabled: def.futures && market === "spot",
          })),
        ]}
        onChange={(indicator) => onChange(makeRule(indicator))}
      />
      <AnimatePresence initial={false} mode="wait">
        {rule && definition && measure ? (
          <motion.div
            key={`${rule.indicator}:${rule.measure}`}
            className="bt-rule-fields"
            initial={reduced ? false : { opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: reduced ? 0 : -4 }}
            transition={{ duration: reduced ? 0 : 0.16 }}
          >
            {definition.measures.length > 1 || rule.measure === "change_pct" ? (
              <div className="bt-rule-options">
                {definition.measures.length > 1 ? (
                  <SelectField
                    title="Что проверяем"
                    label={`${label}: расчёт`}
                    value={rule.measure}
                    options={definition.measures.map((m) => ({
                      value: m.value,
                      label: m.label,
                    }))}
                    onChange={(measure) => {
                      const next = makeRule(rule.indicator, measure);
                      onChange({
                        ...next,
                        ...(next.period ? { period: rule.period } : {}),
                        ...(hasWindow(next) && rule.windowHours
                          ? { windowHours: rule.windowHours }
                          : {}),
                      });
                    }}
                  />
                ) : null}
                {rule.measure === "change_pct" ? (
                  <SelectField
                    title="Направление изменения"
                    label={`${label}: направление изменения`}
                    value={rule.direction!}
                    options={[
                      { value: "up", label: "Рост" },
                      { value: "down", label: "Снижение" },
                      { value: "both", label: "В любую сторону" },
                    ]}
                    onChange={(direction) =>
                      onChange({
                        ...rule,
                        direction: direction as IndicatorRule["direction"],
                      })
                    }
                  />
                ) : null}
              </div>
            ) : null}
            <div className="bt-rule-parameters">
              <SelectField
                title={
                  rule.measure === "signal_cross" ? "Пересечение" : "Условие"
                }
                label={`${label}: сравнение`}
                value={rule.operator}
                options={
                  rule.measure === "signal_cross"
                    ? operators.filter((o) => o.value.startsWith("crosses"))
                    : operators
                }
                onChange={(operator) =>
                  onChange({ ...rule, operator: operator as RuleOperator })
                }
              />
              {rule.measure !== "signal_cross" ? (
                <Numeric
                  title={`Порог${measure.unit ? `, ${measure.unit}` : ""}`}
                  label={`${label}: порог`}
                  value={rule.threshold}
                  min={min}
                  max={max}
                  onChange={(threshold) => onChange({ ...rule, threshold })}
                />
              ) : null}
              {hasWindow(rule) ? (
                <Numeric
                  title="За какое время"
                  label={`${label}: окно в часах`}
                  value={rule.windowHours}
                  min={step}
                  max={720}
                  step={step}
                  unit="ч"
                  onChange={(windowHours) => onChange({ ...rule, windowHours })}
                />
              ) : null}
              {definition.period ? (
                <Numeric
                  title="Период индикатора"
                  label={`${label}: период в свечах`}
                  value={rule.period}
                  min={2}
                  max={400}
                  step={1}
                  unit="св."
                  onChange={(period) => onChange({ ...rule, period })}
                />
              ) : null}
            </div>
            {hasWindow(rule) ? (
              <div
                className="bt-window-presets"
                aria-label={`${label}: быстрое окно`}
              >
                {[step, 4, 12, 24, 72, 168]
                  .filter((h, i, a) => h % step === 0 && a.indexOf(h) === i)
                  .map((hours) => (
                    <button
                      type="button"
                      key={hours}
                      aria-pressed={rule.windowHours === hours}
                      onClick={() => onChange({ ...rule, windowHours: hours })}
                    >
                      {hours < 24 ? `${hours} ч` : `${hours / 24} д`}
                    </button>
                  ))}
              </div>
            ) : null}
            <p className="bt-rule-summary">{describeRule(rule)}</p>
            <p className="bt-caption">{measure.hint}</p>
            {rule.period ? (
              <p className="bt-caption">
                {rule.period} свечей × {step} ч = {rule.period * step} ч истории
                для периода индикатора.
              </p>
            ) : null}
            {definition.trades ? (
              <p className="bt-caption">
                По реальным сделкам Bybit. При первом тесте выбранные дни
                загружаются из архива; повторный тест использует сохранённые
                данные.
              </p>
            ) : null}
          </motion.div>
        ) : null}
      </AnimatePresence>
      {problem ? (
        <p className="bt-rule-error" role="status">
          {problem}
        </p>
      ) : null}
    </div>
  );
}
export function ConditionBuilder({
  value,
  onChange,
  depth = 0,
  label = "Условия",
  timeframe,
  market,
  limits,
  usage,
  onLimit,
}: {
  value: Condition;
  onChange: (v: Condition) => void;
  depth?: number;
  label?: string;
  timeframe: string;
  market: string;
  limits: BacktestLimits;
  usage: ConditionUsage;
  onLimit: (message: string) => void;
}) {
  const reduced = useReducedMotion();
  const prefix = useId();
  const [identities, setIdentities] = useState(() =>
    Array.from({ length: 8 }, (_, i) => i),
  );
  if (value.kind !== "and" && value.kind !== "or")
    return (
      <RuleFields
        value={value}
        onChange={onChange}
        label={label}
        timeframe={timeframe}
        market={market}
      />
    );
  const update = (index: number, child: Condition) =>
    onChange({
      ...value,
      children: value.children.map((c, i) => (i === index ? child : c)),
    });
  const addChild = (group: boolean) => {
    if (usage.nodes + (group ? 2 : 1) > 32) {
      onLimit("В стратегии может быть до 32 элементов: условий и групп вместе.");
      return;
    }
    if (value.children.length >= 8) {
      onLimit("В одной группе может быть до 8 элементов. Добавьте условие в другую группу.");
      return;
    }
    if (value.children.length >= limits.maxGroupChildren || !canAdd(usage, limits, group) || (group && depth >= limits.maxDepth)) {
      onLimit(limitNotice(limits, group ? "Дополнительные и вложенные группы" : "Дополнительные условия"));
      return;
    }
    onChange({ ...value, children: [...value.children, group ? { kind: "or", children: [makeRule()] } : makeRule()] });
  };
  return (
    <div
      className={
        depth ? "bt-condition-group bt-nested-group" : "bt-condition-group"
      }
    >
      <div className="bt-group-heading">
        <span>{label}</span>
        <AnimatedSelect
          className="bt-select"
          ariaLabel={`${label}: логика`}
          value={value.kind}
          options={[
            { value: "and", label: "Все условия (AND)" },
            { value: "or", label: "Любое условие (OR)" },
          ]}
          onChange={(kind) =>
            onChange({ ...value, kind: kind as "and" | "or" })
          }
        />
      </div>
      <div className="bt-condition-list">
        <AnimatePresence initial={false}>
          {value.children.map((child, index) => (
            <motion.div
              key={`${prefix}:${identities[index]}`}
              layout={reduced ? false : "position"}
              className="bt-rule-wrap"
              initial={reduced ? false : { height: 0, opacity: 0 }}
              animate={{ height: "auto", opacity: 1 }}
              exit={{ height: 0, opacity: 0 }}
              transition={{ duration: reduced ? 0 : 0.25 }}
            >
              {index > 0 ? (
                <span className="bt-rule-connector">
                  {value.kind === "and" ? "И" : "ИЛИ"}
                </span>
              ) : null}
              <div className="bt-condition-row">
                <ConditionBuilder
                  value={child}
                  onChange={(v) => update(index, v)}
                  depth={depth + 1}
                  label={`${label} ${index + 1}`}
                  timeframe={timeframe}
                  market={market}
                  limits={limits}
                  usage={usage}
                  onLimit={onLimit}
                />
                <button
                  className="bt-icon-button bt-remove"
                  type="button"
                  disabled={value.children.length === 1}
                  aria-label={`Удалить ${label.toLowerCase()} ${index + 1}`}
                  onClick={() => {
                    setIdentities((ids) => [
                      ...ids.filter((_, i) => i !== index),
                      Math.max(...ids) + 1,
                    ]);
                    onChange({
                      ...value,
                      children: value.children.filter((_, i) => i !== index),
                    });
                  }}
                >
                  <Icon name="close" />
                </button>
              </div>
            </motion.div>
          ))}
        </AnimatePresence>
      </div>
      <div className="bt-group-actions">
        <button
          className="bt-button bt-small"
          type="button"
          onClick={() => addChild(false)}
        >
          <Icon name="plus" size={15} />
          Добавить условие
        </button>
        {depth < 2 ? (
          <button
            className="bt-button bt-small"
            type="button"
            onClick={() => addChild(true)}
          >
            <Icon name="plus" size={15} />
            Добавить группу
          </button>
        ) : null}
      </div>
    </div>
  );
}
