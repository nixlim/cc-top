package config

// DefaultConfig returns a Config with all default values.
// These defaults allow cc-top to work out of the box with zero configuration.
func DefaultConfig() Config {
	return Config{
		Receiver: ReceiverConfig{
			GRPCPort: 4317,
			HTTPPort: 4318,
			Bind:     "127.0.0.1",
		},
		Scanner: ScannerConfig{
			IntervalSeconds: 5,
		},
		Alerts: AlertsConfig{
			CostSurgeThresholdPerHour:    2.00,
			SessionCostThreshold:         5.00,
			RunawayTokenVelocity:         50000,
			RunawayTokenSustainedMinutes: 2,
			LoopDetectorThreshold:        3,
			LoopDetectorWindowMinutes:    5,
			ErrorStormCount:              10,
			StaleSessionHours:            2,
			ContextPressurePercent:       80,
			HighRejectionPercent:         50,
			HighRejectionWindowMinutes:   5,
			Notifications: NotificationConfig{
				SystemNotify: true,
			},
		},
		Display: DisplayConfig{
			EventBufferSize:      1000,
			RefreshRateMS:        500,
			CostColorGreenBelow:  0.50,
			CostColorYellowBelow: 2.00,
		},
		Models: defaultModelContextLimits(),
		Pricing: map[string][4]float64{
			"claude-sonnet-4-5-20250929": {3.00, 15.00, 0.30, 3.75},
			"claude-opus-4-6":            {5.00, 25.00, 0.50, 6.25},
			"claude-haiku-4-5-20251001":  {1.00, 5.00, 0.10, 1.25},
		},
	}
}

// defaultModelContextLimits returns the built-in model context token limits.
func defaultModelContextLimits() map[string]int {
	return map[string]int{
		"claude-sonnet-4-5-20250929": 200000,
		"claude-opus-4-6":            200000,
		"claude-haiku-4-5-20251001":  200000,
	}
}
