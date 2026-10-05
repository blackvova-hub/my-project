package telegramapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetUpdatesUsesBoundedLongPolling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/getUpdates" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("offset") != "17" || query.Get("timeout") != "25" || query.Get("limit") != "100" {
			t.Fatalf("unexpected query: %v", query)
		}
		if query.Get("allowed_updates") != `["message","callback_query"]` {
			t.Fatalf("allowed_updates=%q", query.Get("allowed_updates"))
		}
		_ = json.NewEncoder(w).Encode(getUpdatesResponse{
			OK: true,
			Result: []Update{{
				UpdateID: 21,
				Message:  &Message{Text: "/start test-link", Chat: Chat{ID: 42}},
			}},
		})
	}))
	defer server.Close()

	api := New(nil, Config{BotToken: "test-token"})
	api.baseURL = server.URL
	updates, err := api.getUpdates(context.Background(), 17)
	if err != nil {
		t.Fatalf("getUpdates: %v", err)
	}
	if len(updates) != 1 || updates[0].UpdateID != 21 || updates[0].Message == nil {
		t.Fatalf("updates=%+v", updates)
	}
}

func TestRunPollingAdvancesOffset(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		switch call {
		case 1:
			if got := r.URL.Query().Get("offset"); got != "0" {
				t.Errorf("initial offset=%q", got)
			}
			_ = json.NewEncoder(w).Encode(getUpdatesResponse{OK: true, Result: []Update{{UpdateID: 41}}})
		case 2:
			if got := r.URL.Query().Get("offset"); got != "42" {
				t.Errorf("next offset=%q", got)
			}
			_ = json.NewEncoder(w).Encode(getUpdatesResponse{OK: true})
			cancel()
		default:
			_ = json.NewEncoder(w).Encode(getUpdatesResponse{OK: true})
		}
	}))
	defer server.Close()

	api := New(nil, Config{BotToken: "test-token"})
	api.baseURL = server.URL
	done := make(chan error, 1)
	go func() { done <- api.RunPolling(ctx) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunPolling error=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunPolling did not stop after context cancellation")
	}
	if calls.Load() < 2 {
		t.Fatalf("poll calls=%d", calls.Load())
	}
}

func TestGetUpdatesErrorDoesNotExposeBotToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	const token = "secret-local-bot-token"
	api := New(nil, Config{BotToken: token})
	api.baseURL = server.URL
	_, err := api.getUpdates(context.Background(), 0)
	if err == nil {
		t.Fatal("expected Telegram API error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error exposes bot token: %q", err)
	}
}
