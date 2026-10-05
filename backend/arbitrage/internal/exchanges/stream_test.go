package exchanges

import (
	"encoding/json"
	"shortlong/arbitrage/internal/market"
	"testing"
	"time"
)

func instrument(e string) market.Instrument {
	i := market.Instrument{Exchange: e, Market: "perpetual", Native: "BTCUSDT", Asset: "BTC", PriceScale: 1, QuantityScale: 1, Step: .001, TakerFee: .001}
	i.ID = i.Key()
	return i
}
func TestSixSnapshotProtocols(t *testing.T) {
	cases := map[string]string{
		"binance": `{"stream":"btcusdt@depth5@100ms","data":{"E":1800000000000,"u":42,"b":[["99","2"]],"a":[["100","3"]]}}`,
		"bybit":   `{"topic":"orderbook.1.BTCUSDT","type":"snapshot","ts":1800000000000,"data":{"s":"BTCUSDT","u":42,"b":[["99","2"]],"a":[["100","3"]]}}`,
		"okx":     `{"arg":{"instId":"BTCUSDT"},"data":[{"ts":"1800000000000","seqId":42,"bids":[["99","2","0","1"]],"asks":[["100","3","0","1"]]}]}`,
		"bitget":  `{"arg":{"instId":"BTCUSDT"},"action":"snapshot","data":[{"ts":"1800000000000","seq":42,"bids":[["99","2"]],"asks":[["100","3"]]}]}`,
		"gateio":  `{"event":"all","result":{"contract":"BTCUSDT","t":1800000000000,"id":42,"bids":[{"p":"99","s":2}],"asks":[{"p":"100","s":3}]}}`,
		"kucoin":  `{"type":"message","topic":"/contractMarket/level2Depth5:BTCUSDT","data":{"timestamp":1800000000000,"sequence":42,"bids":[["99","2"]],"asks":[["100","3"]]}}`,
	}
	for e, raw := range cases {
		t.Run(e, func(t *testing.T) {
			i := instrument(e)
			i.QuantityScale = .01
			b, ok, err := DecodeBook(e, "perpetual", []byte(raw), map[string]market.Instrument{"BTCUSDT": i}, time.Unix(1800000000, 0))
			if err != nil || !ok || b.Bids[0].Quantity != .02 || b.Asks[0].Price != 100 || b.Sequence != 42 {
				t.Fatal(b, ok, err)
			}
		})
	}
}
func TestRejectsErrorsDeltasAndCrossedBooks(t *testing.T) {
	i := instrument("bybit")
	for _, raw := range []string{`{"event":"error"}`, `{"topic":"orderbook.1.BTCUSDT","type":"delta","data":{}}`, `{"topic":"orderbook.1.BTCUSDT","type":"snapshot","data":{"s":"BTCUSDT","b":[["100","2"]],"a":[["99","2"]]}}`} {
		if _, ok, err := DecodeBook("bybit", "perpetual", []byte(raw), map[string]market.Instrument{"BTCUSDT": i}, time.Now()); err == nil || ok {
			t.Fatal(raw, ok, err)
		}
	}
	if _, ok, err := DecodeBook("gateio", "perpetual", []byte(`{"event":"update","result":{"contract":"BTCUSDT","bids":[{"p":"99","s":1}]}}`), map[string]market.Instrument{"BTCUSDT": instrument("gateio")}, time.Now()); ok || err != nil {
		t.Fatal("partial gate delta treated as snapshot")
	}
}
func TestGateSpotAndBundledPriceScale(t *testing.T) {
	i := instrument("gateio")
	i.Market = "spot"
	i.PriceScale = 1000
	i.QuantityScale = 1000
	i.ID = i.Key()
	raw := `{"event":"update","result":{"s":"BTCUSDT","t":1800000000000,"bids":[["99","2"]],"asks":[["100","3"]]}}`
	b, ok, err := DecodeBook("gateio", "spot", []byte(raw), map[string]market.Instrument{"BTCUSDT": i}, time.Now())
	if !ok || err != nil || b.Asks[0].Price != .1 || b.Asks[0].Quantity != 3000 {
		t.Fatal(b, err)
	}
}
func TestNumbersDoNotMakeUnknownFundingZero(t *testing.T) {
	for _, v := range []any{nil, "", json.Number("NaN"), "bad"} {
		if text(v) == "0" {
			t.Fatal(v)
		}
	}
}
