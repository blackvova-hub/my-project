package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newLiquidationWebsocketServer(t *testing.T, validate func(*http.Request, map[string]any) error, messages []any) (*httptest.Server, <-chan error) {
	t.Helper()
	errorsCh := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			errorsCh <- err
			return
		}
		defer conn.Close()
		var subscription map[string]any
		if err := conn.ReadJSON(&subscription); err != nil {
			errorsCh <- err
			return
		}
		if err := validate(r, subscription); err != nil {
			errorsCh <- err
			return
		}
		for _, message := range messages {
			if err := conn.WriteJSON(message); err != nil {
				errorsCh <- err
				return
			}
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	return server, errorsCh
}

func assertLiquidationFeedBucket(t *testing.T, feed liquidationFeed, catalog *liquidationCatalog, nativeSymbol string, ts int64, wantTotal, wantLong, wantShort float64, serverErrors <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewLiquidationStore()
	errCh := make(chan error, 1)
	go func() { errCh <- feed.Consume(ctx, catalog, store) }()
	instrument, ok := catalog.lookup(nativeSymbol)
	if !ok {
		t.Fatalf("native symbol %s missing from catalog", nativeSymbol)
	}
	key := NewInstrumentKey(feed.Exchange(), MarketTypePerpetual, instrument.CanonicalSymbol)
	deadline := time.After(2 * time.Second)
	for {
		bucket := store.Get(key, ts)
		if bucket.Total == wantTotal && bucket.Long == wantLong && bucket.Short == wantShort {
			if !store.IsConnected() {
				t.Fatal("feed stored data before subscription readiness")
			}
			cancel()
			select {
			case <-errCh:
			case <-time.After(time.Second):
				t.Fatal("feed did not stop after cancellation")
			}
			return
		}
		select {
		case err := <-serverErrors:
			t.Fatalf("websocket server: %v", err)
		case err := <-errCh:
			t.Fatalf("feed stopped before expected liquidation: %v", err)
		case <-deadline:
			t.Fatalf("liquidation bucket = %+v, want total=%v long=%v short=%v", bucket, wantTotal, wantLong, wantShort)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestOKXLiquidationFeedConvertsContractsToUSD(t *testing.T) {
	const ts = int64(1_800_010)
	server, serverErrors := newLiquidationWebsocketServer(t, func(_ *http.Request, subscription map[string]any) error {
		if subscription["op"] != "subscribe" || subscription["id"] != "liquidations" {
			return fmt.Errorf("unexpected OKX subscription: %#v", subscription)
		}
		return nil
	}, []any{
		map[string]any{"event": "subscribe", "arg": map[string]any{"channel": "liquidation-orders", "instType": "SWAP"}},
		map[string]any{"arg": map[string]any{"channel": "liquidation-orders", "instType": "SWAP"}, "data": []any{map[string]any{
			"instId": "BTC-USDT-SWAP", "details": []any{map[string]any{"posSide": "long", "side": "sell", "bkPx": "50000", "sz": "2", "ts": "1800010"}},
		}}},
	})
	defer server.Close()
	feed := &okxLiquidationFeed{baseLiquidationFeed: newBaseLiquidationFeed(Config{WSURL: "ws" + strings.TrimPrefix(server.URL, "http"), HTTPTimeout: time.Second})}
	catalog := newLiquidationCatalog([]liquidationInstrument{{CanonicalSymbol: "BTCUSDT", NativeSymbol: "BTC-USDT-SWAP", ContractValue: 0.01}})
	assertLiquidationFeedBucket(t, feed, catalog, "BTC-USDT-SWAP", ts, 1000, 1000, 0, serverErrors)
}

func TestBitgetLiquidationFeedUsesQuoteAmountDirectly(t *testing.T) {
	const ts = int64(1_800_020)
	server, serverErrors := newLiquidationWebsocketServer(t, func(_ *http.Request, subscription map[string]any) error {
		if subscription["op"] != "subscribe" {
			return fmt.Errorf("unexpected Bitget subscription: %#v", subscription)
		}
		return nil
	}, []any{
		map[string]any{"event": "subscribe", "arg": map[string]any{"instType": "usdt-futures", "topic": "liquidation"}},
		map[string]any{"action": "update", "arg": map[string]any{"instType": "usdt-futures", "topic": "liquidation"}, "data": []any{
			map[string]any{"symbol": "ETHUSDT", "side": "sell", "price": "3000", "amount": "750.5", "ts": "1800020"},
		}},
	})
	defer server.Close()
	feed := &bitgetLiquidationFeed{baseLiquidationFeed: newBaseLiquidationFeed(Config{WSURL: "ws" + strings.TrimPrefix(server.URL, "http"), HTTPTimeout: time.Second})}
	catalog := newLiquidationCatalog([]liquidationInstrument{{CanonicalSymbol: "ETHUSDT", NativeSymbol: "ETHUSDT", ContractValue: 1}})
	assertLiquidationFeedBucket(t, feed, catalog, "ETHUSDT", ts, 750.5, 0, 750.5, serverErrors)
}

func TestGateLiquidationFeedUsesMultiplierAndSignedSide(t *testing.T) {
	const ts = int64(1_800_030)
	server, serverErrors := newLiquidationWebsocketServer(t, func(request *http.Request, subscription map[string]any) error {
		if request.Header.Get("X-Gate-Size-Decimal") != "1" || subscription["channel"] != "futures.public_liquidates" || subscription["event"] != "subscribe" {
			return fmt.Errorf("unexpected Gate subscription: header=%q body=%#v", request.Header.Get("X-Gate-Size-Decimal"), subscription)
		}
		return nil
	}, []any{
		map[string]any{"channel": "futures.public_liquidates", "event": "subscribe", "result": map[string]any{"status": "success"}},
		map[string]any{"channel": "futures.public_liquidates", "event": "update", "time_ms": ts, "result": []any{
			map[string]any{"contract": "BTC_USDT", "price": "50000", "size": "-2", "time_ms": ts},
		}},
	})
	defer server.Close()
	feed := &gateLiquidationFeed{baseLiquidationFeed: newBaseLiquidationFeed(Config{WSURL: "ws" + strings.TrimPrefix(server.URL, "http"), HTTPTimeout: time.Second})}
	catalog := newLiquidationCatalog([]liquidationInstrument{{CanonicalSymbol: "BTCUSDT", NativeSymbol: "BTC_USDT", ContractValue: 0.0001}})
	assertLiquidationFeedBucket(t, feed, catalog, "BTC_USDT", ts, 10, 10, 0, serverErrors)
}
