package similarity

import (
	"context"
	bt "shortlong/backtest"
	"shortlong/backtest/marketdata"
)

type Pipeline struct {
	Store    Store
	Storage  *marketdata.ClickHouse
	Index    VectorIndex
	Outcomes Outcomes
}

func (p Pipeline) Process(ctx context.Context, t Task) (bool, error) {
	from, to := t.From-int64(2*t.Span)*Step, t.To
	if t.Kind == "outcomes" {
		from = t.From - Step
		to = t.To + int64(t.Span)*Step
	}
	candles, e := p.Storage.Read(ctx, t.Market, t.Symbol, "5m", from, to)
	if e != nil {
		return false, e
	}
	byTime := make(map[int64]int, len(candles))
	for i, c := range candles {
		byTime[c.Time] = i
	}
	slice := func(start, end int64) []bt.Candle {
		i, ok := byTime[start]
		if !ok {
			return nil
		}
		j, ok := byTime[end-Step]
		if !ok || j < i {
			return nil
		}
		return candles[i : j+1]
	}
	step := int64(t.Stride) * Step
	start := (t.From + step - 1) / step * step
	missing := false
	points := []Candidate{}
	outcomes := []OutcomeRow{}
	for end := start; end < t.To; end += step {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if t.Kind == "features" {
			c := slice(end-int64(2*t.Span)*Step, end)
			s, err := Features(c, t.Span)
			if err != nil {
				missing = true
				continue
			}
			points = append(points, Candidate{ID: WindowID(t.Market, t.Symbol, t.Span, end), Vector: s.Vector(p.Store.Config.Weights), Payload: Payload{t.Market, t.Symbol, end, s.Trend, s.Volatility, s.Volume}})
		} else {
			c := slice(end-Step, end+int64(t.Span)*Step)
			o, err := Future(c, end, t.Span)
			if err != nil {
				missing = true
				continue
			}
			outcomes = append(outcomes, OutcomeRow{t.Market, t.Symbol, end, o})
		}
	}
	// Durable writes first. A crash before Finish repeats deterministic point IDs.
	if e = p.Index.Upsert(ctx, t.Span, points); e != nil {
		return false, e
	}
	if e = p.Outcomes.Insert(ctx, outcomes); e != nil {
		return false, e
	}
	return missing, nil
}
