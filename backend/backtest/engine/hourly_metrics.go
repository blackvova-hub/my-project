package engine

import bt "shortlong/backtest"

const minuteMillis int64 = 60000
const hourMillis int64 = 3600000

func (s *seriesCache) isMinuteSeries() bool {
	return len(s.candles) > 1 && s.candles[1].Time-s.candles[0].Time == minuteMillis
}

func hourlyMetric(indicator string) bool {
	switch indicator {
	case "openInterest", "cvd", "delta", "tradeBuyVolume", "tradeSellVolume":
		return true
	}
	return false
}

// External data stays at its native hourly frequency. Missing values in the
// first partial listing hour remain missing, and are never forward-filled.
func (s *seriesCache) hourlyMetricSeries(c bt.Condition) []float64 {
	if s.hourly == nil {
		candles := make([]bt.Candle, 0, len(s.candles)/60+1)
		for i, candle := range s.candles {
			closing := candle.Time + minuteMillis
			if closing%hourMillis != 0 {
				continue
			}
			candle.Time = closing - hourMillis
			candles = append(candles, candle)
			s.hourlyIndices = append(s.hourlyIndices, i)
		}
		s.hourly = newCache(candles)
	}
	out := emptySeries(len(s.candles))
	values := s.hourly.ruleSeries(c)
	for i, index := range s.hourlyIndices {
		out[index] = values[i]
	}
	return out
}
