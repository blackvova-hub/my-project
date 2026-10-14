import test from "node:test";
import assert from "node:assert/strict";
import {
  calendarRange,
  dateInZone,
  yearOverYear,
  normalizeObservations,
  readReadingState,
  safeExternalURL,
  reactionSeries,
  calendarICS,
  comparisonObservations,
  macroFetchRange,
  rollingNewsFrom,
  assetDestination,
  isStoredArticle,
} from "../src/pages/public/news-macro/model.ts";
import {
  stamp,
  importanceReasonLabel,
} from "../src/pages/public/news-macro/format.ts";

test("calendar covers DST spring day with 23 hours and autumn day with 25", () => {
  const spring = calendarRange("2026-03-08", "today", "America/New_York");
  assert.equal(spring.from, "2026-03-08T05:00:00.000Z");
  assert.equal(spring.to, "2026-03-09T04:00:00.000Z");
  const autumn = calendarRange("2026-11-01", "today", "America/New_York");
  assert.equal(Date.parse(autumn.to) - Date.parse(autumn.from), 25 * 3600000);
});
test("calendar week starts on Monday and month handles leap year", () => {
  const week = calendarRange("2026-10-14", "week", "Asia/Vladivostok");
  assert.equal(week.first, "2026-10-12");
  assert.equal(week.from, "2026-10-11T14:00:00.000Z");
  assert.equal(week.last, "2026-10-18");
  const month = calendarRange("2028-02-16", "month", "UTC");
  assert.equal(month.last, "2028-02-29");
  assert.equal(
    dateInZone(new Date("2026-10-13T16:00:00Z"), "Asia/Vladivostok"),
    "2026-10-14",
  );
});
test("normalization preserves gaps and excludes zero/absent baselines", () => {
  assert.deepEqual(
    normalizeObservations([
      { date: "2026-01-01", value: null },
      { date: "2026-01-02", value: 100 },
      { date: "2026-01-03", value: 110 },
    ]).map((x) => x.value),
    [null, 0, 10.000000000000009],
  );
  assert.deepEqual(
    normalizeObservations([{ date: "2026-01-01", value: 0 }]),
    [],
  );
});
test("year over year uses matching calendar month, never prior observation", () => {
  const result = yearOverYear([
    { date: "2025-01-01", value: 100 },
    { date: "2025-03-01", value: 150 },
    { date: "2026-01-01", value: 110 },
    { date: "2026-02-01", value: 120 },
  ]);
  assert.equal(result[3].value, null);
  assert.ok(Math.abs(result[2].value - 10) < 0.001);
});
test("reading schema rejects corruption, unknown versions, unsafe IDs", () => {
  assert.deepEqual(readReadingState("bad").saved, []);
  assert.deepEqual(readReadingState('{"version":2,"saved":["a"]}').saved, []);
  assert.deepEqual(
    readReadingState('{"version":1,"saved":["a","a",123],"read":null}').saved,
    ["a"],
  );
  assert.equal(safeExternalURL("javascript:alert(1)"), null);
  assert.equal(
    safeExternalURL("https://example.com/news"),
    "https://example.com/news",
  );
});
test("reaction baseline uses last fully closed candle before event and refuses gaps", () => {
  const result = reactionSeries(
    [
      { time: 600, close: 100 },
      { time: 900, close: 120 },
      { time: 1200, close: 110 },
    ],
    1000,
  );
  assert.equal(result[0].value, 0);
  assert.ok(Math.abs(result[1].value - 20) < 0.001);
  assert.deepEqual(
    reactionSeries(
      [
        { time: 100, close: 100 },
        { time: 1200, close: 110 },
      ],
      1200,
    ),
    [],
  );
});
test("calendar export contains UTC time and escapes user visible text", () => {
  const text = calendarICS({
    id: "cpi",
    title: "CPI, USA",
    description: "Line one\nLine two",
    scheduledAt: "2026-10-14T12:30:00Z",
    sourceUrl: "https://bls.gov",
  });
  assert.ok(text.includes("DTSTART:20261014T123000Z"));
  assert.ok(text.includes("SUMMARY:CPI\\, USA"));
  assert.equal(
    calendarICS({
      id: "x",
      title: "x",
      description: "",
      scheduledAt: null,
      sourceUrl: "",
    }),
    null,
  );
});
test("comparison shares reference date and refuses nonpositive baselines", () => {
  const result = comparisonObservations(
    [
      { date: "2026-01-01", value: 100 },
      { date: "2026-02-01", value: 110 },
      { date: "2026-03-01", value: 120 },
    ],
    [
      { date: "2026-01-15", value: 200 },
      { date: "2026-02-15", value: 220 },
      { date: "2026-03-15", value: 230 },
    ],
  );
  assert.equal(result.date, "2026-01-15");
  assert.equal(result.leftBase.date, "2026-01-01");
  assert.equal(result.rightBase.date, "2026-01-15");
  assert.ok(Math.abs(result.left[0].value - 10) < 0.001);
  assert.equal(
    comparisonObservations(
      [{ date: "2026-01-01", value: -1 }],
      [{ date: "2026-01-01", value: 2 }],
    ),
    null,
  );
});
test("comparison refuses disjoint history and a one-point overlap", () => {
  assert.equal(
    comparisonObservations(
      [
        { date: "2025-01-01", value: 100 },
        { date: "2025-02-01", value: 110 },
      ],
      [
        { date: "2026-01-01", value: 100 },
        { date: "2026-02-01", value: 110 },
      ],
    ),
    null,
  );
  assert.equal(
    comparisonObservations(
      [
        { date: "2026-01-01", value: 100 },
        { date: "2026-02-01", value: 110 },
      ],
      [
        { date: "2026-01-15", value: 100 },
        { date: "2026-02-15", value: 110 },
      ],
    ),
    null,
  );
});
test("five-year inflation display retains warm-up year for all sixty observations", () => {
  assert.equal(macroFetchRange("5y", true), "all");
  const points = Array.from({ length: 72 }, (_, index) => {
    const date = new Date(Date.UTC(2020, index, 1));
    return { date: date.toISOString().slice(0, 10), value: 100 + index };
  });
  const fiveYearValues = yearOverYear(points).filter(
    (point) => point.date >= "2021-01-01",
  );
  assert.equal(fiveYearValues.length, 60);
  assert.equal(
    fiveYearValues.filter((point) => point.value !== null).length,
    60,
  );
  assert.ok(Math.abs(fiveYearValues[0].value - 12) < 0.001);
});
test("rolling news cutoff advances with minutes rather than extending selected period", () => {
  const minute = Date.parse("2026-10-14T10:00:00Z") / 60000;
  assert.equal(rollingNewsFrom("24", minute), "2026-10-13T10:00:00.000Z");
  assert.equal(rollingNewsFrom("24", minute + 120), "2026-10-13T12:00:00.000Z");
  assert.equal(rollingNewsFrom("", minute), "");
});
test("related fiat and unsupported assets never become fabricated crypto pairs", () => {
  assert.equal(assetDestination("EUR"), "/news/macro?id=ECB_EURUSD");
  assert.equal(assetDestination("USD"), "/news/macro?id=DTWEXBGS");
  assert.equal(
    assetDestination("ETH"),
    "/charts?symbol=ETHUSDT&exchange=bybit&market=spot",
  );
  assert.equal(assetDestination("SPX"), "/news/live?asset=SPX");
});
test("saved article snapshots reject corrupt values that cannot render safely", () => {
  const item = {
    id: "news-a",
    title: "Title",
    url: "https://example.com",
    publishedAt: "2026-10-14T10:00:00Z",
    source: "Official",
    sourceId: "source-a",
    category: "macro",
    language: "en",
    assets: ["USD"],
    tags: [],
    importanceReason: [],
    importance: "low",
    importanceScore: 15,
    breaking: false,
  };
  assert.equal(isStoredArticle(item), true);
  assert.equal(
    isStoredArticle({ ...item, summary: { text: "broken" } }),
    false,
  );
  assert.equal(isStoredArticle({ ...item, publishedAt: "bad date" }), false);
  assert.equal(
    isStoredArticle({ ...item, importanceScore: { value: 15 } }),
    false,
  );
});
test("calendar export folds UTF-8 long text within RFC5545 limit without losing characters", () => {
  const title = "Индекс потребительских цен ".repeat(8);
  const text = calendarICS({
    id: "cpi",
    title,
    description: "Строка\rс возвратом",
    scheduledAt: "2026-10-14T12:30:00Z",
    sourceUrl: "https://bls.gov",
  });
  for (const line of text.split("\r\n"))
    assert.ok(new TextEncoder().encode(line).length <= 75);
  const unfolded = text.replace(/\r\n /g, "");
  assert.ok(unfolded.includes(`SUMMARY:${title}`));
  assert.ok(unfolded.includes("DESCRIPTION:Строка\\nс возвратом"));
  assert.ok(!unfolded.includes("DTEND:"));
});
test("date-only observations preserve precision while exact timestamps respect selected timezone", () => {
  assert.equal(
    stamp("2026-10-09", "Asia/Vladivostok"),
    stamp("2026-10-09", "America/New_York"),
  );
  assert.ok(!/\d{2}:\d{2}/.test(stamp("2026-10-09", "Asia/Vladivostok")));
  assert.ok(
    stamp("2026-10-14T12:30:00Z", "America/New_York").includes("08:30"),
  );
  assert.ok(
    stamp("2026-10-14T12:30:00Z", "Asia/Vladivostok").includes("22:30"),
  );
  assert.ok(
    importanceReasonLabel("official_source:+15").includes(
      "Официальный источник: +15",
    ),
  );
  assert.ok(
    !importanceReasonLabel("unknown_rule:+20").includes("unknown_rule"),
  );
});
