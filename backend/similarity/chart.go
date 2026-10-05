package similarity

import (
	"context"
	"fmt"
	"math"
	bt "shortlong/backtest"

	"golang.org/x/sync/errgroup"
)

type ChartBar struct {
	Time  int64   `json:"time"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
}

type MatchChart struct {
	ID      string     `json:"id"`
	Candles []ChartBar `json:"candles"`
}

// Read the original candles once for the six displayed matches, including saved
// jobs with older, compressed chart data. Changing the display interval does not
// run a search, access the exchange, build an index, or write duplicate history.
func (s *Searcher) Charts(ctx context.Context, result Response) ([]MatchChart, error) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if result.Query.Window < 1 || result.Query.Window > 2016 {
		return nil, fmt.Errorf("invalid chart window")
	}
	out := make([]MatchChart, min(6, len(result.Matches)))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(3)
	for i, m := range result.Matches[:len(out)] {
		g.Go(func() error {
			end := min(result.Query.End, m.End+int64(min(result.Query.Window, 288))*Step)
			if m.Start >= m.End || m.End-m.Start > 2016*Step || end < m.End {
				return fmt.Errorf("invalid chart bounds")
			}
			candles, err := s.Storage.Read(ctx, m.Market, m.Symbol, "5m", m.Start, end)
			if err != nil {
				return err
			}
			out[i] = MatchChart{ID: m.ID, Candles: ChartBars(candles, 1, m.End)}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// Bounded presentation data, read only for selected matches; never stored as raw duplicates.
// Do not aggregate across the matching boundary or across missing bars.
func ChartBars(c []bt.Candle, group int, boundary int64) []ChartBar {
	out := []ChartBar{}
	for i := 0; i < len(c); {
		first := c[i]
		bar := ChartBar{first.Time / 1000, first.Open, first.High, first.Low, first.Close}
		j := i + 1
		for j < len(c) && j < i+group && c[j].Time == c[j-1].Time+Step && !(first.Time < boundary && c[j].Time >= boundary) {
			bar.High = math.Max(bar.High, c[j].High)
			bar.Low = math.Min(bar.Low, c[j].Low)
			bar.Close = c[j].Close
			j++
		}
		out = append(out, bar)
		i = j
	}
	return out
}
