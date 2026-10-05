package etherscan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"alert-engine/internal/onchain"
)

func TestTransfersParsesERC20AndNativeETH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("action") {
		case "eth_blockNumber":
			_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":"0xc8"}`))
		case "getblocknobytime":
			_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":"100"}`))
		case "tokentx":
			_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":[{"hash":"0xerc20","contractAddress":"0xtoken","tokenSymbol":"USDC","from":"0xFROM","to":"0xTO","value":"1250000","tokenDecimal":"6","timeStamp":"1767225600","blockNumber":"123"}]}`))
		case "txlist":
			_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":[{"hash":"0xeth","from":"0xFROM","to":"0xTO","value":"2000000000000000000","timeStamp":"1767225601","blockNumber":"124","isError":"0"}]}`))
		default:
			t.Fatalf("unexpected action %q", r.URL.Query().Get("action"))
		}
	}))
	defer srv.Close()

	p := New("key", "1")
	p.BaseURL = srv.URL
	batch, err := p.Transfers(context.Background(), "0xfrom", onchain.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Transfers) != 2 {
		t.Fatalf("transfers=%#v", batch.Transfers)
	}
	if got := batch.Transfers[0]; got.Symbol != "USDC" || got.TokenAmount != 1.25 || got.FromAddress != "0xfrom" {
		t.Fatalf("bad erc20 parse: %#v", got)
	}
	if got := batch.Transfers[1]; got.Symbol != "ETH" || got.TokenAmount != 2 || got.TokenAddress != "" {
		t.Fatalf("bad native parse: %#v", got)
	}
	if batch.Cursor.BlockNumber != 200 || batch.Cursor.UpdatedAt.IsZero() {
		t.Fatalf("cursor=%#v", batch.Cursor)
	}
}

func TestTransfersResumesAfterPersistentCursor(t *testing.T) {
	var starts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("action")
		if action == "getblocknobytime" {
			t.Fatal("block-by-time must not be requested for a persisted cursor")
		}
		if action == "eth_blockNumber" {
			_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":"0xc8"}`))
			return
		}
		starts = append(starts, r.URL.Query().Get("startblock"))
		_, _ = w.Write([]byte(`{"status":"0","message":"No transactions found","result":"No transactions found"}`))
	}))
	defer srv.Close()

	p := New("", "1")
	p.BaseURL = srv.URL
	batch, err := p.Transfers(context.Background(), "0x1", onchain.Cursor{BlockNumber: 150})
	if err != nil {
		t.Fatal(err)
	}
	if len(starts) != 2 || starts[0] != "151" || starts[1] != "151" {
		t.Fatalf("startblock values=%v", starts)
	}
	if batch.Cursor.BlockNumber != 200 {
		t.Fatalf("cursor=%#v", batch.Cursor)
	}
}

func TestTransfersPartialSuccessDoesNotAdvanceCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("action") {
		case "eth_blockNumber":
			_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":"0xc8"}`))
		case "tokentx":
			http.Error(w, "temporary", http.StatusBadGateway)
		case "txlist":
			_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":[{"hash":"0xeth","from":"0xFROM","to":"0xTO","value":"1000000000000000000","timeStamp":"1767225601","blockNumber":"160","isError":"0"}]}`))
		default:
			t.Fatalf("unexpected action %q", r.URL.Query().Get("action"))
		}
	}))
	defer srv.Close()

	p := New("", "1")
	p.BaseURL = srv.URL
	before := onchain.Cursor{BlockNumber: 150, Token: "opaque", UpdatedAt: time.Unix(100, 0).UTC()}
	batch, err := p.Transfers(context.Background(), "0x1", before)
	if err == nil || len(batch.Transfers) != 1 {
		t.Fatalf("transfers=%#v err=%v", batch.Transfers, err)
	}
	if batch.Cursor != before {
		t.Fatalf("partial fetch advanced cursor: before=%#v after=%#v", before, batch.Cursor)
	}
}

func TestTransfersNeverFallsBackToGenesis(t *testing.T) {
	requestedTransfers := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("action") {
		case "eth_blockNumber":
			_, _ = w.Write([]byte(`{"status":"1","message":"OK","result":"0xc8"}`))
		case "getblocknobytime":
			http.Error(w, "temporary", http.StatusBadGateway)
		default:
			requestedTransfers = true
		}
	}))
	defer srv.Close()

	p := New("", "1")
	p.BaseURL = srv.URL
	batch, err := p.Transfers(context.Background(), "0x1", onchain.Cursor{})
	if err == nil {
		t.Fatal("expected initial cursor error")
	}
	if requestedTransfers {
		t.Fatal("transfer endpoint was called after initial cursor lookup failed")
	}
	if batch.Cursor.BlockNumber != 0 {
		t.Fatalf("cursor=%#v", batch.Cursor)
	}
}
