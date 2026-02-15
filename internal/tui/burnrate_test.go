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

func TestRenderCostDisplay(t *testing.T) {
	tests := []struct {
		name       string
		cost       string
		availH     int
		availW     int
		wantRows   int
		wantDollar bool
	}{
		{"large_font", "42.50", 10, 80, 5, true},
		{"medium_font", "42.50", 3, 80, 3, true},
		{"plain_fallback_short", "42.50", 2, 80, 1, true},
		{"plain_fallback_narrow", "42.50", 10, 10, 1, true},
		{"large_with_big_number", "1234.56", 10, 80, 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderCostDisplay(tt.cost, tt.availH, tt.availW, costGreenStyle)
			if result == "" {
				t.Error("renderCostDisplay returned empty string")
			}
			lines := strings.Split(result, "\n")
			if len(lines) != tt.wantRows {
				t.Errorf("got %d rows, want %d", len(lines), tt.wantRows)
			}
			// All sizes should contain a "$" somewhere.
			if tt.wantDollar && !strings.Contains(stripAnsi(result), "$") {
				t.Errorf("result should contain $, got %q", result)
			}
		})
	}
}

func TestRenderCostDisplay_DynamicScaling(t *testing.T) {
	// Verify that increasing available height produces more rows (larger font).
	small := renderCostDisplay("1.23", 2, 80, costGreenStyle)
	medium := renderCostDisplay("1.23", 3, 80, costGreenStyle)
	large := renderCostDisplay("1.23", 5, 80, costGreenStyle)

	smallRows := len(strings.Split(small, "\n"))
	medRows := len(strings.Split(medium, "\n"))
	largeRows := len(strings.Split(large, "\n"))

	if smallRows >= medRows {
		t.Errorf("small (%d rows) should be shorter than medium (%d rows)", smallRows, medRows)
	}
	if medRows >= largeRows {
		t.Errorf("medium (%d rows) should be shorter than large (%d rows)", medRows, largeRows)
	}
}

func TestShortModel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"claude-opus-4-6", "opus-4-6"},
		{"claude-sonnet-4-5-20250929", "sonnet-4-5"},
		{"claude-haiku-4-5-20251001", "haiku-4-5"},
		{"unknown", "unknown"},
		{"", ""},
		{"claude-", "claude-"},
		{"custom-model", "custom-model"},
	}

	for _, tt := range tests {
		got := shortModel(tt.input)
		if got != tt.want {
			t.Errorf("shortModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenderBurnRatePanel_WithProjections(t *testing.T) {
	cfg := config.DefaultConfig()
	mockBR := &mockBurnRateProvider{
		global: burnrate.BurnRate{
			TotalCost:         10.00,
			HourlyRate:        1.00,
			Trend:             burnrate.TrendFlat,
			TokenVelocity:     5000,
			DailyProjection:   24.00,
			MonthlyProjection: 720.00,
		},
	}

	m := NewModel(cfg, WithBurnRateProvider(mockBR))
	m.width = 120
	m.height = 40
	m.cachedBurnRate = m.computeBurnRate()

	panel := m.renderBurnRatePanel(60, 14)
	stripped := stripAnsi(panel)

	if !strings.Contains(stripped, "Day $24.00") {
		t.Error("panel should contain daily projection")
	}
	if !strings.Contains(stripped, "Mon $720.00") {
		t.Error("panel should contain monthly projection")
	}
}

func TestRenderBurnRatePanel_WithPerModel(t *testing.T) {
	cfg := config.DefaultConfig()
	mockBR := &mockBurnRateProvider{
		global: burnrate.BurnRate{
			TotalCost:     20.00,
			HourlyRate:    4.00,
			Trend:         burnrate.TrendUp,
			TokenVelocity: 10000,
			PerModel: []burnrate.ModelBurnRate{
				{Model: "claude-opus-4-6", HourlyRate: 3.00, TotalCost: 15.00},
				{Model: "claude-sonnet-4-5-20250929", HourlyRate: 1.00, TotalCost: 5.00},
			},
			DailyProjection:   96.00,
			MonthlyProjection: 2880.00,
		},
	}

	m := NewModel(cfg, WithBurnRateProvider(mockBR))
	m.width = 120
	m.height = 40
	m.cachedBurnRate = m.computeBurnRate()

	panel := m.renderBurnRatePanel(60, 16)
	stripped := stripAnsi(panel)

	if !strings.Contains(stripped, "opus-4-6") {
		t.Error("panel should contain shortened opus model name")
	}
	if !strings.Contains(stripped, "sonnet-4-5") {
		t.Error("panel should contain shortened sonnet model name")
	}
}

func TestRenderBurnRatePanel_SingleModelNoBreakdown(t *testing.T) {
	cfg := config.DefaultConfig()
	mockBR := &mockBurnRateProvider{
		global: burnrate.BurnRate{
			TotalCost:  5.00,
			HourlyRate: 1.00,
			PerModel: []burnrate.ModelBurnRate{
				{Model: "claude-opus-4-6", HourlyRate: 1.00, TotalCost: 5.00},
			},
		},
	}

	m := NewModel(cfg, WithBurnRateProvider(mockBR))
	m.width = 120
	m.height = 40
	m.cachedBurnRate = m.computeBurnRate()

	panel := m.renderBurnRatePanel(60, 14)
	stripped := stripAnsi(panel)

	// Single model should NOT show per-model breakdown.
	if strings.Contains(stripped, "opus-4-6") {
		t.Error("single model should not show per-model breakdown")
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

	// Global view: computeBurnRate populates the cache, getBurnRate reads it.
	m.cachedBurnRate = m.computeBurnRate()
	br := m.getBurnRate()
	if br.TotalCost != 10.00 {
		t.Errorf("global burn rate TotalCost = %.2f, want 10.00", br.TotalCost)
	}

	// Session-specific view.
	m.selectedSession = "sess-001"
	m.cachedBurnRate = m.computeBurnRate()
	br = m.getBurnRate()
	if br.TotalCost != 5.00 {
		t.Errorf("session burn rate TotalCost = %.2f, want 5.00", br.TotalCost)
	}
}
