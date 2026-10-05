# Тест стратегии: design specification

The implementation uses `concept-builder.png` and `concept-results.png`, generated with the built-in Image Gen tool. Concept dimensions: 1536 × 1024. These are design references; all UI is native React, CSS and chart rendering.

## System

- Forest background #111c16, surface #19271f, border #304337; text #edf4ef, muted #9bad9e, mint #b5ebc5; loss #f08080. No decorative gradient or illustration assets.
- System sans; heading 32/40px 700, sections 18/26px 600, body 14/21px, captions 12/18px. Controls explicitly 14px, 40px minimum height. Radius 10–12px, spacing 8/12/16/24/32px.
- Existing header retained; add Тест стратегии to desktop and mobile navigation. Intentional difference from generated header: retain existing brand, typography and account control. Page content width up to 1408px to preserve concept density.
- One market band, a 65/35 strategy/execution split, results below. No nested card grids. Mobile: single column, wrapping controls, horizontally scrollable trade table, visible navigation.
- Icons are small consistent 24×24 outline SVGs (plus, close, chart, download, history, chevron); run uses filled play triangle. No additional imagery.
- Focus uses mint outline; disabled controls visibly dimmed. Motion only short state transitions and real progress; reduced-motion supported.

## Copy and structure lock

Тест стратегии; Проверьте правила на истории рынка — до первой реальной сделки.; История тестов.

01 Рынок и период: Биржа; Bybit; Тип рынка; Spot; Futures; Торговые пары (до 5); Добавить; Таймфрейм; 5m/15m/1h/4h; Период (UTC); С; По.

02 Условия стратегии: Направление торговли; Long (покупка); Short (продажа); Вход в позицию; Все условия (AND); Любое условие (OR); Добавить условие; Выход из позиции; Тейк-профит (TP), %; Стоп-лосс (SL), %; Выход по условию.

03 Капитал и исполнение: Начальный капитал, USDT; Размер позиции, %; Комиссия, %; Проскальзывание, %; Общий капитал для всех монет. Без кредитного плеча.; Запустить тест; Вход на следующей свече · Комиссия на входе и выходе.

Empty: Результат появится здесь; Настройте стратегию и запустите тест.

Results: Результаты теста; Скачать сделки; Доходность; Макс. просадка; Прибыльных сделок; Сделок; Profit factor; Sharpe; Капитал; С учётом комиссии и проскальзывания; Сделки; Монета; Направление; Вход (UTC); Выход (UTC); Цена входа; Цена выхода; PnL; Причина; Как считается результат.

Required functional extensions in the same system: real queued/loading/calculating/cancelled/failed states with cancel/retry, validation and provider errors, owned job history, paginated trades, versioned JSON strategy import/export, optional exit condition editor, nested AND/OR groups. Dates and results are real data, not locked example values. Futures methodology explicitly discloses that funding and exchange lot-size constraints are excluded.

## Component boundaries

BacktestPage composes MarketForm, ConditionBuilder, execution fields, JobHistory and BacktestResults; separate typed API/query module, model defaults/validation, chart and CSS tokens. Route lazy-loaded. Query keys include user identity; polling runs only for active jobs. No mock data in production.
