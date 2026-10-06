package verification

import (
	"encoding/json"
	"math"
	"shortlong/arbitrage/internal/engine"
	"shortlong/arbitrage/internal/market"
	"testing"
	"time"
)

func flag(b bool) *bool { return &b }
func fixture(now time.Time) (engine.Opportunity, market.Book, market.Book, market.VenueEvidence, market.VenueEvidence) {
	i := market.Instrument{Exchange: "bitget", Market: "spot", Native: "ETHUSDT", Asset: "ETH", Step: .001, PriceScale: 1, QuantityScale: 1, TakerFee: .001, DelistingKnown: true, CatalogAt: now.UnixMilli()}
	i.ID = i.Key()
	b := market.Book{Instrument: i, Bids: []market.Level{{Price: 99, Quantity: 100}}, Asks: []market.Level{{Price: 100, Quantity: .5}, {Price: 102, Quantity: 2}}, Valid: true, ReceivedAt: now.UnixMilli()}
	j := i
	j.Exchange = "kucoin"
	j.Native = "ETH-USDT"
	j.ID = j.Key()
	s := b
	s.Instrument = j
	s.Asks = []market.Level{{Price: 110, Quantity: 100}}
	s.Bids = []market.Level{{Price: 106, Quantity: 100}}
	w := market.WalletEvidence{At: now.UnixMilli(), Chains: []market.Chain{{ID: "eip155:1", Name: "eth", Deposit: flag(true), Withdraw: flag(true), WithdrawFee: ptr(.01), WithdrawRate: ptr(0), MinWithdraw: ptr(.001), MinDeposit: ptr(.001)}}}
	be := market.VenueEvidence{Instrument: &i, Trading: flag(true), CatalogAt: now.UnixMilli(), Wallet: w}
	se := be
	se.Instrument = &j
	return engine.Opportunity{Strategy: "spot-spot"}, b, s, be, se
}
func getCheck(p Path, key string) Check {
	for _, c := range p.Checks {
		if c.Key == key {
			return c
		}
	}
	panic(key)
}
func TestExactBudgetAndTransferQuantityNeverBecomeNetProfit(t *testing.T) {
	now := time.Now()
	o, b, s, be, se := fixture(now)
	r := Assess(o, b, s, be, se, 100, now)
	if len(r.Paths) != 2 {
		t.Fatal(r)
	}
	for _, p := range r.Paths {
		if getCheck(p, "vwap").State != "pass" || getCheck(p, "fees").State != "unknown" || p.NetProfit != nil || p.State != "incomplete" {
			t.Fatal(p)
		}
		if math.Abs(*p.Calculation.BuyQuantity-.99) > 1e-8 || math.Abs(*p.Calculation.BuyVWAP-99.98/.99) > 1e-8 {
			t.Fatal(p.Calculation)
		}
	}
	if math.Abs(*r.Paths[1].Calculation.SellQuantity-.98) > 1e-8 {
		t.Fatal(r.Paths[1].Calculation)
	}
	raw, _ := json.Marshal(r)
	var decoded map[string]any
	json.Unmarshal(raw, &decoded)
	if decoded["paths"].([]any)[0].(map[string]any)["netProfit"] != nil {
		t.Fatal("unknown encoded as zero")
	}
}
func TestClosedWithdrawalBlocksTransferOnly(t *testing.T) {
	now := time.Now()
	o, b, s, be, se := fixture(now)
	be.Wallet.Chains = append([]market.Chain{}, be.Wallet.Chains...)
	be.Wallet.Chains[0].Withdraw = flag(false)
	r := Assess(o, b, s, be, se, 100, now)
	if r.Paths[0].State == "blocked" || getCheck(r.Paths[0], "wallet").State != "na" || r.Paths[1].State != "blocked" {
		t.Fatal(r.Paths)
	}
}
func TestTickerDoesNotProveAssetAndNetworksMustMatch(t *testing.T) {
	now := time.Now()
	o, b, s, be, se := fixture(now)
	be.Wallet.Chains = append([]market.Chain{}, be.Wallet.Chains...)
	se.Wallet.Chains = append([]market.Chain{}, se.Wallet.Chains...)
	be.Wallet.Chains[0].Contract = "0xaaa"
	se.Wallet.Chains[0].Contract = "0xbbb"
	r := Assess(o, b, s, be, se, 100, now)
	if getCheck(r.Paths[0], "identity").State != "fail" || r.Paths[0].NetProfit != nil {
		t.Fatal(r)
	}
	se.Wallet.Chains[0].ID = "tron:mainnet"
	se.Wallet.Chains[0].Contract = "0xaaa"
	r = Assess(o, b, s, be, se, 100, now)
	if getCheck(r.Paths[1], "network").State == "pass" || getCheck(r.Paths[0], "identity").State == "pass" {
		t.Fatal(r)
	}
	be.Wallet.At = now.Add(-61 * time.Second).UnixMilli()
	se.Wallet.At = be.Wallet.At
	if getCheck(Assess(o, b, s, be, se, 100, now).Paths[0], "identity").State != "unknown" {
		t.Fatal("stale metadata passed")
	}
}
func TestStaleQuotesDelistingAndPartialBooksBlock(t *testing.T) {
	now := time.Now()
	o, b, s, be, se := fixture(now)
	b.SourceAt = now.Add(-6 * time.Second).UnixMilli()
	if Assess(o, b, s, be, se, 100, now).Paths[0].State != "blocked" {
		t.Fatal("stale quote passed")
	}
	b.SourceAt = 0
	be.Instrument.DelistingAt = now.Add(time.Hour).UnixMilli()
	if getCheck(Assess(o, b, s, be, se, 100, now).Paths[0], "delisting").State != "fail" {
		t.Fatal("scheduled delisting passed")
	}
	be.Instrument.DelistingAt = 0
	if getCheck(Assess(o, b, s, be, se, 1000, now).Paths[0], "vwap").State != "fail" {
		t.Fatal("partial filled full amount")
	}
}
func TestMissingWithdrawalFeeDoesNotReusePreTransferVWAP(t *testing.T) {
	now := time.Now()
	o, b, s, be, se := fixture(now)
	be.Wallet.Chains = append([]market.Chain{}, be.Wallet.Chains...)
	be.Wallet.Chains[0].WithdrawFee = nil
	p := Assess(o, b, s, be, se, 100, now).Paths[1]
	if p.Calculation.SellQuantity != nil || p.Calculation.SellVWAP != nil || p.Calculation.GrossUSDT != nil || getCheck(p, "vwap").State != "unknown" {
		t.Fatal(p)
	}
}
