package chartconfigapi

import "testing"

func TestNormalizeConfigDefaultsAndValidation(t *testing.T) {
	config, err := normalizeConfig(ChartConfig{
		Version:            1,
		SelectedInterval:   "5m",
		ActiveIndicatorIDs: []string{"ema", "rsi", "mss", "order_blocks", "ema"},
		DrawingSets: map[string][]ChartDrawing{
			"bybit:BTCUSDT:5m": {{
				ID: 1, Type: "trend", Points: []DrawingPoint{{Time: 1, Price: 100}, {Time: 2, Price: 110}},
			}},
		},
	})
	if err != nil {
		t.Fatalf("normalize valid config: %v", err)
	}
	if len(config.ActiveIndicatorIDs) != 4 || config.ActiveIndicatorIDs[0] != "ema" || config.ActiveIndicatorIDs[1] != "rsi" || config.ActiveIndicatorIDs[2] != "mss" || config.ActiveIndicatorIDs[3] != "order_blocks" {
		t.Fatalf("unexpected normalized indicators: %#v", config.ActiveIndicatorIDs)
	}
	if len(config.DrawingSets["bybit:BTCUSDT:5m"]) != 1 {
		t.Fatalf("unexpected normalized drawings: %#v", config.DrawingSets)
	}

	for name, candidate := range map[string]ChartConfig{
		"version":   {Version: 2, SelectedInterval: "5m"},
		"interval":  {Version: 1, SelectedInterval: "2m"},
		"indicator": {Version: 1, SelectedInterval: "5m", ActiveIndicatorIDs: []string{"unknown"}},
		"drawing type": {
			Version: 1, SelectedInterval: "5m",
			DrawingSets: map[string][]ChartDrawing{"bybit:BTCUSDT:5m": {{ID: 1, Type: "circle", Points: []DrawingPoint{{Time: 1, Price: 1}}}}},
		},
		"drawing points": {
			Version: 1, SelectedInterval: "5m",
			DrawingSets: map[string][]ChartDrawing{"bybit:BTCUSDT:5m": {{ID: 1, Type: "trend", Points: []DrawingPoint{{Time: 1, Price: 1}}}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeConfig(candidate); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDefaultConfigStartsWithIndicatorsDisabled(t *testing.T) {
	config := defaultConfig()
	if config.Version != 1 || config.SelectedInterval != "1m" {
		t.Fatalf("unexpected default: %#v", config)
	}
	if len(config.ActiveIndicatorIDs) != 0 {
		t.Fatalf("indicators must be disabled by default: %#v", config.ActiveIndicatorIDs)
	}
	if config.DrawingSets == nil || len(config.DrawingSets) != 0 {
		t.Fatalf("drawings must start empty: %#v", config.DrawingSets)
	}
}
