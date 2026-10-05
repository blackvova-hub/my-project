package marketdata

import (
	"context"
	"os"
	bt "shortlong/backtest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMinuteArchiveRealClickHouse(t *testing.T) {
	endpoint := os.Getenv("BACKTEST_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("isolated ClickHouse not configured")
	}
	ch := NewClickHouse(endpoint, "backtest", os.Getenv("BACKTEST_TEST_CLICKHOUSE_PASSWORD"))
	ch.MinuteArchive = true
	ctx := context.Background()
	if e := ch.Init(ctx); e != nil {
		t.Fatal(e)
	}
	symbol := "QA" + strings.ToUpper(strconv.FormatInt(time.Now().UnixNano(), 36)) + "USDT"
	from := time.Now().Add(-time.Hour).Truncate(5 * time.Minute).UnixMilli()
	candles := []bt.Candle{}
	for i := 0; i < 5; i++ {
		candles = append(candles, bt.Candle{Time: from + int64(i)*60000, Open: 10, High: 12, Low: 9, Close: 11, Volume: 2})
	}
	for _, market := range []string{"spot", "linear"} {
		if e := ch.InsertMinutes(ctx, "bybit", market, symbol, candles); e != nil {
			t.Fatal(e)
		}
		if e := ch.InsertMinutes(ctx, "bybit", market, symbol, candles); e != nil {
			t.Fatal(e)
		}
		// Binance's same symbol has incomplete minutes and distinct prices.
		other := append([]bt.Candle(nil), candles[:4]...)
		for i := range other {
			other[i].Open = 20
			other[i].High = 22
			other[i].Low = 19
			other[i].Close = 21
		}
		if e := ch.InsertMinutes(ctx, "binance", market, symbol, other); e != nil {
			t.Fatal(e)
		}
		minute, e := ch.ReadMinutes(ctx, "bybit", market, symbol, "1m", from, from+300000)
		if e != nil || len(minute) != 5 {
			t.Fatalf("retry dedup: %d %v", len(minute), e)
		}
		bars, e := ch.Read(ctx, market, symbol, "5m", from, from+300000)
		if e != nil || len(bars) != 1 || bars[0].Close != 11 || bars[0].Volume != 10 {
			t.Fatalf("aggregate: %v %v", bars, e)
		}
		bars, e = ch.ReadMinutes(ctx, "binance", market, symbol, "5m", from, from+300000)
		if e != nil || len(bars) != 0 {
			t.Fatalf("gap fabricated a candle: %v %v", bars, e)
		}
	}
}
