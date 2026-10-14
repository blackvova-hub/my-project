# News & Macro data adapters

Integrated into the existing Go API as `macroapi.New(pool)`, `Run(ctx)` and
`Routes(router)`. A nil pool supports a standalone preview with the same real
upstream adapters. No runtime fixtures, generated observations or consensus
forecasts are used.

## API

- `GET /api/macro/overview`: catalog with 90 most recent points per series,
  recent/upcoming events, source health and retrieval timestamp.
- `GET /api/macro/series?id=CPIAUCSL&range=1y|2y|5y|all`: available historical
  observations. Points contain source observation dates, not release timestamps.
- `GET /api/macro/calendar?from=...&to=...`: RFC3339 interval `[from,to)`, including
  offsets for DST-aware local days. Two date-only arguments use inclusive UTC
  dates. Maximum range 370 days. Exact timestamps remain UTC; FOMC dates with
  no published time remain date-only.
- `GET /api/macro/markets`: actual BTC/ETH exchange quotes, official daily macro
  observations, optional `sentiment` object with Alternative.me Bitcoin Fear &
  Greed, and explicit unavailable states for unlicensed assets. `asOf` means the
  actual latest successful collection of the relevant sources, never the HTTP
  response clock; it is empty until a provider successfully synchronizes.

Calendar `actual`, `previous` and `forecast` remain null: the subscribed official
calendars do not provide verified release vintages or consensus estimates.
`seriesId` and an explicitly supplied `referencePeriod` link to historical
observations. A passed schedule has status `elapsed`, not proof of publication.
Changes to a stable event UID retain the previous schedule and show postponed.
Historical macro charts contain the latest available revised observations.
The revision table records **when this collector detected a revision**, not the
original publication vintage; it cannot support claims about historical
surprise or price reactions without a separate verified event study.

## Sources and access checks

Live access checked on 14 October 2026 with the actual adapters: all 22 macro
series, 534 calendar entries and BTC/ETH quotes were available. Availability is
dynamic; the interface exposes failures and keeps the last successful cache.

| Source | Access and usage |
| --- | --- |
| [FRED](https://fred.stlouisfed.org/docs/api/fred/) | Primary official government-series catalog. With `FRED_API_KEY`, use observations API (free registration), serialized below one request/second. Without a key, use two fixed [FRED Graph CSV downloads](https://fred.stlouisfed.org/graph/fredgraph.csv?id=CPIAUCSL) per six hours. FRED limits graphs to 12 lines and returns a ZIP of CSV files for mixed frequencies; bounded ZIP parsing is included. No discovery, crawling or full database mirroring. |
| [FRED legal terms](https://fred.stlouisfed.org/legal/terms/) | Preserve original provider and FRED attribution. Use this fixed subset of government/public-domain series. Redistribution of proprietary third-party series for commercial use requires their owner's permission. Do not imply FRED endorsement. The custom static explanations do not train or query an AI model using FRED content. |
| [BLS Public Data API](https://www.bls.gov/developers/api_signature_v2.htm) | Direct fallback for CPI, core CPI, unemployment and payroll employment. One bundled query per six hours (at most four/day), up to ten years of history. Optional `BLS_API_KEY` stays server-side. [Unregistered limits](https://www.bls.gov/developers/api_faqs.htm): 25 queries/day, 25 series/query, ten years/query. |
| [BLS calendar](https://www.bls.gov/help/hlpiCAL.htm) | Official [ICS subscription](https://www.bls.gov/schedule/news_release/bls.ics), refreshed hourly. It provides actual schedules, not forecasts/actual values. Declared `US-Eastern` VTIMEZONE is mapped to America/New_York, including DST. |
| [BEA calendar](https://www.bea.gov/news/schedule/icalendar) | Official [ICS subscription](https://www.bea.gov/news/schedule/ics/online-calendar-subscription.ics), refreshed hourly. Contains exact UTC schedules for GDP, personal income/PCE and other releases; overlapping feeds are normalized and deduplicated. |
| [Federal Reserve](https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm) | Official meeting calendar, refreshed hourly. Final meeting day is stored as a date; no intraday decision time is invented. [Board disclaimer](https://www.federalreserve.gov/disclaimer.htm): Board material is public domain unless otherwise indicated. |
| [ECB SDMX](https://data.ecb.europa.eu/help/api/data) | Free official CSV API; no key. Deposit facility rate at dates of changes and EUR/USD daily reference fixing. [ESCB reuse policy](https://www.ecb.europa.eu/stats/ecb_statistics/governance_and_quality_framework/html/usage_policy.en.html) permits reuse of standard-format statistics with attribution. These are reference observations, not realtime trading quotes. |
| [Bybit](https://bybit-exchange.github.io/docs/v5/market/tickers) / [Binance](https://developers.binance.com/docs/binance-spot-api-docs/rest-api/market-data-endpoints) | Public spot market ticker and daily OHLC endpoints, no secret credentials. Fixed BTC/ETH pairs, low request volume, five-minute refresh. Binance provides fallback if Bybit fails. USDT quote currency and exchange are retained. Recheck exchange data terms for any downstream resale or extended redistribution. |
| [Alternative.me](https://alternative.me/crypto/fear-and-greed-index/#api) | Public Fear & Greed API. The source explicitly allows commercial display with attribution immediately next to the data, without impersonating the provider. One fixed request/hour, 90 daily observations; no key. Value/classification/timestamp are obtained from the API, validated, and never inferred from price movement. Daily index is Bitcoin-specific, not sentiment for all assets. Freshness limit 48 hours. Verified live: value 62, classification Greed, observed 14 October 2026 00:00 UTC. |

S&P 500, NASDAQ 100, ICE DXY and gold are **not** substituted with fake quotes,
ETFs, crypto gold tokens or visually similar series. A permitted quote feed is
needed to populate them. `DTWEXBGS` is the Federal Reserve broad trade-weighted
dollar index and is explicitly described as distinct from DXY.

## Reliability, persistence and quotas

- Bounded HTTP timeout/body size and ZIP expansion, finite numeric validation,
  observation/date deduplication, no retries that amplify provider errors.
- Upstream calls happen only in `Run`. HTTP reads do not consume provider quota.
- Frequency-aware observation age: daily seven days, weekly 21 days, monthly
  80 days, quarterly 200 days; rate-at-change series has no artificial expiry.
- Last successful snapshot persists in `news_macro_cache`, with normalized
  history and changed observations in the migration `071_news_macro.sql`.
- Series/date composite index supports history ranges. Unchanged observations
  do not rewrite database rows; histories are only persisted after new series
  retrievals. Prior attempts retain rate limits across process restarts.
- Request URLs containing keys never enter error messages or source metadata.
- Failed calendar requests retry after five minutes; successful schedules remain
  hourly. FRED/BLS observation budgets retain their six-hour interval. Attempts
  survive process restarts. BEA availability must be checked from the deployed
  API container: successful access from the host alone does not prove it.
- Source failure is separate from missing data. No zero-price or zero-forecast
  defaults are used. Lost provider access keeps timestamped stale values.

## Verification

`go test ./internal/httpapi/macro` checks CSV/missing values/annual averages,
mixed-frequency ZIPs, timezone aliases/DST, date-only events, exclusive date
boundaries, duplicate and postponed events, cache-only routes, observation age,
credential redaction and exchange quote normalization.

The opt-in `MACRO_TEST_DATABASE_URL` test is restricted to the isolated local
`news_macro_terminal_test` database at port 25432. It creates a private temporary
schema, verifies unchanged observations produce no revisions, changed history
produces one revision, and cache/source/sentiment restore works; it does not
modify the preview schema. `SENTIMENT_LIVE_TEST=1` checks only Alternative.me
without requesting the other providers again.

`MACRO_LIVE_TEST=1 go test -run TestLiveProviders -v ./internal/httpapi/macro`
performs an opt-in live provider smoke check. Run it sparingly because it
consumes real source quotas. Race checking requires a CGo-capable Go toolchain.
