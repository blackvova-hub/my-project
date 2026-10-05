package analytics

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

func closeTo(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-7 {
		t.Fatalf("got %.10f want %.10f", got, want)
	}
}
func fill(id, side string, q, price, fee float64, at int64) Fill {
	return Fill{ID: id, ConnectionID: "account-1", Exchange: "binance", Symbol: "BTCUSDT", Market: "linear", PositionSide: "BOTH", Side: side, Quantity: q, Price: price, Fee: fee, FeeKnown: true, FeeCurrency: "USDT", At: at}
}
func TestReconstructPartialEntriesExits(t *testing.T) {
	fills := []Fill{fill("1", "BUY", .1, 100, 1, 1000), fill("2", "BUY", .2, 130, 2, 2000), fill("3", "SELL", .05, 140, .5, 3000), fill("4", "SELL", .25, 150, 2.5, 5000)}
	trades := Reconstruct(fills, []Ledger{{ConnectionID: "account-1", Symbol: "BTCUSDT", Category: "funding", Currency: "USDT", Amount: -2, At: 2500}})
	if len(trades) != 1 {
		t.Fatal(trades)
	}
	r := trades[0]
	closeTo(t, r.Entry, 120)
	closeTo(t, r.Exit, 148.33333333333334)
	closeTo(t, r.Gross, 8.5)
	closeTo(t, r.Fees, 6)
	closeTo(t, r.Net, .5)
	if r.ClosedAt == nil || len(r.Fills) != 4 || !r.Complete {
		t.Fatal(r)
	}
}
func TestReversalSplitsInventoryAndFees(t *testing.T) {
	r := Reconstruct([]Fill{fill("a", "BUY", 1, 100, 1, 1000), fill("b", "SELL", 2, 110, 2, 2000), fill("c", "BUY", 1, 90, 1, 3000)}, nil)
	if len(r) != 2 {
		t.Fatal(r)
	}
	closeTo(t, r[0].Net, 18)
	closeTo(t, r[1].Net, 8)
	closeTo(t, r[0].Fees+r[1].Fees, 4)
}
func TestHedgeModeNeverNetsOpposingPositions(t *testing.T) {
	a := fill("a", "BUY", 1, 100, 1, 1000)
	a.PositionSide = "LONG"
	b := fill("b", "SELL", 1, 110, 1, 2000)
	b.PositionSide = "SHORT"
	r := Reconstruct([]Fill{a, b}, []Ledger{{ConnectionID: "account-1", Symbol: "BTCUSDT", Category: "funding", Currency: "USDT", Amount: -8, At: 2500}})
	if len(r) != 2 || r[0].ClosedAt != nil || r[1].ClosedAt != nil {
		t.Fatal(r)
	}
	closeTo(t, r[0].Funding+r[1].Funding, 0)
}
func TestMissingOpeningAndUnvaluedFeesAreIncomplete(t *testing.T) {
	f := fill("a", "SELL", 1, 100, 1, 1000)
	q := 1.0
	f.ClosedQuantity = &q
	r := Reconstruct([]Fill{f}, nil)
	if r[0].Complete {
		t.Fatal("unknown opening cost considered complete")
	}
	f = fill("b", "BUY", 1, 100, 1, 1000)
	f.FeeKnown = false
	r = Reconstruct([]Fill{f, fill("c", "SELL", 1, 110, 1, 2000)}, nil)
	if r[0].Complete {
		t.Fatal("unvalued fee considered complete")
	}
}
func TestExcursionsTrackPartialInventory(t *testing.T) {
	r := Reconstruct([]Fill{fill("a", "BUY", 2, 100, 0, 60000), fill("b", "SELL", 1, 110, 0, 180000), fill("c", "SELL", 1, 108, 0, 360000)}, nil)[0]
	mfe, mae, captured := Excursions(r, []Candle{{Time: 120, High: 112, Low: 98}, {Time: 240, High: 115, Low: 104}})
	if mfe == nil || mae == nil || captured == nil {
		t.Fatal("missing excursion")
	}
	closeTo(t, *mfe, 25)
	closeTo(t, *mae, -4)
	closeTo(t, *captured, 72)
}
func TestEncryptionBindsCredentialsToOwner(t *testing.T) {
	block, _ := aes.NewCipher(make([]byte, 32))
	gcm, _ := cipher.NewGCM(block)
	a := API{encryption: gcm}
	b, err := a.seal("one", Credentials{"key", "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret") {
		t.Fatal("plaintext secret")
	}
	got, err := a.open("one", b)
	if err != nil || got.Secret != "secret" {
		t.Fatal(got, err)
	}
	if _, err = a.open("two", b); err == nil {
		t.Fatal("different owner decrypted credential")
	}
	b[len(b)-1] ^= 1
	if _, err = a.open("one", b); err == nil {
		t.Fatal("modified ciphertext accepted")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestReadOnlyValidationFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		exchange, body string
		ok             bool
	}{{"bybit", `{"retCode":0,"result":{"readOnly":1}}`, true}, {"bybit", `{"retCode":0,"result":{"readOnly":0}}`, false}, {"bybit", `{"retCode":0,"result":{}}`, false}, {"binance", `{"enableReading":true,"enableWithdrawals":false}`, true}, {"binance", `{"enableReading":true,"enableFutures":true}`, false}, {"binance", `{"enableReading":true,"permitsUniversalTransfer":true}`, false}} {
		t.Run(tc.exchange+tc.body, func(t *testing.T) {
			x := newExchange(tc.exchange, Credentials{"test-key", "test-secret"})
			x.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					t.Fatal("mutation attempted")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
			})
			err := x.validate(context.Background())
			if (err == nil) != tc.ok {
				t.Fatalf("read-only validation: %v", err)
			}
		})
	}
}
