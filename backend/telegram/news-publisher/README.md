# Telegram news publisher

Небольшой сервис, который читает новые записи из PostgreSQL-таблицы `news_items`
и публикует их в Telegram-канал. Внешние сайты он не парсит: наполнение
`news_items` остаётся задачей существующего `news-bot`.

## Настройка

1. Добавьте бота в канал администратором с правом публикации сообщений.
2. Скопируйте `.env.example` в `.env` или добавьте в существующий `.env`:

```env
TELEGRAM_CHANNEL_ID=@channel_username
```

Для приватного канала используется числовой ID вида `-1001234567890`.

Сервис принимает `PG_DSN`/`DATABASE_URL` либо собирает подключение из
`PG_HOST`, `PG_PORT`, `PG_USER`, `PG_DATABASE`, `PG_SSLMODE` и
`POSTGRES_PASSWORD`.

## Как работает дедупликация

При старте сервис создаёт в той же БД таблицу `telegram_news_publications`.
Ключом является пара `news_id + channel_id`. Успешно отправленная новость
повторно не публикуется. Неуспешная отправка повторяется с задержкой от 15 секунд
до 5 минут. После аварийного завершения зависшая публикация возвращается в
очередь через `NEWS_CLAIM_TIMEOUT`.

Первый запуск берёт только новости, добавленные за `NEWS_STARTUP_LOOKBACK`,
по умолчанию за два часа, чтобы не выложить в канал всю историю базы.

## Локальный запуск

```bash
go mod download
go test ./...
set -a; . ./.env; set +a
go run ./cmd/news-publisher
```

PowerShell:

```powershell
Get-Content .env | Where-Object { $_ -match '^[^#].+=' } | ForEach-Object {
  $name, $value = $_ -split '=', 2
  Set-Item -Path "Env:$name" -Value $value
}
go run ./cmd/news-publisher
```

## Docker

Сервис подключён к корневому `compose.yaml` backend-сервера. Запуск из
корня проекта:

```bash
docker compose up -d --build telegram_news_publisher
docker compose logs -f telegram_news_publisher
```

Отдельная сборка при необходимости:

```bash
docker build -t telegram-news-publisher .
docker run --env-file .env telegram-news-publisher
```
