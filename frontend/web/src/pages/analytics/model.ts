import type {
  Dataset,
  Dimension,
  Insight,
  Ledger,
  Range,
  Snapshot,
  Trade,
  TradeFilter,
} from "./types";
import { displayLabel } from "./locale.ts";

export const DAY = 86_400_000;
export const ranges: Range[] = ["7D", "30D", "90D", "YTD", "1Y", "ALL"];
export const weekdays = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];
export const categories: Record<string, string> = {
  trading: "Торговая прибыль",
  rewards: "Вознаграждения и начисления",
  fees: "Торговые комиссии",
  funding: "Финансирование",
  funding_received: "Получено за финансирование",
  funding_paid: "Уплачено за финансирование",
  interest: "Проценты по займу",
  withdrawal_fee: "Комиссии за вывод",
  deposit: "Пополнения",
  withdrawal: "Выводы",
  transfer: "Переводы между счетами",
  other: "Прочие изменения",
  asset_exchange: "Обмен активов на споте",
};
export const sum = <T>(items: T[], fn: (v: T) => number) =>
  items.reduce((n, v) => n + fn(v), 0);
export const money = (
  n: number | null | undefined,
  signed = false,
  digits = 2,
) =>
  n == null || !Number.isFinite(n)
    ? "—"
    : `${n < 0 ? "−" : signed && n > 0 ? "+" : ""}${Math.abs(n).toLocaleString("ru-RU", { minimumFractionDigits: digits, maximumFractionDigits: digits })}\u00a0$`;
export const pct = (n: number | null | undefined) =>
  n == null || !Number.isFinite(n)
    ? "—"
    : `${n.toLocaleString("ru-RU", { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%`;
export const number = (n: number | null | undefined, digits = 2) =>
  n == null
    ? "—"
    : !Number.isFinite(n)
      ? "∞"
      : n.toLocaleString("ru-RU", { maximumFractionDigits: digits });
export const dateKey = (at: number) => new Date(at).toISOString().slice(0, 10);
export const timestamp = (at: number) =>
  new Date(at).toLocaleString("ru-RU", {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    timeZone: "UTC",
  });
export const duration = (minutes: number) =>
  minutes < 60
    ? `${Math.round(minutes)} мин`
    : `${Math.floor(minutes / 60)} ч ${Math.round(minutes % 60)} мин`;
export function rangeStart(range: Range, now: number) {
  if (range === "ALL") return 0;
  if (range === "YTD") return Date.UTC(new Date(now).getUTCFullYear(), 0, 1);
  return (
    now - ({ "7D": 7, "30D": 30, "90D": 90, "1Y": 365 }[range] ?? 30) * DAY
  );
}
export function dimension(t: Trade, key: Dimension) {
  const d = new Date(t.openedAt);
  switch (key) {
    case "weekday":
      return weekdays[(d.getUTCDay() + 6) % 7];
    case "hour": {
      const h = Math.floor(d.getUTCHours() / 4) * 4;
      return `${String(h).padStart(2, "0")}–${String(h + 4).padStart(2, "0")}`;
    }
    case "leverage":
      return t.leverage == null
        ? "Неизвестно"
        : t.leverage < 3
          ? "1–3x"
          : t.leverage < 5
            ? "3–5x"
            : t.leverage < 10
              ? "5–10x"
              : "10x+";
    case "duration":
      return t.duration < 5
        ? "< 5 мин"
        : t.duration < 30
          ? "5–30 мин"
          : t.duration < 120
            ? "30 мин–2 ч"
            : t.duration < 480
              ? "2–8 ч"
              : "8 ч и более";
    case "size":
      return t.size < 1000
        ? "< 1 тыс. $"
        : t.size < 10000
          ? "1–10 тыс. $"
          : t.size < 50000
            ? "10–50 тыс. $"
            : "50 тыс. $ и более";
    default:
      return t[key];
  }
}
export function filterTrades(trades: Trade[], f: TradeFilter) {
  const ids = f.ids ? new Set(f.ids) : null;
  return trades.filter(
    (t) =>
      (!f.exchange || t.exchange === f.exchange) &&
      (!f.symbol || t.symbol === f.symbol) &&
      (!f.side || t.side === f.side) &&
      (!f.result || (f.result === "profit" ? t.net > 0 : t.net < 0)) &&
      (!f.leverage || dimension(t, "leverage") === f.leverage) &&
      (!f.duration || dimension(t, "duration") === f.duration) &&
      (!f.date || dateKey(t.closedAt ?? t.openedAt) === f.date) &&
      (!f.tag || t.tag.toLowerCase().includes(f.tag.toLowerCase())) &&
      (!f.strategy ||
        t.strategy.toLowerCase().includes(f.strategy.toLowerCase())) &&
      (!f.weekday || dimension(t, "weekday") === f.weekday) &&
      (!f.hour || dimension(t, "hour") === f.hour) &&
      (!f.size || dimension(t, "size") === f.size) &&
      (!ids || ids.has(t.id)),
  );
}
export function stats(trades: Trade[]) {
  const closed = trades.filter((t) => t.closedAt != null && t.complete);
  const wins = closed.filter((t) => t.net > 0),
    losses = closed.filter((t) => t.net < 0);
  const winning = sum(wins, (t) => t.net),
    losing = -sum(losses, (t) => t.net);
  const avgWin = wins.length ? winning / wins.length : null,
    avgLoss = losses.length ? -losing / losses.length : null;
  const net = sum(closed, (t) => t.net);
  const daily = new Map<string, number>();
  closed.forEach((t) => {
    const key = dateKey(t.closedAt!);
    daily.set(key, (daily.get(key) ?? 0) + t.net);
  });
  const days = [...daily.values()];
  return {
    n: closed.length,
    net,
    gross: sum(closed, (t) => t.gross),
    fees: sum(closed, (t) => t.fees),
    funding: sum(closed, (t) => t.funding),
    wins: wins.length,
    losses: losses.length,
    winRate: closed.length ? (wins.length / closed.length) * 100 : null,
    pf: losing ? winning / losing : winning ? Infinity : null,
    avgWin,
    avgLoss,
    payoff: avgWin != null && avgLoss ? avgWin / -avgLoss : null,
    expectancy: closed.length ? net / closed.length : null,
    best: closed.length ? Math.max(...closed.map((t) => t.net)) : null,
    worst: closed.length ? Math.min(...closed.map((t) => t.net)) : null,
    bestDay: days.length ? Math.max(...days) : null,
    worstDay: days.length ? Math.min(...days) : null,
  };
}
export function group(trades: Trade[], key: Dimension) {
  const map = new Map<string, Trade[]>();
  trades.forEach((t) => {
    const k = dimension(t, key);
    const bucket = map.get(k) ?? [];
    bucket.push(t);
    map.set(k, bucket);
  });
  return [...map]
    .map(([label, items]) => ({ label, trades: items, ...stats(items) }))
    .sort((a, b) => b.net - a.net);
}
export const isCashflow = (category: string) =>
  ["deposit", "withdrawal", "transfer"].includes(category);
export const isUSD = (l: Ledger) =>
  ["USDT", "USD", "USDC"].includes(l.currency);
export function selectData(data: Dataset, account: string, range: Range) {
  const start = rangeStart(range, data.serverTime);
  const connections = data.connections.filter(
    (c) => !account || c.id === account,
  );
  const ids = new Set(connections.map((c) => c.id));
  const allTrades = data.trades.filter((t) => ids.has(t.connectionId));
  const allLedger = data.ledger.filter((l) => ids.has(l.connectionId));
  const ledger = allLedger.filter(
    (l) => l.at >= start && l.at <= data.serverTime && isUSD(l),
  );
  const trades = allTrades.filter(
    (t) =>
      (t.closedAt ?? t.openedAt) >= start &&
      (t.closedAt ?? t.openedAt) <= data.serverTime,
  );
  const snapshots = data.snapshots
    .filter((s) => ids.has(s.connectionId))
    .sort((a, b) => a.at - b.at);
  return {
    start,
    connections,
    allTrades,
    allLedger,
    ledger,
    trades,
    snapshots,
  };
}
export type EquityPoint = {
  at: number;
  observedAt: Record<string, number>;
  equity: number;
  wallet: number;
  invested: number;
  benchmark: number | null;
  net: number;
  returnPct: number;
  drawdown: number;
};
export function equitySeries(
  snapshots: Snapshot[],
  ledger: Ledger[],
  connectionCount: number,
  start: number,
): EquityPoint[] {
  const latest = new Map<string, Snapshot>();
  const points: EquityPoint[] = [];
  let initial = 0,
    flow = 0,
    unitsReturn = 1,
    peakReturn = 1;
  let previousEquity = 0;
  const cash = ledger
    .filter((l) => isUSD(l) && isCashflow(l.category))
    .sort((a, b) => a.at - b.at);
  const cashByAccount = new Map<string, Ledger[]>();
  const cashIndex = new Map<string, number>();
  cash.forEach((l) => {
    const list = cashByAccount.get(l.connectionId) ?? [];
    list.push(l);
    cashByAccount.set(l.connectionId, list);
  });
  let firstBTC: number | null = null;
  const grouped = new Map<number, Snapshot[]>();
  snapshots.forEach((s) => {
    const bucket = grouped.get(s.at) ?? [];
    bucket.push(s);
    grouped.set(s.at, bucket);
  });
  for (const [at, rows] of grouped) {
    let intervalFlow = 0;
    rows.forEach((s) => {
      const previous = latest.get(s.connectionId);
      const accountCash = cashByAccount.get(s.connectionId) ?? [];
      let index = cashIndex.get(s.connectionId) ?? 0;
      while (index < accountCash.length && accountCash[index].at <= s.at) {
        if (points.length && previous && accountCash[index].at > previous.at)
          intervalFlow += accountCash[index].amount;
        index++;
      }
      cashIndex.set(s.connectionId, index);
      latest.set(s.connectionId, s);
    });
    if (latest.size < connectionCount || !connectionCount) continue;
    const values = [...latest.values()],
      equity = sum(values, (s) => s.equity),
      wallet = sum(values, (s) => s.wallet);
    const btc =
      rows.find((s) => s.btcPrice != null)?.btcPrice ??
      values.find((s) => s.btcPrice != null)?.btcPrice ??
      null;
    if (!points.length) {
      initial = equity;
      firstBTC = btc;
    }
    flow += intervalFlow;
    if (points.length && previousEquity > 0)
      unitsReturn *= Math.max(0, (equity - intervalFlow) / previousEquity);
    peakReturn = Math.max(peakReturn, unitsReturn);
    points.push({
      at,
      observedAt: Object.fromEntries(values.map((s) => [s.connectionId, s.at])),
      equity,
      wallet,
      invested: initial + flow,
      benchmark: btc && firstBTC ? (initial * btc) / firstBTC : null,
      net: equity - initial - flow,
      returnPct: (unitsReturn - 1) * 100,
      drawdown: (unitsReturn / peakReturn - 1) * 100,
    });
    previousEquity = equity;
  }
  const before = points.filter((p) => p.at <= start).at(-1);
  return [...(before ? [before] : []), ...points.filter((p) => p.at > start)];
}
export function drawdown(points: EquityPoint[]) {
  if (!points.length)
    return {
      current: null,
      max: null,
      days: null,
      recovery: null,
      peak: null,
      bottom: null,
      loss: null,
    };
  let max = 0,
    recovery: number | null = null,
    peakAt = points[0].at,
    maxPeakAt = peakAt,
    peakValue = points[0].equity,
    bottom = points[0].equity,
    currentPeakValue = peakValue;
  for (const p of points) {
    if (p.drawdown >= -1e-8) {
      if (max < 0 && recovery == null && p.at > maxPeakAt)
        recovery = Math.round((p.at - maxPeakAt) / DAY);
      peakAt = p.at;
      currentPeakValue = p.equity;
    }
    if (p.drawdown < max) {
      max = p.drawdown;
      maxPeakAt = peakAt;
      peakValue = currentPeakValue;
      bottom = p.equity;
      recovery = null;
    }
  }
  const last = points.at(-1)!;
  return {
    current: last.drawdown,
    max,
    days: last.drawdown < 0 ? Math.floor((last.at - peakAt) / DAY) : 0,
    recovery,
    peak: currentPeakValue,
    bottom,
    loss: last.equity - currentPeakValue,
    maxPeak: peakValue,
  };
}
export function portfolio(data: Dataset, account: string, range: Range) {
  const selected = selectData(data, account, range);
  const series = equitySeries(
    selected.snapshots,
    selected.allLedger,
    selected.connections.length,
    selected.start,
  );
  const latest = new Map<string, Snapshot>();
  selected.snapshots.forEach((s) => latest.set(s.connectionId, s));
  const now = [...latest.values()];
  const equity =
    now.length === selected.connections.length && now.length
      ? sum(now, (s) => s.equity)
      : null;
  const first = series[0],
    last = series.at(-1);
  const baseline = first && last && first.at !== last.at;
  const net = baseline ? last.net - first.net : null;
  const trading = sum(
    selected.ledger.filter((l) => l.category === "trading"),
    (l) => l.amount,
  );
  const costs = -sum(
    selected.ledger.filter(
      (l) =>
        ["fees", "funding", "interest", "withdrawal_fee"].includes(
          l.category,
        ) && l.amount < 0,
    ),
    (l) => l.amount,
  );
  const assets = new Map<string, number>();
  now
    .flatMap((s) => s.assets)
    .forEach((a) =>
      assets.set(a.symbol, (assets.get(a.symbol) ?? 0) + a.value),
    );
  return {
    ...selected,
    series,
    now,
    equity,
    net,
    trading,
    costs,
    assets: [...assets]
      .map(([symbol, value]) => ({ symbol, value }))
      .sort((a, b) => b.value - a.value),
    positions: now.flatMap((s) => s.positions),
    drawdown: drawdown(
      equitySeries(
        selected.snapshots,
        selected.allLedger,
        selected.connections.length,
        0,
      ),
    ),
    stats: stats(selected.trades),
    observedFrom: first?.at ?? null,
  };
}
export type Portfolio = ReturnType<typeof portfolio>;
export function insights(data: Dataset, p: Portfolio): Insight[] {
  const out: Insight[] = [];
  const closed = p.trades.filter((t) => t.closedAt && t.complete);
  const add = (
    type: string,
    category: Insight["category"],
    title: string,
    text: string,
    evidence: string,
    trades: Trade[],
    positive = false,
  ) =>
    out.push({
      id: type,
      type,
      category,
      title,
      text,
      evidence,
      confidence:
        trades.length >= 100
          ? "Высокая"
          : trades.length >= 30
            ? "Средняя"
            : "Низкая",
      sample: trades.length,
      trades: trades.map((t) => t.id),
      positive,
    });
  const symbols = group(closed, "symbol");
  for (const segment of symbols
    .filter((s) => s.n >= 30 && s.net < 0 && (s.pf ?? Infinity) < 0.8)
    .slice(0, 1))
    add(
      "weak-symbol",
      "Торговля",
      `${segment.label} ухудшил результат`,
      `Сделок: ${segment.n}. Чистый результат: ${money(segment.net, true)}, фактор прибыли: ${number(segment.pf)}.`,
      "Правило: не менее 30 сделок, чистый результат ниже нуля, фактор прибыли меньше 0,8.",
      segment.trades,
    );
  const strong = symbols.filter((s) => s.n >= 30 && s.net > 0)[0];
  if (strong)
    add(
      "strong-symbol",
      "Торговля",
      `${strong.label} принёс наибольшую прибыль`,
      `Закрытых сделок: ${strong.n}. Чистый результат: ${money(strong.net, true)}.`,
      "Наибольший чистый результат среди инструментов с 30 сделками и более.",
      strong.trades,
      true,
    );
  const weak = group(closed, "hour").find(
    (s) => s.n >= 30 && s.net < 0 && (s.pf ?? Infinity) < 0.8,
  );
  if (weak)
    add(
      "weak-time",
      "Торговля",
      `Торговля в ${weak.label} UTC была убыточной`,
      `Сделок с открытием в этом интервале: ${weak.n}. Результат: ${money(weak.net, true)}.`,
      "Группировка по времени открытия в UTC: не менее 30 сделок и фактор прибыли меньше 0,8.",
      weak.trades,
    );
  const leveraged = closed.filter((t) => (t.leverage ?? 0) >= 10),
    ls = stats(leveraged);
  if (ls.n >= 30 && (ls.pf ?? Infinity) < 0.8)
    add(
      "leverage",
      "Торговля",
      "Высокое плечо ухудшило результат",
      `Сделок с плечом от 10×: ${ls.n}. Результат: ${money(ls.net, true)}, фактор прибыли: ${number(ls.pf)}.`,
      "Учитываются только сделки с известным плечом на момент открытия.",
      leveraged,
    );
  const captured = closed
    .filter((t) => t.captured != null)
    .sort((a, b) => a.captured! - b.captured!);
  if (captured.length >= 30) {
    const n = captured.length,
      median =
        n % 2
          ? captured[Math.floor(n / 2)].captured!
          : (captured[n / 2 - 1].captured! + captured[n / 2].captured!) / 2;
    if (median < 50)
      add(
        "exit",
        "Торговля",
        "Получено менее половины возможной прибыли",
        `Медианная доля полученной прибыли: ${pct(median)}. Сделок с данными о движении цены: ${n}.`,
        "Медиана отношения прибыли до расходов к максимальной наблюдаемой прибыли внутри сделки.",
        captured,
      );
  }
  const funding = -sum(
    p.ledger.filter((l) => l.category === "funding" && l.amount < 0),
    (l) => l.amount,
  );
  const gross = Math.max(0, p.trading);
  if (gross > 0 && funding / gross > 0.15)
    add(
      "funding",
      "Расходы",
      "Финансирование заметно уменьшает прибыль",
      `Уплачено за финансирование: ${money(funding)}, или ${pct((funding / gross) * 100)} торговой прибыли.`,
      "Уплаченное финансирование превышает 15% положительной торговой прибыли по журналу операций.",
      closed.filter((t) => t.funding < 0),
    );
  const length = data.serverTime - p.start;
  const previous = p.allLedger.filter(
    (l) => l.at >= p.start - length && l.at < p.start && isUSD(l),
  );
  const priorCost = -sum(
    previous.filter(
      (l) =>
        ["fees", "funding", "interest", "withdrawal_fee"].includes(
          l.category,
        ) && l.amount < 0,
    ),
    (l) => l.amount,
  );
  const priorGross = sum(
    previous.filter((l) => l.category === "trading"),
    (l) => l.amount,
  );
  if (
    length > 0 &&
    p.start > 0 &&
    gross > 0 &&
    priorGross > 0 &&
    priorCost > 0 &&
    p.costs / gross > (priorCost / priorGross) * 1.25
  )
    add(
      "cost-spike",
      "Расходы",
      "Доля торговых расходов выросла",
      `Доля расходов в прибыли выросла на ${pct((p.costs / gross / (priorCost / priorGross) - 1) * 100)} относительно предыдущего периода той же длины.`,
      "Доля расходов в прибыли выросла более чем на 25%. Прибыль до расходов положительна в обоих периодах.",
      closed,
    );
  if ((p.drawdown.days ?? 0) >= 7)
    add(
      "drawdown",
      "Риски",
      "Просадка ещё не восстановлена",
      `Дней ниже наблюдаемого максимума стоимости портфеля: ${p.drawdown.days}.`,
      `Просадка без влияния денежных потоков; по снимкам счёта с ${p.observedFrom ? new Date(p.observedFrom).toLocaleDateString("ru-RU", { timeZone: "UTC" }) : "первой синхронизации"}.`,
      closed,
    );
  const exposure = sum(p.positions, (t) => t.notional),
    assetExposure = new Map<string, number>();
  p.positions.forEach((t) =>
    assetExposure.set(
      t.symbol,
      (assetExposure.get(t.symbol) ?? 0) + t.notional,
    ),
  );
  const concentrated = [...assetExposure].find(
    ([, v]) => exposure > 0 && v / exposure > 0.6,
  );
  if (concentrated)
    add(
      "concentration",
      "Риски",
      `${concentrated[0]} занимает основную долю позиций`,
      `На этот актив приходится ${pct((concentrated[1] / exposure) * 100)} текущего общего объёма позиций.`,
      "На актив приходится более 60% общего объёма позиций. Оценка текущего состояния, а не прогноз.",
      closed.filter((t) => t.symbol === concentrated[0]),
    );
  const recent = p.allTrades.filter(
      (t) => t.openedAt >= data.serverTime - 14 * DAY,
    ),
    prior = p.allTrades.filter(
      (t) =>
        t.openedAt >= data.serverTime - 28 * DAY &&
        t.openedAt < data.serverTime - 14 * DAY,
    );
  if (recent.length >= 30 && prior.length >= 30) {
    const a = sum(recent, (t) => t.size) / recent.length,
      b = sum(prior, (t) => t.size) / prior.length;
    if (b > 0 && a / b > 1.3)
      add(
        "behavior",
        "Торговля",
        "Средний размер позиции вырос",
        `Средний объём входа вырос на ${pct((a / b - 1) * 100)} за последние две недели.`,
        "Средний объём входа за последние 14 дней по сравнению с предыдущими 14 днями; не менее 30 сделок в каждом периоде.",
        recent,
      );
  }
  return out;
}
export function csvExport(trades: Trade[]) {
  const cells = (v: unknown) =>
    `"${String(v ?? "")
      .replace(/^[=+@-]/, "'$&")
      .replaceAll('"', '""')}"`;
  const header = [
    "Инструмент",
    "Сторона",
    "Биржа",
    "Цена входа",
    "Цена выхода",
    "Объём",
    "Плечо",
    "До расходов",
    "Комиссии",
    "Финансирование",
    "Чистый результат",
    "Длительность, мин",
    "Открытие, UTC",
    "Закрытие, UTC",
    "Метка",
    "Стратегия",
    "Полная история",
  ];
  const lines = [
    header,
    ...trades.map((t) => [
      t.symbol,
      displayLabel(t.side),
      displayLabel(t.exchange),
      t.entry,
      t.exit,
      t.size,
      t.leverage,
      t.complete ? t.gross : "",
      t.fees,
      t.funding,
      t.complete ? t.net : "",
      t.duration,
      new Date(t.openedAt).toISOString(),
      t.closedAt ? new Date(t.closedAt).toISOString() : "",
      t.tag,
      t.strategy,
      t.complete ? "Да" : "Нет",
    ]),
  ];
  return "\ufeff" + lines.map((line) => line.map(cells).join(",")).join("\r\n");
}
