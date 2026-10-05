package market

import (
	"testing"
	"time"
)

func TestReviewedAliasesAndBundles(t *testing.T) {
	allow := AssetSet(DefaultAssets)
	for _, c := range []struct {
		raw, asset string
		scale      float64
		ok         bool
	}{{"XBT", "BTC", 1, true}, {"1000PEPE", "PEPE", 1000, true}, {"1000BTC", "", 0, false}, {"UNKNOWN", "", 0, false}, {"1INCH", "", 0, false}} {
		a, s, ok := Asset(c.raw, allow)
		if a != c.asset || s != c.scale || ok != c.ok {
			t.Fatal(c, a, s, ok)
		}
	}
}
func TestBookRejectsDuplicatesAndInvalidMetadata(t *testing.T) {
	i := Instrument{Exchange: "binance", Market: "spot", Native: "BTCUSDT", Asset: "BTC", Step: .001, PriceScale: 1, QuantityScale: 1, TakerFee: .001}
	i.ID = i.Key()
	b := Book{Instrument: i, Bids: []Level{{99, 1}, {99, 2}}, Asks: []Level{{100, 1}}, ReceivedAt: time.Now().UnixMilli(), Valid: true}
	if b.Validate() == nil {
		t.Fatal("duplicate accepted")
	}
	b.Bids = b.Bids[:1]
	b.Instrument.MinQuantity = -1
	if b.Validate() == nil {
		t.Fatal("invalid minimum accepted")
	}
}
