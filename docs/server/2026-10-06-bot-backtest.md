# Восстановление Telegram-бота и запуска бэктеста

6 октября 2026, server01 (`192.168.0.124`).

## Причина и исправление бота

`crypto_telegram_admin_bot` оставался running/healthy, но `getUpdates` постоянно завершался `dial tcp ...:443: i/o timeout`, иногда `lookup ... server misbehaving`. Такие ошибки присутствуют уже в начале просмотренного 72-часового окна, 3 октября. Это сетевой отказ доступа к Telegram, а не потеря токена или привязки чата. Прямой HTTPS-запрос к Telegram с сервера тоже завершился таймаутом, запрос из уже использующего `awg0` контейнера успешно получил ответ Telegram.

- В серверном Compose overlay бот подключён к существующей сети egress с адресом `172.31.254.4`, MTU 1420, приоритетным шлюзом и DNS `1.1.1.1`/`1.0.0.1`.
- Существующий routing timer поддерживает новое правило `32122 from 172.31.254.4/32 lookup 51820`. Скрипт отказывается заменять правило с чужим владельцем. Прежние правила Gate/engine сохранены.
- Healthcheck теперь проверяет не только Docker/state, но и Telegram `getMe` с общим таймаутом 8 секунд. Недоступный Telegram больше не выдаётся за healthy.
- Ошибки HTTP-транспорта и Telegram URL в выдаваемых логах скрывают токен. Старые серверные логи не переписывались.
- Бот пересобран и пересоздан. Volume авторизации сохранён. VPN-ключи, маршруты хоста и другие приложения не менялись. Проверка не отправляла тестовых сообщений в Telegram.

После обновления: бот healthy, ручной healthcheck exit 0, restart count 0, ошибок polling в новых логах нет; таймер routing Result=success, правило 32122 присутствует.

## Ошибка со скриншота

`crypto.randomUUID()` доступен в secure context, а сайт открыт по LAN HTTP. Поэтому нажатие «Запустить тест» раньше падало до отправки запроса. [MDN randomUUID](https://developer.mozilla.org/en-US/docs/Web/API/Crypto/randomUUID), [MDN getRandomValues](https://developer.mozilla.org/en-US/docs/Web/API/Crypto/getRandomValues).

Добавлен `requestKey.ts`: используется native UUID при наличии, иначе UUIDv4 из криптографических случайных байтов `getRandomValues`, доступных по HTTP. `Math.random` для ключей не применяется. Если криптографического источника нет, интерфейс показывает ошибку. Существующая семантика ключа сохранена: повтор запроса использует тот же ключ, новая конфигурация/успешный запуск сбрасывает его.

Frontend пересобран и развёрнут. Реальный запуск Bybit Spot BTCUSDT за 1 января 2026 через браузер по HTTP завершился успешно; повтор POST с тем же requestKey вернул ту же задачу. Перезагрузка, реальные сделки, equity chart и мобильный результат проверены. Тестовые завершённые задачи остались в истории администратора.

## Что найдено в остальных логах

- API, frontend, arbitrage engine, базы и архив работают; шесть collectors running. В ядре за 24 часа не найдено OOM/I/O/filesystem ошибок. SSD занят на 16%, HDD на 12%, доступно около 25 GiB RAM. Таймер снимков host-health работает успешно.
- News collector продолжает получать отдельные внешние HTTP 429 и TLS timeouts. Его сеть и источники в этом исправлении не менялись.
- Семь старых `crypto_scanner_*` остановлены с exit 0 одновременно 21 сентября, restart policy `no`. Их не запускали; это отдельные процессы от работающих шести arbitrage collectors. Потоки старых ликвидаций соответственно не обновляются.
- PostgreSQL содержит отменённые клиентом запросы; сервер работает healthy. Диагностические строки worker `xlen_error=<nil>`/`pel_error=<nil>` не являются ошибкой, несмотря на совпадение слова error при поиске.

## Проверки и файлы

- 6 Go-тестов admin-bot и `go vet ./...`: пройдены.
- 4 теста владения/идемпотентности routing rules: пройдены локально и на сервере.
- Все 50 Node-тестов frontend: пройдены, включая 3 новых для UUID.
- TypeScript/Vite build и ESLint: пройдены.
- Playwright/Edge на реальном HTTP-сайте: 5 проверок, 0 JS errors, настоящий завершённый бэктест без API-моков. Первый прогон остановился на неподдерживаемой бирже в тестовом импорте; исправлен только тестовый payload на Bybit. Следующий прогон обнаружил нестабильность ожидания входа; добавлено ожидание успешного login HTTP-ответа. Последние два прогона прошли.

Изменены серверный Compose overlay, routing script/его тесты, admin-bot `main.go`/тесты/README, backtest `BacktestPage.tsx`, новый `requestKey.ts`, Node и HTTP browser regression tests. Структура и стиль страницы сохранены.

[Результат браузерной проверки](bot-backtest/result.json), [desktop](bot-backtest/backtest-http-desktop.png), [mobile](bot-backtest/backtest-http-mobile.png).

Backup исходников и build/log reports: `/opt/shortlong/reports/bot-backtest-fix-20261006`. Секреты в отчёт не включены.
