# Replay workspace redesign

The existing home page and the user's replay sketches were the visual references. The final [desktop](server-live/desktop.png), [open position](server-live/position.png), and [mobile](server-live/viewport-390.png) browser captures were inspected at their native sizes.

| Area | Result |
| --- | --- |
| Hierarchy | A prominent Replay/virtual account header identifies the mode; balance and session PnL remain secondary. |
| Layout | The taller chart and trade card sit side by side on desktop; the card moves below the chart on narrow screens. The journal stays below the work area. |
| Controls | Symbol, indicators and volume stay beneath the chart. Replay controls form a full-width strip under the chart rather than floating over candles. |
| Trade state | Without a position, the card shows size presets and large Long/Short actions. With a position, it shows entry/current price, position PnL and a single Close action. |
| Styling | The page uses the site's green gradient, rounded panels, borders and responsive gutters. Drawing colors are fixed by tool; there is no color picker. |

The candle chart keeps its current zoom width while advancing, including after a step back. Drawing tools now support click-click as well as drag, selection, moving, locking, visibility, magnet, inline text and undo. The virtual trade model has no commission or hidden deduction; journal and CSV have no commission field.

Validation: production TypeScript/Vite build, ESLint for changed source, six replay model tests, and an authenticated Playwright run against the deployed archive and frontend. The [browser checks](server-live/checks.json) cover playback, stable zoom, trades, drawing tools, journal/CSV, archive selection and failure states, fullscreen, and 1280/1024/768/390 px layouts without horizontal overflow or page errors. The frontend and backend containers were healthy and `/api/health` returned 200 after deployment. Server source backups are under `/opt/shortlong/backups/replay-chart-20260926`.
