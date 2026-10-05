package trongrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"alert-engine/internal/onchain"
)

type Provider struct {
	BaseURL, APIKey string
	Client          *http.Client
}

func New(baseURL, apiKey string) *Provider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.trongrid.io"
	}
	return &Provider{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Client: &http.Client{Timeout: 20 * time.Second}}
}
func (p *Provider) ID() string    { return "trongrid" }
func (p *Provider) Chain() string { return "tron" }
func (p *Provider) ExplorerAddressURL(a string) string {
	return "https://tronscan.org/#/address/" + url.PathEscape(a)
}
func (p *Provider) ExplorerTxURL(h string) string {
	return "https://tronscan.org/#/transaction/" + url.PathEscape(h)
}
func (p *Provider) Transfers(ctx context.Context, address string, cursor onchain.Cursor) (onchain.Batch, error) {
	since := cursor.UpdatedAt
	if since.IsZero() {
		since = time.Now().UTC().Add(-2 * time.Hour)
	}
	native, nerr := p.native(ctx, address, since)
	tokens, terr := p.tokens(ctx, address, since)
	all := append(native, tokens...)
	next := cursor
	for _, tr := range all {
		if tr.BlockNumber > next.BlockNumber {
			next.BlockNumber = tr.BlockNumber
		}
	}
	next.UpdatedAt = time.Now().UTC()
	return onchain.Batch{Transfers: all, Cursor: next}, errors.Join(nerr, terr)
}
func (p *Provider) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+path, nil)
	if err != nil {
		return err
	}
	if p.APIKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", p.APIKey)
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("trongrid status %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
func (p *Provider) native(ctx context.Context, address string, since time.Time) ([]onchain.Transfer, error) {
	var body struct {
		Data []struct {
			ID    string `json:"txID"`
			Time  int64  `json:"block_timestamp"`
			Block int64  `json:"blockNumber"`
			Raw   struct {
				Contracts []struct {
					Type      string `json:"type"`
					Parameter struct {
						Value struct {
							Owner  string  `json:"owner_address"`
							To     string  `json:"to_address"`
							Amount float64 `json:"amount"`
						} `json:"value"`
					} `json:"parameter"`
				} `json:"contract"`
			} `json:"raw_data"`
		} `json:"data"`
	}
	path := "/v1/accounts/" + url.PathEscape(address) + "/transactions?only_confirmed=true&limit=200&visible=true&min_timestamp=" + strconv.FormatInt(since.UnixMilli(), 10)
	if err := p.get(ctx, path, &body); err != nil {
		return nil, err
	}
	out := []onchain.Transfer{}
	for _, row := range body.Data {
		for _, c := range row.Raw.Contracts {
			if c.Type != "TransferContract" {
				continue
			}
			v := c.Parameter.Value
			out = append(out, onchain.Transfer{Chain: "tron", TxHash: row.ID, Symbol: "TRX", FromAddress: v.Owner, ToAddress: v.To, BlockNumber: row.Block, BlockTime: time.UnixMilli(row.Time).UTC(), TokenAmount: v.Amount / 1e6, Metadata: map[string]any{"provider": p.ID(), "native": true}})
		}
	}
	return out, nil
}
func (p *Provider) tokens(ctx context.Context, address string, since time.Time) ([]onchain.Transfer, error) {
	var body struct {
		Data []struct {
			ID    string `json:"transaction_id"`
			Time  int64  `json:"block_timestamp"`
			Block int64  `json:"block_number"`
			From  string `json:"from"`
			To    string `json:"to"`
			Value string `json:"value"`
			Token struct {
				Address  string `json:"address"`
				Symbol   string `json:"symbol"`
				Decimals int    `json:"decimals"`
			} `json:"token_info"`
		} `json:"data"`
	}
	path := "/v1/accounts/" + url.PathEscape(address) + "/transactions/trc20?only_confirmed=true&limit=200&min_timestamp=" + strconv.FormatInt(since.UnixMilli(), 10)
	if err := p.get(ctx, path, &body); err != nil {
		return nil, err
	}
	out := []onchain.Transfer{}
	for _, row := range body.Data {
		amount, _ := strconv.ParseFloat(row.Value, 64)
		for i := 0; i < row.Token.Decimals; i++ {
			amount /= 10
		}
		out = append(out, onchain.Transfer{Chain: "tron", TxHash: row.ID, TokenAddress: row.Token.Address, Symbol: row.Token.Symbol, FromAddress: row.From, ToAddress: row.To, BlockNumber: row.Block, BlockTime: time.UnixMilli(row.Time).UTC(), TokenAmount: amount, Metadata: map[string]any{"provider": p.ID(), "standard": "trc20"}})
	}
	return out, nil
}
