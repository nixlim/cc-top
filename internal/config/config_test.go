package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigParser_Defaults(t *testing.T) {
	result, err := LoadFrom("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("expected no error for missing config file, got: %v", err)
	}

	cfg := result.Config

	if cfg.Receiver.GRPCPort != 4317 {
		t.Errorf("default grpc_port: want 4317, got %d", cfg.Receiver.GRPCPort)
	}
	if cfg.Receiver.HTTPPort != 4318 {
		t.Errorf("default http_port: want 4318, got %d", cfg.Receiver.HTTPPort)
	}
	if cfg.Receiver.Bind != "127.0.0.1" {
		t.Errorf("default bind: want 127.0.0.1, got %s", cfg.Receiver.Bind)
	}
	if cfg.Scanner.IntervalSeconds != 5 {
		t.Errorf("default interval_seconds: want 5, got %d", cfg.Scanner.IntervalSeconds)
	}
	if cfg.Alerts.CostSurgeThresholdPerHour != 2.00 {
		t.Errorf("default cost_surge_threshold_per_hour: want 2.00, got %f", cfg.Alerts.CostSurgeThresholdPerHour)
	}
	if cfg.Alerts.RunawayTokenVelocity != 50000 {
		t.Errorf("default runaway_token_velocity: want 50000, got %d", cfg.Alerts.RunawayTokenVelocity)
	}
	if cfg.Alerts.RunawayTokenSustainedMinutes != 2 {
		t.Errorf("default runaway_token_sustained_minutes: want 2, got %d", cfg.Alerts.RunawayTokenSustainedMinutes)
	}
	if cfg.Alerts.LoopDetectorThreshold != 3 {
		t.Errorf("default loop_detector_threshold: want 3, got %d", cfg.Alerts.LoopDetectorThreshold)
	}
	if cfg.Alerts.LoopDetectorWindowMinutes != 5 {
		t.Errorf("default loop_detector_window_minutes: want 5, got %d", cfg.Alerts.LoopDetectorWindowMinutes)
	}
	if cfg.Alerts.ErrorStormCount != 10 {
		t.Errorf("default error_storm_count: want 10, got %d", cfg.Alerts.ErrorStormCount)
	}
	if cfg.Alerts.StaleSessionHours != 2 {
		t.Errorf("default stale_session_hours: want 2, got %d", cfg.Alerts.StaleSessionHours)
	}
	if cfg.Alerts.ContextPressurePercent != 80 {
		t.Errorf("default context_pressure_percent: want 80, got %d", cfg.Alerts.ContextPressurePercent)
	}
	if cfg.Alerts.SessionCostThreshold != 5.00 {
		t.Errorf("default session_cost_threshold: want 5.00, got %f", cfg.Alerts.SessionCostThreshold)
	}
	if cfg.Alerts.HighRejectionPercent != 50 {
		t.Errorf("default high_rejection_percent: want 50, got %d", cfg.Alerts.HighRejectionPercent)
	}
	if cfg.Alerts.HighRejectionWindowMinutes != 5 {
		t.Errorf("default high_rejection_window_minutes: want 5, got %d", cfg.Alerts.HighRejectionWindowMinutes)
	}
	if !cfg.Alerts.Notifications.SystemNotify {
		t.Error("default system_notify: want true, got false")
	}
	if cfg.Display.EventBufferSize != 1000 {
		t.Errorf("default event_buffer_size: want 1000, got %d", cfg.Display.EventBufferSize)
	}
	if cfg.Display.RefreshRateMS != 500 {
		t.Errorf("default refresh_rate_ms: want 500, got %d", cfg.Display.RefreshRateMS)
	}
	if cfg.Display.CostColorGreenBelow != 0.50 {
		t.Errorf("default cost_color_green_below: want 0.50, got %f", cfg.Display.CostColorGreenBelow)
	}
	if cfg.Display.CostColorYellowBelow != 2.00 {
		t.Errorf("default cost_color_yellow_below: want 2.00, got %f", cfg.Display.CostColorYellowBelow)
	}

	if len(cfg.Models) != 3 {
		t.Errorf("default models: want 3 entries, got %d", len(cfg.Models))
	}
	if cfg.Models["claude-sonnet-4-5-20250929"] != 200000 {
		t.Errorf("default sonnet context limit: want 200000, got %d", cfg.Models["claude-sonnet-4-5-20250929"])
	}
	if cfg.Models["claude-opus-4-6"] != 200000 {
		t.Errorf("default opus context limit: want 200000, got %d", cfg.Models["claude-opus-4-6"])
	}

	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings for missing file, got %v", result.Warnings)
	}
}

func TestConfigParser_CustomPorts(t *testing.T) {
	tomlData := `
[receiver]
grpc_port = 5317
http_port = 5318
bind = "0.0.0.0"
`
	result, err := LoadFromString(tomlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := result.Config
	if cfg.Receiver.GRPCPort != 5317 {
		t.Errorf("grpc_port: want 5317, got %d", cfg.Receiver.GRPCPort)
	}
	if cfg.Receiver.HTTPPort != 5318 {
		t.Errorf("http_port: want 5318, got %d", cfg.Receiver.HTTPPort)
	}
	if cfg.Receiver.Bind != "0.0.0.0" {
		t.Errorf("bind: want 0.0.0.0, got %s", cfg.Receiver.Bind)
	}

	if cfg.Scanner.IntervalSeconds != 5 {
		t.Errorf("default interval_seconds should be preserved: want 5, got %d", cfg.Scanner.IntervalSeconds)
	}
	if cfg.Display.EventBufferSize != 1000 {
		t.Errorf("default event_buffer_size should be preserved: want 1000, got %d", cfg.Display.EventBufferSize)
	}
}

func TestConfigParser_PartialConfig(t *testing.T) {
	tomlData := `
[scanner]
interval_seconds = 10

[alerts]
cost_surge_threshold_per_hour = 5.00
error_storm_count = 20
`
	result, err := LoadFromString(tomlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := result.Config

	if cfg.Scanner.IntervalSeconds != 10 {
		t.Errorf("interval_seconds: want 10, got %d", cfg.Scanner.IntervalSeconds)
	}
	if cfg.Alerts.CostSurgeThresholdPerHour != 5.00 {
		t.Errorf("cost_surge_threshold_per_hour: want 5.00, got %f", cfg.Alerts.CostSurgeThresholdPerHour)
	}
	if cfg.Alerts.ErrorStormCount != 20 {
		t.Errorf("error_storm_count: want 20, got %d", cfg.Alerts.ErrorStormCount)
	}

	if cfg.Receiver.GRPCPort != 4317 {
		t.Errorf("grpc_port default: want 4317, got %d", cfg.Receiver.GRPCPort)
	}
	if cfg.Receiver.HTTPPort != 4318 {
		t.Errorf("http_port default: want 4318, got %d", cfg.Receiver.HTTPPort)
	}
	if cfg.Alerts.LoopDetectorThreshold != 3 {
		t.Errorf("loop_detector_threshold default: want 3, got %d", cfg.Alerts.LoopDetectorThreshold)
	}
	if cfg.Display.EventBufferSize != 1000 {
		t.Errorf("event_buffer_size default: want 1000, got %d", cfg.Display.EventBufferSize)
	}
}

func TestConfigParser_InvalidValue(t *testing.T) {
	tests := []struct {
		name string
		toml string
	}{
		{
			name: "negative grpc_port",
			toml: `[receiver]
grpc_port = -1`,
		},
		{
			name: "port over 65535",
			toml: `[receiver]
grpc_port = 70000`,
		},
		{
			name: "zero http_port",
			toml: `[receiver]
http_port = 0`,
		},
		{
			name: "negative scanner interval",
			toml: `[scanner]
interval_seconds = -5`,
		},
		{
			name: "zero event_buffer_size",
			toml: `[display]
event_buffer_size = 0`,
		},
		{
			name: "negative context_pressure_percent",
			toml: `[alerts]
context_pressure_percent = -10`,
		},
		{
			name: "context_pressure_percent over 100",
			toml: `[alerts]
context_pressure_percent = 101`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFromString(tt.toml)
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestConfigParser_UnknownKey(t *testing.T) {
	tomlData := `
[receiver]
grpc_port = 4317

[mysterious_section]
foo = "bar"

[another_unknown]
baz = 42
`
	result, err := LoadFromString(tomlData)
	if err != nil {
		t.Fatalf("unknown keys should not cause errors, got: %v", err)
	}

	if len(result.Warnings) == 0 {
		t.Error("expected warnings for unknown keys, got none")
	}

	foundMysterious := false
	foundAnother := false
	for _, w := range result.Warnings {
		if w == `unknown config key: "mysterious_section"` {
			foundMysterious = true
		}
		if w == `unknown config key: "another_unknown"` {
			foundAnother = true
		}
	}
	if !foundMysterious {
		t.Error("expected warning for mysterious_section, not found")
	}
	if !foundAnother {
		t.Error("expected warning for another_unknown, not found")
	}

	if result.Config.Receiver.GRPCPort != 4317 {
		t.Errorf("grpc_port should still be loaded: want 4317, got %d", result.Config.Receiver.GRPCPort)
	}
}

func TestConfigParser_ModelContextLimits(t *testing.T) {
	tomlData := `
[models]
"claude-sonnet-4-5-20250929" = 200000
"claude-opus-4-6" = 200000
"claude-haiku-4-5-20251001" = 200000
"my-custom-model" = 128000

[models.pricing]
"claude-sonnet-4-5-20250929" = [3.00, 15.00, 0.30, 3.75]
"my-custom-model" = [1.00, 5.00, 0.10, 1.25]
`
	result, err := LoadFromString(tomlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg := result.Config

	if cfg.Models["claude-sonnet-4-5-20250929"] != 200000 {
		t.Errorf("sonnet context limit: want 200000, got %d", cfg.Models["claude-sonnet-4-5-20250929"])
	}
	if cfg.Models["claude-opus-4-6"] != 200000 {
		t.Errorf("opus context limit: want 200000, got %d", cfg.Models["claude-opus-4-6"])
	}
	if cfg.Models["my-custom-model"] != 128000 {
		t.Errorf("custom model context limit: want 128000, got %d", cfg.Models["my-custom-model"])
	}

	sonnetPricing, ok := cfg.Pricing["claude-sonnet-4-5-20250929"]
	if !ok {
		t.Fatal("sonnet pricing not found")
	}
	if sonnetPricing[0] != 3.00 || sonnetPricing[1] != 15.00 {
		t.Errorf("sonnet pricing: want [3.00, 15.00, ...], got %v", sonnetPricing)
	}

	customPricing, ok := cfg.Pricing["my-custom-model"]
	if !ok {
		t.Fatal("custom model pricing not found")
	}
	if customPricing[0] != 1.00 || customPricing[1] != 5.00 {
		t.Errorf("custom pricing: want [1.00, 5.00, ...], got %v", customPricing)
	}
}

func TestConfigParser_FileLoad(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	tomlContent := `
[receiver]
grpc_port = 9317

[display]
event_buffer_size = 2000
`
	if err := os.WriteFile(configPath, []byte(tomlContent), 0644); err != nil {
		t.Fatalf("writing test config file: %v", err)
	}

	result, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Config.Receiver.GRPCPort != 9317 {
		t.Errorf("grpc_port from file: want 9317, got %d", result.Config.Receiver.GRPCPort)
	}
	if result.Config.Display.EventBufferSize != 2000 {
		t.Errorf("event_buffer_size from file: want 2000, got %d", result.Config.Display.EventBufferSize)
	}
	if result.Config.Receiver.HTTPPort != 4318 {
		t.Errorf("http_port default: want 4318, got %d", result.Config.Receiver.HTTPPort)
	}
}

func TestConfigParser_EmptyString(t *testing.T) {
	result, err := LoadFromString("")
	if err != nil {
		t.Fatalf("unexpected error for empty config: %v", err)
	}

	if result.Config.Receiver.GRPCPort != 4317 {
		t.Errorf("grpc_port: want 4317, got %d", result.Config.Receiver.GRPCPort)
	}
}

func TestConfigParser_SessionCostThresholdDefault(t *testing.T) {
	result, err := LoadFromString("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Config.Alerts.SessionCostThreshold != 5.00 {
		t.Errorf("default session_cost_threshold: want 5.00, got %f", result.Config.Alerts.SessionCostThreshold)
	}
}

func TestConfigParser_SessionCostThresholdCustom(t *testing.T) {
	tomlData := `
[alerts]
session_cost_threshold = 10.00
`
	result, err := LoadFromString(tomlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Config.Alerts.SessionCostThreshold != 10.00 {
		t.Errorf("session_cost_threshold: want 10.00, got %f", result.Config.Alerts.SessionCostThreshold)
	}
}

func TestConfigParser_SessionCostThresholdInvalid(t *testing.T) {
	tomlData := `
[alerts]
session_cost_threshold = -1.0
`
	_, err := LoadFromString(tomlData)
	if err == nil {
		t.Error("expected validation error for negative session_cost_threshold")
	}
}

func TestConfigParser_HighRejectionDefaults(t *testing.T) {
	result, err := LoadFromString("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Config.Alerts.HighRejectionPercent != 50 {
		t.Errorf("default high_rejection_percent: want 50, got %d", result.Config.Alerts.HighRejectionPercent)
	}
	if result.Config.Alerts.HighRejectionWindowMinutes != 5 {
		t.Errorf("default high_rejection_window_minutes: want 5, got %d", result.Config.Alerts.HighRejectionWindowMinutes)
	}
}

func TestConfigParser_HighRejectionCustom(t *testing.T) {
	tomlData := `
[alerts]
high_rejection_percent = 75
high_rejection_window_minutes = 10
`
	result, err := LoadFromString(tomlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Config.Alerts.HighRejectionPercent != 75 {
		t.Errorf("high_rejection_percent: want 75, got %d", result.Config.Alerts.HighRejectionPercent)
	}
	if result.Config.Alerts.HighRejectionWindowMinutes != 10 {
		t.Errorf("high_rejection_window_minutes: want 10, got %d", result.Config.Alerts.HighRejectionWindowMinutes)
	}
}

func TestConfigParser_HighRejectionInvalid(t *testing.T) {
	tests := []struct {
		name string
		toml string
	}{
		{
			name: "high_rejection_percent zero",
			toml: `[alerts]
high_rejection_percent = 0`,
		},
		{
			name: "high_rejection_percent over 100",
			toml: `[alerts]
high_rejection_percent = 101`,
		},
		{
			name: "high_rejection_window_minutes zero",
			toml: `[alerts]
high_rejection_window_minutes = 0`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFromString(tt.toml)
			if err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}

func TestStorageConfig_Defaults(t *testing.T) {
	result, err := LoadFrom("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("expected no error for missing config file, got: %v", err)
	}

	cfg := result.Config

	if cfg.Storage.DBPath != "~/.local/share/cc-top/cc-top.db" {
		t.Errorf("default db_path: want ~/.local/share/cc-top/cc-top.db, got %s", cfg.Storage.DBPath)
	}
	if cfg.Storage.RetentionDays != 7 {
		t.Errorf("default retention_days: want 7, got %d", cfg.Storage.RetentionDays)
	}
	if cfg.Storage.SummaryRetentionDays != 90 {
		t.Errorf("default summary_retention_days: want 90, got %d", cfg.Storage.SummaryRetentionDays)
	}
}

func TestStorageConfig_ParseCustom(t *testing.T) {
	tomlData := `
[storage]
db_path = "/custom/path/data.db"
retention_days = 14
summary_retention_days = 180
`
	result, err := LoadFromString(tomlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Config.Storage.DBPath != "/custom/path/data.db" {
		t.Errorf("db_path: want /custom/path/data.db, got %s", result.Config.Storage.DBPath)
	}
	if result.Config.Storage.RetentionDays != 14 {
		t.Errorf("retention_days: want 14, got %d", result.Config.Storage.RetentionDays)
	}
	if result.Config.Storage.SummaryRetentionDays != 180 {
		t.Errorf("summary_retention_days: want 180, got %d", result.Config.Storage.SummaryRetentionDays)
	}
}

func TestStorageConfig_ValidationRejectsZeroRetention(t *testing.T) {
	tests := []struct {
		name string
		toml string
	}{
		{
			name: "retention_days zero",
			toml: `[storage]
retention_days = 0`,
		},
		{
			name: "summary_retention_days zero",
			toml: `[storage]
summary_retention_days = 0`,
		},
		{
			name: "retention_days negative",
			toml: `[storage]
retention_days = -1`,
		},
		{
			name: "summary_retention_days negative",
			toml: `[storage]
summary_retention_days = -5`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFromString(tt.toml)
			if err == nil {
				t.Error("expected validation error for non-positive retention value, got nil")
			}
		})
	}
}

func TestStorageConfig_EmptyDBPath(t *testing.T) {
	tomlData := `
[storage]
db_path = ""
retention_days = 7
summary_retention_days = 90
`
	result, err := LoadFromString(tomlData)
	if err != nil {
		t.Fatalf("expected no error for empty db_path, got: %v", err)
	}

	if result.Config.Storage.DBPath != "" {
		t.Errorf("db_path: want empty string, got %s", result.Config.Storage.DBPath)
	}
}

func TestStorageConfig_UnknownKeyWarning(t *testing.T) {
	tomlData := `
[storage]
db_path = "/tmp/test.db"
retention_days = 7
summary_retention_days = 90
unknown_field = "value"
`
	result, err := LoadFromString(tomlData)
	if err != nil {
		t.Fatalf("unknown keys within sections should not cause errors, got: %v", err)
	}

	if result.Config.Storage.DBPath != "/tmp/test.db" {
		t.Errorf("db_path: want /tmp/test.db, got %s", result.Config.Storage.DBPath)
	}
	if result.Config.Storage.RetentionDays != 7 {
		t.Errorf("retention_days: want 7, got %d", result.Config.Storage.RetentionDays)
	}
}
