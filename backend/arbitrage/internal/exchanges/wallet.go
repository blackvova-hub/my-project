package exchanges

import (
	"context"
	"net/url"
	"shortlong/arbitrage/internal/market"
	"strconv"
	"strings"
	"time"
)

// Reviewed aliases only. Unknown chains are not joined by fuzzy name matching.
func ChainID(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "eth", "ethereum", "erc20":
		return "eip155:1"
	case "bsc", "bep20":
		return "eip155:56"
	case "arbitrum", "arbitrumone", "arb", "arbitrum one":
		return "eip155:42161"
	case "optimism", "op":
		return "eip155:10"
	case "base":
		return "eip155:8453"
	case "polygon", "matic":
		return "eip155:137"
	case "trx", "trc20", "tron":
		return "tron:mainnet"
	case "sol", "solana":
		return "solana:mainnet"
	case "btc", "bitcoin", "bech32", "btc-segwit":
		return "bitcoin:mainnet"
	}
	return ""
}
func optionalNumber(v any) *float64 {
	n, err := strconv.ParseFloat(text(v), 64)
	if err != nil || !market.Finite(n) || n < 0 {
		return nil
	}
	return &n
}
func optionalBool(v any) *bool {
	if b, ok := v.(bool); ok {
		return &b
	}
	if s, ok := v.(string); ok && (s == "true" || s == "false") {
		b := s == "true"
		return &b
	}
	return nil
}
func enabledFlag(v any) *bool {
	if text(v) != "0" && text(v) != "1" {
		return nil
	}
	b := text(v) == "0"
	return &b
}
func (c *Client) Wallet(ctx context.Context, asset string) market.WalletEvidence {
	w := market.WalletEvidence{Chains: []market.Chain{}}
	var path string
	switch c.Exchange {
	case "gateio":
		path = "/api/v4/wallet/currency_chains?currency=" + url.QueryEscape(asset)
	case "bitget":
		path = "/api/v2/spot/public/coins?coin=" + url.QueryEscape(asset)
	case "kucoin":
		path = "/api/v3/currencies/" + url.PathEscape(asset)
	default:
		w.Message = "Сети и ввод/вывод требуют авторизованного API биржи"
		return w
	}
	w.Source = c.Exchange + path
	v, err := c.request(ctx, "GET", c.BaseSpot, path)
	if err != nil {
		w.Message = "Публичные данные сети временно недоступны"
		return w
	}
	var rows []any
	switch c.Exchange {
	case "gateio":
		rows = arr(v)
	case "kucoin":
		r := nested(rec(v), "data")
		if str(r, "currency") != asset {
			w.Message = "Ответ относится к другому активу"
			return w
		}
		rows = arr(r["chains"])
	case "bitget":
		for _, r := range arr(rec(v)["data"]) {
			x := rec(r)
			if str(x, "coin") == asset {
				rows = arr(x["chains"])
				break
			}
		}
	}
	for _, raw := range rows {
		x := rec(raw)
		ch := market.Chain{}
		switch c.Exchange {
		case "gateio":
			ch.Name = str(x, "chain")
			ch.Contract = str(x, "contract_address")
			ch.Deposit = enabledFlag(x["is_deposit_disabled"])
			ch.Withdraw = enabledFlag(x["is_withdraw_disabled"])
			if text(x["is_disabled"]) == "1" {
				f := false
				ch.Deposit = &f
				ch.Withdraw = &f
			}
		case "bitget":
			ch.Name = str(x, "chain")
			ch.Contract = str(x, "contractAddress")
			ch.Deposit = optionalBool(x["rechargeable"])
			ch.Withdraw = optionalBool(x["withdrawable"])
			ch.WithdrawFee = optionalNumber(x["withdrawFee"])
			// Do not guess the units/formula of an extra tariff.
			if n := optionalNumber(x["extraWithdrawFee"]); n != nil && *n == 0 {
				ch.WithdrawRate = n
			}
			ch.MinWithdraw = optionalNumber(x["minWithdrawAmount"])
			ch.MinDeposit = optionalNumber(x["minDepositAmount"])
		case "kucoin":
			ch.Name = str(x, "chainId")
			ch.Contract = str(x, "contractAddress")
			ch.Deposit = optionalBool(x["isDepositEnabled"])
			ch.Withdraw = optionalBool(x["isWithdrawEnabled"])
			ch.WithdrawFee = optionalNumber(x["withdrawalMinFee"])
			ch.WithdrawRate = optionalNumber(x["withdrawFeeRate"])
			ch.MinWithdraw = optionalNumber(x["withdrawalMinSize"])
			ch.MinDeposit = optionalNumber(x["depositMinSize"])
		}
		ch.ID = ChainID(ch.Name)
		if strings.HasPrefix(ch.ID, "eip155:") {
			ch.Contract = strings.ToLower(ch.Contract)
		}
		if ch.Name != "" {
			w.Chains = append(w.Chains, ch)
		}
	}
	if len(w.Chains) == 0 {
		w.Message = "Биржа не вернула проверяемые сети"
		return w
	}
	w.At = time.Now().UnixMilli()
	return w
}
