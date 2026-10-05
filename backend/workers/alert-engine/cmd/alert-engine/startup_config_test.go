package main

import (
	"strings"
	"testing"
)

func validProductionConfig() Config {
	return Config{
		RulesSlotFilter:     "SLOT_1",
		MarketConsumerGroup: "alert-engine-slot1",
		MarketStreams:       []string{"candles:v3:bybit:perpetual"},
		SnapshotPath:        "/app/state/slot1.snapshot",
		TelegramBotToken:    "configured",
	}
}

func TestValidateStartupConfigRequiresProductionInputs(t *testing.T) {
	valid := validProductionConfig()
	if err := validateStartupConfig(valid); err != nil {
		t.Fatalf("valid production config rejected: %v", err)
	}
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"telegram token", func(c *Config) { c.TelegramBotToken = "" }, "TELEGRAM_BOT_TOKEN"},
		{"slot", func(c *Config) { c.RulesSlotFilter = "" }, "RULES_SLOT_FILTER"},
		{"consumer group", func(c *Config) { c.MarketConsumerGroup = "" }, "MARKET_CONSUMER_GROUP"},
		{"streams", func(c *Config) { c.MarketStreams = nil }, "MARKET_STREAMS"},
		{"snapshot", func(c *Config) { c.SnapshotPath = "" }, "SPARSE_SNAPSHOT_PATH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.edit(&cfg)
			if err := validateStartupConfig(cfg); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v want fragment %q", err, tt.want)
			}
		})
	}
}

func TestLoadedRuleWindowBoundaries(t *testing.T) {
	for _, value := range []int{1, 60, 720, 721, 1440} {
		if !isValidLoadedRuleWindow(value, alertWindow24hMaxMinutes) {
			t.Fatalf("production loader rejected %d", value)
		}
	}
	for _, value := range []int{0, 1441} {
		if isValidLoadedRuleWindow(value, alertWindow24hMaxMinutes) {
			t.Fatalf("production loader accepted %d", value)
		}
	}
}
