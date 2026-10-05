// src/pages/public/scanners/ScannerConfig.tsx
import { useCallback, useEffect, useId, useMemo, useState } from "react";
import { LayoutGroup, motion, useReducedMotion } from "framer-motion";
import type {
  ApiCreateScannerRuleInput,
  ApiScannerCondition,
  ApiScannerRule,
  ApiUpdateScannerRuleInput,
  ScannerExchange,
  ScannerExchangeSelection,
} from "./types";
import { useCreateScannerRule, useScannerRules, useUpdateScannerRule } from "./queries";
import { useAuth } from "../../../shared/auth/AuthContext";
import { useToast } from "../../../shared/ui/ToastProvider";
import { AnimatedNativeSelect } from "../../../shared/ui/AnimatedSelect";
import { ALERT_WINDOW_MAX_MINUTES, isValidAlertWindowMinutes } from "./alertWindow";

type SymbolsMode = "ALL" | "CUSTOM";
type Indicator =
  | "price"
  | "openInterest"
  | "volume"
  | "delta"
  | "cvd"
  | "orderbookBid"
  | "orderbookAsk"
  | "orderbookSpread"
  | "orderbookBidDepth"
  | "orderbookAskDepth"
  | "tradeBuyVolume"
  | "tradeSellVolume"
  | "longRatio"
  | "shortRatio"
  | "liquidations"
  | "liquidationsCombined"
  | "liquidationsLong"
  | "liquidationsShort";
type Direction = "up" | "down" | "both";

type PricePoint = "open" | "high" | "low" | "close";

type RuleRow = {
  id: string;
  symbolsMode: SymbolsMode;
  symbolsRaw: string;
  indicator: Indicator;
  priceStart: PricePoint;
  priceEnd: PricePoint;
  direction: Direction;
  negate?: boolean;
  percent: number;
  amountK: number | "";
  windowMinutes: number;
};

type ScannerUIConfig = {
  enabled: boolean;
  exchange: ScannerExchangeSelection;
  rows: RuleRow[];
  updatedAt: string;
};

const DEFAULT_COOLDOWN_SECONDS = 0;
const INDICATORS: Array<{ value: Indicator; label: string }> = [
  { value: "price", label: "Цена" },
  { value: "openInterest", label: "Открытый интерес (OI)" },
  { value: "volume", label: "Объём" },
  { value: "delta", label: "Дельта" },
  { value: "cvd", label: "CVD" },
  { value: "orderbookBid", label: "Orderbook best bid" },
  { value: "orderbookAsk", label: "Orderbook best ask" },
  { value: "orderbookSpread", label: "Orderbook spread" },
  { value: "orderbookBidDepth", label: "Orderbook bid depth" },
  { value: "orderbookAskDepth", label: "Orderbook ask depth" },
  { value: "tradeBuyVolume", label: "Trade buy volume" },
  { value: "tradeSellVolume", label: "Trade sell volume" },
  { value: "longRatio", label: "Long ratio" },
  { value: "shortRatio", label: "Short ratio" },
  { value: "liquidations", label: "Ликвидации (USD)" },
  { value: "liquidationsCombined", label: "Совокупные ликвидации (USD)" },
  { value: "liquidationsLong", label: "Ликвидации лонгов (USD)" },
  { value: "liquidationsShort", label: "Ликвидации шортов (USD)" },
];

const isAmountIndicator = (indicator: Indicator) => indicator.startsWith("liquidations");

const ORDERBOOK_SET = new Set<Indicator>([
  "orderbookBid",
  "orderbookAsk",
  "orderbookSpread",
  "orderbookBidDepth",
  "orderbookAskDepth",
]);
const ORDERBOOK_ENABLED = import.meta.env.VITE_ENABLE_ORDERBOOK === "true";
const PRICE_POINTS: Array<{ value: PricePoint; label: string }> = [
  { value: "open", label: "Открытие" },
  { value: "high", label: "Максимум" },
  { value: "low", label: "Минимум" },
  { value: "close", label: "Закрытие" },
];

function parsePriceIndicator(raw: string | null | undefined): {
  indicator: Indicator;
  priceStart: PricePoint;
  priceEnd: PricePoint;
} | null {
  const value = String(raw ?? "").trim();
  if (value === "price") {
    return { indicator: "price", priceStart: "close", priceEnd: "close" };
  }
  if (value === "open" || value === "high" || value === "low" || value === "close") {
    const point = value as PricePoint;
    return { indicator: "price", priceStart: point, priceEnd: point };
  }
  if (value.startsWith("price:")) {
    const parts = value.split(":").map((p) => p.trim());
    if (parts.length === 3) {
      const start = parts[1] as PricePoint;
      const end = parts[2] as PricePoint;
      if (PRICE_POINTS.some((p) => p.value === start) && PRICE_POINTS.some((p) => p.value === end)) {
        return { indicator: "price", priceStart: start, priceEnd: end };
      }
    }
  }
  return null;
}

function toApiIndicator(row: RuleRow) {
  if (row.indicator !== "price") return row.indicator;
  return `price:${row.priceStart}:${row.priceEnd}`;
}

type ScannerSlot = "SLOT_1" | "SLOT_2" | "SLOT_3";

function exchangeStorageKey(userKey: string, slot: ScannerSlot) {
  return `scanner-exchange:${userKey}:${slot}`;
}

function readRememberedExchange(userKey: string, slot: ScannerSlot): ScannerExchangeSelection | null {
  if (typeof window === "undefined") return null;
  try {
    const value = window.localStorage.getItem(exchangeStorageKey(userKey, slot));
    return value === "bybit" || value === "binance" || value === "all" ? value : null;
  } catch {
    return null;
  }
}

function rememberExchange(userKey: string, slot: ScannerSlot, exchange: ScannerExchangeSelection) {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(exchangeStorageKey(userKey, slot), exchange);
  } catch {
    // The scanner still works when storage is unavailable (for example in private mode).
  }
}

function slotToParam(slot: ScannerSlot) {
  if (slot === "SLOT_3") return "3";
  if (slot === "SLOT_2") return "2";
  return "1";
}

function slotLabel(slot: ScannerSlot) {
  switch (slot) {
    case "SLOT_2":
      return "Сканер 2";
    case "SLOT_3":
      return "Сканер 3";
    default:
      return "Сканер 1";
  }
}

function toMs(iso: string | undefined): number {
  const ts = Date.parse(String(iso ?? ""));
  return Number.isFinite(ts) ? ts : 0;
}

function pickSlotRule(
  rules: ApiScannerRule[],
  slot: ScannerSlot,
  exchange?: ScannerExchange
): ApiScannerRule | null {
  const slotRules = rules.filter(
    (r) =>
      (r.scanner_slot ?? "SLOT_1") === slot &&
      (!exchange || (r.exchange ?? "bybit") === exchange)
  );
  if (slotRules.length === 0) return null;

  // If several historical rows exist for one slot, show the same one that effectively drives signals.
  const enabledRules = slotRules.filter((r) => !!r.enabled);
  const pool = enabledRules.length ? enabledRules : slotRules;

  return [...pool].sort((a, b) => {
    const byUpdatedAt = toMs(b.updated_at) - toMs(a.updated_at);
    if (byUpdatedAt !== 0) return byUpdatedAt;
    return b.id - a.id;
  })[0];
}

function uid(prefix: string) {
  return `${prefix}_${Math.random().toString(16).slice(2)}_${Date.now()}`;
}

function clamp(n: number, min: number, max: number) {
  if (!Number.isFinite(n)) return min;
  return Math.max(min, Math.min(max, n));
}

function normalizeSymbols(raw: string): { symbols: string[]; errors: string[] } {
  const parts = raw
    .toUpperCase()
    .replace(/;/g, ",")
    .split(/[\s,]+/g)
    .map((s) => s.trim())
    .filter(Boolean);

  const errors: string[] = [];
  const symbols: string[] = [];
  const re = /^[A-Z0-9]{3,30}$/;

  for (const part of parts) {
    let s = part.replace(/[/-]/g, "");
    if (!s.endsWith("USDT")) {
      s = `${s}USDT`;
    }
    if (!re.test(s)) {
      errors.push(`Некорректный символ: ${part}`);
      continue;
    }
    if (!symbols.includes(s)) symbols.push(s);
  }
  return { symbols, errors };
}

function stripUsdt(symbol: string) {
  return symbol.endsWith("USDT") ? symbol.slice(0, -4) : symbol;
}

function symbolsForUi(raw: string): string {
  const value = String(raw ?? "").trim();
  if (!value || value === "*" || value.toUpperCase() === "ALL") return "";
  return normalizeSymbols(value)
    .symbols
    .map(stripUsdt)
    .join(" ");
}

function defaultRow(): RuleRow {
  return {
    id: uid("r1"),
    symbolsMode: "ALL",
    symbolsRaw: "",
    indicator: "price",
    priceStart: "close",
    priceEnd: "close",
    direction: "down",
    negate: false,
    percent: 1,
    amountK: 50,
    windowMinutes: 30,
  };
}

function defaultConfig(exchange: ScannerExchangeSelection = "bybit"): ScannerUIConfig {
  return {
    enabled: false,
    exchange,
    rows: [defaultRow()],
    updatedAt: new Date().toISOString(),
  };
}

function validateRows(rows: RuleRow[]) {
  const errs: string[] = [];
  if (rows.length === 0) {
    errs.push("Добавь хотя бы одно условие.");
    return errs;
  }
  if (rows.length > 3) {
    errs.push("Максимум 3 условия AND на сканер.");
  }
  const base = rows[0] ?? defaultRow();
  rows.forEach((r, idx) => {
    if (r.symbolsMode !== base.symbolsMode || r.symbolsRaw !== base.symbolsRaw) {
      errs.push(`Строка #${idx + 1}: символ должен совпадать с первым условием.`);
    }
    if (r.windowMinutes !== base.windowMinutes) {
      errs.push(`Строка #${idx + 1}: окно должно совпадать с первым условием.`);
    }
    if (r.symbolsMode === "CUSTOM") {
      const { symbols, errors } = normalizeSymbols(r.symbolsRaw);
      if (symbols.length === 0) errs.push(`Строка #${idx + 1}: укажи хотя бы один символ.`);
      errs.push(...errors.map((e) => `Строка #${idx + 1}: ${e}`));
    }
    if (isAmountIndicator(r.indicator)) {
      if (Number(r.amountK) <= 0) errs.push(`Строка #${idx + 1}: сумма должна быть > 0.`);
    } else if (r.percent < 0 || r.percent > 300) {
      errs.push(`Строка #${idx + 1}: процент должен быть 0..300.`);
    }
    if (!isValidAlertWindowMinutes(r.windowMinutes, ALERT_WINDOW_MAX_MINUTES))
      errs.push(`Строка #${idx + 1}: окно должно быть 1..${ALERT_WINDOW_MAX_MINUTES} минут.`);
  });

  return errs;
}

function ruleToRows(rule: ApiScannerRule): RuleRow[] {
  const base: RuleRow = {
    id: uid("r1"),
    symbolsMode: rule.symbol === "*" ? "ALL" : "CUSTOM",
    symbolsRaw: rule.symbol === "*" ? "" : symbolsForUi(rule.symbol),
    indicator: "price",
    priceStart: "close",
    priceEnd: "close",
    direction: "down",
    negate: false,
    percent: 1,
    amountK: 50,
    windowMinutes: clamp(Number(rule.window_minutes ?? 15), 1, ALERT_WINDOW_MAX_MINUTES),
  };

  const conds = rule.conditions?.length ? rule.conditions : undefined;
  if (!conds) {
    const priceParsed = parsePriceIndicator(rule.indicator);
    const indicator = priceParsed
      ? priceParsed.indicator
      : INDICATORS.some((i) => i.value === rule.indicator)
        ? (rule.indicator as Indicator)
        : "price";
  const percent = clamp(Number(rule.threshold_percent ?? 10), 0, 300);
    return [
      {
        ...base,
        indicator,
        priceStart: priceParsed?.priceStart ?? base.priceStart,
        priceEnd: priceParsed?.priceEnd ?? base.priceEnd,
        direction: rule.direction === "down" ? "down" : rule.direction === "both" ? "both" : "up",
        percent,
        amountK: clamp(Number((rule.threshold_amount ?? 0) / 1000), 0, 1_000_000),
      },
    ];
  }

  return conds.slice(0, 3).map((c, idx) => {
    const priceParsed = parsePriceIndicator(c.indicator);
    const indicator = priceParsed
      ? priceParsed.indicator
      : INDICATORS.some((i) => i.value === c.indicator)
        ? (c.indicator as Indicator)
        : "price";
    return {
      ...base,
      id: uid(`r${idx + 1}`),
      indicator,
      priceStart: priceParsed?.priceStart ?? base.priceStart,
      priceEnd: priceParsed?.priceEnd ?? base.priceEnd,
      direction: c.direction === "down" ? "down" : c.direction === "up" ? "up" : "both",
      negate: !!c.negate,
      percent: clamp(Number(c.threshold_percent ?? 10), 0, 300),
      amountK: clamp(Number((c.threshold_amount ?? 0) / 1000), 0, 1_000_000),
    };
  });
}

function applyRuleMeta(cfg: ScannerUIConfig, rule: ApiScannerRule): ScannerUIConfig {
  return {
    ...cfg,
    updatedAt: rule.updated_at || new Date().toISOString(),
  };
}

function buildApiInput(cfg: ScannerUIConfig, exchange: ScannerExchange): ApiCreateScannerRuleInput {
  const base = cfg.rows[0] ?? defaultRow();

  let symbol = "*";
  if (base.symbolsMode === "CUSTOM") {
    const parsed = normalizeSymbols(base.symbolsRaw);
    symbol = parsed.symbols.length ? parsed.symbols.join(" ") : "*";
  }

  const conditions: ApiScannerCondition[] = cfg.rows.slice(0, 3).map((row) => {
    const isAmount = isAmountIndicator(row.indicator);
    return {
      indicator: toApiIndicator(row),
      direction: isAmount ? "up" : row.direction,
      threshold_percent: isAmount ? null : row.percent,
      threshold_amount: isAmount ? Number(row.amountK || 0) * 1000 : null,
      negate: !!row.negate,
    };
  });

  const first = conditions[0];

  return {
    exchange,
    market_type: "perpetual",
    indicator: first?.indicator ?? toApiIndicator(base),
    symbol,
    window_minutes: base.windowMinutes,
    threshold_percent: first?.threshold_percent ?? null,
    threshold_amount: first?.threshold_amount ?? null,
    direction: first?.direction ?? base.direction,
    cooldown_seconds: DEFAULT_COOLDOWN_SECONDS,
    enabled: cfg.enabled,
    conditions,
  };
}

const fieldLabel = "text-xs uppercase tracking-wide text-muted-foreground";
const inputCls =
  "w-full rounded-xl border border-border bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground focus:border-ring focus:ring-2 focus:ring-ring outline-none";
const selectCls =
  "w-full rounded-xl border border-border bg-background px-3 py-2 text-foreground focus:border-ring focus:ring-2 focus:ring-ring outline-none";
const rangeCls = "scanner-range mt-2 w-full";

function Segmented({
  value,
  onChange,
  options,
}: {
  value: string;
  onChange: (v: string) => void;
  options: Array<{ value: string; label: string }>;
}) {
	const layoutId = useId();
	const reduceMotion = useReducedMotion();
  return (
	<LayoutGroup id={layoutId}>
	  <div className="inline-flex rounded-xl border border-border bg-background p-1">
	    {options.map((o) => {
	      const active = o.value === value;
	      return (
	        <motion.button
	          key={o.value}
	          type="button"
	          onClick={() => onChange(o.value)}
	          whileTap={reduceMotion ? undefined : { scale: 0.98 }}
	          className={
	            "relative isolate rounded-lg px-3 py-1.5 text-sm font-semibold transition-colors duration-200 " +
	            (active ? "text-primary-foreground" : "text-foreground hover:bg-secondary")
	          }
	        >
	          {active ? (
	            <motion.span
	              layoutId="scanner-config-segment-active"
	              className="absolute inset-0 -z-10 rounded-lg bg-primary"
	              transition={reduceMotion ? { duration: 0 } : { type: "spring", stiffness: 520, damping: 38, mass: 0.7 }}
	            />
	          ) : null}
	          <span className="relative z-10">{o.label}</span>
	        </motion.button>
	      );
	    })}
	  </div>
	</LayoutGroup>
  );
}

export default function ScannerConfig({ slot }: { slot: ScannerSlot }) {
  const { user, primaryExchange } = useAuth();
  const userKey = user?.id ?? "anon";

  const [cfg, setCfg] = useState<ScannerUIConfig>(() =>
    defaultConfig(readRememberedExchange(userKey, slot) ?? primaryExchange ?? "bybit")
  );
  const [rulesLoaded, setRulesLoaded] = useState(false);
  const toast = useToast();
  const [isStarting, setIsStarting] = useState(false);
  const [isStopping, setIsStopping] = useState(false);
  const [isSaving, setIsSaving] = useState(false);

  const rulesQuery = useScannerRules(userKey, slotToParam(slot));
  const createRule = useCreateScannerRule(userKey);
  const updateRule = useUpdateScannerRule(userKey);

  const baseRow = cfg.rows[0] ?? defaultRow();
  const hasCombinedLiquidations = cfg.rows.some(
    (row) => row.indicator === "liquidationsCombined"
  );
  const validations = useMemo(() => validateRows(cfg.rows), [cfg.rows]);
  const isLoadingRules = !rulesLoaded && rulesQuery.isLoading;
  const isBusy = isStarting || isStopping || isSaving;

  // ВАЖНО: статус и кнопки должны переключаться мгновенно.
  // Поэтому используем локальное состояние cfg.enabled (оно меняется оптимистично в start/stop),
  // а ответ rulesQuery используем только для первичной инициализации конфигурации.
  const statusEnabled = cfg.enabled;

  const statusLabel = isLoadingRules ? "LOADING" : statusEnabled ? "RUNNING" : "STOPPED";
  const statusClass = isLoadingRules
    ? "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-200"
    : statusEnabled
      ? "border-border-strong bg-accent text-primary"
      : "border-border bg-background text-foreground";

  useEffect(() => {
    const id = setTimeout(() => {
      setRulesLoaded(false);
      setCfg(defaultConfig(readRememberedExchange(userKey, slot) ?? primaryExchange ?? "bybit"));
    }, 0);
    return () => clearTimeout(id);
  }, [userKey, slot, primaryExchange]);

  useEffect(() => {
    if (rulesLoaded || !rulesQuery.data) return;
    const rules = rulesQuery.data;
    const id = setTimeout(() => {
      const bybitRule = pickSlotRule(rules, slot, "bybit");
      const binanceRule = pickSlotRule(rules, slot, "binance");
      const activeBybit = !!bybitRule?.enabled;
      const activeBinance = !!binanceRule?.enabled;
      const rememberedExchange = readRememberedExchange(userKey, slot);
      const exchange: ScannerExchangeSelection =
        rememberedExchange ??
        (activeBybit && activeBinance
          ? "all"
          : activeBinance
            ? "binance"
            : activeBybit
              ? "bybit"
              : binanceRule && !bybitRule
                  ? "binance"
                  : bybitRule && !binanceRule
                    ? "bybit"
                    : primaryExchange ?? "bybit");
      rememberExchange(userKey, slot, exchange);
      const candidates = [bybitRule, binanceRule].filter(Boolean) as ApiScannerRule[];
      const selectedRule =
        exchange === "bybit" ? bybitRule : exchange === "binance" ? binanceRule : null;
      const first =
        selectedRule ?? [...candidates].sort((a, b) => toMs(b.updated_at) - toMs(a.updated_at))[0];
      if (first) {
        setCfg((prev) => ({
          ...applyRuleMeta(prev, first),
          rows: ruleToRows(first),
          exchange,
          enabled: activeBybit || activeBinance,
        }));
      } else {
        setCfg(defaultConfig(rememberedExchange ?? primaryExchange ?? "bybit"));
      }
      setRulesLoaded(true);
    }, 0);
    return () => clearTimeout(id);
  }, [rulesLoaded, rulesQuery.data, slot, primaryExchange, userKey]);

  const hasPriceDropOverLimit = useCallback(() => {
    return cfg.rows.some((row) => {
      const isPrice = row.indicator === "price";
      const isDown = row.direction === "down" || row.direction === "both";
      return isPrice && isDown && row.percent > 100;
    });
  }, [cfg.rows]);

  function patchRow(index: number, patch: Partial<RuleRow>) {
    setCfg((prev) => {
      const rows = [...prev.rows];
      const current = rows[index] ?? defaultRow();
      rows[index] = { ...current, ...patch };
      return { ...prev, rows };
    });
  }

  function patchShared(patch: Partial<RuleRow>) {
    setCfg((prev) => ({
      ...prev,
      rows: prev.rows.map((row) => ({ ...row, ...patch })),
    }));
  }

  function addCondition() {
    setCfg((prev) => {
      if (prev.rows.length >= 3) return prev;
      const base = prev.rows[0] ?? defaultRow();
      return {
        ...prev,
        rows: [
          ...prev.rows,
          {
            ...defaultRow(),
            symbolsMode: base.symbolsMode,
            symbolsRaw: base.symbolsRaw,
            windowMinutes: base.windowMinutes,
          },
        ],
      };
    });
  }

  function removeCondition(index: number) {
    setCfg((prev) => {
      if (prev.rows.length <= 1) return prev;
      return { ...prev, rows: prev.rows.filter((_, i) => i !== index) };
    });
  }

  async function persistRule(nextEnabled?: boolean) {
    const enabled = nextEnabled ?? cfg.enabled;
    if (!hasCombinedLiquidations) {
      rememberExchange(userKey, slot, cfg.exchange);
    }
    const targetExchanges: ScannerExchange[] =
      hasCombinedLiquidations
        ? ["bybit"]
        : cfg.exchange === "all"
          ? ["bybit", "binance"]
          : [cfg.exchange];
    const saved = await Promise.all(
      targetExchanges.map((exchange) => {
        const input = buildApiInput({ ...cfg, enabled }, exchange);
        input.scanner_slot = slot;
        return createRule.mutateAsync(input);
      })
    );

    const refreshed = await rulesQuery.refetch();
    const knownRules = refreshed.data ?? rulesQuery.data ?? [];
    const inactiveRules = knownRules.filter(
      (rule) =>
        (rule.scanner_slot ?? "SLOT_1") === slot &&
        !targetExchanges.includes(rule.exchange ?? "bybit") &&
        rule.enabled
    );
    const disablePatch: ApiUpdateScannerRuleInput = { enabled: false };
    await Promise.all(
      inactiveRules.map((rule) =>
        updateRule.mutateAsync({ id: String(rule.id), patch: disablePatch })
      )
    );

    const newest = [...saved].sort((a, b) => toMs(b.updated_at) - toMs(a.updated_at))[0];
    if (newest) {
      setCfg((prev) => applyRuleMeta({ ...prev, enabled }, newest));
    }
    return newest;
  }

  async function start() {
    if (isStarting || isStopping || isSaving) {
      return;
    }
    setIsStarting(true);
    if (isLoadingRules) {
      toast.info("Подожди, правила пользователя ещё загружаются.");
      setIsStarting(false);
      return;
    }
    if (!rulesLoaded && rulesQuery.isLoading) {
      toast.info("Подожди, правила пользователя ещё загружаются.");
      setIsStarting(false);
      return;
    }
    if (validations.length) {
      const summary = validations.slice(0, 2).join("; ");
      toast.error(summary || "Исправь ошибки в конфигурации перед запуском.");
      setIsStarting(false);
      return;
    }

    // Оптимистично включаем сразу, чтобы UI не ждал refetch.
    const prevEnabled = cfg.enabled;
    setCfg((prev) => ({ ...prev, enabled: true }));

    try {
      if (hasPriceDropOverLimit()) {
        toast.info("Параметры могут не примениться: цена не может упасть более чем на 100%.");
      }
      setCfg((prev) => ({ ...prev, updatedAt: new Date().toISOString() }));
      await persistRule(true);
      toast.success("Сканирование включено и конфигурация сохранена.");
    } catch (err) {
      // Откат при ошибке
      setCfg((prev) => ({ ...prev, enabled: prevEnabled }));
      const message = err instanceof Error ? err.message : "Ошибка запуска";
      toast.error(`Не удалось запустить: ${message}`);
    } finally {
      setIsStarting(false);
    }
  }

  async function stop() {
    if (isStarting || isStopping || isSaving) {
      return;
    }
    setIsStopping(true);
    if (isLoadingRules) {
      toast.info("Подожди, правила пользователя ещё загружаются.");
      setIsStopping(false);
      return;
    }

    // Оптимистично выключаем сразу, чтобы UI не ждал refetch.
    const prevEnabled = cfg.enabled;
    setCfg((prev) => ({ ...prev, enabled: false }));

    try {
      await persistRule(false);
      toast.success("Сканирование остановлено.");
    } catch (err) {
      // Откат при ошибке
      setCfg((prev) => ({ ...prev, enabled: prevEnabled }));
      const message = err instanceof Error ? err.message : "Ошибка остановки";
      toast.error(`Не удалось остановить: ${message}`);
    } finally {
      setIsStopping(false);
    }
  }

  async function save() {
    if (isStarting || isStopping || isSaving) {
      return;
    }
    setIsSaving(true);
    if (isLoadingRules) {
      toast.info("Подожди, правила пользователя ещё загружаются.");
      setIsSaving(false);
      return;
    }
    if (!rulesLoaded && rulesQuery.isLoading) {
      toast.info("Подожди, правила пользователя ещё загружаются.");
      setIsSaving(false);
      return;
    }
    if (validations.length) {
      const summary = validations.slice(0, 2).join("; ");
      toast.error(summary || "Исправь ошибки перед сохранением.");
      setIsSaving(false);
      return;
    }

    try {
      if (hasPriceDropOverLimit()) {
        toast.info("Параметры могут не примениться: цена не может упасть более чем на 100%.");
      }
      setCfg((prev) => ({ ...prev, updatedAt: new Date().toISOString() }));
      await persistRule();
      toast.success("Конфигурация сохранена.");
    } catch (err) {
      const message = err instanceof Error ? err.message : "Ошибка сохранения";
      toast.error(`Не удалось сохранить в backend: ${message}`);
    } finally {
      setIsSaving(false);
    }
  }

  function reset() {
    rememberExchange(userKey, slot, cfg.exchange);
    setCfg(defaultConfig(cfg.exchange));
    toast.info("Конфигурация сброшена.");
  }

  if (isLoadingRules && !rulesLoaded) {
    return (
      <div className="relative overflow-hidden rounded-2xl border border-border bg-card p-6">
        <div className="relative animate-pulse space-y-4">
          <div className="h-5 w-32 rounded-full bg-secondary" />
          <div className="h-4 w-48 rounded-full bg-secondary" />
          <div className="h-24 w-full rounded-2xl border border-border bg-card" />
          <div className="h-10 w-40 rounded-xl bg-secondary" />
        </div>
      </div>
    );
  }

  const sym = normalizeSymbols(baseRow.symbolsRaw);

  return (
    <div className="relative overflow-hidden rounded-2xl border border-border bg-card p-6">

      {/* header */}
      <div className="relative flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <h2 className="text-lg font-semibold text-foreground">
              {slotLabel(slot)}
            </h2>
            <span
              className={
                "rounded-full px-2.5 py-1 text-xs font-semibold border " + statusClass
              }
            >
              {statusLabel}
            </span>
          </div>

        </div>

        <div className="flex shrink-0 flex-wrap gap-2">
          <button
            onClick={start}
            disabled={isLoadingRules || isBusy}
            className="rounded-xl bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground hover:bg-primary-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:opacity-50"
          >
            Запустить
          </button>
          <button
            onClick={stop}
            disabled={isLoadingRules || isBusy}
            className="rounded-xl bg-secondary px-4 py-2 text-sm font-semibold text-foreground hover:bg-accent border border-border"
          >
            Остановить
          </button>
          <button
            onClick={save}
            disabled={isLoadingRules || isBusy}
            className="rounded-xl border border-border bg-secondary px-4 py-2 text-sm font-semibold text-secondary-foreground hover:bg-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:opacity-50"
          >
            Сохранить
          </button>
          <button
            onClick={reset}
            className="rounded-xl border border-border bg-background px-4 py-2 text-sm font-semibold text-foreground hover:bg-secondary"
          >
            Сбросить
          </button>
        </div>
      </div>

      {/* form */}
      <div className="relative mt-5 grid grid-cols-1 gap-4 rounded-2xl border border-border bg-background p-4 md:grid-cols-12 overflow-hidden">
        {/* exchange */}
        {hasCombinedLiquidations ? null : (
          <div className="relative md:col-span-12">
            <div className={fieldLabel}>Биржа</div>
            <div className="mt-2">
              <Segmented
                value={cfg.exchange}
                onChange={(value) => {
                  const exchange = value as ScannerExchangeSelection;
                  rememberExchange(userKey, slot, exchange);
                  setCfg((prev) => ({
                    ...prev,
                    exchange,
                  }));
                }}
                options={[
                  { value: "bybit", label: "Bybit" },
                  { value: "binance", label: "Binance" },
                  { value: "all", label: "Все" },
                ]}
              />
            </div>
          </div>
        )}
        {/* pairs */}
        <div className="relative md:col-span-6">
          <div className={fieldLabel}>Криптопары</div>
          <div className="mt-2">
            <Segmented
              value={baseRow.symbolsMode}
              onChange={(v) => patchShared({ symbolsMode: v as SymbolsMode })}
              options={[
                { value: "ALL", label: "Все пары" },
                { value: "CUSTOM", label: "Свои символы" },
              ]}
            />
          </div>

          {baseRow.symbolsMode === "CUSTOM" && (
            <div className="mt-3">
              <input
                value={baseRow.symbolsRaw}
                onChange={(e) => patchShared({ symbolsRaw: e.target.value.toUpperCase() })}
                placeholder="BTC ETH SOL"
                className={inputCls}
              />
              <div className="mt-2 text-xs text-muted-foreground">
                Вводи тикеры через пробел. Можно без USDT — система подставит сама.
              </div>
              <div className="mt-2 flex flex-wrap items-center gap-2">
                {sym.symbols.length > 0 ? (
                  sym.symbols.map((symbol) => (
                    <span
                      key={symbol}
                      className="rounded-full border border-border bg-background px-2 py-0.5 text-xs text-foreground"
                    >
                      {stripUsdt(symbol)}
                    </span>
                  ))
                ) : (
                  <span className="text-xs text-muted-foreground">Введи символы (например BTC ETH SOL)</span>
                )}
              </div>
            </div>
          )}
        </div>

        {/* window */}
        <div className="relative md:col-span-6">
          <div className={fieldLabel}>Окно</div>
          <div className="mt-2 flex items-center gap-2">
            <input
              type="number"
              min={1}
              max={ALERT_WINDOW_MAX_MINUTES}
              value={baseRow.windowMinutes}
              onChange={(e) => patchShared({ windowMinutes: clamp(Number(e.target.value), 1, ALERT_WINDOW_MAX_MINUTES) })}
              className={inputCls}
            />
            <span className="text-sm text-muted-foreground">мин</span>
          </div>
          <div className="mt-2 text-xs text-muted-foreground">За сколько минут считаем изменение</div>
          <input
            type="range"
            min={1}
            max={ALERT_WINDOW_MAX_MINUTES}
            value={baseRow.windowMinutes}
            onChange={(e) => patchShared({ windowMinutes: clamp(Number(e.target.value), 1, ALERT_WINDOW_MAX_MINUTES) })}
            className={rangeCls + " mt-3"}
            style={{
              "--value": baseRow.windowMinutes,
              "--min": 1,
              "--max": ALERT_WINDOW_MAX_MINUTES,
            } as React.CSSProperties}
          />
          <div className="mt-1 text-xs text-muted-foreground">1 … {ALERT_WINDOW_MAX_MINUTES} минут</div>
        </div>

        {/* conditions */}
        <div className="md:col-span-12">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className={fieldLabel}>Условия AND</div>
            <button
              type="button"
              onClick={addCondition}
              disabled={cfg.rows.length >= 3}
              className="rounded-xl border border-border bg-background px-3 py-2 text-xs font-semibold text-foreground hover:bg-background disabled:opacity-50"
            >
              + Добавить AND
            </button>
          </div>

          <div className="mt-3 grid gap-3">
            {cfg.rows.map((row, idx) => {
              const isAmount = isAmountIndicator(row.indicator);
              return (
                <div
                  key={row.id}
                  className="rounded-2xl border border-border bg-background p-4"
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="text-sm font-semibold text-foreground">Условие {idx + 1}</div>
                    {idx > 0 && (
                      <button
                        type="button"
                        onClick={() => removeCondition(idx)}
                        className="text-xs text-destructive hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                      >
                        Удалить
                      </button>
                    )}
                  </div>

                  <div className="mt-3 grid gap-3 md:grid-cols-2">
                    {row.indicator !== "price" ? (
                      <>
                        <div>
                          <div className={fieldLabel}>Индикатор</div>
                          <AnimatedNativeSelect
                            value={row.indicator}
                            onChange={(e) => {
                              const next = e.target.value as Indicator;
                              patchRow(idx, {
                                indicator: next,
                                priceStart: next === "price" ? row.priceStart ?? "close" : row.priceStart,
                                priceEnd: next === "price" ? row.priceEnd ?? "close" : row.priceEnd,
                                direction: isAmountIndicator(next) ? "up" : row.direction,
                                percent: row.percent,
                              });
                            }}
                            className={selectCls + " mt-2"}
                          >
                            {INDICATORS.filter((i) => ORDERBOOK_ENABLED || !ORDERBOOK_SET.has(i.value)).map((i) => (
                              <option key={i.value} value={i.value}>
                                {i.label}
                              </option>
                            ))}
                          </AnimatedNativeSelect>
                        </div>
                        <div>
                          <div className={fieldLabel}>Направление</div>
                          <AnimatedNativeSelect
                            value={isAmount ? "up" : row.direction}
                            onChange={(e) => patchRow(idx, { direction: e.target.value as Direction })}
                            className={selectCls + " mt-2"}
                            disabled={isAmount}
                          >
                            <option value="down">Уменьшится</option>
                            <option value="up">Увеличится</option>
                            {!isAmount ? <option value="both">В обе стороны</option> : null}
                          </AnimatedNativeSelect>
                          <div className="mt-2 text-xs text-muted-foreground">
                            {isAmount
                              ? "Для ликвидаций направление фиксировано: сигнал срабатывает при росте объёма ликвидаций."
                              : "Для неценовых индикаторов: куда движется показатель (вверх/вниз)."}
                          </div>
                        </div>
                        <div className="md:col-span-2">
                          <div className={fieldLabel}>Порог срабатывания</div>
                          {isAmount ? (
                            <>
                              <div className="mt-2 flex items-center gap-2">
                                <button
                                  type="button"
                                  onClick={() => patchRow(idx, { negate: true })}
                                  disabled={!!row.negate}
                                  className={
                                    "rounded-xl border border-border px-3 py-2 text-sm font-semibold " +
                                    (row.negate
                                      ? "bg-rose-500/15 text-destructive border-rose-500/30"
                                      : "bg-background text-foreground hover:bg-secondary")
                                  }
                                  title="Логическое НЕ (NOT): инвертирует условие"
                                >
                                  НЕ
                                </button>
                                {row.negate ? (
                                  <button
                                    type="button"
                                    onClick={() => patchRow(idx, { negate: false })}
                                    className="rounded-xl border border-border bg-background px-2.5 py-2 text-sm text-foreground hover:bg-secondary"
                                    title="Убрать НЕ"
                                  >
                                    ×
                                  </button>
                                ) : null}

                                <input
                                  type="number"
                                  min={1}
                                  step={1}
                                  value={row.amountK}
                                  onChange={(e) => {
                                    const raw = e.target.value;
                                    patchRow(idx, {
                                      amountK: raw === "" ? "" : clamp(Number(raw), 1, 1_000_000),
                                    });
                                  }}
                                  className={inputCls}
                                />
                                <span className="text-sm text-muted-foreground">тыс $</span>
                              </div>
                              <div className="mt-1 text-xs text-muted-foreground">
                                {row.negate
                                  ? "НЕ: условие сработает, когда объём ликвидаций будет МЕНЬШЕ указанной суммы."
                                  : "Например 50 = 50k USD"}
                              </div>
                            </>
                          ) : (
                            <>
                              <div className="mt-2 flex items-center gap-2">
                                <button
                                  type="button"
                                  onClick={() => patchRow(idx, { negate: true })}
                                  disabled={!!row.negate}
                                  className={
                                    "rounded-xl border border-border px-3 py-2 text-sm font-semibold " +
                                    (row.negate
                                      ? "bg-rose-500/15 text-destructive border-rose-500/30"
                                      : "bg-background text-foreground hover:bg-secondary")
                                  }
                                  title="Логическое НЕ (NOT): инвертирует условие"
                                >
                                  НЕ
                                </button>
                                {row.negate ? (
                                  <button
                                    type="button"
                                    onClick={() => patchRow(idx, { negate: false })}
                                    className="rounded-xl border border-border bg-background px-2.5 py-2 text-sm text-foreground hover:bg-secondary"
                                    title="Убрать НЕ"
                                  >
                                    ×
                                  </button>
                                ) : null}

                                <input
                                  type="number"
                                  min={0}
                                  max={100}
                                  value={row.percent}
                                  onChange={(e) =>
                                    patchRow(idx, {
                                      percent: clamp(Number(e.target.value), 0, 100),
                                    })
                                  }
                                  className={inputCls}
                                />
                                <span className="text-sm text-muted-foreground">%</span>
                              </div>
                              <input
                                type="range"
                                min={0}
                                max={100}
                                value={row.percent}
                                onChange={(e) =>
                                  patchRow(idx, {
                                    percent: clamp(Number(e.target.value), 0, 100),
                                  })
                                }
                                className={rangeCls + " w-full"}
                                style={{
                                  "--value": row.percent,
                                  "--min": 0,
                                  "--max": 100,
                                } as React.CSSProperties}
                              />
                              <div className="mt-1 text-xs text-muted-foreground">
                                {row.negate
                                  ? "НЕ: условие сработает, когда изменение будет НЕ больше порога (т.е. меньше или равно)."
                                  : "0 … 100"}
                              </div>
                            </>
                          )}
                        </div>
                      </>
                    ) : (
                      <>
                        <div>
                          <div className={fieldLabel}>Индикатор</div>
                          <AnimatedNativeSelect
                            value={row.indicator}
                            onChange={(e) => {
                              const next = e.target.value as Indicator;
                              patchRow(idx, {
                                indicator: next,
                                priceStart: next === "price" ? row.priceStart ?? "close" : row.priceStart,
                                priceEnd: next === "price" ? row.priceEnd ?? "close" : row.priceEnd,
                                direction: isAmountIndicator(next) ? "up" : row.direction,
                                percent: row.percent,
                              });
                            }}
                            className={selectCls + " mt-2"}
                          >
                            {INDICATORS.filter((i) => ORDERBOOK_ENABLED || !ORDERBOOK_SET.has(i.value)).map((i) => (
                              <option key={i.value} value={i.value}>
                                {i.label}
                              </option>
                            ))}
                          </AnimatedNativeSelect>
                        </div>
                        <div>
                          <div className={fieldLabel}>Направление</div>
                          <AnimatedNativeSelect
                            value={isAmount ? "up" : row.direction}
                            onChange={(e) => patchRow(idx, { direction: e.target.value as Direction })}
                            className={selectCls + " mt-2"}
                            disabled={isAmount}
                          >
                            <option value="down">Уменьшится</option>
                            <option value="up">Увеличится</option>
                            {!isAmount ? <option value="both">В обе стороны</option> : null}
                          </AnimatedNativeSelect>
                        </div>
                        <div className="md:col-span-2 grid gap-3 md:grid-cols-2">
                          <div>
                            <div className={fieldLabel}>Начало → Конец</div>
                            <div className="mt-2 grid grid-cols-2 gap-2">
                              <AnimatedNativeSelect
                                value={row.priceStart}
                                onChange={(e) => patchRow(idx, { priceStart: e.target.value as PricePoint })}
                                className={selectCls}
                              >
                                {PRICE_POINTS.map((p) => (
                                  <option key={p.value} value={p.value}>
                                    {p.label}
                                  </option>
                                ))}
                              </AnimatedNativeSelect>
                              <AnimatedNativeSelect
                                value={row.priceEnd}
                                onChange={(e) => patchRow(idx, { priceEnd: e.target.value as PricePoint })}
                                className={selectCls}
                              >
                                {PRICE_POINTS.map((p) => (
                                  <option key={p.value} value={p.value}>
                                    {p.label}
                                  </option>
                                ))}
                              </AnimatedNativeSelect>
                            </div>
                            <div className="mt-2 text-xs text-muted-foreground">
                              Открытие/Закрытие — значения свечи (open/close). Например: сравни “закрытие” сейчас и “закрытие” N минут назад.
                            </div>
                          </div>

                          <div>
                            <div className={fieldLabel}>Порог срабатывания</div>
                            <div className="mt-2 flex items-center gap-2">
                              <button
                                type="button"
                                onClick={() => patchRow(idx, { negate: true })}
                                disabled={!!row.negate}
                                className={
                                  "rounded-xl border border-border px-3 py-2 text-sm font-semibold " +
                                  (row.negate
                                    ? "bg-rose-500/15 text-destructive border-rose-500/30"
                                    : "bg-background text-foreground hover:bg-secondary")
                                }
                                title="Логическое НЕ (NOT): инвертирует условие"
                              >
                                НЕ
                              </button>
                              {row.negate ? (
                                <button
                                  type="button"
                                  onClick={() => patchRow(idx, { negate: false })}
                                  className="rounded-xl border border-border bg-background px-2.5 py-2 text-sm text-foreground hover:bg-secondary"
                                  title="Убрать НЕ"
                                >
                                  ×
                                </button>
                              ) : null}

                              <input
                                type="number"
                                min={0}
                                max={300}
                                value={row.percent}
                                onChange={(e) =>
                                  patchRow(idx, {
                                    percent: clamp(Number(e.target.value), 0, 300),
                                  })
                                }
                                className={inputCls}
                              />
                              <span className="text-sm text-muted-foreground">%</span>
                            </div>
                            <input
                              type="range"
                              min={0}
                              max={300}
                              value={row.percent}
                              onChange={(e) =>
                                patchRow(idx, {
                                  percent: clamp(Number(e.target.value), 0, 300),
                                })
                              }
                              className={rangeCls}
                              style={{
                                "--value": row.percent,
                                "--min": 0,
                                "--max": 300,
                              } as React.CSSProperties}
                            />
                            <div className="mt-1 text-xs text-muted-foreground">
                              {row.negate
                                ? "НЕ: условие сработает, когда изменение будет НЕ больше порога (т.е. меньше или равно)."
                                : "0 … 300"}
                            </div>
                          </div>
                        </div>
                      </>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>

      <style>{`
        @keyframes scan {
          from { transform: translateY(0); }
          to { transform: translateY(220%); }
        }

        .scanner-range {
          -webkit-appearance: none;
          appearance: none;
          height: 8px;
          border-radius: 999px;
          background: var(--secondary);
          outline: none;
        }

        .scanner-range::-webkit-slider-thumb {
          -webkit-appearance: none;
          appearance: none;
          width: 18px;
          height: 18px;
          border-radius: 999px;
          background: var(--primary);
          border: 1px solid var(--ring);
          cursor: pointer;
        }

        .scanner-range::-moz-range-thumb {
          width: 18px;
          height: 18px;
          border-radius: 999px;
          background: var(--primary);
          border: 1px solid var(--ring);
          cursor: pointer;
        }

        .scanner-range::-moz-range-track {
          height: 8px;
          border-radius: 999px;
          background: var(--secondary);
        }
      `}</style>
    </div>
  );
}
