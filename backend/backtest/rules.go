package backtest

import (
	"errors"
	"fmt"
	"time"
)

// Measures have explicit units. Signed flows use a sum or an imbalance ratio,
// never a percentage of an arbitrary cumulative baseline (which may be zero).
var IndicatorMeasures = map[string][]string{
	"price":           {"change_pct"},
	"volume":          {"change_pct", "relative", "sum"},
	"openInterest":    {"change_pct"},
	"cvd":             {"sum", "imbalance_pct"},
	"delta":           {"sum", "imbalance_pct"},
	"tradeBuyVolume":  {"change_pct", "sum"},
	"tradeSellVolume": {"change_pct", "sum"},
	"rsi":             {"value", "change"},
	"macd":            {"histogram", "line", "signal_cross"},
	"ema":             {"distance_pct"},
	"sma":             {"distance_pct"},
	"atr":             {"value"},
	"stochastic":      {"value"},
	"mfi":             {"value"},
	"obv":             {"imbalance_pct"},
	"bollinger":       {"position", "width"},
	"volatility":      {"value"},
}

func HasWindow(c Condition) bool {
	switch c.Indicator {
	case "price", "volume", "openInterest", "cvd", "delta", "tradeBuyVolume", "tradeSellVolume", "obv", "volatility":
		return true
	case "rsi":
		return c.Measure == "change"
	}
	return false
}
func HasPeriod(c Condition) bool {
	switch c.Indicator {
	case "rsi", "ema", "sma", "atr", "stochastic", "mfi", "bollinger":
		return true
	}
	return false
}
func validateIndicator(c Condition) error {
	if c.Left != nil || c.Right != nil || len(c.Children) != 0 || c.Event != "" || c.WithinBars != 0 {
		return errors.New("Лишние параметры условия индикатора")
	}
	measures, ok := IndicatorMeasures[c.Indicator]
	if !ok {
		return fmt.Errorf("Неизвестный индикатор: %s", c.Indicator)
	}
	found := false
	for _, m := range measures {
		found = found || m == c.Measure
	}
	if !found {
		return errors.New("Недопустимый способ расчёта индикатора")
	}
	switch c.Operator {
	case "lt", "lte", "gt", "gte", "crosses_above", "crosses_below":
	default:
		return errors.New("Неизвестный оператор сравнения")
	}
	if !between(c.Threshold, -1e12, 1e12) {
		return errors.New("Некорректный порог индикатора")
	}
	if c.Measure == "change_pct" {
		if (c.Direction != "up" && c.Direction != "down" && c.Direction != "both") || c.Threshold < 0 {
			return errors.New("Выберите направление изменения и положительный порог в процентах")
		}
	} else if c.Direction != "" {
		return errors.New("Направление применяется только к изменению в процентах")
	}
	if HasWindow(c) {
		if c.WindowHours < 1 || c.WindowHours > 720 {
			return errors.New("Окно условия — от 1 до 720 часов")
		}
	} else if c.WindowHours != 0 {
		return errors.New("У этого индикатора используется период в свечах, а не окно изменения")
	}
	if HasPeriod(c) {
		if c.Period < 2 || c.Period > 400 {
			return errors.New("Период индикатора должен быть от 2 до 400 свечей")
		}
	} else if c.Period != 0 {
		return errors.New("Лишний период индикатора")
	}
	if (c.Indicator == "rsi" || c.Indicator == "mfi" || c.Indicator == "stochastic") && c.Measure == "value" && !between(c.Threshold, 0, 100) {
		return errors.New("Порог осциллятора — от 0 до 100")
	}
	if c.Indicator == "rsi" && c.Measure == "change" && !between(c.Threshold, -100, 100) {
		return errors.New("Изменение RSI — от −100 до 100 пунктов")
	}
	if c.Measure == "imbalance_pct" && !between(c.Threshold, -100, 100) {
		return errors.New("Баланс объёма — от −100 до 100%")
	}
	if (c.Indicator == "atr" || c.Indicator == "volatility" || c.Measure == "relative" || c.Measure == "width" || (c.Measure == "sum" && c.Indicator != "cvd" && c.Indicator != "delta")) && c.Threshold < 0 {
		return errors.New("Для этой величины порог не может быть отрицательным")
	}
	if c.Measure == "signal_cross" && (c.Threshold != 0 || (c.Operator != "crosses_above" && c.Operator != "crosses_below")) {
		return errors.New("MACD: выберите пересечение сигнальной линии")
	}
	return nil
}
func (r Request) WalkConditions(fn func(Condition)) {
	var walk func(Condition)
	walk = func(c Condition) {
		fn(c)
		for _, child := range c.Children {
			walk(child)
		}
	}
	walk(r.Strategy.Entry)
	if r.Strategy.Exit != nil {
		walk(*r.Strategy.Exit)
	}
}
func (r Request) validateIndicators() error {
	var invalid error
	r.WalkConditions(func(c Condition) {
		if r.Strategy.Version == 2 && (c.Kind == "compare" || c.Kind == "smc") {
			invalid = errors.New("Для стратегии версии 2 выберите индикатор и его параметры")
			return
		}
		if c.Kind != "indicator" || invalid != nil {
			return
		}
		if r.Timeframe != "1h" && r.Timeframe != "4h" {
			invalid = errors.New("Для новых условий доступны таймфреймы 1h и 4h")
			return
		}
		if HasWindow(c) && time.Duration(c.WindowHours)*time.Hour%Interval(r.Timeframe) != 0 {
			invalid = errors.New("Окно условия должно быть кратно таймфрейму: 1 или 4 часа")
		}
		if c.Indicator == "openInterest" && r.Market != "linear" {
			invalid = errors.New("Открытый интерес доступен только на Futures")
		}
	})
	return invalid
}
func (c Condition) RequiredBars(step time.Duration) int {
	n := int(time.Duration(c.WindowHours) * time.Hour / step)
	switch c.Indicator {
	case "volume", "tradeBuyVolume", "tradeSellVolume":
		if c.Measure == "change_pct" || c.Measure == "relative" {
			n *= 2
		}
	case "macd":
		n = 175
	}
	if HasPeriod(c) {
		n += c.Period * 5
	}
	return n + 1
}

// External series are fetched only as far back as their own warmup requires.
func (r Request) MetricWarmup() map[string]int {
	out := map[string]int{}
	r.WalkConditions(func(c Condition) {
		if c.Kind != "indicator" {
			return
		}
		key := ""
		switch c.Indicator {
		case "openInterest":
			key = "oi"
		case "cvd", "delta", "tradeBuyVolume", "tradeSellVolume":
			key = "trades"
		}
		if key != "" {
			n := c.RequiredBars(Interval(r.Timeframe)) + 1
			if n > out[key] {
				out[key] = n
			}
		}
	})
	return out
}
