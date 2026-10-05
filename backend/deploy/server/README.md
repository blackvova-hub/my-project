# Server01 deployment

Deployed 2026-09-21 to `server@192.168.0.124`, Ubuntu 26.04.1.
Application: <http://192.168.0.124> (LAN only, HTTP).
Administrator: `admin@local.dev`; password is in root-readable
`/opt/shortlong/admin-access.json`. Email/password login, email verified,
admin role and Pro subscription; no CAPTCHA or second factor on this LAN setup.
Do not expose this configuration publicly without TLS and stronger credentials.

## Layout

- Source: `/opt/shortlong`; compose wrapper: `backend/deploy/server/compose.sh`.
- Docker, application and PostgreSQL: main Plextor SSD (`/var/lib/shortlong/postgres`).
- Compressed ClickHouse candle archive: HDD `/srv/archive/shortlong-clickhouse`.
- `/srv/fast` (Apacer SSD) is not used or mounted by any project container.
- Docker starts at boot; services use restart policies. Docker's systemd drop-in
  requires the archive mount before starting containers.
- Database/service ports are internal; frontend listens on `192.168.0.124:80`.
- Passwords are generated independently in `backend/.env` (mode 0600).

## Archive scope and operation

The archive downloads closed **1-minute OHLCV** candles from Bybit and Binance,
for currently listed USDT spot pairs and USDT perpetual contracts. Initial
catalog: Binance spot 483 / perpetual 510; Bybit spot 389 / perpetual 761.
The catalog refreshes every 12 hours. This is not a promise of delisted pairs,
all quote currencies, inverse/delivery contracts or all-time history.

Initial history target is the preceding **12 months**, limited by listing date
where supplied by the exchange. Backfill runs in the background, newest first,
with per-series persistent progress and retryable source gaps. Continuous
archive catch-up is scheduled hourly, in batches to reduce HDD random writes;
this does not change the 1-minute resolution. Interactive market charts use
exchange data and open at the 1-minute interval.

Raw production storage is `shortlong_backtest.candles_1m` only. Production had
no legacy `candles` table to delete; the old 5-minute ingestion process is not
running. Other intervals are aggregated from minute candles on demand, not
stored as a second raw archive. Existing backtest/similarity analysis remains
Bybit-based and accepts its supported analytical intervals (5m and above).
Rows are isolated by exchange, market and symbol; replay is deduplicated.

Eight archive workers share exchange-specific rate limits. Progress advances
only after durable ClickHouse acknowledgement. Startup resumes persisted
progress. Source holes are reported separately and retried, never filled with
invented candles. `processedRows` is ingestion progress, not a unique-row count.

## Operations

Run on the server:

```sh
sudo /opt/shortlong/backend/deploy/server/status.sh
sudo /opt/shortlong/backend/deploy/server/compose.sh logs --tail=100 market_history
sudo /opt/shortlong/backend/deploy/server/compose.sh restart market_history
sudo /opt/shortlong/backend/deploy/server/compose.sh up -d --build
```

Twenty production services cover the web app, PostgreSQL/Redis/ClickHouse/
Qdrant, exchange scanners, alert workers, history, backtests, similarity and
news ingestion. An empty alert-rule list is normal for the fresh installation.
Scanners need their historical warm-up before all derived windows are ready.

The external Timeweb agent returned `agent_not_found`; its IDs are disabled in
the server override. RSS/news ingestion remains enabled; provider-assisted
summaries require working agent credentials. Telegram account polling and
external news-channel publishing are intentionally not enabled; publishing is
an explicit `telegram-publish` compose profile to avoid replay into an existing
channel. No external Telegram message was sent as a deployment test.

## Verification

Reports are under `/opt/shortlong/reports`:

- `go-tests-final.log`: `go test` and `go vet` for all seven Go modules; fresh
  migrations and archive/backtest/similarity integration tests against isolated
  PostgreSQL, Redis, ClickHouse and Qdrant. Other optional integrations without
  their dedicated environment remain subject to their normal skip conditions.
- `frontend-unit-tests.log`: 21 passed, no failures or skips.
- `live-api-checks.json`: authenticated login; a real BTC minute checked against
  each of four exchange/market combinations; completed spot/perpetual Bybit
  backtests; completed similarity run with charts; production table inventory.
- `archive-before-restart.json` / `archive-after-restart.json`: durable progress
  and live resume check.
- `browser-checks.json` and screenshots: real browser login, BTC watchlist,
  default 1m selected with live exchange candles, backtest builder and mobile
  layout. Browser ran as a client on the workstation against the server only;
  it did not start local application services. API/frontend default settings
  were aligned to 1m; `chart-default-test.log` covers the API regression check.

Frontend production build passed using the same native dependency workaround
as its Dockerfile. Test services run only on the server and were stopped after
verification. No application services were started on the workstation.

Regular off-machine backups, public TLS/domain deployment and full-history
completion are not claimed by this initial deployment. Monitor available disk
space and source-gap counts as the first backfill progresses.
