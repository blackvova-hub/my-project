package analytics

import "time"

type Connection struct {
	ID            string     `json:"id"`
	Exchange      string     `json:"exchange"`
	AccountType   string     `json:"accountType"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	Error         string     `json:"error"`
	Warnings      []string   `json:"warnings"`
	CoverageFrom  time.Time  `json:"coverageFrom"`
	SyncedThrough *time.Time `json:"syncedThrough"`
	LastSync      *time.Time `json:"lastSync"`
}
type Credentials struct {
	Key    string `json:"key"`
	Secret string `json:"secret"`
}
type Fill struct {
	ID             string   `json:"id"`
	ConnectionID   string   `json:"connectionId"`
	Exchange       string   `json:"exchange"`
	Symbol         string   `json:"symbol"`
	Market         string   `json:"market"`
	Side           string   `json:"side"`
	PositionSide   string   `json:"positionSide"`
	At             int64    `json:"at"`
	Price          float64  `json:"price"`
	Quantity       float64  `json:"quantity"`
	Fee            float64  `json:"fee"`
	FeeCurrency    string   `json:"feeCurrency"`
	FeeKnown       bool     `json:"feeKnown"`
	Maker          bool     `json:"maker"`
	ClosedQuantity *float64 `json:"closedQuantity,omitempty"`
	Leverage       *float64 `json:"leverage,omitempty"`
	Action         string   `json:"action,omitempty"`
}
type Ledger struct {
	ID           string  `json:"id"`
	ConnectionID string  `json:"connectionId"`
	Exchange     string  `json:"exchange"`
	At           int64   `json:"at"`
	Category     string  `json:"category"`
	Symbol       string  `json:"symbol"`
	Currency     string  `json:"currency"`
	Amount       float64 `json:"amount"`
	AmountExact  string  `json:"amountExact"`
	TradeID      string  `json:"tradeId,omitempty"`
}
type Asset struct {
	Symbol string  `json:"symbol"`
	Value  float64 `json:"value"`
}
type Position struct {
	Symbol      string   `json:"symbol"`
	Side        string   `json:"side"`
	Quantity    float64  `json:"quantity"`
	Entry       float64  `json:"entry"`
	Mark        float64  `json:"mark"`
	Notional    float64  `json:"notional"`
	Unrealized  float64  `json:"unrealized"`
	Leverage    *float64 `json:"leverage"`
	StopLoss    *float64 `json:"stopLoss"`
	TakeProfit  *float64 `json:"takeProfit"`
	Liquidation *float64 `json:"liquidation"`
}
type Snapshot struct {
	ConnectionID string     `json:"connectionId"`
	At           int64      `json:"at"`
	Equity       float64    `json:"equity"`
	Wallet       float64    `json:"wallet"`
	Unrealized   float64    `json:"unrealized"`
	Margin       float64    `json:"margin"`
	Assets       []Asset    `json:"assets"`
	Positions    []Position `json:"positions"`
	BTCPrice     *float64   `json:"btcPrice"`
}
type Trade struct {
	ID           string   `json:"id"`
	ConnectionID string   `json:"connectionId"`
	Exchange     string   `json:"exchange"`
	Symbol       string   `json:"symbol"`
	Market       string   `json:"market"`
	Side         string   `json:"side"`
	OpenedAt     int64    `json:"openedAt"`
	ClosedAt     *int64   `json:"closedAt"`
	Entry        float64  `json:"entry"`
	Exit         float64  `json:"exit"`
	Quantity     float64  `json:"quantity"`
	Remaining    float64  `json:"remaining"`
	Size         float64  `json:"size"`
	Gross        float64  `json:"gross"`
	Fees         float64  `json:"fees"`
	Funding      float64  `json:"funding"`
	Net          float64  `json:"net"`
	Duration     float64  `json:"duration"`
	Leverage     *float64 `json:"leverage"`
	MFE          *float64 `json:"mfe"`
	MAE          *float64 `json:"mae"`
	Captured     *float64 `json:"captured"`
	StopLoss     *float64 `json:"stopLoss"`
	TakeProfit   *float64 `json:"takeProfit"`
	Liquidation  *float64 `json:"liquidation"`
	Complete     bool     `json:"complete"`
	Tag          string   `json:"tag"`
	Strategy     string   `json:"strategy"`
	Fills        []Fill   `json:"fills"`
}
type Candle struct {
	Time  int64   `json:"time"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
}
type Dataset struct {
	Connections []Connection `json:"connections"`
	Trades      []Trade      `json:"trades"`
	Ledger      []Ledger     `json:"ledger"`
	Snapshots   []Snapshot   `json:"snapshots"`
	Warnings    []string     `json:"warnings"`
	ServerTime  int64        `json:"serverTime"`
}
