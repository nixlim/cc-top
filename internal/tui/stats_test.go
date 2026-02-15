package tui

import (
	"strings"
	"testing"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/stats"
)

func TestRenderStats_Empty(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewStats))
	m.width = 120
	m.height = 40

	view := m.renderStats()
	if !strings.Contains(view, "Stats") {
		t.Error("stats view should contain 'Stats'")
	}
	if !strings.Contains(view, "Code Metrics") {
		t.Error("stats view should contain 'Code Metrics'")
	}
}

func TestRenderStats_WithData(t *testing.T) {
	cfg := config.DefaultConfig()
	mockStats := &mockStatsProvider{
		global: stats.DashboardStats{
			LinesAdded:   500,
			LinesRemoved: 200,
			Commits:      12,
			PRs:          3,
			ToolAcceptance: map[string]float64{
				"Edit":  0.85,
				"Write": 0.92,
			},
			CacheEfficiency: 0.75,
			AvgAPILatency:   2.5,
			ModelBreakdown: []stats.ModelStats{
				{Model: "sonnet-4.5", TotalCost: 5.00, TotalTokens: 100000},
				{Model: "haiku-4.5", TotalCost: 0.50, TotalTokens: 50000},
			},
			TopTools: []stats.ToolUsage{
				{ToolName: "Bash", Count: 50},
				{ToolName: "Edit", Count: 30},
				{ToolName: "Write", Count: 20},
			},
			ErrorRate: 0.05,
		},
	}

	m := NewModel(cfg, WithStartView(ViewStats), WithStatsProvider(mockStats))
	m.width = 120
	m.height = 40

	view := m.renderStats()
	if !strings.Contains(view, "500") {
		t.Error("stats view should contain lines added count")
	}
	if !strings.Contains(view, "Code Metrics") {
		t.Error("stats view should contain 'Code Metrics'")
	}
	if !strings.Contains(view, "Tool Acceptance") {
		t.Error("stats view should contain 'Tool Acceptance'")
	}
	if !strings.Contains(view, "API Performance") {
		t.Error("stats view should contain 'API Performance'")
	}
	if !strings.Contains(view, "Model Breakdown") {
		t.Error("stats view should contain 'Model Breakdown'")
	}
	if !strings.Contains(view, "Top Tools") {
		t.Error("stats view should contain 'Top Tools'")
	}
}

func TestRenderStats_SessionSpecific(t *testing.T) {
	cfg := config.DefaultConfig()
	mockStats := &mockStatsProvider{
		global: stats.DashboardStats{LinesAdded: 500},
		perSess: map[string]stats.DashboardStats{
			"sess-001": {LinesAdded: 100},
		},
	}

	m := NewModel(cfg, WithStartView(ViewStats), WithStatsProvider(mockStats))
	m.width = 120
	m.height = 40
	m.selectedSession = "sess-001"

	ds := m.getStats()
	if ds.LinesAdded != 100 {
		t.Errorf("session stats LinesAdded = %d, want 100", ds.LinesAdded)
	}
}

func TestRenderProgressBar(t *testing.T) {
	tests := []struct {
		ratio float64
		width int
	}{
		{0.0, 20},
		{0.5, 20},
		{1.0, 20},
		{0.25, 10},
		{-0.1, 20}, // should clamp to 0
		{1.5, 20},  // should clamp to 1
	}

	for _, tt := range tests {
		bar := renderProgressBar(tt.ratio, tt.width)
		if bar == "" {
			t.Errorf("renderProgressBar(%.1f, %d) returned empty string", tt.ratio, tt.width)
		}
	}
}

func TestRenderStats_NilProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewStats))
	m.width = 120
	m.height = 40

	// Should not panic with nil provider.
	view := m.renderStats()
	if view == "" {
		t.Error("renderStats with nil provider returned empty string")
	}
}

func TestRenderCodeSection(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg)

	ds := stats.DashboardStats{
		LinesAdded:   1234,
		LinesRemoved: 567,
		Commits:      8,
		PRs:          2,
	}

	section := m.renderCodeSection(ds)
	if !strings.Contains(section, "1,234") {
		t.Error("code section should contain formatted lines added")
	}
	if !strings.Contains(section, "567") {
		t.Error("code section should contain lines removed")
	}
}

func TestRenderModelBreakdown_Empty(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg)

	ds := stats.DashboardStats{}
	section := m.renderModelBreakdown(ds)
	if !strings.Contains(section, "No model data") {
		t.Error("empty model breakdown should show 'No model data'")
	}
}

func TestRenderTokenBreakdown(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg)

	ds := stats.DashboardStats{
		TokenBreakdown: map[string]int64{
			"input":         50000,
			"output":        25000,
			"cacheRead":     100000,
			"cacheCreation": 5000,
		},
	}

	section := m.renderTokenBreakdownSection(ds)
	if !strings.Contains(section, "Token Breakdown") {
		t.Error("should contain 'Token Breakdown'")
	}
	if !strings.Contains(section, "50,000") {
		t.Error("should contain formatted input count")
	}
	if !strings.Contains(section, "100,000") {
		t.Error("should contain formatted cacheRead count")
	}
}

func TestRenderTokenBreakdown_Empty(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg)

	ds := stats.DashboardStats{}
	section := m.renderTokenBreakdownSection(ds)
	if !strings.Contains(section, "No token data") {
		t.Error("empty token breakdown should show 'No token data'")
	}
}

func TestRenderTopTools_Empty(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg)

	ds := stats.DashboardStats{}
	section := m.renderTopTools(ds)
	if !strings.Contains(section, "No tool data") {
		t.Error("empty top tools should show 'No tool data'")
	}
}
