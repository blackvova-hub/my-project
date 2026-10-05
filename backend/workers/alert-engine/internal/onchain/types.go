package onchain

import (
	"context"
	"time"
)

// Cursor is persisted per provider, chain and watched address. BlockNumber is
// used by block-based APIs; Token supports providers with opaque pagination.
type Cursor struct {
	BlockNumber int64
	Token       string
	UpdatedAt   time.Time
}

type Batch struct {
	Transfers []Transfer
	Cursor    Cursor
}

type Transfer struct {
	Chain        string
	TxHash       string
	TokenAddress string
	Symbol       string
	FromAddress  string
	ToAddress    string
	BlockNumber  int64
	BlockTime    time.Time
	USDValue     float64
	TokenAmount  float64
	Metadata     map[string]any
}

type Provider interface {
	ID() string
	Chain() string
	Transfers(ctx context.Context, address string, cursor Cursor) (Batch, error)
	ExplorerAddressURL(address string) string
	ExplorerTxURL(hash string) string
}
