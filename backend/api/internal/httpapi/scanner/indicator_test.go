package scannerapi

import "testing"

func TestScannerPublishedIndicatorsAreAcceptedForEveryExchange(t *testing.T) {
	indicators := []string{
		"openInterest", "fundingRate", "markPrice", "indexPrice",
		"orderbookBid", "orderbookAsk", "orderbookSpread",
		"tradeLastPrice", "tradeLastSize", "tradeLastSide",
		"tradeBuyVolume", "tradeSellVolume", "tradeBuyUsd", "tradeSellUsd", "tradeCount",
		"longRatio", "shortRatio", "longShortRatio", "liquidations", "liquidationsCombined", "liquidationsLong", "liquidationsShort",
	}
	for _, indicator := range indicators {
		if !isValidIndicator(indicator) {
			t.Errorf("published indicator %q is rejected by API", indicator)
		}
	}
}

func TestSupportedExchangeMarketCombinations(t *testing.T) {
	tests := []struct {
		exchange   string
		marketType string
		want       bool
	}{
		{exchange: "bybit", marketType: "perpetual", want: true},
		{exchange: "bybit", marketType: "spot", want: false},
		{exchange: "binance", marketType: "perpetual", want: true},
		{exchange: "binance", marketType: "spot", want: false},
		{exchange: "unknown", marketType: "perpetual", want: false},
	}
	for _, tt := range tests {
		if got := isSupportedInstrument(tt.exchange, tt.marketType); got != tt.want {
			t.Errorf("isSupportedInstrument(%q, %q) = %v, want %v", tt.exchange, tt.marketType, got, tt.want)
		}
	}
}

func TestVenueNormalizationKeepsUnknownValuesInvalid(t *testing.T) {
	if got := normalizeExchange("Kraken"); got != "kraken" {
		t.Fatalf("normalizeExchange() = %q", got)
	}
	if isValidExchange(normalizeExchange("Kraken")) {
		t.Fatal("unknown exchange must not silently become Bybit")
	}
	if got := normalizeMarketType("linear"); got != "perpetual" {
		t.Fatalf("normalizeMarketType(linear) = %q", got)
	}
}
