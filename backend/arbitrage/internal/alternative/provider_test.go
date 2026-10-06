package alternative

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"shortlong/arbitrage/internal/market"
	"strings"
	"testing"
	"time"
)

func TestUSDReferenceRejectsCrossedAndIssuerErrors(t *testing.T) {
	for _, test := range []struct {
		body string
		ok   bool
	}{
		{`{"error":[],"result":{"USDTZUSD":{"bids":[["0.99","20",1]],"asks":[["0.991","10",1]]}}}`, true},
		{`{"error":[],"result":{"USDTZUSD":{"bids":[["1.01","20"]],"asks":[["1","10"]]}}}`, false},
		{`{"error":["unavailable"],"result":{}}`, false},
	} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, test.body) }))
		p := New()
		p.KrakenURL = s.URL
		q, err := p.usd(context.Background(), "USDT")
		s.Close()
		if (err == nil) != test.ok {
			t.Fatal(err)
		}
		if test.ok && (q.Ask != .991 || q.ObservedAt == 0) {
			t.Fatal(q)
		}
	}
}
func TestReviewedPoolCatalogDoesNotAdmitTickerImpostors(t *testing.T) {
	tokens := []string{"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"}
	rows := []map[string]any{}
	for _, project := range []string{"uniswap-v3", "other"} {
		rows = append(rows, map[string]any{"pool": project, "chain": "Ethereum", "project": project, "symbol": "USDC-WETH", "tvlUsd": 200000, "apyBase": 4.5, "poolMeta": "0.05%", "underlyingTokens": tokens})
	}
	rows = append(rows, map[string]any{"pool": "impostor", "chain": "Ethereum", "project": "uniswap-v3", "symbol": "USDC-WETH", "tvlUsd": 1000000, "apyBase": 100, "poolMeta": "0.05%", "underlyingTokens": []string{tokens[0], "0x0000000000000000000000000000000000000001"}})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(map[string]any{"data": rows}) }))
	defer s.Close()
	p := New()
	p.YieldsURL = s.URL
	list, err := p.yields(context.Background())
	if err != nil || len(list) != 1 || list[0].Asset != "ETH" || list[0].TickSpacing != 10 {
		t.Fatal(list, err)
	}
}
func TestRPCPinnedBlockChainIdentityAndTickPrice(t *testing.T) {
	chain := "0x1"
	seenBlock := false
	root := new(big.Int).Lsh(big.NewInt(1), 96)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw json.RawMessage
		json.NewDecoder(r.Body).Decode(&raw)
		if strings.HasPrefix(string(raw), "[") {
			var calls []rpcRequest
			json.Unmarshal(raw, &calls)
			responses := []map[string]any{}
			for _, call := range calls {
				args := call.Params.([]any)
				if args[1] != "0x123" {
					t.Error("mixed chain blocks")
				}
				seenBlock = true
				data := args[0].(map[string]any)["data"].(string)
				value := "0x" + fmt.Sprintf("%064x", 123)
				if data == "0x3850c7bd" {
					value = "0x" + fmt.Sprintf("%064x%064x", root, 0) + strings.Repeat("0", 64*5)
				}
				responses = append(responses, map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": value})
			}
			json.NewEncoder(w).Encode(responses)
			return
		}
		var call rpcRequest
		json.Unmarshal(raw, &call)
		var result any = chain
		if call.Method == "eth_getBlockByNumber" {
			result = map[string]any{"number": "0x123", "timestamp": fmt.Sprintf("0x%x", time.Now().Unix())}
		}
		json.NewEncoder(w).Encode(map[string]any{"id": call.ID, "result": result})
	}))
	defer s.Close()
	p := New()
	p.RPC["Ethereum"] = s.URL
	pool := market.LPPool{Token0: "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", Token1: "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2", Fee: 500, TickSpacing: 10}
	list, err := p.prices(context.Background(), "Ethereum", []market.LPPool{pool})
	if err != nil || len(list) != 1 || !seenBlock || list[0].PriceUSDC != 1e12 || list[0].TickPriceUSDC != 1e12 {
		t.Fatal(list, err)
	}
	chain = "0xa4b1"
	if _, err := p.prices(context.Background(), "Ethereum", []market.LPPool{pool}); err == nil {
		t.Fatal("wrong chain accepted")
	}
}
