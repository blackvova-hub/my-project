# News bot

Точка входа и старый production pipeline остаются в `cmd/collector/main.go`. Индивидуальные адаптеры официальных источников находятся в `internal/sources/`; отдельного V2 pipeline нет.

## Источники

- Cointelegraph RSS;
- Telegram channel `cryptoattack24` via public HTML preview (text only, emoji and non-coin Cyrillic hashtags removed, digests skipped);
- Bybit Announcements V5 API (HTML-парсер удалён из активного кода);
- SEC RSS и SEC EDGAR submissions по CIK;
- Federal Reserve RSS: monetary policy, enforcement, press releases, speeches;
- Upbit Announcements API и Bithumb Notices API;
- SoSoValue BTC/ETH ETF flows и разбивка по фондам;
- Alternative.me Fear & Greed;
- DefiLlama TVL, stablecoin supply, DEX volume, fees и DeFi open interest;
- сохранённые legacy-источники WuBlockchain, CryptoPanic, CoinDesk, Binance и дополнительные RSS.

Каждый адаптер возвращает `sources.Item`. `PublishedAt` — только время публикации/полученного снимка, а объявленное время листинга или события хранится отдельно в nullable `EventAt`.

## Обработка

1. Индивидуальный адаптер получает и фильтрует данные источника.
2. Legacy pipeline применяет глобальные include/exclude фильтры, если они заданы.
3. Оригинальные поля сохраняются отдельно, перевод выполняется через Timeweb Agent.
4. URL/title дедупликация сохраняет только новую публикацию.
5. `news_items` получает `original_*`, `translated_*`, `source_id`, `source_type`, `published_at`, `event_at`, `event_type`, `assets` и явный `publication_eligible`.

## Настройка

Telegram parser is enabled by default. Optional settings are `TELEGRAM_CRYPTOATTACK_ENABLED`, `TELEGRAM_CRYPTOATTACK_URL`, `TELEGRAM_CRYPTOATTACK_LIMIT` and `TELEGRAM_CRYPTOATTACK_INTERVAL`.

Полный пример находится в `.env.example`. Для работы без ключей доступны Cointelegraph, Bybit, SEC RSS/EDGAR, Fed, Upbit, Bithumb, Fear & Greed и DefiLlama.

Нужно задать вручную:

- `SOSOVALUE_API_KEY` и `SOSOVALUE_ETF_ENABLED=true` для ETF flows;
- `TIMEWEB_API_KEY` и `TIMEWEB_AGENT_ACCESS_ID` для перевода;
- при необходимости свой список `SEC_COMPANY_CIKS`.

On-chain monitor работает в `worker`, а не в news-bot. Он использует существующий `wallet_registry`; включение Ethereum: `ONCHAIN_MONITOR_ENABLED=true`, `ETHERSCAN_API_KEY=...`, `ONCHAIN_MIN_USD=1000000`.

## Запуск

```bash
docker compose up -d --build news_bot worker backend telegram_news_publisher
```
