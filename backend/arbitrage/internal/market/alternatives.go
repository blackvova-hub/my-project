package market

type ExternalStatus struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	Message    string `json:"message,omitempty"`
	ObservedAt int64  `json:"observedAt"`
	Count      int    `json:"count"`
}
type USDQuote struct {
	Bid        float64 `json:"bid"`
	Ask        float64 `json:"ask"`
	ObservedAt int64   `json:"observedAt"`
}
type LPPool struct {
	TickPriceUSDC float64 `json:"tickPriceUsdc"`
	ID            string  `json:"id"`
	Address       string  `json:"address"`
	Network       string  `json:"network"`
	Asset         string  `json:"asset"`
	Symbol        string  `json:"symbol"`
	Token0        string  `json:"token0"`
	Token1        string  `json:"token1"`
	Fee           int     `json:"fee"`
	TVLUSD        float64 `json:"tvlUsd"`
	HistoricalAPY float64 `json:"historicalApy"`
	PriceUSDC     float64 `json:"priceUsdc"`
	ObservedAt    int64   `json:"observedAt"`
	YieldAt       int64   `json:"yieldAt"`
	Tick          int     `json:"tick"`
	TickSpacing   int     `json:"tickSpacing"`
}
type AlternativeData struct {
	USDTUSD USDQuote         `json:"usdtUsd"`
	USDCUSD USDQuote         `json:"usdcUsd"`
	Pools   []LPPool         `json:"pools"`
	Sources []ExternalStatus `json:"sources"`
}
