import { useRef, useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ConditionBuilder } from "./ConditionBuilder";
import {
  downloadBlob,
  makeExit,
  requestSchema,
  normalizeRequest,
  type BacktestRequest,
  type Strategy,
} from "./model";
import { Icon, NumberField, Panel, SectionTitle, Segments } from "./ui";
import { type BacktestLimits, conditionUsage, canAdd, planLimitError, limitNotice } from "./limits";

export function StrategyForm({
  request,
  onChange,
  onError,
  limits,
  onLimit,
}: {
  request: BacktestRequest;
  onChange: (r: BacktestRequest) => void;
  onError: (message: string) => void;
  limits: BacktestLimits;
  onLimit: (message: string) => void;
}) {
  const usage = conditionUsage(request.strategy.entry, request.strategy.exit);
  const importInput = useRef<HTMLInputElement>(null);
  const reduced = useReducedMotion();
  const [importNotice, setImportNotice] = useState("");
  const change = (next: Partial<Strategy>) =>
    onChange({ ...request, strategy: { ...request.strategy, ...next } });
  const importJSON = async (file?: File) => {
    if (!file) return;
    try {
      if (file.size > 32768) throw new Error("JSON должен быть меньше 32 КБ");
      const raw: unknown = JSON.parse(await file.text());
      const stack: { v: unknown; depth: number }[] = [{ v: raw, depth: 0 }];
      while (stack.length) {
        const { v, depth } = stack.pop()!;
        if (depth > 14) throw new Error("Слишком глубокая вложенность JSON");
        if (v && typeof v === "object")
          Object.values(v).forEach((child) =>
            stack.push({ v: child, depth: depth + 1 }),
          );
      }
      const parsed = requestSchema.safeParse(raw);
      if (!parsed.success) throw new Error("Некорректный формат стратегии");
      const normalized = normalizeRequest(parsed.data);
      const planError = planLimitError(normalized, limits);
      if (planError) { onLimit(planError); return; }
      onChange(normalized);
      setImportNotice(
        parsed.data.timeframe === "5m" || parsed.data.timeframe === "15m"
          ? "Стратегия загружена. Таймфрейм заменён на 1 час — проверьте период и условия."
          : "Стратегия загружена. Проверьте настройки перед запуском.",
      );
    } catch (e) {
      onError(e instanceof Error ? e.message : "Не удалось прочитать JSON");
    } finally {
      if (importInput.current) importInput.current.value = "";
    }
  };
  return (
    <Panel className="bt-panel bt-strategy">
      <div className="bt-strategy-heading">
        <SectionTitle>Условия стратегии</SectionTitle>
        <div className="bt-json-actions">
          <button
            type="button"
            className="bt-button"
            onClick={() => importInput.current?.click()}
          >
            <Icon name="upload" size={16} /> Импорт JSON
          </button>
          <button
            type="button"
            className="bt-button bt-export"
            onClick={() => {
              if (!requestSchema.safeParse(request).success) {
                onError(
                  "Перед экспортом заполните корректно все параметры стратегии",
                );
                return;
              }
              downloadBlob(
                new Blob([JSON.stringify(request, null, 2)], {
                  type: "application/json",
                }),
                "strategy-v2.json",
              );
            }}
          >
            <Icon name="download" size={16} /> Экспорт JSON
          </button>
          <input
            ref={importInput}
            hidden
            type="file"
            accept="application/json,.json"
            aria-label="Импорт стратегии"
            onChange={(e) => void importJSON(e.target.files?.[0])}
          />
        </div>
      </div>
      {importNotice ? (
        <p className="bt-import-notice bt-caption" role="status">
          {importNotice}
        </p>
      ) : null}
      <div className="bt-direction bt-field">
        <span className="bt-label">Направление торговли</span>
        <Segments
          label="Направление торговли"
          value={request.strategy.direction}
          options={[
            { value: "long", label: "Long (покупка)" },
            {
              value: "short",
              label: "Short (продажа)",
              disabled: request.market === "spot",
            },
          ]}
          onChange={(direction) => change({ direction })}
        />
      </div>
      <ConditionBuilder
        limits={limits}
        usage={usage}
        onLimit={onLimit}
        timeframe={request.timeframe}
        market={request.market}
        value={request.strategy.entry}
        onChange={(entry) => change({ entry })}
        label="Вход в позицию"
      />
      <div className="bt-exit">
        <h3>Выход из позиции</h3>
        <div className="bt-exit-fields">
          <NumberField
            label="Тейк-профит (TP), %"
            hint="0 — без тейк-профита"
            value={request.strategy.takeProfitPct}
            min={0}
            max={request.strategy.direction === "short" ? 99.99 : 1000}
            onChange={(takeProfitPct) => change({ takeProfitPct })}
          />
          <NumberField
            label="Стоп-лосс (SL), %"
            value={request.strategy.stopLossPct}
            min={0.01}
            max={99}
            onChange={(stopLossPct) => change({ stopLossPct })}
          />
          <label className="bt-switch">
            <input
              type="checkbox"
              checked={!!request.strategy.exit}
              onChange={(e) => {
                if (e.target.checked && !canAdd(usage, limits, true)) {
                  onLimit(usage.nodes + 2 > 32
                    ? "Для выхода по условию нужны два свободных элемента в стратегии: группа и условие."
                    : limitNotice(limits, "Дополнительные условия выхода"));
                  return;
                }
                change({ exit: e.target.checked ? makeExit() : undefined });
              }}
            />
            <span className="bt-switch-track" />
            <span>
              Выход по условию<small>Дополнительно к TP и SL</small>
            </span>
          </label>
        </div>
        <AnimatePresence initial={false}>
          {request.strategy.exit ? (
            <motion.div
              key="exit"
              initial={{ height: 0, opacity: 0 }}
              animate={{ height: "auto", opacity: 1 }}
              exit={{ height: 0, opacity: 0 }}
              transition={{
                duration: reduced ? 0 : 0.3,
                ease: [0.22, 1, 0.36, 1],
              }}
              style={{ overflow: "hidden" }}
            >
              <ConditionBuilder
                limits={limits}
                usage={usage}
                onLimit={onLimit}
                timeframe={request.timeframe}
                market={request.market}
                value={request.strategy.exit}
                onChange={(exit) => change({ exit })}
                label="Условие выхода"
              />
            </motion.div>
          ) : null}
        </AnimatePresence>
      </div>
    </Panel>
  );
}

export function ExecutionForm({
  value,
  onChange,
  busy,
  disabled,
}: {
  value: Strategy;
  onChange: (s: Strategy) => void;
  busy: boolean;
  disabled: boolean;
}) {
  return (
    <Panel className="bt-panel bt-execution">
      <SectionTitle>Капитал и исполнение</SectionTitle>
      <div className="bt-execution-fields">
        <NumberField
          label="Начальный капитал, USDT"
          value={value.initialCapital}
          min={100}
          max={100_000_000}
          onChange={(initialCapital) => onChange({ ...value, initialCapital })}
        />
        <NumberField
          label="Размер позиции, %"
          value={value.positionSizePct}
          min={0.1}
          max={100}
          onChange={(positionSizePct) =>
            onChange({ ...value, positionSizePct })
          }
        />
        <NumberField
          label="Комиссия, %"
          value={value.feePct}
          max={5}
          onChange={(feePct) => onChange({ ...value, feePct })}
        />
        <NumberField
          label="Проскальзывание, %"
          value={value.slippagePct}
          max={5}
          onChange={(slippagePct) => onChange({ ...value, slippagePct })}
        />
      </div>
      <p className="bt-execution-note">
        Общий капитал для всех монет. Без кредитного плеча.
      </p>
      <button className="bt-run" type="submit" disabled={disabled}>
        <Icon name="play" size={21} />
        {busy ? "Тест выполняется…" : "Запустить тест"}
      </button>
      <p className="bt-run-note">
        Вход на следующей свече · Комиссия на входе и выходе
      </p>
    </Panel>
  );
}
