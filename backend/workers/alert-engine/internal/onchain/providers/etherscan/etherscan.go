package etherscan

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
	APIKey  string
	ChainID string
	BaseURL string
	Client  *http.Client
}

func New(apiKey, chainID string) *Provider {
	return &Provider{APIKey: apiKey, ChainID: chainID, BaseURL: "https://api.etherscan.io/v2/api", Client: &http.Client{Timeout: 15 * time.Second}}
}
func (p *Provider) ID() string    { return "etherscan" }
func (p *Provider) Chain() string { return "ethereum" }
func (p *Provider) ExplorerAddressURL(address string) string {
	return "https://etherscan.io/address/" + url.PathEscape(address)
}
func (p *Provider) ExplorerTxURL(hash string) string {
	return "https://etherscan.io/tx/" + url.PathEscape(hash)
}

// Transfers fetches ERC-20 and native ETH independently. A failure of one
// endpoint is returned together with the successful data from the other.
func (p *Provider) Transfers(ctx context.Context, address string, cursor onchain.Cursor) (onchain.Batch, error) {
	if strings.TrimSpace(address) == "" {
		return onchain.Batch{}, fmt.Errorf("empty address")
	}
	latest, latestErr := p.latestBlock(ctx)
	if latestErr != nil {
		return onchain.Batch{Cursor: cursor}, fmt.Errorf("latest block: %w", latestErr)
	}
	start := cursor.BlockNumber + 1
	if cursor.BlockNumber <= 0 {
		var err error
		start, err = p.blockAtTime(ctx, time.Now().UTC().Add(-2*time.Hour))
		if err != nil {
			return onchain.Batch{Cursor: cursor}, fmt.Errorf("initial block: %w", err)
		}
	}
	if start <= 0 {
		return onchain.Batch{Cursor: cursor}, fmt.Errorf("initial block lookup returned %d", start)
	}
	tokens, tokenErr := p.fetch(ctx, "tokentx", address, start)
	native, nativeErr := p.fetch(ctx, "txlist", address, start)
	all := append(tokens, native...)
	for _, tr := range all {
		if tr.BlockNumber > latest {
			latest = tr.BlockNumber
		}
	}
	next := cursor
	if tokenErr == nil && nativeErr == nil {
		next = onchain.Cursor{BlockNumber: maxInt64(latest, cursor.BlockNumber), UpdatedAt: time.Now().UTC()}
	}
	return onchain.Batch{Transfers: all, Cursor: next}, errors.Join(tokenErr, nativeErr)
}

func (p *Provider) fetch(ctx context.Context, action, address string, startBlock int64) ([]onchain.Transfer, error) {
	q := p.baseQuery()
	q.Set("module", "account")
	q.Set("action", action)
	q.Set("address", address)
	q.Set("startblock", strconv.FormatInt(startBlock, 10))
	q.Set("endblock", "latest")
	q.Set("sort", "asc")
	var result json.RawMessage
	if err := p.get(ctx, q, &result); err != nil {
		return nil, fmt.Errorf("%s: %w", action, err)
	}
	var rows []struct {
		Hash     string `json:"hash"`
		Contract string `json:"contractAddress"`
		Symbol   string `json:"tokenSymbol"`
		From     string `json:"from"`
		To       string `json:"to"`
		Value    string `json:"value"`
		Decimals string `json:"tokenDecimal"`
		Time     string `json:"timeStamp"`
		Block    string `json:"blockNumber"`
		Error    string `json:"isError"`
	}
	if err := json.Unmarshal(result, &rows); err != nil {
		return nil, err
	}
	out := make([]onchain.Transfer, 0, len(rows))
	for _, row := range rows {
		if row.Error == "1" {
			continue
		}
		sec, err := strconv.ParseInt(row.Time, 10, 64)
		if err != nil || sec <= 0 {
			continue
		}
		amount, err := strconv.ParseFloat(row.Value, 64)
		if err != nil {
			continue
		}
		symbol, contract, decimals := row.Symbol, row.Contract, 0
		if action == "txlist" {
			symbol, contract, decimals = "ETH", "", 18
		} else {
			decimals, _ = strconv.Atoi(row.Decimals)
		}
		if decimals > 0 {
			amount /= pow10(decimals)
		}
		// txlist also contains contract calls with zero native value. They are
		// transactions, but not ETH transfers and must not enter the event engine.
		if amount <= 0 {
			continue
		}
		block, _ := strconv.ParseInt(row.Block, 10, 64)
		out = append(out, onchain.Transfer{Chain: p.Chain(), TxHash: row.Hash, TokenAddress: strings.ToLower(contract), Symbol: strings.ToUpper(symbol), FromAddress: strings.ToLower(row.From), ToAddress: strings.ToLower(row.To), BlockNumber: block, BlockTime: time.Unix(sec, 0).UTC(), TokenAmount: amount, Metadata: map[string]any{"provider": p.ID(), "transferAction": action}})
	}
	return out, nil
}

func (p *Provider) latestBlock(ctx context.Context) (int64, error) {
	q := p.baseQuery()
	q.Set("module", "proxy")
	q.Set("action", "eth_blockNumber")
	var result json.RawMessage
	if err := p.get(ctx, q, &result); err != nil {
		return 0, err
	}
	var hexValue string
	if err := json.Unmarshal(result, &hexValue); err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimPrefix(hexValue, "0x"), 16, 64)
}

func (p *Provider) blockAtTime(ctx context.Context, at time.Time) (int64, error) {
	q := p.baseQuery()
	q.Set("module", "block")
	q.Set("action", "getblocknobytime")
	q.Set("timestamp", strconv.FormatInt(at.Unix(), 10))
	q.Set("closest", "before")
	var result json.RawMessage
	if err := p.get(ctx, q, &result); err != nil {
		return 0, err
	}
	var value string
	if err := json.Unmarshal(result, &value); err != nil {
		return 0, err
	}
	block, err := strconv.ParseInt(value, 10, 64)
	return block, err
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (p *Provider) baseQuery() url.Values {
	q := url.Values{}
	q.Set("chainid", p.ChainID)
	if p.APIKey != "" {
		q.Set("apikey", p.APIKey)
	}
	return q
}

func (p *Provider) get(ctx context.Context, q url.Values, result *json.RawMessage) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("status %s", resp.Status)
	}
	var body struct {
		Status  string          `json:"status"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	if body.Status == "0" && body.Message != "No transactions found" && string(body.Result) != `"No transactions found"` {
		return fmt.Errorf("%s", strings.TrimSpace(string(body.Result)))
	}
	if body.Status == "0" {
		*result = json.RawMessage("[]")
		return nil
	}
	*result = body.Result
	return nil
}

func pow10(n int) float64 {
	v := 1.0
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}
