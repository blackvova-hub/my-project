// Package backtest defines the versioned strategy contract shared by API and worker.
package backtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"
)

const EngineVersion = "2.0.0"

type Operand struct {
	Kind   string  `json:"kind"`
	Period int     `json:"period,omitempty"`
	Value  float64 `json:"value,omitempty"`
}

// A zero threshold is meaningful. Preserve it in persisted strategies and API
// responses without adding an irrelevant value field to indicator operands.
func (o Operand) MarshalJSON() ([]byte, error) {
	if o.Kind == "constant" {
		return json.Marshal(struct {
			Kind  string  `json:"kind"`
			Value float64 `json:"value"`
		}{o.Kind, o.Value})
	}
	type wire Operand
	return json.Marshal(wire(o))
}

// A node is either an AND/OR group or an operand comparison. New indicators can
// be added without changing the tree or the execution protocol.
type Condition struct {
	Kind        string      `json:"kind"`
	Operator    string      `json:"operator,omitempty"`
	Left        *Operand    `json:"left,omitempty"`
	Right       *Operand    `json:"right,omitempty"`
	Children    []Condition `json:"children,omitempty"`
	Event       string      `json:"event,omitempty"`
	Direction   string      `json:"direction,omitempty"`
	WithinBars  int         `json:"withinBars,omitempty"`
	Indicator   string      `json:"indicator,omitempty"`
	Measure     string      `json:"measure,omitempty"`
	WindowHours int         `json:"windowHours,omitempty"`
	Period      int         `json:"period,omitempty"`
	Threshold   float64     `json:"threshold,omitempty"`
}

func (c Condition) MarshalJSON() ([]byte, error) {
	type wire Condition
	if c.Kind == "indicator" {
		return json.Marshal(struct {
			wire
			Threshold float64 `json:"threshold"`
		}{wire(c), c.Threshold})
	}
	return json.Marshal(wire(c))
}

func (c *Condition) UnmarshalJSON(data []byte) error {
	type wire Condition
	var value wire
	input := struct {
		*wire
		Threshold *float64 `json:"threshold"`
	}{wire: &value}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return err
	}
	if value.Kind == "indicator" && input.Threshold == nil {
		return errors.New("Укажите числовой порог индикатора")
	}
	if input.Threshold != nil {
		value.Threshold = *input.Threshold
	}
	*c = Condition(value)
	return nil
}

type Strategy struct {
	Version         int        `json:"version"`
	Direction       string     `json:"direction"`
	Entry           Condition  `json:"entry"`
	Exit            *Condition `json:"exit,omitempty"`
	TakeProfitPct   float64    `json:"takeProfitPct"`
	StopLossPct     float64    `json:"stopLossPct"`
	PositionSizePct float64    `json:"positionSizePct"`
	FeePct          float64    `json:"feePct"`
	SlippagePct     float64    `json:"slippagePct"`
	InitialCapital  float64    `json:"initialCapital"`
}

type Request struct {
	Exchange  string    `json:"exchange"`
	Market    string    `json:"market"`
	Symbols   []string  `json:"symbols"`
	Timeframe string    `json:"timeframe"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"` // exclusive; all included candles must be closed
	Strategy  Strategy  `json:"strategy"`
}

var symbolPattern = regexp.MustCompile(`^[A-Z0-9]{2,24}USDT$`)

func ValidSymbol(s string) bool { return symbolPattern.MatchString(s) }
func Interval(tf string) time.Duration {
	switch tf {
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "1h":
		return time.Hour
	case "4h":
		return 4 * time.Hour
	}
	return 0
}
func finite(n float64) bool          { return !math.IsNaN(n) && !math.IsInf(n, 0) }
func between(n, lo, hi float64) bool { return finite(n) && n >= lo && n <= hi }

func (r Request) Validate(now time.Time) error {
	if r.Exchange != "bybit" || (r.Market != "spot" && r.Market != "linear") {
		return errors.New("Поддерживаются только Bybit Spot и USDT Futures")
	}
	if len(r.Symbols) < 1 || len(r.Symbols) > 10 {
		return errors.New("Выберите от 1 до 10 монет")
	}
	seen := map[string]bool{}
	for _, s := range r.Symbols {
		if !ValidSymbol(s) || seen[s] {
			return errors.New("Некорректные или повторяющиеся торговые пары")
		}
		seen[s] = true
	}
	step := Interval(r.Timeframe)
	if step == 0 {
		return errors.New("Допустимые таймфреймы: 5m, 15m, 1h, 4h")
	}
	earliest := now.UTC().Truncate(24*time.Hour).AddDate(-1, 0, 0)
	if r.From.IsZero() || r.To.IsZero() || !r.To.After(r.From) || r.To.After(now.UTC().Truncate(step)) || r.From.Before(earliest) || r.To.After(r.From.AddDate(1, 0, 0)) {
		return errors.New("Выберите завершённый период в пределах последнего года (UTC)")
	}
	if !r.From.Equal(r.From.Truncate(step)) || !r.To.Equal(r.To.Truncate(step)) {
		return errors.New("Границы периода должны совпадать с границами свечей")
	}
	if r.To.Sub(r.From) < 2*step {
		return errors.New("Для теста нужны как минимум две свечи")
	}
	s := r.Strategy
	if s.Version != 1 && s.Version != 2 {
		return errors.New("Неподдерживаемая версия стратегии")
	}
	if s.Direction != "long" && s.Direction != "short" {
		return errors.New("Выберите Long или Short")
	}
	if r.Market == "spot" && s.Direction == "short" {
		return errors.New("Short доступен только на Futures")
	}
	if !between(s.InitialCapital, 100, 1e8) || !between(s.PositionSizePct, 0.1, 100) || !between(s.FeePct, 0, 5) || !between(s.SlippagePct, 0, 5) || !between(s.TakeProfitPct, 0, 1000) || !between(s.StopLossPct, 0.01, 99) {
		return errors.New("Проверьте капитал, размер позиции, комиссию, проскальзывание и стоп-лосс")
	}
	if s.Direction == "short" && s.TakeProfitPct >= 100 {
		return errors.New("Тейк-профит для Short должен быть меньше 100%")
	}
	nodes := 0
	if err := validateNode(s.Entry, 0, &nodes); err != nil {
		return err
	}
	if s.Exit != nil {
		if err := validateNode(*s.Exit, 0, &nodes); err != nil {
			return err
		}
	}
	if err := r.validateIndicators(); err != nil {
		return err
	}
	return nil
}

func validateNode(c Condition, depth int, nodes *int) error {
	*nodes++
	if depth > 3 || *nodes > 32 {
		return errors.New("Не более 32 условий и 3 уровней вложенности")
	}
	if c.Kind == "indicator" {
		return validateIndicator(c)
	}
	if c.Indicator != "" || c.Measure != "" || c.WindowHours != 0 || c.Period != 0 || c.Threshold != 0 {
		return errors.New("Лишние параметры условия индикатора")
	}
	if c.Kind == "smc" {
		if c.Left != nil || c.Right != nil || c.Operator != "" || len(c.Children) != 0 || (c.Direction != "bullish" && c.Direction != "bearish") || c.WithinBars < 1 || c.WithinBars > 100 {
			return errors.New("Smart Money: выберите направление и окно от 1 до 100 свечей")
		}
		switch c.Event {
		case "bos", "choch", "mss", "liquidity_sweep", "fvg", "order_block":
			return nil
		default:
			return errors.New("Неизвестное событие Smart Money")
		}
	}
	if c.Event != "" || c.Direction != "" || c.WithinBars != 0 {
		return errors.New("Параметры Smart Money допустимы только для события")
	}
	if c.Kind == "and" || c.Kind == "or" {
		if len(c.Children) < 1 || len(c.Children) > 8 || c.Left != nil || c.Right != nil || c.Operator != "" {
			return errors.New("Группа должна содержать от 1 до 8 условий")
		}
		for _, n := range c.Children {
			if err := validateNode(n, depth+1, nodes); err != nil {
				return err
			}
		}
		return nil
	}
	if c.Kind != "compare" || c.Left == nil || c.Right == nil || len(c.Children) > 0 {
		return errors.New("Некорректный формат условия")
	}
	switch c.Operator {
	case "gt", "lt", "gte", "lte", "crosses_above", "crosses_below":
	default:
		return errors.New("Неизвестный оператор сравнения")
	}
	if c.Left.Kind == "constant" && c.Right.Kind == "constant" {
		return errors.New("Хотя бы одна сторона условия должна быть индикатором")
	}
	for _, o := range []*Operand{c.Left, c.Right} {
		switch o.Kind {
		case "constant":
			if !between(o.Value, -1e9, 1e9) || o.Period != 0 {
				return errors.New("Некорректное числовое значение")
			}
		case "close", "macd", "macd_signal", "macd_histogram":
			if o.Period != 0 || o.Value != 0 {
				return errors.New("Лишние параметры индикатора")
			}
		case "rsi", "ema", "sma":
			if o.Period < 2 || o.Period > 400 || o.Value != 0 {
				return errors.New("Период индикатора должен быть от 2 до 400")
			}
		default:
			return fmt.Errorf("Неизвестный индикатор: %s", o.Kind)
		}
	}
	return nil
}

func (r Request) WarmupBars() int {
	n := 1
	var visit func(Condition)
	visit = func(c Condition) {
		if c.Kind == "indicator" {
			p := c.RequiredBars(Interval(r.Timeframe))
			if p > n {
				n = p
			}
		}
		if c.Kind == "smc" && n < 200+c.WithinBars {
			n = 200 + c.WithinBars
		}
		for _, o := range []*Operand{c.Left, c.Right} {
			if o != nil {
				p := o.Period
				if o.Kind == "macd" || o.Kind == "macd_signal" || o.Kind == "macd_histogram" {
					p = 35
				}
				if p*5 > n {
					n = p * 5
				}
			}
		}
		for _, child := range c.Children {
			visit(child)
		}
	}
	visit(r.Strategy.Entry)
	if r.Strategy.Exit != nil {
		visit(*r.Strategy.Exit)
	}
	return n + 1
}

type Candle struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
	// Enriched only for a requested backtest; raw OHLCV storage stays unchanged.
	Metrics map[string]float64 `json:"metrics,omitempty"`
}
type Trade struct {
	Symbol     string  `json:"symbol"`
	Direction  string  `json:"direction"`
	EntryTime  int64   `json:"entryTime"`
	ExitTime   int64   `json:"exitTime"`
	EntryPrice float64 `json:"entryPrice"`
	ExitPrice  float64 `json:"exitPrice"`
	Quantity   float64 `json:"quantity"`
	Fees       float64 `json:"fees"`
	PnL        float64 `json:"pnl"`
	PnLPct     float64 `json:"pnlPct"`
	Reason     string  `json:"reason"`
}
type EquityPoint struct {
	Time  int64   `json:"time"`
	Value float64 `json:"value"`
}
type Metrics struct {
	ProfitPct      float64  `json:"profitPct"`
	MaxDrawdownPct float64  `json:"maxDrawdownPct"`
	WinRate        float64  `json:"winRate"`
	TradeCount     int      `json:"tradeCount"`
	ProfitFactor   *float64 `json:"profitFactor"`
	Sharpe         *float64 `json:"sharpe"`
	FinalCapital   float64  `json:"finalCapital"`
	TotalFees      float64  `json:"totalFees"`
}
type Result struct {
	EngineVersion string        `json:"engineVersion"`
	Metrics       Metrics       `json:"metrics"`
	Equity        []EquityPoint `json:"equity"`
	Trades        []Trade       `json:"trades"`
	Signals       []SignalStats `json:"signals,omitempty"`
}

type SignalStats struct {
	Symbol        string `json:"symbol"`
	EvaluatedBars int    `json:"evaluatedBars"`
	MatchedBars   int    `json:"matchedBars"`
}
