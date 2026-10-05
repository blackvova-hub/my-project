package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestDispatcherDeliveryWaitDoesNotGateRuleFanout protects the rule fanout
// boundary: one terminal outbox result must not prevent later rules/users for
// the same candle from being durably enqueued.
func TestDispatcherDeliveryWaitDoesNotGateRuleFanout(t *testing.T) {
	parsed, err := parser.ParseFile(token.NewFileSet(), "production_engine.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	var handle *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "deliverUnifiedOutcomes" {
			handle = function
			break
		}
	}
	if handle == nil {
		t.Fatal("deliverUnifiedOutcomes was not found")
	}

	var waitInsideRuleFanout bool
	ast.Inspect(handle.Body, func(node ast.Node) bool {
		loop, ok := node.(*ast.RangeStmt)
		if !ok {
			return true
		}
		rangeIdentifier, ok := loop.X.(*ast.Ident)
		if !ok || rangeIdentifier.Name != "outcomes" {
			return true
		}
		ast.Inspect(loop.Body, func(bodyNode ast.Node) bool {
			call, ok := bodyNode.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && identifier.Name == "waitSignalOutboxDeliveryBatch" {
				waitInsideRuleFanout = true
			}
			return true
		})
		return true
	})
	if waitInsideRuleFanout {
		t.Fatal("delivery wait must not run inside the outcome fanout loop: one dead-letter would suppress later users")
	}
}

func TestOutboxPayloadRejectsNullAndMissingRequiredFieldsWithoutPanic(t *testing.T) {
	pool := newUnavailableOutboxRegressionPool(t)
	tests := []struct {
		name     string
		fragment string
		process  func(context.Context) error
	}{
		{
			name:     "important null",
			fragment: "invalid important-event outbox payload",
			process: func(ctx context.Context) error {
				return processClaimedImportantEventDelivery(ctx, pool, nil, Config{}, pendingImportantEventDelivery{id: 1, payload: []byte("null"), claimToken: "claim"})
			},
		},
		{
			name:     "important missing required fields",
			fragment: "invalid important-event outbox payload",
			process: func(ctx context.Context) error {
				return processClaimedImportantEventDelivery(ctx, pool, nil, Config{}, pendingImportantEventDelivery{id: 2, payload: []byte("{}"), claimToken: "claim"})
			},
		},
		{
			name:     "signal null",
			fragment: "invalid signal outbox payload",
			process: func(ctx context.Context) error {
				return processClaimedSignalDelivery(ctx, pool, nil, Config{}, pendingSignalDelivery{id: 3, payload: []byte("null"), claimToken: "claim"})
			},
		},
		{
			name:     "signal missing required fields",
			fragment: "invalid signal outbox payload",
			process: func(ctx context.Context) error {
				return processClaimedSignalDelivery(ctx, pool, nil, Config{}, pendingSignalDelivery{id: 4, payload: []byte("{}"), claimToken: "claim"})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("poison payload caused panic: %v", recovered)
				}
			}()
			err := test.process(ctx)
			if err == nil || !strings.Contains(err.Error(), test.fragment) {
				t.Fatalf("payload must be rejected as %q, got %v", test.fragment, err)
			}
		})
	}
}

type outboxRegressionRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip outboxRegressionRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestTelegramTransportErrorDoesNotExposeBotToken(t *testing.T) {
	const tokenValue = "123456789:regression-secret-token"
	originalTransport := http.DefaultTransport
	http.DefaultTransport = outboxRegressionRoundTripper(func(request *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("synthetic transport failure for %s", request.URL.String())
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	err := sendTelegramMessage(tokenValue, 42, "test")
	if err == nil {
		t.Fatal("synthetic Telegram transport failure was not returned")
	}
	if strings.Contains(err.Error(), tokenValue) {
		t.Fatalf("Telegram error exposes bot token: %q", err.Error())
	}
}

func newUnavailableOutboxRegressionPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig("postgres://outbox:outbox@127.0.0.1:1/outbox?connect_timeout=1")
	if err != nil {
		t.Fatalf("parse unavailable PostgreSQL config: %v", err)
	}
	config.MaxConns = 1
	config.MaxConnLifetime = time.Second
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("create unavailable PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
