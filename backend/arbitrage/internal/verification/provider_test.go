package verification

import (
	"context"
	"net/http"
	"net/http/httptest"
	"shortlong/arbitrage/internal/market"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderDeduplicatesConcurrentCatalogAndWalletRequests(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.Contains(r.URL.Path, "currencies") {
			w.Write([]byte(`{"code":"200000","data":{"currency":"ETH","chains":[{"chainId":"eth","contractAddress":"","isDepositEnabled":true,"isWithdrawEnabled":true}]}}`))
		} else {
			w.Write([]byte(`{"code":"200000","data":[{"symbol":"ETH-USDT","baseCurrency":"ETH","quoteCurrency":"USDT","enableTrading":true,"baseIncrement":"0.001","baseMinSize":"0.001","minFunds":"1"}]}`))
		}
	}))
	defer server.Close()
	p := New()
	p.clients["kucoin"].BaseSpot = server.URL
	i := market.Instrument{ID: "kucoin:spot:ETH-USDT", Exchange: "kucoin", Market: "spot", Asset: "ETH"}
	var wg sync.WaitGroup
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := p.Evidence(context.Background(), i)
			if e.Trading == nil || !*e.Trading || e.Wallet.At == 0 {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 2 {
		t.Fatal("duplicate requests", calls.Load())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	i.ID = "kucoin:spot:MISSING-USDT"
	e := p.Evidence(ctx, i)
	if e.Trading == nil || *e.Trading {
		t.Fatal("missing market accepted")
	}
}
