# Бэктест / Replay — отдельная страница

Route `/replay`, authenticated, separate header item «Бэктест». Existing `/backtest` is the automated strategy tester. Concept: `concept.png`, 1536×1024, generated with built-in Image Gen. User requires the existing site's style and header; these override generated differences in logo, header width and account icon. No approval pause was requested.

User subsequently requested drawing, indicators and trading. Updated working concept: `concept-trading.png` (actual native size 1469×1071). Keep the shared header/logo/footer as explicitly requested. Add a left drawing rail, indicator popover, paper-account strip and trade table. This version supersedes the initial concept. Image Gen was briefed with the exact site palette, labels, complete primary screen and these additions; outputs are design references only, never shipped as raster UI.

Drawing tools: cursor, trend, horizontal, rectangle, Fibonacci, brush; 1/2/3px strokes, user colour, undo/clear. Chart anchors use time and price so drawings follow pan/zoom. A fresh replay timeline clears drawings and trades while preserving indicators. Indicators reuse the existing engine: SMA/EMA/Bollinger/VWAP overlays, RSI or MACD subpane. Paper account starts at 10,000 USDT, one position, shown Close execution, fee both sides, no leverage/funding/slippage. Orders require paused replay. Balances and trade history are in-memory; CSV explicitly offered before page refresh.

Intentional functional copy additions: archive coverage dates, request limit, date start/reset warning, commission description, export CSV and session-refresh note. Concept's standalone SMA checkbox is consolidated into the indicator menu. Actual prices/dates/rows replace illustrative concept values. Lightweight Charts attribution remains in the chart. Trade journal grows below the viewport as needed; shared footer remains below the tool.

## Design system before implementation

Existing PublicLayout forest gradient (#1f3320 → #151d17), unchanged shared header and footer. Main 1424px maximum, 24px gutters; heading 36/40 semibold, subtitle 16/24 muted. Single archive toolbar and single chart frame, 16px corners, white/10 borders. Chart #121c18, subtle grid, green #34d399/red #fb7185 candles, cyan SMA. Controls 40–44px high, 10px corners, 14px text; primary emerald→cyan. System sans-serif, tabular prices/times. SVG outline icons 20px/1.7 stroke, filled media play/pause/step. No raster assets in product UI, overlays, marketing panels or ornamental badges.

Allowed copy: existing header; «Бэктест», «Воспроизведение рынка по историческим свечам», «Тест стратегии»; Биржа, Рынок, Пара, Интервал, С, По, Открыть архив; Объём, SMA 20, Replay; Выбрать свечу, Play/Pause accessible label, Следующая свеча, speed, UTC timestamp, К текущей свече, Завершить Replay; Архив OHLCV · Только закрытые свечи; keyboard hints. Required functional extensions: UTC date selection, loading/errors/empty archive/gap notice/end of replay/selection prompt and archive bounds. Displayed data always comes from selected series.

## Ownership and workflow

ReplayPage owns archive selection/loading and replay reducer. ArchiveForm owns inputs. ReplayChart owns Lightweight Charts and incremental updates, hover OHLC, volume/SMA, selection line, camera, 140ms opacity reveal of completed OHLC. ReplayControls owns playback/date/keyboard controls. Pure model enforces visible prefix and bounded transitions. No fabricated intrabar path. All chart series, crosshair data and SMA use visible prefix only. Future stays hidden when reselecting; explicit exit restores archive. Space toggles playback, right arrow advances, Escape cancels selection; inputs retain native keyboard handling.

Mobile: wrapping form (2 columns then full-width submit), chart min-height 360, wrapped toolbar and replay controls with 44px touch targets, no horizontal document overflow. Date input offers keyboard/touch equivalent of chart selection. Reduced motion disables candle fade and camera animation. During manual history pan, playback preserves camera until user returns.

## Data and checks

GET `/api/replay/symbols?exchange=&market=` queries indexed existing PostgreSQL catalog. GET `/api/replay/candles` reads only downloaded closed candles, exchange-isolated, complete aggregate buckets, max 10,000 bars and 31 days per request; never downloads or invents missing history. Missing intervals are counted. Auth enforced on both. Integration checks use real server archive and no external messages. Unit/API tests cover boundaries, future exclusion, reducer, missing data, authentication; browser checks cover desktop/mobile, playback/pause/step/speed/date/exit, OHLC, pan/zoom and navigation. Browser/IAB unavailable: Playwright Chromium fallback.

References: https://www.tradingview.com/support/solutions/43000712747-bar-replay-how-and-why-to-test-a-strategy-in-the-past/ (including linked screenshots); https://tradingview.github.io/lightweight-charts/docs (incremental series.update).
