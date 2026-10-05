package blockstream

import (
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
	BaseURL string
	Client  *http.Client
}

func New(baseURL string) *Provider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://blockstream.info/api"
	}
	return &Provider{BaseURL: strings.TrimRight(baseURL, "/"), Client: &http.Client{Timeout: 15 * time.Second}}
}
func (p *Provider) ID() string    { return "blockstream" }
func (p *Provider) Chain() string { return "bitcoin" }
func (p *Provider) ExplorerAddressURL(a string) string {
	return "https://blockstream.info/address/" + url.PathEscape(a)
}
func (p *Provider) ExplorerTxURL(h string) string {
	return "https://blockstream.info/tx/" + url.PathEscape(h)
}

type tx struct {
	TxID string `json:"txid"`
	Vin  []struct {
		Prevout *struct {
			Address string `json:"scriptpubkey_address"`
			Value   int64  `json:"value"`
		} `json:"prevout"`
	} `json:"vin"`
	Vout []struct {
		Address string `json:"scriptpubkey_address"`
		Value   int64  `json:"value"`
	} `json:"vout"`
	Status struct {
		Confirmed bool  `json:"confirmed"`
		Height    int64 `json:"block_height"`
		Time      int64 `json:"block_time"`
	} `json:"status"`
}

func (p *Provider) Transfers(ctx context.Context, address string, cursor onchain.Cursor) (onchain.Batch, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/address/"+url.PathEscape(address)+"/txs", nil)
	if err != nil {
		return onchain.Batch{}, err
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return onchain.Batch{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return onchain.Batch{}, fmt.Errorf("blockstream status %s", resp.Status)
	}
	var rows []tx
	if err = json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return onchain.Batch{}, err
	}
	batch := onchain.Batch{Cursor: cursor}
	for _, row := range rows {
		if !row.Status.Confirmed || row.Status.Height <= cursor.BlockNumber {
			continue
		}
		if row.Status.Height > batch.Cursor.BlockNumber {
			batch.Cursor.BlockNumber = row.Status.Height
		}
		tr, ok := toTransfer(address, row)
		if ok {
			batch.Transfers = append(batch.Transfers, tr)
		}
	}
	batch.Cursor.UpdatedAt = time.Now().UTC()
	return batch, nil
}
func toTransfer(address string, row tx) (onchain.Transfer, bool) {
	var in, out int64
	counterparty := ""
	for _, v := range row.Vin {
		if v.Prevout == nil {
			continue
		}
		if v.Prevout.Address == address {
			in += v.Prevout.Value
		} else if counterparty == "" {
			counterparty = v.Prevout.Address
		}
	}
	for _, v := range row.Vout {
		if v.Address == address {
			out += v.Value
		} else if counterparty == "" {
			counterparty = v.Address
		}
	}
	delta := out - in
	if delta == 0 {
		return onchain.Transfer{}, false
	}
	from, to := counterparty, address
	amount := delta
	if delta < 0 {
		from, to = address, counterparty
		amount = -delta
	}
	if counterparty == "" {
		counterparty = "unknown"
		if delta > 0 {
			from = counterparty
		} else {
			to = counterparty
		}
	}
	return onchain.Transfer{Chain: "bitcoin", TxHash: row.TxID, Symbol: "BTC", FromAddress: from, ToAddress: to, BlockNumber: row.Status.Height, BlockTime: time.Unix(row.Status.Time, 0).UTC(), TokenAmount: float64(amount) / 1e8, Metadata: map[string]any{"provider": "blockstream", "native": true}}, true
}
