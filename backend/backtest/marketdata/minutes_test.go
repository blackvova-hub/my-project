package marketdata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	bt "shortlong/backtest"
	"strings"
	"testing"
)

func TestMinuteArchiveExchangeIsolationAndCompleteAggregation(t *testing.T) {
	seen := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		seen = append(seen, q.Get("param_exchange"))
		sql := q.Get("query")
		for _, want := range []string{"candles_1m FINAL", "exchange={exchange:String}", "HAVING count()=intDiv"} {
			if !strings.Contains(sql, want) {
				t.Errorf("missing %s", want)
			}
		}
		if q.Get("param_step") != "300000" {
			t.Error("wrong aggregate duration")
		}
		fmt.Fprint(w, `{"bucket":300000,"open":1,"high":2,"low":1,"close":2,"volume":5}`)
	}))
	defer srv.Close()
	ch := NewClickHouse(srv.URL, "", "")
	for _, venue := range []string{"bybit", "binance"} {
		rows, e := ch.ReadMinutes(context.Background(), venue, "spot", "BTCUSDT", "5m", 300000, 600000)
		if e != nil || len(rows) != 1 || rows[0].Time != 300000 {
			t.Fatalf("%v %v", rows, e)
		}
	}
	if strings.Join(seen, ",") != "bybit,binance" {
		t.Fatal(seen)
	}
	if _, e := ch.ReadMinutes(context.Background(), "other", "spot", "BTCUSDT", "1m", 0, 60000); e == nil {
		t.Fatal("invalid venue accepted")
	}
}

func TestMinuteAggregateRejectsGaps(t *testing.T) {
	base := []bt.Candle{}
	for i := 0; i < 5; i++ {
		base = append(base, bt.Candle{Time: 300000 + int64(i)*60000, Open: 1, High: 2, Low: 1, Close: 2, Volume: 1})
	}
	out, e := aggregateFrom(base, "5m", 300000, 600000, 60000)
	if e != nil || len(out) != 1 || out[0].Volume != 5 {
		t.Fatalf("%v %v", out, e)
	}
	if _, e = aggregateFrom(base[:4], "5m", 300000, 600000, 60000); e == nil {
		t.Fatal("gap accepted")
	}
}

func TestBinanceMinuteParsingAndFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "exchangeInfo") {
			io.WriteString(w, `{"symbols":[{"symbol":"BTCUSDT","status":"TRADING","quoteAsset":"USDT","contractType":"PERPETUAL"},{"symbol":"BTCUSDC","status":"TRADING","quoteAsset":"USDC","contractType":"PERPETUAL"},{"symbol":"OLDUSDT","status":"BREAK","quoteAsset":"USDT","contractType":"PERPETUAL"},{"symbol":"FUTUSDT","status":"TRADING","quoteAsset":"USDT","contractType":"CURRENT_QUARTER"}]}`)
			return
		}
		if r.URL.Query().Get("interval") != "1m" || r.URL.Query().Get("endTime") != "419999" {
			t.Error(r.URL.RawQuery)
		}
		io.WriteString(w, `[[300000,"1","2","1","2","3"],[360000,"2","3","1","2","4"]]`)
	}))
	defer srv.Close()
	b := NewBinance(srv.URL, srv.URL, nil)
	cat, e := b.Instruments(context.Background(), "linear")
	if e != nil || len(cat) != 1 || cat[0].Symbol != "BTCUSDT" {
		t.Fatalf("%v %v", cat, e)
	}
	rows, e := b.Candles(context.Background(), "linear", "BTCUSDT", "1m", 300000, 420000)
	if e != nil || len(rows) != 2 || rows[1].Volume != 4 {
		t.Fatalf("%v %v", rows, e)
	}
}

func TestMinuteModeRefusesFiveMinuteWrites(t *testing.T) {
	ch := &ClickHouse{MinuteArchive: true}
	if e := ch.Insert(context.Background(), "spot", "BTCUSDT", "5m", nil); e == nil {
		t.Fatal("5m write accepted")
	}
}
