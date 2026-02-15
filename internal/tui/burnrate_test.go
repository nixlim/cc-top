package tui

import (
	"strings"
	"testing"

	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/config"
)

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{1000, "1,000"},
		{1234, "1,234"},
		{12345, "12,345"},
		{123456, "123,456"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
	}

	for _, tt := range tests {
		got := formatNumber(tt.input)
		if got != tt.want {
			t.Errorf("formatNumber(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTrendArrow(t *testing.T) {
	tests := []struct {
		trend burnrate.TrendDirection
		want  string
	}{
		{burnrate.TrendUp, "^"},
		{burnrate.TrendDown, "v"},
		{burnrate.TrendFlat, "-"},
	}

	for _, tt := range tests {
		got := trendArrow(tt.trend)
		if got != tt.want {
			t.Errorf("trendArrow(%v) = %q, want %q", tt.trend, got, tt.want)
		}
	}
}

func TestGetRateColor(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg)

	tests := []struct {
		rate float64
		want burnrate.RateColor
	}{
		{0.0, burnrate.ColorGreen},
		{0.25, burnrate.ColorGreen},
		{0.49, burnrate.ColorGreen},
		{0.50, burnrate.ColorYellow},
		{1.00, burnrate.ColorYellow},
		{1.99, burnrate.ColorYellow},
		{2.00, burnrate.ColorRed},
		{5.00, burnrate.ColorRed},
		{100.0, burnrate.ColorRed},
	}

	for _, tt := range tests {
		got := m.getRateColor(tt.rate)
		if got != tt.want {
			t.Errorf("getRateColor(%.2f) = %v, want %v", tt.rate, got, tt.want)
		}
	}
}

func TestGetRateColor_CustomThresholds(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.CostColorGreenBelow = 1.00
	cfg.Display.CostColorYellowBelow = 5.00

	m := NewModel(cfg)

	tests := []struct {
		rate float64
		want burnrate.RateColor
	}{
		{0.50, burnrate.ColorGreen},
		{0.99, burnrate.ColorGreen},
		{1.00, burnrate.ColorYellow},
		{4.99, burnrate.ColorYellow},
		{5.00, burnrate.ColorRed},
	}

	for _, tt := range tests {
		got := m.getRateColor(tt.rate)
		if got != tt.want {
			t.Errorf("custom getRateColor(%.2f) = %v, want %v", tt.rate, got, tt.want)
		}
	}
}

func TestRenderBurnRatePanel(t *testing.T) {
	cfg := config.DefaultConfig()
	mockBR := &mockBurnRateProvider{
		global: burnrate.BurnRate{
			TotalCost:     42.50,
			HourlyRate:    3.00,
			Trend:         burnrate.TrendUp,
			TokenVelocity: 15000,
		},
	}

	m := NewModel(cfg, WithBurnRateProvider(mockBR))
	m.width = 120
	m.height = 40

	panel := m.renderBurnRatePanel(60, 10)
	if panel == "" {
		t.Error("renderBurnRatePanel returned empty string")
	}
	if !strings.Contains(panel, "Burn Rate") {
		t.Error("burn rate panel should contain title 'Burn Rate'")
	}
}

func TestRenderBurnRatePanel_NilProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg)
	m.width = 120
	m.height = 40

	// Should not panic with nil provider.
	panel := m.renderBurnRatePanel(60, 10)
	if panel == "" {
		t.Error("renderBurnRatePanel returned empty string with nil provider")
	}
	if !strings.Contains(panel, "$0.00") {
		t.Error("burn rate panel with nil provider should show $0.00")
	}
}

func TestRenderBigNumber(t *testing.T) {
	// Just verify it doesn't panic and produces output.
	result := renderBigNumber("$1.23", costGreenStyle)
	if result == "" {
		t.Error("renderBigNumber returned empty string")
	}
	// Should have 5 rows.
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Errorf("renderBigNumber produced %d lines, want 5", len(lines))
	}
}

func TestRenderBurnRatePanel_SessionAware(t *testing.T) {
	cfg := config.DefaultConfig()
	mockBR := &mockBurnRateProvider{
		global: burnrate.BurnRate{TotalCost: 10.00},
		perSess: map[string]burnrate.BurnRate{
			"sess-001": {TotalCost: 5.00, HourlyRate: 1.00},
		},
	}

	m := NewModel(cfg, WithBurnRateProvider(mockBR))
	m.width = 120
	m.height = 40

	// Global view.
	br := m.getBurnRate()
	if br.TotalCost != 10.00 {
		t.Errorf("global burn rate TotalCost = %.2f, want 10.00", br.TotalCost)
	}

	// Session-specific view.
	m.selectedSession = "sess-001"
	br = m.getBurnRate()
	if br.TotalCost != 5.00 {
		t.Errorf("session burn rate TotalCost = %.2f, want 5.00", br.TotalCost)
	}
}
