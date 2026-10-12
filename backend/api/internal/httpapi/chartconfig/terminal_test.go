package chartconfigapi

import (
	"encoding/json"
	"testing"
)

func TestTerminalDrawingFamiliesAndLegacyConfiguration(t *testing.T) {
	for kind, count := range allowedDrawingPointCounts {
		if count < 1 {
			count = 2
		}
		points := make([]DrawingPoint, count)
		for i := range points {
			points[i] = DrawingPoint{Time: 1700000000 + int64(i)*60, Price: 100 + float64(i)}
		}
		config := defaultConfig()
		d := ChartDrawing{ID: 1, Type: kind, Points: points, Color: "#61d99b", Width: 2, Dash: "solid"}
		switch kind {
		case "text", "screen-text", "note", "screen-note", "callout", "comment", "price-note", "emoji":
			d.Text = "Проверка"
		}
		config.DrawingSets["bybit:linear:BTCUSDT:1h"] = []ChartDrawing{d}
		normalized, err := normalizeConfig(config)
		if err != nil {
			t.Errorf("%s: %v", kind, err)
			continue
		}
		raw, err := json.Marshal(normalized)
		if err != nil {
			t.Fatal(err)
		}
		var restored ChartConfig
		if err = json.Unmarshal(raw, &restored); err != nil {
			t.Fatal(err)
		}
		if _, err = normalizeConfig(restored); err != nil {
			t.Errorf("%s round trip: %v", kind, err)
		}
	}
	legacy := defaultConfig()
	legacy.DrawingSets["bybit:BTCUSDT:1m"] = []ChartDrawing{{ID: 3, Type: "trend", Points: []DrawingPoint{{Time: 1700000000, Price: 100}, {Time: 1700000060, Price: 110}}}}
	if _, err := normalizeConfig(legacy); err != nil {
		t.Fatal(err)
	}
}
func TestTerminalStyleValidation(t *testing.T) {
	for _, d := range []ChartDrawing{{Color: "url(https://example.com)"}, {Width: 100}, {Dash: "unknown"}, {Screen: []float64{2, .5}}, {Levels: []float64{101}}, {Quantity: -1}, {Sample: []DrawingSample{{Time: 1, Open: 100, High: 90, Low: 80, Close: 95, Volume: 1}}}} {
		if err := validateDrawingStyle(d); err == nil {
			t.Errorf("accepted invalid style: %+v", d)
		}
	}
}
