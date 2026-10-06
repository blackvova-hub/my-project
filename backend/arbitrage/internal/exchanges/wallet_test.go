package exchanges

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWalletEvidenceParsersPreserveUnknownAndDisabled(t *testing.T) {
	fixtures := map[string]string{
		"gateio": `[{"chain":"ETH","contract_address":"0xAbC","is_disabled":0,"is_deposit_disabled":0,"is_withdraw_disabled":1}]`,
		"bitget": `{"code":"00000","data":[{"coin":"ABC","chains":[{"chain":"ETH","contractAddress":"0xAbC","withdrawable":"false","rechargeable":"true","withdrawFee":"0.5","extraWithdrawFee":"0","minWithdrawAmount":"2","minDepositAmount":"1"}]}]}`,
		"kucoin": `{"code":"200000","data":{"currency":"ABC","chains":[{"chainId":"erc20","contractAddress":"0xAbC","isWithdrawEnabled":false,"isDepositEnabled":true,"withdrawalMinFee":"0.5","withdrawFeeRate":"0","withdrawalMinSize":"2","depositMinSize":"1"}]}}`,
	}
	for exchange, body := range fixtures {
		t.Run(exchange, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-BAPI-API-KEY") != "" || r.Method != "GET" {
					t.Error("private access")
				}
				w.Write([]byte(body))
			}))
			defer server.Close()
			c := New(exchange, "*")
			c.BaseSpot = server.URL
			x := c.Wallet(context.Background(), "ABC")
			if x.At == 0 || len(x.Chains) != 1 {
				t.Fatal(x)
			}
			ch := x.Chains[0]
			if ch.ID != "eip155:1" || ch.Contract != "0xabc" || ch.Deposit == nil || !*ch.Deposit || ch.Withdraw == nil || *ch.Withdraw {
				t.Fatal(ch)
			}
			if exchange == "gateio" && (ch.WithdrawFee != nil || ch.MinDeposit != nil) {
				t.Fatal("missing values became zero")
			}
		})
	}
}
func TestWalletUnknownFieldsErrorsAndCaseSensitiveChains(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":"00000","data":[{"coin":"ABC","chains":[{"chain":"SOL","contractAddress":"CaseSensitiveMint","withdrawFee":"invalid"}]}]}`))
	}))
	defer server.Close()
	c := New("bitget", "*")
	c.BaseSpot = server.URL
	w := c.Wallet(context.Background(), "ABC")
	ch := w.Chains[0]
	if ch.Contract != "CaseSensitiveMint" || ch.Withdraw != nil || ch.Deposit != nil || ch.WithdrawFee != nil {
		t.Fatal(ch)
	}
	for _, e := range []string{"binance", "bybit", "okx"} {
		w := New(e, "*").Wallet(context.Background(), "BTC")
		if w.At != 0 || len(w.Chains) != 0 || w.Message == "" {
			t.Fatal(w)
		}
	}
	if ChainID("ERC20") == ChainID("TRC20") || ChainID("unknown") != "" {
		t.Fatal("fuzzy network matching")
	}
}
