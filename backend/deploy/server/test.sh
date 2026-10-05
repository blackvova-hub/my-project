#!/bin/sh
set -eu
cd /src/backend
export GOWORK=off GOMAXPROCS=2
export BACKTEST_TEST_DATABASE_URL='postgres://postgres:backtest-test-only@127.0.0.1:25432/backtest_test?sslmode=disable'
export BACKTEST_TEST_CLICKHOUSE_URL='http://127.0.0.1:28123'
export BACKTEST_TEST_CLICKHOUSE_PASSWORD='backtest-test-only'
export BACKTEST_TEST_REDIS_ADDR='127.0.0.1:26379'
export SIMILARITY_TEST_DATABASE_URL="$BACKTEST_TEST_DATABASE_URL"
export SIMILARITY_TEST_CLICKHOUSE_URL="$BACKTEST_TEST_CLICKHOUSE_URL"
export SIMILARITY_TEST_CLICKHOUSE_PASSWORD="$BACKTEST_TEST_CLICKHOUSE_PASSWORD"
export SIMILARITY_TEST_REDIS_ADDR="$BACKTEST_TEST_REDIS_ADDR"
export SIMILARITY_TEST_QDRANT_URL='http://127.0.0.1:26333'
# Exercise the real fresh-database migration runner before dependent modules.
(cd api && TEST_DATABASE_URL="$BACKTEST_TEST_DATABASE_URL" go test -p 2 ./internal/database -run '^TestApplyMigrationsSmoke$' -count=1)
for module in backtest similarity api scanners/market-data workers/alert-engine news/collector telegram/news-publisher; do
  printf '\nTesting %s\n' "$module"
  (cd "$module" && go test -p 2 ./... -count=1 -timeout=10m && go vet -p 2 ./...)
done
