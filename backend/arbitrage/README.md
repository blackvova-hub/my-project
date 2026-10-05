# Арбитражный backend

Отдельный Go-модуль и семь процессов: шесть сборщиков и один расчётный HTTP-сервис. Использует публичные API Binance, Bybit, OKX, Bitget, Gate.io и KuCoin. Биржевые ключи не нужны. Исполнение ордеров не реализовано.

## Запуск в существующем проекте

Из `C:/dev/backend`, с существующим настроенным `.env`:

```powershell
docker compose -f compose.yaml -f compose.local.yaml build arbitrage_engine backend frontend
docker compose -f compose.yaml -f compose.local.yaml up -d --no-build --force-recreate backend frontend arbitrage_engine arbitrage_binance arbitrage_bybit arbitrage_okx arbitrage_bitget arbitrage_gateio arbitrage_kucoin
```

Страница: `http://localhost:3000/arbitrage`, после обычного входа. Команда перезапускает API, frontend и новые сервисы. На сервере используется основной `compose.yaml` без локального override. Публичный порт engine не открывается: доступ к нему идёт через авторизованный API сайта.

Проверка состояния:

```powershell
docker compose -f compose.yaml -f compose.local.yaml ps arbitrage_engine arbitrage_binance arbitrage_bybit arbitrage_okx arbitrage_bitget arbitrage_gateio arbitrage_kucoin
docker compose -f compose.yaml -f compose.local.yaml logs --tail 50 arbitrage_engine arbitrage_bybit
```

`health` engine проверяет актуальность чтения Redis. Состояние каждого биржевого рынка, количество свежих стаканов и покрытие показываются на странице. Работающий процесс сам по себе не означает доступность всех бирж.

## Параметры

| Переменная | Назначение |
| --- | --- |
| `EXCHANGE` | Биржа отдельного collector; задаётся Compose |
| `REDIS_ADDR`, `REDIS_PASSWORD` | Существующий Redis |
| `ARBITRAGE_ASSETS` | Список базовых монет; по умолчанию 30, применяется ко всем семи процессам |
| `HTTP_ADDR` | Engine, по умолчанию `:8093` |
| `ARBITRAGE_URL` | В существующем API: `http://arbitrage_engine:8093` |

Для самостоятельного запуска collector поддерживает `<EXCHANGE>_SPOT_BASE_URL`, `<EXCHANGE>_FUTURES_BASE_URL` и, кроме KuCoin, соответствующие `*_WS_URL`. Префикс Gate.io — `GATEIO`. В Compose индивидуальные адреса задаются через `environment` сервиса в override-файле. KuCoin получает адрес и временный публичный токен через `bullet-public`.

Добавление монет в `ARBITRAGE_ASSETS` требует проверки идентичности актива на биржах. Совпадение тикеров само по себе недостаточно. Поддержаны явные соответствия `XBT → BTC` и контракты `1000PEPE`, `1000SHIB`, `1000BONK`, `1000FLOKI`; произвольное удаление числовых префиксов запрещено.

## Интервалы и хранение

- Стаканы: публичные WebSocket; снимки первого уровня Bybit и до пяти уровней остальных бирж.
- Redis: публикация полных небольших снимков раз в 500 мс; ключи `arbitrage:v1:*`.
- Engine: общий снимок раз в секунду. Пользовательские фильтры применяются к нему, отдельные биржевые соединения для клиентов не создаются.
- Frontend: запрос каждые 2 с, без фонового опроса скрытой вкладки; есть пауза.
- Каталог: обновление через переподключение каждые 30 минут. Funding: запрос каждые 30 с; неизвестные или устаревшие ставки не заменяются нулём.
- Возраст котировки: минимум времени биржи и получения, по умолчанию максимум 10 с. Пользователь выбирает 1–30 с. Потеря статуса сборщика сразу исключает рынок.
- Длительность: непрерывный положительный спред лучшего bid/ask при свежести 10 с, проверка раз в секунду. После отключения или перезапуска engine отсчёт начинается заново.
- История: выборочные наблюдения раз в 30 с, до 300 подходящих связок на каждый режим, с серверными базовыми параметрами: 1000 USDT, спред от −1 %, возраст 10 с. Хранение и выдача за последние 6 ч; не полная история всех связок и не исторический PnL.
- PostgreSQL: только индивидуальные настройки и избранное, таблица `arbitrage_preferences`, миграция `070_arbitrage.sql`.

## HTTP-контракт

Внутри сети engine: `GET /health`, `GET /opportunities`, `GET /history?id=<route-id>`.

Снаружи, через существующую авторизацию:

```text
GET /api/arbitrage/opportunities
GET /api/arbitrage/history?id=<24 hex characters>
GET /api/arbitrage/preferences
PUT /api/arbitrage/preferences
```

Параметры поиска: `strategy`, `amount`, `minSpread`, `maxAgeMs`, `horizonHours`, `exitSpread`, `feeSpot`, `feePerpetual`, `exchanges`, `assets`, `interExchange`, `sort`, `limit`. Комиссии в запросе — проценты. Funding в источниках — доля. Суммы — USDT, количество — единицы базовой монеты.

Ответ поиска: `rows`, `books`, `sources`, `assets`, `configuredAssets`, `at`, `transport`. `books` — словарь по ID инструмента: каждый стакан передаётся один раз. Frontend связывает его со строками, сохраняя единый снимок. `fundingEstimate` и `scenarioPnl` равны `null`, если свежего funding недостаточно. Пустая выдача и ошибка транспорта — разные состояния.

## Проверки

Из этого каталога:

```powershell
$env:GOWORK='off'
go test ./...
go vet ./...
go run ./cmd/check
```

Последняя команда делает реальную сетевую проверку каталогов, funding и WebSocket; ничего не торгует. Внешние таймауты и ограничения доступа возможны и отображаются в результате.

Интеграционные Go-тесты подключаются только при заданном `ARBITRAGE_TEST_REDIS_ADDR`. Используют отдельные Redis DB 13 и 14; задавайте изолированный тестовый Redis. Тест PostgreSQL в модуле API использует `ARBITRAGE_TEST_DATABASE_URL`, создаёт и удаляет собственную схему.

Проверка гонок, включая на Windows через Linux-контейнер:

```powershell
docker build --target test -t shortlong-arbitrage-test:local .
docker run --rm shortlong-arbitrage-test:local
```

Для включения Redis-интеграций в контейнере добавьте `-e ARBITRAGE_TEST_REDIS_ADDR=host.docker.internal:26379`, если запущена изолированная инфраструктура `backend/backtest/compose.test.yaml`.

Фронтенд, из `frontend/web`: `npm run test:arbitrage`, `npm run build`, `npm run lint`. Реальная браузерная проверка: `npm run test:arbitrage:live` с `ARBITRAGE_E2E_STORAGE` либо `ARBITRAGE_E2E_EMAIL` и `ARBITRAGE_E2E_PASSWORD` тестового пользователя. Она временно меняет его настройки и восстанавливает исходные значения; для изначально отсутствующих настроек сохраняются стандартные параметры. Не используйте личную учётную запись для автоматических проверок.

Подробный отчёт: [docs/arbitrage/report.md](../../docs/arbitrage/report.md).
