param(
    [ValidateSet('start', 'status', 'logs', 'audit')]
    [string]$Action = 'status'
)

$ErrorActionPreference = 'Stop'
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$historyComposeArgs = @('compose', '-f', (Join-Path $PSScriptRoot 'compose.yaml'), '-f', (Join-Path $PSScriptRoot 'compose.local.yaml'))

switch ($Action) {
    'start' {
        & docker @historyComposeArgs up -d --build --force-recreate backend clickhouse backtest_worker market_history frontend
    }
    'status' {
        & docker @historyComposeArgs exec -T market_history /app/history-sync -status
    }
    'logs' {
        & docker @historyComposeArgs logs --tail 50 -f market_history
    }
    'audit' {
        # Aggregate only real, deduplicated source rows; gaps before the first
        # available candle are deliberately excluded from internal-gap counts.
        @'
SELECT market, count() AS series, sum(candles) AS candles_5m,
       min(first_utc) AS first_utc, max(last_utc) AS last_utc,
       countIf(missing_slots > 0) AS series_with_internal_gaps,
       sum(missing_slots) AS internal_missing_slots
FROM (
    SELECT market, symbol, count() AS candles,
           fromUnixTimestamp64Milli(min(time),'UTC') AS first_utc,
           fromUnixTimestamp64Milli(max(time),'UTC') AS last_utc,
           intDiv(max(time)-min(time),300000)+1-count() AS missing_slots
    FROM shortlong_backtest.candles FINAL
    WHERE timeframe='5m'
    GROUP BY market,symbol
) GROUP BY market ORDER BY market FORMAT PrettyCompactMonoBlock;
SELECT formatReadableSize(sum(bytes_on_disk)) AS candle_storage
FROM system.parts WHERE active AND database='shortlong_backtest' AND table='candles'
FORMAT PrettyCompactMonoBlock;
'@ | & docker @historyComposeArgs exec -T clickhouse sh -c 'exec clickhouse-client --user "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" --multiquery'
        if ($LASTEXITCODE -ne 0) { throw 'ClickHouse audit failed' }
        @'
SELECT market,symbol,last_error,failures,retry_at
FROM market_history_series WHERE active AND last_error<>'' ORDER BY updated_at DESC LIMIT 20;
SELECT market,symbol,count(*) AS incomplete_ranges,sum(missing_candles) AS missing_source_bars
FROM market_history_gaps GROUP BY market,symbol ORDER BY missing_source_bars DESC LIMIT 20;
'@ | & docker @historyComposeArgs exec -T postgres psql -U postgres -d alerts -v ON_ERROR_STOP=1
    }
}
if ($LASTEXITCODE -ne 0) { throw "History command failed with exit code $LASTEXITCODE" }
