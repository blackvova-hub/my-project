package marketapi

import (
	"net/http"
	"sort"
	"time"
)

type liquidationObservation struct {
	Time  int64   `json:"time"`
	Value float64 `json:"value"`
}

// Only observed positive events are returned. Missing minutes are unknown,
// not zeros; repeated compact batches replace their existing minute value.
func (c *liquidationCache) history(symbol, exchange string, now time.Time) []liquidationObservation {
	result := make([]liquidationObservation, 0)
	if c == nil {
		return result
	}
	cutoff, latest := now.Add(-liquidationWindow).UnixMilli(), now.UnixMilli()
	byTime := make(map[int64]float64)
	c.mu.RLock()
	for venue, byMinute := range c.buckets[symbol] {
		if exchange != "all" && venue != exchange {
			continue
		}
		for minute, value := range byMinute {
			if minute >= cutoff && minute <= latest {
				byTime[minute/1000] += value
			}
		}
	}
	c.mu.RUnlock()
	for ts, value := range byTime {
		result = append(result, liquidationObservation{Time: ts, Value: value})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Time < result[j].Time })
	return result
}

func (a *API) handleLiquidationHistory(w http.ResponseWriter, r *http.Request) {
	symbol, ok := normalizeDerivativeSymbol(r.URL.Query().Get("symbol"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_symbol"})
		return
	}
	exchange := r.URL.Query().Get("exchange")
	switch exchange {
	case "all", "bybit", "binance", "okx", "bitget", "gateio":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_exchange"})
		return
	}
	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, struct {
		Symbol      string                   `json:"symbol"`
		Exchange    string                   `json:"exchange"`
		WindowHours int                      `json:"windowHours"`
		Partial     bool                     `json:"partial"`
		AsOf        string                   `json:"asOf"`
		Points      []liquidationObservation `json:"points"`
	}{symbol, exchange, 24, true, now.Format(time.RFC3339), a.liqCache.history(symbol, exchange, now)})
}
