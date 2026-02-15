// Package config handles loading and validating cc-top configuration from TOML files.
//
// The configuration file is optional. When absent, all values use sensible defaults
// that allow cc-top to work out of the box. The config file location is
// ~/.config/cc-top/config.toml.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds all cc-top configuration loaded from TOML.
type Config struct {
	Receiver ReceiverConfig
	Scanner  ScannerConfig
	Alerts   AlertsConfig
	Display  DisplayConfig
	Models   map[string]int        // model name -> context token limit
	Pricing  map[string][4]float64 // model name -> [input, output, cache_read, cache_creation] per million
}

// ReceiverConfig configures the OTLP receiver endpoints.
type ReceiverConfig struct {
	GRPCPort int    `toml:"grpc_port"`
	HTTPPort int    `toml:"http_port"`
	Bind     string `toml:"bind"`
}

// ScannerConfig configures the process scanner.
type ScannerConfig struct {
	IntervalSeconds int `toml:"interval_seconds"`
}

// AlertsConfig configures alert thresholds and notification behaviour.
type AlertsConfig struct {
	CostSurgeThresholdPerHour float64            `toml:"cost_surge_threshold_per_hour"`
	RunawayTokenVelocity      int                `toml:"runaway_token_velocity"`
	LoopDetectorThreshold     int                `toml:"loop_detector_threshold"`
	LoopDetectorWindowMinutes int                `toml:"loop_detector_window_minutes"`
	ErrorStormCount           int                `toml:"error_storm_count"`
	StaleSessionHours         int                `toml:"stale_session_hours"`
	ContextPressurePercent    int                `toml:"context_pressure_percent"`
	Notifications             NotificationConfig `toml:"notifications"`
}

// NotificationConfig controls system notification behaviour.
type NotificationConfig struct {
	SystemNotify bool `toml:"system_notify"`
}

// DisplayConfig configures TUI display parameters.
type DisplayConfig struct {
	EventBufferSize      int     `toml:"event_buffer_size"`
	RefreshRateMS        int     `toml:"refresh_rate_ms"`
	CostColorGreenBelow  float64 `toml:"cost_color_green_below"`
	CostColorYellowBelow float64 `toml:"cost_color_yellow_below"`
}

// LoadResult contains the loaded configuration and any warnings encountered during parsing.
type LoadResult struct {
	Config   Config
	Warnings []string
}

// defaultConfigPath returns the default config file path (~/.config/cc-top/config.toml).
func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "cc-top", "config.toml")
}

// Load reads and parses the TOML config file from the default location.
// If the file does not exist, it returns all defaults with no error.
// Unknown keys produce warnings but not errors.
func Load() (*LoadResult, error) {
	return LoadFrom(defaultConfigPath())
}

// LoadFrom reads and parses the TOML config file from the specified path.
// If the file does not exist, it returns all defaults with no error.
// Unknown keys produce warnings but not errors.
func LoadFrom(path string) (*LoadResult, error) {
	cfg := DefaultConfig()
	result := &LoadResult{Config: cfg}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Config file not found = use all defaults, no error.
			return result, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Use a raw map to detect unknown keys before structured decoding.
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Detect unknown top-level keys.
	knownTopLevel := map[string]bool{
		"receiver": true,
		"scanner":  true,
		"alerts":   true,
		"display":  true,
		"models":   true,
	}
	for key := range raw {
		if !knownTopLevel[key] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("unknown config key: %q", key))
		}
	}

	// Parse into a tomlFile structure that matches the TOML layout, including
	// the [models] table with its nested [models.pricing] sub-table.
	var tf tomlFile
	if _, err := toml.Decode(string(data), &tf); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	// Apply parsed values over defaults.
	mergeFromRaw(&result.Config, &tf, raw)
	mergeModelsFromRaw(&result.Config, raw)

	// Validate the final config.
	if err := validate(&result.Config); err != nil {
		return nil, err
	}

	return result, nil
}

// tomlFile mirrors the TOML structure for decoding purposes.
// The [models] table has both context limits (bare keys) and a [models.pricing] sub-table.
type tomlFile struct {
	Receiver *ReceiverConfig `toml:"receiver"`
	Scanner  *ScannerConfig  `toml:"scanner"`
	Alerts   *AlertsConfig   `toml:"alerts"`
	Display  *DisplayConfig  `toml:"display"`
	Models   *tomlModels     `toml:"models"`
}

// tomlModels handles the [models] table which contains both context limits
// as bare key-value pairs and a [models.pricing] sub-table.
type tomlModels struct {
	Pricing map[string]tomlPricing `toml:"pricing"`
	// Remaining keys are context limits; we decode them from raw.
}

type tomlPricing [4]float64

// mergeFromRaw applies explicitly set TOML values over the defaults in cfg.
// It uses the raw map to detect which keys were explicitly present (including zero values)
// and the decoded tomlFile struct to get the typed values.
func mergeFromRaw(cfg *Config, tf *tomlFile, raw map[string]any) {
	if tf.Receiver != nil {
		if section, ok := rawSection(raw, "receiver"); ok {
			if _, exists := section["grpc_port"]; exists {
				cfg.Receiver.GRPCPort = tf.Receiver.GRPCPort
			}
			if _, exists := section["http_port"]; exists {
				cfg.Receiver.HTTPPort = tf.Receiver.HTTPPort
			}
			if _, exists := section["bind"]; exists {
				cfg.Receiver.Bind = tf.Receiver.Bind
			}
		}
	}
	if tf.Scanner != nil {
		if section, ok := rawSection(raw, "scanner"); ok {
			if _, exists := section["interval_seconds"]; exists {
				cfg.Scanner.IntervalSeconds = tf.Scanner.IntervalSeconds
			}
		}
	}
	if tf.Alerts != nil {
		if section, ok := rawSection(raw, "alerts"); ok {
			if _, exists := section["cost_surge_threshold_per_hour"]; exists {
				cfg.Alerts.CostSurgeThresholdPerHour = tf.Alerts.CostSurgeThresholdPerHour
			}
			if _, exists := section["runaway_token_velocity"]; exists {
				cfg.Alerts.RunawayTokenVelocity = tf.Alerts.RunawayTokenVelocity
			}
			if _, exists := section["loop_detector_threshold"]; exists {
				cfg.Alerts.LoopDetectorThreshold = tf.Alerts.LoopDetectorThreshold
			}
			if _, exists := section["loop_detector_window_minutes"]; exists {
				cfg.Alerts.LoopDetectorWindowMinutes = tf.Alerts.LoopDetectorWindowMinutes
			}
			if _, exists := section["error_storm_count"]; exists {
				cfg.Alerts.ErrorStormCount = tf.Alerts.ErrorStormCount
			}
			if _, exists := section["stale_session_hours"]; exists {
				cfg.Alerts.StaleSessionHours = tf.Alerts.StaleSessionHours
			}
			if _, exists := section["context_pressure_percent"]; exists {
				cfg.Alerts.ContextPressurePercent = tf.Alerts.ContextPressurePercent
			}
			if _, exists := section["notifications"]; exists {
				cfg.Alerts.Notifications = tf.Alerts.Notifications
			}
		}
	}
	if tf.Display != nil {
		if section, ok := rawSection(raw, "display"); ok {
			if _, exists := section["event_buffer_size"]; exists {
				cfg.Display.EventBufferSize = tf.Display.EventBufferSize
			}
			if _, exists := section["refresh_rate_ms"]; exists {
				cfg.Display.RefreshRateMS = tf.Display.RefreshRateMS
			}
			if _, exists := section["cost_color_green_below"]; exists {
				cfg.Display.CostColorGreenBelow = tf.Display.CostColorGreenBelow
			}
			if _, exists := section["cost_color_yellow_below"]; exists {
				cfg.Display.CostColorYellowBelow = tf.Display.CostColorYellowBelow
			}
		}
	}
}

// rawSection returns the sub-map for a given top-level TOML section.
func rawSection(raw map[string]any, key string) (map[string]any, bool) {
	v, ok := raw[key]
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

// mergeModelsFromRaw parses the [models] table from the raw TOML map.
// Context limits are bare key = int entries; pricing is the [models.pricing] sub-table.
func mergeModelsFromRaw(cfg *Config, raw map[string]any) {
	modelsRaw, ok := raw["models"]
	if !ok {
		return
	}
	modelsMap, ok := modelsRaw.(map[string]any)
	if !ok {
		return
	}

	for key, val := range modelsMap {
		if key == "pricing" {
			// Handle [models.pricing] sub-table.
			pricingMap, ok := val.(map[string]any)
			if !ok {
				continue
			}
			if cfg.Pricing == nil {
				cfg.Pricing = make(map[string][4]float64)
			}
			for model, priceVal := range pricingMap {
				priceSlice, ok := priceVal.([]any)
				if !ok || len(priceSlice) != 4 {
					continue
				}
				var prices [4]float64
				valid := true
				for i, v := range priceSlice {
					switch n := v.(type) {
					case float64:
						prices[i] = n
					case int64:
						prices[i] = float64(n)
					default:
						valid = false
					}
				}
				if valid {
					cfg.Pricing[model] = prices
				}
			}
			continue
		}
		// Context limit: key = model name, value = int.
		switch n := val.(type) {
		case int64:
			if cfg.Models == nil {
				cfg.Models = make(map[string]int)
			}
			cfg.Models[key] = int(n)
		}
	}
}

// LoadFromString parses TOML config from a string. Useful for testing.
func LoadFromString(data string) (*LoadResult, error) {
	cfg := DefaultConfig()
	result := &LoadResult{Config: cfg}

	if data == "" {
		return result, nil
	}

	// Use a raw map to detect unknown keys.
	var raw map[string]any
	if _, err := toml.Decode(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Detect unknown top-level keys.
	knownTopLevel := map[string]bool{
		"receiver": true,
		"scanner":  true,
		"alerts":   true,
		"display":  true,
		"models":   true,
	}
	for key := range raw {
		if !knownTopLevel[key] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("unknown config key: %q", key))
		}
	}

	// Parse structured portions.
	var tf tomlFile
	if _, err := toml.Decode(data, &tf); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	mergeFromRaw(&result.Config, &tf, raw)
	mergeModelsFromRaw(&result.Config, raw)

	if err := validate(&result.Config); err != nil {
		return nil, err
	}

	return result, nil
}

// validate checks that the configuration values are within valid ranges.
func validate(cfg *Config) error {
	var errs []string

	// Port validation: 1-65535.
	if cfg.Receiver.GRPCPort < 1 || cfg.Receiver.GRPCPort > 65535 {
		errs = append(errs, fmt.Sprintf("grpc_port must be 1-65535, got %d", cfg.Receiver.GRPCPort))
	}
	if cfg.Receiver.HTTPPort < 1 || cfg.Receiver.HTTPPort > 65535 {
		errs = append(errs, fmt.Sprintf("http_port must be 1-65535, got %d", cfg.Receiver.HTTPPort))
	}

	// Positive thresholds.
	if cfg.Scanner.IntervalSeconds < 1 {
		errs = append(errs, fmt.Sprintf("scanner interval_seconds must be positive, got %d", cfg.Scanner.IntervalSeconds))
	}
	if cfg.Alerts.CostSurgeThresholdPerHour <= 0 {
		errs = append(errs, fmt.Sprintf("cost_surge_threshold_per_hour must be positive, got %f", cfg.Alerts.CostSurgeThresholdPerHour))
	}
	if cfg.Alerts.RunawayTokenVelocity < 1 {
		errs = append(errs, fmt.Sprintf("runaway_token_velocity must be positive, got %d", cfg.Alerts.RunawayTokenVelocity))
	}
	if cfg.Alerts.LoopDetectorThreshold < 1 {
		errs = append(errs, fmt.Sprintf("loop_detector_threshold must be positive, got %d", cfg.Alerts.LoopDetectorThreshold))
	}
	if cfg.Alerts.LoopDetectorWindowMinutes < 1 {
		errs = append(errs, fmt.Sprintf("loop_detector_window_minutes must be positive, got %d", cfg.Alerts.LoopDetectorWindowMinutes))
	}
	if cfg.Alerts.ErrorStormCount < 1 {
		errs = append(errs, fmt.Sprintf("error_storm_count must be positive, got %d", cfg.Alerts.ErrorStormCount))
	}
	if cfg.Alerts.StaleSessionHours < 1 {
		errs = append(errs, fmt.Sprintf("stale_session_hours must be positive, got %d", cfg.Alerts.StaleSessionHours))
	}
	if cfg.Alerts.ContextPressurePercent < 1 || cfg.Alerts.ContextPressurePercent > 100 {
		errs = append(errs, fmt.Sprintf("context_pressure_percent must be 1-100, got %d", cfg.Alerts.ContextPressurePercent))
	}

	// Positive buffer size.
	if cfg.Display.EventBufferSize < 1 {
		errs = append(errs, fmt.Sprintf("event_buffer_size must be positive, got %d", cfg.Display.EventBufferSize))
	}
	if cfg.Display.RefreshRateMS < 1 {
		errs = append(errs, fmt.Sprintf("refresh_rate_ms must be positive, got %d", cfg.Display.RefreshRateMS))
	}

	// Cost thresholds must be positive.
	if cfg.Display.CostColorGreenBelow <= 0 {
		errs = append(errs, fmt.Sprintf("cost_color_green_below must be positive, got %f", cfg.Display.CostColorGreenBelow))
	}
	if cfg.Display.CostColorYellowBelow <= 0 {
		errs = append(errs, fmt.Sprintf("cost_color_yellow_below must be positive, got %f", cfg.Display.CostColorYellowBelow))
	}

	// Model context limits must be positive.
	for model, limit := range cfg.Models {
		if limit < 1 {
			errs = append(errs, fmt.Sprintf("model %q context limit must be positive, got %d", model, limit))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation error: %s", strings.Join(errs, "; "))
	}
	return nil
}
