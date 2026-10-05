package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestBybitSubscriptionCoverageWaitsForEveryBatch(t *testing.T) {
	for _, test := range []struct {
		name          string
		secondSuccess bool
	}{
		{name: "all batches acknowledged", secondSuccess: true},
		{name: "later batch rejected", secondSuccess: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			firstACK := make(chan struct{})
			releaseSecond := make(chan struct{})
			serverErrors := make(chan error, 1)
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				conn, err := upgrader.Upgrade(w, request, nil)
				if err != nil {
					serverErrors <- err
					return
				}
				defer conn.Close()
				type subscription struct {
					Op    string   `json:"op"`
					ReqID string   `json:"req_id"`
					Args  []string `json:"args"`
				}
				requests := make([]subscription, 2)
				for index := range requests {
					if err := conn.ReadJSON(&requests[index]); err != nil {
						serverErrors <- err
						return
					}
					if requests[index].Op != "subscribe" || requests[index].ReqID == "" || len(requests[index].Args) == 0 {
						serverErrors <- fmt.Errorf("invalid subscription request: %+v", requests[index])
						return
					}
				}
				if err := conn.WriteJSON(map[string]any{"op": "subscribe", "req_id": requests[0].ReqID, "success": true}); err != nil {
					serverErrors <- err
					return
				}
				close(firstACK)
				<-releaseSecond
				if err := conn.WriteJSON(map[string]any{"op": "subscribe", "req_id": requests[1].ReqID, "success": test.secondSuccess, "ret_msg": "rejected"}); err != nil {
					serverErrors <- err
					return
				}
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			}))
			defer server.Close()

			symbols := make([]string, 21)
			for index := range symbols {
				symbols[index] = fmt.Sprintf("SYM%02dUSDT", index)
			}
			adapter := NewBybitAdapter(Config{
				Exchange: string(ExchangeBybit), MarketType: string(MarketTypePerpetual),
				WSURL: "ws" + strings.TrimPrefix(server.URL, "http"), WSReadTimeoutSeconds: 2,
			})
			trades, liquidations := NewTradeStore(), NewLiquidationStore()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			errCh := make(chan error, 1)
			go func() { errCh <- adapter.RunStreams(ctx, symbols, trades, liquidations) }()

			select {
			case <-firstACK:
			case err := <-serverErrors:
				t.Fatal(err)
			case <-time.After(2 * time.Second):
				t.Fatal("first subscription ACK was not observed")
			}
			minute := time.Now().UTC().Add(time.Minute).Truncate(time.Minute).UnixMilli()
			if trades.CoversMinute(minute) || liquidations.CoversMinute(minute) {
				t.Fatal("coverage became ready before all subscription batches were acknowledged")
			}
			close(releaseSecond)

			if !test.secondSuccess {
				select {
				case err := <-errCh:
					if !errors.Is(err, errBybitSubscription) {
						t.Fatalf("RunStreams error=%v want subscription error", err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("failed subscription did not stop the connection")
				}
				if trades.CoversMinute(minute) || liquidations.CoversMinute(minute) {
					t.Fatal("failed subscription enabled coverage")
				}
				return
			}

			deadline := time.Now().Add(2 * time.Second)
			for !trades.CoversMinute(minute) || !liquidations.CoversMinute(minute) {
				if time.Now().After(deadline) {
					t.Fatal("coverage did not become ready after all ACKs")
				}
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
			select {
			case <-errCh:
			case <-time.After(time.Second):
				t.Fatal("RunStreams did not stop after cancellation")
			}
		})
	}
}

func TestBybitSubscriptionACKRejectsUnknownAndDuplicateIDs(t *testing.T) {
	tracker, err := newBybitSubscriptionAcks([]string{"one", "two"})
	if err != nil {
		t.Fatal(err)
	}
	truth := true
	if _, err := tracker.observe(wsMessage{ReqID: "unknown", Success: &truth}); !errors.Is(err, errBybitSubscription) {
		t.Fatalf("unknown ACK error=%v", err)
	}
	if ready, err := tracker.observe(wsMessage{ReqID: "one", Success: &truth}); err != nil || ready {
		t.Fatalf("first ACK ready=%v err=%v", ready, err)
	}
	if _, err := tracker.observe(wsMessage{ReqID: "one", Success: &truth}); !errors.Is(err, errBybitSubscription) {
		t.Fatalf("duplicate ACK error=%v", err)
	}
}
