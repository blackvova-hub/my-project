package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type refreshingBootstrapSource struct {
	compactStreamBootstrapSource
	refresh func()
}

func (s *refreshingBootstrapSource) RangePage(ctx context.Context, stream, start, end string, count int64) (compactStreamPage, error) {
	page, err := s.compactStreamBootstrapSource.RangePage(ctx, stream, start, end, count)
	if err == nil && s.refresh != nil {
		s.refresh()
		s.refresh = nil
	}
	return page, err
}

func TestCompactBootstrapChecksDependenciesNotCacheGeneration(t *testing.T) {
	for _, test := range []struct {
		name       string
		window     int
		threshold  float64
		wantChange bool
	}{
		{name: "equivalent dependency refresh", window: 2, threshold: 7},
		{name: "changed dependencies", window: 1440, wantChange: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			history := newSnapshotTestHistory()
			base := &compactBootstrapPageSource{messages: []redis.XMessage{
				compactBootstrapValidMessage(t, now.Add(-time.Minute), 0),
			}}
			source := &refreshingBootstrapSource{compactStreamBootstrapSource: base}
			source.refresh = func() {
				history.rules.Set(map[string][]Rule{"bybit:perpetual:*": {{
					ID: 8, Exchange: "bybit", MarketType: "perpetual", Symbol: "*", WindowMinutes: test.window,
					Conditions: []Condition{{Indicator: "price", ThresholdPct: test.threshold}},
				}}})
			}
			_, err := BootstrapCompactStreams(context.Background(), source, &fakeCompactBootstrapOwner{}, history, []string{"candles:v3:bybit:perpetual"}, "group", now)
			if test.wantChange != errors.Is(err, errCompactBootstrapRulesChanged) {
				t.Fatalf("error=%v want dependency change=%v", err, test.wantChange)
			}
		})
	}
}
