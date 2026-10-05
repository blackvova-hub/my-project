# Цветовая система frontend

Restyle меняет цвета и состояния существующих компонентов. Тексты, маршруты, бизнес-логика и композиция страниц сохранены. Отдельно исправлены наложение мобильной кнопки AI-помощника на шапку и переполнение поисковой строки Wallet Registry на узком экране; desktop layout сохранен.

## Три темы

Все токены находятся в [`web/src/index.css`](../web/src/index.css): `:root` — Light, `.dark` — Dark, `.green` — Brand Green. Геометрия, spacing и radius общие для всех тем.

| Токен | Light | Dark | Brand Green |
| --- | --- | --- | --- |
| `background` | `#F2F5F3` | `#080D0A` | `#08110C` |
| `card` | `#FBFCFB` | `#0F1612` | `#101A13` |
| `surface-raised` / `popover` | `#FDFEFD` | `#141D17` | `#17251B` |
| `foreground` | `#111914` | `#EBF0EC` | `#ECF2ED` |
| `muted-foreground` | `#607268` | `#9AAA9E` | `#A1B3A5` |
| `border` | `#D4DFD8` | `#26342B` | `#294031` |
| `primary` | `#196C46` | `#54C985` | `#64D995` |
| `primary-hover` | `#145B3B` | `#6CDA99` | `#7BE9A7` |
| `primary-foreground` | `#F7FBF8` | `#07140D` | `#062012` |

Dark использует почти черные поверхности с мягким зеленым оттенком. Light сохраняет тот же характер через серо-зеленый canvas, нейтральные светлые поверхности и более темный primary для читаемости. Brand Green усиливает оттенок темных поверхностей и акцент; карточки и рамки остаются спокойными.

### Назначение токенов

- Canvas: `background` / `foreground`.
- Карточки: `card` / `card-foreground`; вложенные поверхности: `surface-raised`.
- Меню и диалоги: `popover` / `popover-foreground`; затемнение: `scrim`.
- Главное действие: `primary` / `primary-foreground`, hover: `primary-hover`.
- Второстепенные действия: `secondary` / `secondary-foreground` и `border`.
- Подписи: `muted-foreground`; выделение и спокойный hover: `accent` / `accent-foreground`.
- Поля: `input`; фокус: `ring`; ошибки и опасные действия: `destructive`.
- Обычные разделители: `border`; усиленная граница: `border-strong`.
- Небольшие тени меню и модальных окон: `shadow-popover`, `shadow-overlay`. Размеры теней одинаковы, их прозрачность зависит от темы.

Scoped CSS аналитики, backtest, replay и admin использует общие семантические переменные вместо самостоятельных нейтральных палитр. Убраны декоративные светящиеся подложки, glass blur и произвольные черные тени, включая inner shadow MiniScanners. Его tabs получили общий focus ring. Primary выделяет приоритетные действия, а обычные ссылки на биржи используют спокойный secondary.

### Графики и осмысленные цвета

Новый [`chartTheme.ts`](../web/src/shared/market/chartTheme.ts) читает CSS variables через `getComputedStyle`: поверхность, текст, сетку, crosshair, подпись, positive и negative. Canvas-графики обновляют палитру при смене темы через `useTheme`. SVG-графики аналитики также используют semantic tokens для сетки, осей, crosshair, свечей, stops и marker surfaces; серии сохраняют различие через `chart-1`, `chart-3` и `muted-foreground`.

Сохранены цвета с отдельным смыслом: серии и индикаторы графиков, шкала sentiment, предупреждения amber, отдельные статусы и брендовые обозначения бирж. Positive/negative текст и основные торговые состояния используют `primary` / `destructive`; предупреждения получают читаемый вариант для светлой и темных тем. Эти цвета не заменены массово на один зеленый.

## Hero и переключение темы

Hero использует исходный `/images/hero-bg.jpeg`: фотография, кадрирование, фильтры и прежние overlay/vignette не изменены. Их исходные gradients и локальные цвета сохранены как часть существующей обработки фотографии. Новые декоративные gradients не добавлялись. Текст над фотографией использует постоянные `media-foreground` / `media-muted-foreground`, CTA — общие primary tokens.

Существующий компактный переключатель в шапке с солнцем, луной и латинскими `SL` сохранен. Выбор хранится в `localStorage` под ключом `site-theme`; без сохраненного выбора учитывается `prefers-color-scheme`. Анимация цветового перехода длится 240 мс и отключается при `prefers-reduced-motion`.

Программное переключение из компонента внутри `ThemeProvider`:

```tsx
import { useTheme } from "@/shared/theme/ThemeContext";

const { theme, setTheme } = useTheme();
setTheme("light"); // либо "dark" или "green"
```

## Проверка

- Account/demo, три scanner, backtest, replay и admin проверены в Light, Dark и Green при ширинах 1440 и 390 px: 42 комбинации, без ошибок JavaScript и горизонтального переполнения.
- Для защищенных страниц использовался browser mock пользователя Pro/Admin и demo-данные. Изменения авторизации и mock-код в проект не добавлялись. Это визуальная проверка, а не проверка живых торговых операций.
- Главная, News, четыре auth-страницы и три legal-страницы проверены в трех темах при 1440 и 390 px: 54 комбинации, без ошибок страницы и горизонтального переполнения.
- Дополнительно проверены все разделы account/demo, пользовательский dashboard и Wallet Registry в трех темах при 1440 и 390 px. Отложенные dashboard-виджеты проверены после прокрутки; при недоступном локальном API отображаются существующие error/empty states.
- Community и Exchange по действующим маршрутам показывают существующий `FeatureBlocked`. Их исходные компоненты переведены на токены, но содержимое недоступно через обычную навигацию для браузерной проверки.
- Hover/focus/disabled состояния scanner и backtest проверены в трех темах: input ring и outline кнопок используют общие токены.
- Wallet Registry перепроверен отдельно с заполненной таблицей и валидным JSON mock при 390 и 1440 px во всех трех темах. `scrollWidth` равен viewport, ошибок JavaScript нет; широкая таблица прокручивается внутри своего контейнера. Mock GET/POST/PATCH возвращают JSON и код 200, добавленная и измененная строки отображаются. Проверены focus поля, hover поиска и disabled кнопки добавления.
- На сервере главная, login, register и news проверены в трех темах при 1440 и 390 px: 24 комбинации, без ошибок страницы и горизонтального переполнения. `auth/me` возвращает ожидаемый 401 для гостя.
- Диалог профиля, выбор инструмента replay и drawer администратора открыты в трех темах на desktop/mobile. История backtest проверена как существующий встроенный блок, а не модальное окно.
- SVG-график дополнительно проверен при переключении темы без reload: оси, сетка, серии и tooltip меняют цвет. Keyboard focus вкладки mini-scanner отображает outline 2 px с токеном `ring`.
- Итоговые `npm run build` и `npm run lint` после всех исправлений завершились с кодом 0; Vite build — 9,66 с.
- Дополнительный локальный обход получил ошибки разбора HTML вместо JSON из API при недоступном backend. До установки валидного mock эти результаты не подтверждали работу Wallet Registry. Результаты browser mock также не подтверждают live API.
- Итоговая версия frontend развернута на `http://192.168.0.124/`: контейнер `crypto_frontend` healthy, главная и `/health` возвращают 200. После последнего deploy проверены theme tokens осей/сетки аналитики и mobile Wallet Registry во всех трех темах. Для защищенных экранов сохранялось ограничение browser mock/demo.
- Исходники до restyle сохранены на сервере в `/srv/archive/shortlong-frontend-prehome-20260928-143313.tar.gz`; временные staging-файлы после deploy удалены.

### Screenshots главной

Актуальные screenshots с сервера:

- [Light](theme-screenshots/home-light.png)
- [Dark](theme-screenshots/home-dark.png)
- [Brand Green](theme-screenshots/home-green.png)
- [Анимация переключения](theme-screenshots/theme-switch.webm)

Мобильные варианты: [Light](theme-screenshots/home-light-mobile.png), [Dark](theme-screenshots/home-dark-mobile.png), [Brand Green](theme-screenshots/home-green-mobile.png).

## Измененные исходники

63 файла под `web/src`. Список снят по времени изменения после `2026-09-28 22:55:00`; `chartTheme.ts` — новый файл. `package.json` и зависимости для restyle не менялись.

```text
web/src/app/App.tsx
web/src/index.css
web/src/layouts/PublicLayout.tsx
web/src/pages/admin/dashboard.css
web/src/pages/admin/WalletRegistryPage.tsx
web/src/pages/analytics/analytics.css
web/src/pages/analytics/Charts.tsx
web/src/pages/auth/ForgotPasswordPage.tsx
web/src/pages/auth/LoginPage.tsx
web/src/pages/auth/RegisterPage.tsx
web/src/pages/auth/ResetPasswordPage.tsx
web/src/pages/public/account/AccountPage.tsx
web/src/pages/public/account/AvatarModal.tsx
web/src/pages/public/account/StrategyPairRow.tsx
web/src/pages/public/account/TradeDetailsModal.tsx
web/src/pages/public/account/TradeHistoryTable.tsx
web/src/pages/public/backtest/backtest.css
web/src/pages/public/backtest/EquityChart.tsx
web/src/pages/public/community/CommunityChannelsPage.tsx
web/src/pages/public/community/CommunityFeedPage.tsx
web/src/pages/public/community/CommunityPage.tsx
web/src/pages/public/exchange/ExchangePage.tsx
web/src/pages/public/hero/sections/AboutSection.tsx
web/src/pages/public/hero/sections/BenefitsSection.tsx
web/src/pages/public/hero/sections/BetaSection.tsx
web/src/pages/public/hero/sections/HeroSection.tsx
web/src/pages/public/hero/sections/SubscriptionsSection.tsx
web/src/pages/public/hero/sections/TeamSection.tsx
web/src/pages/public/hero/sections/UiPreviewSection.tsx
web/src/pages/public/home/AuthHomeDashboard.tsx
web/src/pages/public/home/widgets/CommunityWidget.tsx
web/src/pages/public/home/widgets/MarketInsightsWidget.tsx
web/src/pages/public/home/widgets/MarketTickerMarquee.tsx
web/src/pages/public/home/widgets/MiniNewsWidget.tsx
web/src/pages/public/home/widgets/MiniScanners.tsx
web/src/pages/public/home/widgets/MiniSignalsTable.tsx
web/src/pages/public/home/widgets/OverviewCards.tsx
web/src/pages/public/home/widgets/TopMoversWidget.tsx
web/src/pages/public/home/widgets/WatchlistWidget.tsx
web/src/pages/public/legal/CookiesPage.tsx
web/src/pages/public/legal/PrivacyPage.tsx
web/src/pages/public/legal/TermsPage.tsx
web/src/pages/public/news/NewsPage.tsx
web/src/pages/public/replay/replay.css
web/src/pages/public/replay/ReplayChart.tsx
web/src/pages/public/scanners/ScannerConfig.tsx
web/src/pages/public/scanners/ScannersPage.tsx
web/src/pages/public/scanners/SignalsTable.tsx
web/src/shared/exchange/PrimaryExchangeGate.tsx
web/src/shared/market/chartTheme.ts
web/src/shared/market/CoinResearchPanel.tsx
web/src/shared/market/IndicatorMenu.tsx
web/src/shared/market/PerpChart.tsx
web/src/shared/market/SimilarityChart.tsx
web/src/shared/market/SimilarityPanel.tsx
web/src/shared/trades/CloseTradeModal.tsx
web/src/shared/ui/AiAssistantWidget.tsx
web/src/shared/ui/AnimatedSelect.tsx
web/src/shared/ui/CaptchaGate.tsx
web/src/shared/ui/Footer.tsx
web/src/shared/ui/Header.tsx
web/src/shared/ui/ToastProvider.tsx
web/src/shared/ui/TurnstileField.tsx
```

Документация и screenshots лежат отдельно в `frontend/docs/`; вспомогательные browser QA scripts не входят в runtime frontend.
