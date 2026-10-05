# Replay spacing and site styling

Reference: [authenticated home](../site-alignment/home-reference.png), together with the user's supplied home screenshot. This is a focused correction inside the existing design system, so no generated concept was needed. Production changes are confined to `frontend/web/src/pages/public/replay/replay.css`.

## Design and fidelity review

Reference and final browser captures were inspected with `view_image` in the same review pass. The home reference's native 1586 × 992 viewport was checked, plus 1927 × 837 (the supplied home screenshot dimensions), 1024 × 768, 768 × 900, 390 × 844 and 320 × 740.

| Comparison | Reference / previous mismatch | Final result |
| --- | --- | --- |
| Page gutters | Home uses 32 px desktop, 24 px tablet, 16 px mobile; replay touched the edges | Same responsive gutters; 24 px top spacing on desktop |
| Panel geometry | Home uses 16 px corners and white/10 borders; replay was a continuous rectangle | Chart and trading panels use matching corners and borders, with a 24 px gap (16 px on smaller screens) |
| Inner spacing | Home panels inset their content | Chart and trading content inset 24 px on desktop; chart surface also has rounded corners |
| Palette | Home panel #2a3b2b, inner surface #18231c, dark form fields | Same colors, dark #18181b inputs, restrained emerald selected controls |
| Controls | Replay had sharp fields and underline-only tabs | Rounded fields/buttons, bordered emerald active trade tab |
| Typography and icons | Existing site header and replay control typography/iconography | Existing sizes, labels and icon sources retained; no browser-default typography introduced |
| Responsive behavior | Desktop toolbar squeezed the volume toggle at tablet width | Compact layout starts at 900 px; toggle cannot shrink; indicator/date menus stay in the viewport |

Above-the-fold copy audit: CSS-only change; no visible strings added, removed, renamed or reordered in the source. Existing responsive control flow remains; the compact breakpoint moves from 760 to 900 px. Market values and journal count in captures reflect the QA session.

The requested styling is faithfully aligned with the home reference. No material styling mismatches remain in the checked views. Intentional differences: replay retains its chart, drawing tools, playback and trading workflow; mobile permits vertical scrolling to preserve usable chart and control sizes. No home ticker or unrelated dashboard widgets were added.

## Verification

Browser/IAB tools were unavailable. Used Playwright with installed Edge (Chromium), headless, against the local Vite app. Screenshots are actual browser renders. Authentication and archive HTTP responses were intercepted with deterministic QA fixtures; production API connectivity/data freshness were not tested.

Passed: production TypeScript/Vite build; next-candle cursor increment; Play/Pause state changes; Long entry, next candle, close and journal row; EMA checkbox; archive dialog; viewport gutters, horizontal overflow and control bounds at all six sizes; indicator and date popover bounds at all six sizes. No browser page errors. See `checks.json` for dimensions.

Final local captures: [desktop](replay-1586.png), [wide desktop](replay-1927.png), [mobile](replay-390.png). Temporary QA script removed after verification.

## Server deployment

Deployed 2026-09-26 to the documented `server01` at `192.168.0.124`. The address `192.168.0.130` in the local `$server` skill timed out; `.124` identified itself as `server01`, contained the expected `/opt/shortlong` deployment and matched the repository's deployment documentation. The previous CSS source was backed up under `/opt/shortlong/backups`, then only the frontend image and container were rebuilt. Backend, databases and archive services were left running.

The deployed frontend and backend containers report healthy, `/api/health` returns 200, and the served lazy asset is `ReplayPage-thK_oinS.css` with the new radius, desktop gutter and 900 px responsive rules. An authenticated Playwright run against the real server and real candle archive passed the complete replay workflow with no page errors: archive loading, future hiding, exact step, 20x Play/Pause, Long/Short/fees/close/journal/CSV, all indicators, drawing tools, date/candle selection, error and empty states, fullscreen, existing strategy route, and 1280/1024/768/390 px layouts without horizontal overflow.

Server browser evidence: [checks](server-live/checks.json), [desktop](server-live/desktop.png), [mobile](server-live/viewport-390.png), [archive dialog](server-live/archive-dialog.png). The home reference, deployed desktop and deployed mobile images were inspected together with `view_image`; no material mismatch remains.
