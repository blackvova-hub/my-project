# Replay redesign verification — 22 September 2026

> Header and palette superseded by the user's subsequent home-page alignment request. Current comparison: [site-alignment/verification.md](../site-alignment/verification.md). Replay functionality remains as verified below.

Deployed page: **http://192.168.0.124/replay**. Approved full-screen terminal composition adapted to Short&Long colors. The earlier card-based implementation and its verification report are superseded by this version.

## Visual evidence and method

Accepted composition/palette reference: [concept.png](concept.png), generated with the built-in Image Gen edit tool. Latest deployed render: [desktop.png](desktop.png). Native viewport **1586 × 992**; both images inspected with `view_image` in the final comparison pass. Browser/IAB was unavailable in the tool catalog, so screenshots and interaction checks use Playwright with Chromium-based Microsoft Edge. Additional checks: 1280, 1024, 768 and 390px widths, including a fresh mobile page load. [Mobile](mobile-fresh.png), [archive dialog](archive-dialog.png).

## Fidelity ledger

| Point | Concept / render evidence | Repair or intentional difference |
| --- | --- | --- |
| Composition | Full-width chart, slim site header, top tools, left drawing rail, bottom trade dock | Removed heading, permanent archive form, nested cards and site footer from Replay. Header navigation retained. |
| Palette | Near-black green canvas, green chrome, turquoise selection/Play, green buy and red sell | Replaced navy/blue with requested site colors; no forest background or decorative wash. |
| Typography | Legible compact terminal controls and numeric scales | Explicit system font sizes: 14px desktop controls/nav, 12px chart, 15px balance. Increased the first render's small chrome. |
| Chart proportions | Canvas occupies most of screen; narrow candles, volume below, blank future space | 60px rail; flexible canvas; camera span and future offset adapt to width. Fixed desktop-sized blank space on mobile. |
| Transport | Compact floating replay bar over lower chart | Calendar, restart, Play/Pause, step, speed, UTC time and return work. On mobile it docks below the chart. Added explicit exit as a functional control. |
| Trading | Flat tabs and one order row; red Short, green Long, journal/CSV | Replaced standalone cards with dock; mobile order fields use two columns. Position and conditions appear contextually. |
| Icons and assets | Thin SVG tools; code-native candlesticks and labels | Real chart/canvas/SVG interface. Volume toggle matches the concept; chart library attribution retained. No screenshot used as UI. |
| Archive / responsive | Modal picker in same color and control system | Native dialog focus/Escape; responsive tools rail, controls and trading dock; no horizontal document overflow. |

Above-the-fold copy audit: no marketing heading/subtitle or unrelated claims remain. Site-role links (Комьюнити/Админка), selected market/timeframe and actual prices/dates intentionally differ from the illustrative reference. EMA 20 uses the existing indicator engine default, rather than the illustration's EMA 50. «Условия торговли» and exit provide necessary simulator controls. The mock drawing is not seeded into user data; drawing tools create real user annotations. These are recorded functional/data differences, not missing decorative content.

The deployed interface was faithfully compared with the accepted composition and requested site palette. No material layout, clipping, palette or responsive mismatch remained in the final visual pass.

Final pass after the responsive camera fix: repeated the complete live browser suite successfully, then inspected the concept, latest 1586×992 desktop render and fresh 390px mobile render together with `view_image`. The mobile chart now retains readable candle history with a proportional future offset. Browser errors: zero.

## Functional evidence

[Browser results](checks.json), [live archive API results](archive-api-checks.json). Frontend lint and TypeScript/Vite production build pass; six reducer tests cover future truncation, bounds, commission/PnL, resets and the newly fixed initial load / orders during playback. Docker production build passes on the server.

Browser paths: initial archive → immediately enabled Play/trading → exact next candle → 20× replay → pause → Long/Short → close → journal/CSV; date and candle selection; restart; keyboard step; all six indicators and pane switching; five drawing tools, undo/clear, pan/zoom/return; modal focus/Escape; fullscreen; existing strategy tester navigation. No browser runtime exceptions.

The user's ETHBTCUSDT 1m range (28 August–2 September) was loaded from the real archive: 8640 candles, working controls and trading. Flat/zero-volume candles remain honest archive data. API checks exercise Bybit/Binance × Spot/Futures × all five intervals, comparing aggregated OHLCV with constituent minutes. Injected 503 retains the prior workspace; empty data clears it explicitly; oversized ranges are rejected before fetch.

Simulation scope: one position, no leverage, execution at displayed Close, entry/exit fees; orders pause playback atomically. Funding, liquidation and slippage are not modeled. Session lives in memory and CSV exports closed trades. Future candles are excluded from price/volume/indicator inputs until revealed.

Deployment followed the local server skill using verified server01 at `.124` (the skill's `.130` is unreachable). Only frontend rebuilt for this redesign. Previous sources backed up at `/opt/shortlong/backups/pre-replay-rework-20260922.tar.gz`.

Research references: [TradingView historical trading workflow](https://www.tradingview.com/support/solutions/43000691889-learn-to-trade-on-historical-data/), [Replay starting point](https://www.tradingview.com/blog/en/selecting-bar-replay-starting-point-44104/). Layout studied from actual Supercharts before concepting; this remains the site's own archive simulator.
