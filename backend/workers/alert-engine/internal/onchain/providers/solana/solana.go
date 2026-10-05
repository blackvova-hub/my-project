package solana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"alert-engine/internal/onchain"
)

type Provider struct {
	RPCURL string
	Client *http.Client
}

func New(rpcURL string) *Provider {
	if strings.TrimSpace(rpcURL) == "" {
		rpcURL = "https://api.mainnet-beta.solana.com"
	}
	return &Provider{RPCURL: rpcURL, Client: &http.Client{Timeout: 20 * time.Second}}
}
func (p *Provider) ID() string    { return "solana_rpc" }
func (p *Provider) Chain() string { return "solana" }
func (p *Provider) ExplorerAddressURL(a string) string {
	return "https://explorer.solana.com/address/" + url.PathEscape(a)
}
func (p *Provider) ExplorerTxURL(h string) string {
	return "https://explorer.solana.com/tx/" + url.PathEscape(h)
}

type signature struct {
	Signature string `json:"signature"`
	Slot      int64  `json:"slot"`
	BlockTime *int64 `json:"blockTime"`
	Err       any    `json:"err"`
}

func (p *Provider) Transfers(ctx context.Context, address string, cursor onchain.Cursor) (onchain.Batch, error) {
	var sigs []signature
	if err := p.rpc(ctx, "getSignaturesForAddress", []any{address, map[string]any{"limit": 100, "commitment": "confirmed"}}, &sigs); err != nil {
		return onchain.Batch{}, err
	}
	batch := onchain.Batch{Cursor: cursor}
	for _, sig := range sigs {
		if sig.Err != nil || sig.Slot <= cursor.BlockNumber {
			continue
		}
		if sig.Slot > batch.Cursor.BlockNumber {
			batch.Cursor.BlockNumber = sig.Slot
		}
		tr, ok, err := p.transaction(ctx, address, sig)
		if err != nil {
			return batch, err
		}
		if ok {
			batch.Transfers = append(batch.Transfers, tr)
		}
	}
	batch.Cursor.UpdatedAt = time.Now().UTC()
	return batch, nil
}
func (p *Provider) transaction(ctx context.Context, address string, sig signature) (onchain.Transfer, bool, error) {
	var raw struct {
		BlockTime   *int64 `json:"blockTime"`
		Transaction struct {
			Message struct {
				AccountKeys []json.RawMessage `json:"accountKeys"`
			} `json:"message"`
		} `json:"transaction"`
		Meta struct {
			PreBalances  []uint64 `json:"preBalances"`
			PostBalances []uint64 `json:"postBalances"`
		} `json:"meta"`
	}
	if err := p.rpc(ctx, "getTransaction", []any{sig.Signature, map[string]any{"encoding": "jsonParsed", "maxSupportedTransactionVersion": 0, "commitment": "confirmed"}}, &raw); err != nil {
		return onchain.Transfer{}, false, err
	}
	keys := make([]string, 0, len(raw.Transaction.Message.AccountKeys))
	for _, v := range raw.Transaction.Message.AccountKeys {
		var s string
		if json.Unmarshal(v, &s) != nil {
			var o struct {
				Pubkey string `json:"pubkey"`
			}
			_ = json.Unmarshal(v, &o)
			s = o.Pubkey
		}
		keys = append(keys, s)
	}
	idx := -1
	for i, k := range keys {
		if k == address {
			idx = i
			break
		}
	}
	if idx < 0 || idx >= len(raw.Meta.PreBalances) || idx >= len(raw.Meta.PostBalances) {
		return onchain.Transfer{}, false, nil
	}
	delta := int64(raw.Meta.PostBalances[idx]) - int64(raw.Meta.PreBalances[idx])
	if delta == 0 {
		return onchain.Transfer{}, false, nil
	}
	other := "unknown"
	best := int64(0)
	for i, k := range keys {
		if i == idx || i >= len(raw.Meta.PreBalances) || i >= len(raw.Meta.PostBalances) {
			continue
		}
		d := int64(raw.Meta.PostBalances[i]) - int64(raw.Meta.PreBalances[i])
		if abs(d) > abs(best) && d*delta < 0 {
			best = d
			other = k
		}
	}
	from, to := other, address
	amount := delta
	if delta < 0 {
		from, to = address, other
		amount = -delta
	}
	at := time.Now().UTC()
	if raw.BlockTime != nil {
		at = time.Unix(*raw.BlockTime, 0).UTC()
	} else if sig.BlockTime != nil {
		at = time.Unix(*sig.BlockTime, 0).UTC()
	}
	return onchain.Transfer{Chain: "solana", TxHash: sig.Signature, Symbol: "SOL", FromAddress: from, ToAddress: to, BlockNumber: sig.Slot, BlockTime: at, TokenAmount: float64(amount) / 1e9, Metadata: map[string]any{"provider": p.ID(), "native": true}}, true, nil
}
func (p *Provider) rpc(ctx context.Context, method string, params []any, out any) error {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.RPCURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("solana status %s", resp.Status)
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if envelope.Error != nil {
		return fmt.Errorf("solana rpc %d: %s", envelope.Error.Code, envelope.Error.Message)
	}
	return json.Unmarshal(envelope.Result, out)
}
func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
