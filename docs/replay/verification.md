# Historical replay verification — 2026-09-22

> Superseded: the user rejected this first visual implementation. The approved terminal redesign and current checks are documented in [rework/verification.md](rework/verification.md).

Separate authenticated page: **http://192.168.0.124/replay**, header **Бэктест**. Existing **Тест стратегии** remains `/backtest`. Server `.130` from the skill was unreachable; `.124` is the existing documented server01 deployment and was verified over SSH before changes.

## Delivered workflow

- Downloaded archive catalog for Bybit/Binance Spot/Futures. 1m/5m/15m/1h/4h; UTC dates; 31 days and 10,000 bars maximum per load. No background download is initiated by this page, and missing archive candles are never synthesized.
- Candlesticks, volume, OHLC crosshair, horizontal/vertical scale and pan. Replay start by candle or UTC date; future excluded from every chart series. Play/Pause, exact next candle, 0.25x–20x, end state, restart/exit and return to current bar. Camera follows until manually panned away. 140ms alpha reveal animates the complete candle without inventing an intrabar path. Reduced motion skips it.
- Drawing rail: trend line, horizontal level, rectangle, Fibonacci and brush; colour, width, undo and clear. Time/price anchors track chart coordinates.
- Existing indicator engine reused for SMA 20, EMA 20, Bollinger 20, VWAP, RSI 14 and MACD. Only revealed candles enter calculations. Rewinding cancels stale calculations and clears the old indicator series before paint; indicator selection survives replay restart.
- Virtual 10,000 USDT account: one Long/Short position, user amount and fee, shown Close execution while paused, entry/exit fees, floating PnL, closed-trade journal and full CSV. New timeline resets account and drawings. No exchange order is sent. Session state is in memory; the page says to export CSV before refresh. Funding, liquidation and slippage are not modeled.

## Checks and evidence

`npm run lint` passed for the entire frontend. TypeScript/Vite production build passed locally and in the deployed Docker image. Frontend reducer/archive validation plus existing backtest model/plan/window regressions: **15 tests passed**. Go tests: API backtest/replay, market, chart configuration, and backtest marketdata passed; `go vet` passed for the changed API package. Environment-dependent database integration tests retain their normal skip conditions; live archive checks below exercised the actual server.

Browser/IAB was not available in the tool catalog. Used **Playwright with installed Chromium-based Microsoft Edge**, headless, against the deployed server, not a mocked frontend. Functional run: [checks.json](checks.json). Further archive/error/animation checks: [resilience.json](resilience.json). Both require `LIVE_ADMIN_PASSWORD`, with credentials supplied only through the process environment. No credentials are stored in these reports or scripts.

Live checks cover anonymous 401; authenticated navigation; 864 real default candles; click selection and future hiding; one-step and 5x/20x play/pause; Long/Short/commission/close/CSV; all six indicators; five drawing tools and undo/clear; zoom/pan/return; UTC date restart; account/drawing reset; existing strategy route; desktop and 1024/768/390px widths. All four exchange/market combinations were read at every interval; 5m/15m/1h/4h OHLCV was compared with real constituent minute candles. Error injection covers 503 with previous workspace preserved, empty archive without stale/fake data, and excessive minute range rejection. Error injections exist only in the test browser.

## Visual fidelity ledger

Working design: [concept-trading.png](concept-trading.png), generated with built-in Image Gen; actual native size **1469×1071**. Browser evidence: [native viewport](native-1469x1071.png), [desktop](desktop-replay.png), [mobile](replay-390.png). Both the concept and current render were opened with `view_image` in the final visual QA pass. Primary mockup is interpreted together with the user's stronger instruction to preserve the existing site's design and header.

| Comparison | Evidence / result |
| --- | --- |
| Layout and hierarchy | Heading, archive row, one large chart, left drawing rail, replay strip, virtual-account strip and journal follow the concept. Chart height adjusted after initial comparison so trading controls sit closer to the chart. |
| Colour | Existing forest layout retained; chart #121c18, #34d399/#fb7185 candles, emerald→cyan primary buttons, amber drawing level. No raster overlay or background image shipped. |
| Typography | System UI throughout; explicit 14px controls and 12px labels, readable numeric OHLC/price/time labels; 16px mobile form values. Existing shared header typography retained. |
| Icons and controls | SVG cursor, trend/level/zone/Fibonacci/brush, undo/delete, play/pause/next, target/return/close; consistent geometry and focused/disabled states. Colour/width controls and UTC date selection added for requested functionality. |
| Text and copy | Above-the-fold copy checked against design inventory. Header/title/archive/Replay labels preserved. Documented additions: coverage/limits, date/reset controls, indicator count, commission/account notes, CSV. Generated example prices/dates replaced by actual archive data. No marketing or decorative labels added. |
| Containers and spacing | Single terminal, slim toolbar, restrained borders and rounded controls. Responsive rail scrolls horizontally within its own container; page has no horizontal overflow at 1024/768/390. |
| Motion and state | Complete-candle fade at normal motion, no motion under reduced preference; future series and stale OHLC cleared on rewind. Real replay and trading state, no decorative progress. |

Intentional deviations: existing white site logo/header/account button and footer take precedence over the generator's altered header; all indicators live in one menu instead of a separate SMA checkbox; explicit UTC/date/coverage/error/session controls are functional additions; library attribution is retained; journal height grows with real trades and can extend below the viewport. No remaining material visual mismatch was identified within these documented choices. The implementation was visually verified against the working design, not only build-tested.

## Deployment

Sources developed on the workstation, deployed to `/opt/shortlong`. Only backend/frontend containers rebuilt; existing archive services and databases retained. Original changed server sources backed up at `/opt/shortlong/backups/pre-replay-20260922.tar.gz`. Final build log: `/opt/shortlong/reports/replay-final-build.log`. Frontend/backend health checks passed after deployment; archive ingestion remained healthy. No database migration or data deletion was required.

Mechanics reference: [TradingView's official Bar Replay guide and linked screenshots](https://www.tradingview.com/support/solutions/43000712747-bar-replay-how-and-why-to-test-a-strategy-in-the-past/). Rendering uses the already installed [Lightweight Charts](https://tradingview.github.io/lightweight-charts/docs/) rather than copying a branded UI screenshot.
