# Trader analytics implementation spec

> Current UI contract: [Russian localization and shared site navigation](localization.md). That document supersedes the original English copy and the original decision to redirect other product routes. Only the previous profile is replaced; all other routes are restored.

Source of truth: requirements.txt, the user's correction “стиль и все цвета должны соответствовать сайту”, and the existing site captured in site-reference-account.png / site-reference-home.png. The generated concept-*.png files remain layout references only: their near-black/mint palette and invented wordmarks are superseded by the user's explicit site-style instruction. Built-in Image Gen was used for eight screen concepts; prompts are retained in prompts.json. Numerical examples are illustrative only; implementation calculates values from one dataset. No sample values are presented as a user's account.

## Tokens and components

Site color lock: vertical canvas gradient #1f3320 → #1a261c → #151d17, sidebar black/20%, panels white/5%, borders white/10%, text white, secondary white/65%. Accent and profit #34d399; loss #fb7185; primary action gradient #059669 → #0891b2; chart canvas #18231c, benchmark cyan #22d3ee. Existing ui-sans-serif/system-ui stack, tabular financial numbers. Titles 30/36 weight600, metrics28/34 weight600, panel titles17/24 weight600, body/control14/21. Sidebar232px; main padding24px, gap16; panel radius16, control radius12 and height44. Thin18px consistent outline SVG icons; code-native Short&Long wordmark. No production raster assets are needed: charts, data, controls and branding use native components.

Shared families: sidebar NavLink, page header, account/range selectors, primary/secondary buttons, KPI band, Panel, Segments, MetricRows, BreakdownTable, chart canvas, scrollable native table, modal dialog, status/empty/error states. Desktop side-by-side layouts collapse below 1050px; sidebar becomes horizontal scrollable navigation below 760px. Tables scroll inside their region, never the page. Dialogs trap focus and close with Escape.

## Inventory and allowed content

Navigation: Overview, Performance, Trades, Edge, Costs, Risk, Insights; Connections. Account footer opens profile settings. Global controls: All accounts, 7D/30D/90D/YTD/1Y/ALL, Export; Demo data only in explicit demo mode, Exit demo. Live empty state allows Connect exchange and Explore demo. Existing auth/admin and legal routes retained; previous product routes redirect to /account, source retained.

- Overview: Every dollar. Every trade. The full picture. Five KPIs; Portfolio performance with Portfolio value/Net invested/BTC benchmark; Money breakdown; Asset allocation; Recent insights. Drilldown Category → Symbol → Trade → Execution, with ledger records for nontrade cashflow.
- Performance: Understand the quality of your returns. KPI strip, Equity/PnL/Return/Drawdown chart, monthly seven-day PnL calendar with details, sixteen performance metrics, symbol/direction/exchange/weekday breakdowns.
- Trades: From position to every execution. Filters for exchange/symbol/direction/result/leverage/duration/date/tag/strategy; table, selected trade, candles and available entry/exit/risk levels, anatomy, MFE/MAE/profit captured/holding, executions and running PnL. Missing imported attributes display unavailable, never fabricated.
- Edge: Find what actually makes you money. Day×hour heatmap, direction comparison, symbol, leverage, duration, weekday/hour/size/exchange analysis. Cells and rows open the matching trades.
- Costs: Know what it costs to trade. Costs paid, costs/gross profit, cost per dollar, category ledger, maker/taker, funding paid/received/net, costs by symbol/exchange. Slippage is informational and not charged twice.
- Risk: See your exposure before the market moves. Equity, gross exposure, effective leverage, margin usage, signed asset and direction exposure, concentration, current/max drawdown and recovery.
- Insights: Patterns in your data. Evidence behind every insight. Ten deterministic pattern types, sample size and rule-based confidence, affected trades and calculation evidence. Human-readable deterministic explanation always available; AI paraphrasing must not invent calculations.
- Connections: Your exchanges. One clear picture. Bybit/Binance, encrypted read-only credentials, account type, sync/coverage/error states, disconnect, reconciliation evidence. Profile editing uses existing auth API.

## Data principles

Postgres keeps encrypted credentials, immutable deduplicated raw source events and normalized ledger, fills, account snapshots, sync status. Source decimal strings are retained; ledger amounts use NUMERIC. A completed sync commits events, snapshot and watermark atomically; interrupted jobs retry from the last committed watermark. Unknown history, fees in unvalued currencies, unavailable mark history and absent historical leverage/SL/TP are visibly unknown. Historical equity before observed snapshots is not invented. Internal transfers do not become trading PnL. Reconciliation compares independent observed wallet snapshots plus intervening ledger movements, not a back-solved opening balance. Staggered account snapshots apply cash flows only when their own wallet observation advances. Demo data is isolated in memory and never sent as real account data.

No copy trading, DeFi/NFT, social, replay, automated trading, tax, VaR, stress test or chatbot in this MVP. Cross analysis is phase 2 as permitted by the brief.
