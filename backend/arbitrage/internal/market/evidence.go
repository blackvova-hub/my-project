package market

// A missing value is unknown, never an enabled feature or a zero fee.
type Chain struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Contract     string   `json:"contract,omitempty"`
	Deposit      *bool    `json:"deposit"`
	Withdraw     *bool    `json:"withdraw"`
	WithdrawFee  *float64 `json:"withdrawFee"`
	WithdrawRate *float64 `json:"withdrawRate"`
	MinWithdraw  *float64 `json:"minWithdraw"`
	MinDeposit   *float64 `json:"minDeposit"`
}
type WalletEvidence struct {
	Chains  []Chain `json:"chains"`
	At      int64   `json:"at"`
	Source  string  `json:"source"`
	Message string  `json:"message,omitempty"`
}
type VenueEvidence struct {
	Instrument *Instrument    `json:"instrument,omitempty"`
	Trading    *bool          `json:"trading"`
	CatalogAt  int64          `json:"catalogAt"`
	Wallet     WalletEvidence `json:"wallet"`
}
