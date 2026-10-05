# Replay redesign — approved composition, Short&Long palette

User approved the TradingView-like concept and requested site colors. `concept.png` is its Image Gen palette adaptation (1586 × 992). Full screen terminal; no marketing heading, stacked cards, decorative imagery, or website footer. UI is native React/canvas, never a screenshot.

Tokens: background #101b17; toolbar #18271f; border #2b3c32; text #e7eee9; muted #9daea3; active #10b9a5; buy #059669; candle up #26a69a; down #ef5350; EMA #38bdf8. System sans, 14px desktop chrome/12px chart, 14px instrument, 20px logo. Spacing 4/8/12/16/24; 4px controls, 8px transport; flat bands, 60px tools rail, 54px header, 56px toolbar, flexible chart, compact bottom dock. These final measurements replace the initial extraction estimates after native-size visual comparison. Mobile uses 48px header and 13px controls.

Allowed visible UI: existing site navigation/brand; instrument/exchange/market/timeframe; Индикаторы, Объём, Replay, Открыть архив; OHLC/indicator legend; Выбрать свечу, date, restart, Play/Pause, next, speed, UTC time, return; Торговля в Replay, Журнал сделок, CSV; Баланс, PnL, Объём USDT, Комиссия %, Продать / Short, Купить / Long, Закрыть позицию; virtual-account Close/UTC note. Required contextual states: selection guidance, position, errors, end of archive. Existing role-based navigation preserved, an intentional difference from condensed concept.

Archive dialog: same tokens, 2-column fields with exchange/market/symbol/timeframe/from/to, archive coverage, submit; native modal focus/Escape. Mobile: horizontal scrollable tools, wrapped compact toolbar, transport below chart, trading controls in 2 columns, no document overflow. These extensions are needed to support the requested workflow at narrow widths.

Components: page/data orchestration; archive dialog/form; persistent chart; SVG drawing toolbar and canvas; replay transport; trade dock and journal; pure reducer. Default load starts paused with historical context and unrevealed future. Orders during playback pause atomically at visible Close. Restart resets account/drawings; indicators survive. No fabricated intra-candle price paths.

Image generation: built-in image_gen edit; preserve approved layout/copy, replace navy/blue with the tokens above. No production bitmap assets needed for a chart terminal.
