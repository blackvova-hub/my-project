// Aligned vectors retain warm-up gaps as NaN; no future input or zero padding.
export const gaps = (length: number): number[] =>
  Array(length).fill(Number.NaN);
export function mean(x: readonly number[], n: number) {
  const out = gaps(x.length);
  let sum = 0,
    count = 0;
  for (let i = 0; i < x.length; i++) {
    if (Number.isFinite(x[i])) {
      sum += x[i];
      count++;
    }
    if (i >= n && Number.isFinite(x[i - n])) {
      sum -= x[i - n];
      count--;
    }
    if (i >= n - 1 && count === n) out[i] = sum / n;
  }
  return out;
}
export const sum = (x: readonly number[], n: number) =>
  mean(x, n).map((v) => v * n);
export function smooth(x: readonly number[], n: number, wilder = false) {
  const seed = mean(x, n),
    out = gaps(x.length),
    alpha = wilder ? 1 / n : 2 / (n + 1);
  let prev = Number.NaN;
  for (let i = 0; i < x.length; i++) {
    if (!Number.isFinite(x[i])) {
      prev = Number.NaN;
      continue;
    }
    prev = Number.isFinite(prev) ? prev + alpha * (x[i] - prev) : seed[i];
    out[i] = prev;
  }
  return out;
}
export function weighted(x: readonly number[], n: number) {
  const out = gaps(x.length);
  let total = 0,
    weights = 0,
    count = 0;
  for (let i = 0; i < x.length; i++) {
    if (!Number.isFinite(x[i])) {
      total = 0;
      weights = 0;
      count = 0;
      continue;
    }
    if (count < n) {
      count++;
      total += x[i];
      weights += count * x[i];
    } else {
      weights += n * x[i] - total;
      total += x[i] - x[i - n];
    }
    if (count === n) out[i] = weights / ((n * (n + 1)) / 2);
  }
  return out;
}
export function extrema(x: readonly number[], n: number, high = true) {
  const out = gaps(x.length),
    q: number[] = [];
  let head = 0,
    valid = 0;
  for (let i = 0; i < x.length; i++) {
    if (!Number.isFinite(x[i])) {
      q.length = 0;
      head = 0;
      valid = 0;
      continue;
    }
    valid++;
    while (
      q.length > head &&
      (high ? x[q.at(-1)!] <= x[i] : x[q.at(-1)!] >= x[i])
    )
      q.pop();
    q.push(i);
    while (q[head] <= i - n) head++;
    if (valid >= n) out[i] = x[q[head]];
    if (head > 1024) {
      q.splice(0, head);
      head = 0;
    }
  }
  return out;
}
export function deviation(x: readonly number[], n: number, sample = false) {
  const out = gaps(x.length);
  let count = 0,
    m = 0,
    m2 = 0;
  for (let i = 0; i < x.length; i++) {
    if (i >= n && Number.isFinite(x[i - n])) {
      const old = x[i - n];
      if (count <= 1) {
        count = 0;
        m = 0;
        m2 = 0;
      } else {
        const next = (count * m - old) / (count - 1);
        m2 -= (old - m) * (old - next);
        m = next;
        count--;
      }
    }
    if (Number.isFinite(x[i])) {
      count++;
      const delta = x[i] - m;
      m += delta / count;
      m2 += delta * (x[i] - m);
    }
    if (count === n) out[i] = Math.sqrt(Math.max(0, m2 / (sample ? n - 1 : n)));
  }
  return out;
}
export function change(x: readonly number[], n = 1) {
  return x.map((v, i) => (i >= n ? v - x[i - n] : Number.NaN));
}
export function ratio(
  a: readonly number[],
  b: readonly number[],
  zero = Number.NaN,
) {
  return a.map((v, i) =>
    !Number.isFinite(v) || !Number.isFinite(b[i])
      ? Number.NaN
      : b[i] === 0
        ? zero
        : v / b[i],
  );
}
export const diff = (a: readonly number[], b: readonly number[]) =>
  a.map((v, i) => v - b[i]);
export function rsi(x: readonly number[], n: number) {
  const d = change(x),
    gain = smooth(
      d.map((v) => (Number.isFinite(v) ? Math.max(v, 0) : v)),
      n,
      true,
    ),
    loss = smooth(
      d.map((v) => (Number.isFinite(v) ? Math.max(-v, 0) : v)),
      n,
      true,
    );
  return gain.map((v, i) =>
    !Number.isFinite(v) || !Number.isFinite(loss[i])
      ? Number.NaN
      : loss[i] === 0
        ? v === 0
          ? 50
          : 100
        : 100 - 100 / (1 + v / loss[i]),
  );
}
export const roc = (x: readonly number[], n: number) =>
  x.map((v, i) =>
    i >= n && x[i - n] !== 0 ? (v / x[i - n] - 1) * 100 : Number.NaN,
  );
export function trueRange(
  high: readonly number[],
  low: readonly number[],
  close: readonly number[],
) {
  return high.map((v, i) =>
    i
      ? Math.max(
          v - low[i],
          Math.abs(v - close[i - 1]),
          Math.abs(low[i] - close[i - 1]),
        )
      : v - low[i],
  );
}
export function cumulative(x: readonly number[]) {
  let total = 0;
  return x.map((v) => (Number.isFinite(v) ? (total += v) : Number.NaN));
}
export function kernel(x: readonly number[], weights: readonly number[]) {
  const n = weights.length,
    den = weights.reduce((a, b) => a + b, 0),
    out = gaps(x.length);
  for (let i = n - 1; i < x.length; i++) {
    let value = 0;
    for (let j = 0; j < n; j++) value += x[i - n + 1 + j] * weights[j];
    out[i] = value / den;
  }
  return out;
}
