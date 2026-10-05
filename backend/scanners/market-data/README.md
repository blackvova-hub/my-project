# Market scanner

Сканер поддерживает публичные данные Bybit и Binance USDⓈ-M для perpetual-инструментов. Оркестрация работает только через `MarketAdapter`; биржевые форматы не выходят за пределы `BybitAdapter` и `BinanceAdapter`.

## Канонический ключ

Каждый инструмент идентифицируется тройкой:

```text
exchange:marketType:symbol
bybit:perpetual:BTCUSDT
binance:perpetual:BTCUSDT
```

Ключ используется во всех in-memory хранилищах scanner, поэтому одинаковый symbol с разных бирж не смешивается.

## Событие scanner v3

Верхний уровень сохраняет прежние числовые поля для совместимости. Поле `metrics` является источником информации о качестве данных:

```json
{
  "schemaVersion": 3,
  "instrument": {
    "exchange": "binance",
    "marketType": "perpetual",
    "symbol": "BTCUSDT"
  },
  "exchange": "binance",
  "marketType": "perpetual",
  "symbol": "BTCUSDT",
  "ts": 1800000,
  "metrics": {
    "delta": {
      "value": 12.5,
      "valid": true,
      "timestamp": 1859999,
      "source": "binance:websocket"
    }
  },
  "invalidMetrics": []
}
```

У каждой метрики есть значение, `valid`, timestamp источника/наблюдения и явный `source`. Некорректные или отсутствующие числа не маскируются под реальные данные: метрика получает `valid: false` и попадает в `invalidMetrics`.

## Источники данных

- Kline, open interest, account ratio, order book, mark/index price и funding: публичный REST соответствующей биржи.
- Trades/delta/CVD: WebSocket `publicTrade` (Bybit) или `aggTrade` (Binance). REST используется для snapshot/recovery, если WebSocket не покрывает целевую минуту.
- Liquidations: WebSocket `allLiquidation` (Bybit), `forceOrder` (Binance), `liquidation-orders` (OKX), `liquidation` (Bitget UTA) и `futures.public_liquidates` (Gate.io). OKX, Bitget и Gate.io запускаются в режиме `liquidationOnly` и не публикуют свечи, сделки или остальные индикаторы.
- Публичные потоки Binance, OKX, Bitget и Gate.io имеют snapshot-ограничения самих бирж. Источники явно помечаются как `*_snapshot`; worker суммирует доступные в этих потоках значения, ждёт валидную текущую минуту от всех пяти бирж и не создаёт сигнал при неполном покрытии.
- Для OKX размер контракта переводится в USD через актуальный `ctVal`, для Gate.io — через `quanto_multiplier`, а Bitget уже отдаёт `amount` в quote currency. Каталоги контрактов обновляются каждые 30 минут.

## Конфигурация

Основные переменные:

```text
EXCHANGE=bybit|binance|okx|bitget|gateio
MARKET_TYPE=perpetual
COMPACT_REDIS_STREAM=candles:v3:<exchange>:<marketType>
```

Для Binance доступны `BINANCE_FUTURES_BASE_URL` и `BINANCE_FUTURES_WS_URL`; для Bybit — `BYBIT_BASE_URL` и `BYBIT_WS_PUBLIC`. Для liquidation-only сервисов используются `OKX_BASE_URL`/`OKX_PUBLIC_WS_URL`, `BITGET_BASE_URL`/`BITGET_PUBLIC_WS_URL` и `GATEIO_BASE_URL`/`GATEIO_FUTURES_WS_URL`.

В корневом `compose.yaml` scanners публикуют compact batches в `candles:v3:<exchange>:perpetual`, а liquidation-only collectors — в `liquidations:v3:<exchange>:perpetual`. Workers читают пять источников одновременно; обычные правила остаются привязаны к выбранной бирже, а `liquidationsCombined` использует сумму Bybit + Binance + OKX + Bitget + Gate.io.

## Compact transport (production)

Scanners publish one minute/shard batch directly to the production streams:

```text
candles:v3:<exchange>:<marketType>
liquidations:v3:<exchange>:<marketType>
COMPACT_REDIS_STREAM=...
COMPACT_RETENTION_HOURS=26
```

The short JSON wire envelope uses `v=3` for the transport schema and `cv=3` for canonical metric semantics. Candle rows share a sorted catalog and fixed metric layout; liquidation batches omit zero rows while preserving coverage through `q`. Batch IDs are deterministic, so consumers can deduplicate retries.

Retention uses Redis server IDs with `XTRIM MINID ~`. Build, encode, publish, or trim failure fails the scan cycle; there is no secondary legacy publication path.
