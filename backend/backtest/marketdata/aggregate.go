package marketdata

import (
	"fmt"
	bt "shortlong/backtest"
	"shortlong/backtest/engine"
)

// Aggregate only accepts complete, aligned 5m input; gaps never become candles.
func Aggregate(base []bt.Candle, tf string, from, to int64) ([]bt.Candle, error) {
	return aggregateFrom(base, tf, from, to, 300000)
}

func aggregateFrom(base []bt.Candle, tf string, from, to, baseStep int64) ([]bt.Candle, error) {
	step := bt.Interval(tf).Milliseconds()
	if step < baseStep || step%baseStep != 0 || from < 0 || to <= from || from%step != 0 || to%step != 0 {
		return nil, fmt.Errorf("Unaligned aggregate range")
	}
	if err := engine.ValidateCandles(base, from, to, baseStep); err != nil {
		return nil, err
	}
	n := int(step / baseStep)
	out := make([]bt.Candle, 0, len(base)/n)
	for i := 0; i < len(base); i += n {
		c := base[i]
		for j := i + 1; j < i+n; j++ {
			v := base[j]
			if v.High > c.High {
				c.High = v.High
			}
			if v.Low < c.Low {
				c.Low = v.Low
			}
			c.Close = v.Close
			c.Volume += v.Volume
		}
		out = append(out, c)
	}
	return out, nil
}
