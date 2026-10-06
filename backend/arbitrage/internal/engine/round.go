package engine

import "shortlong/arbitrage/internal/market"

// RoundQuantity rounds a single post-transfer sell leg to its venue step.
func RoundQuantity(quantity, step float64) float64 { return commonQuantity(quantity, step, step) }

func LevelCount(levels []market.Level, quantity float64) int { return usedLevels(levels, quantity) }
