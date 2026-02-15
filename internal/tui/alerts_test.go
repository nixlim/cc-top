package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/alerts"
	"github.com/nixlim/cc-top/internal/config"
)

func TestRenderAlertsPanel_NoAlerts(t *testing.T) {
	cfg := config.DefaultConfig()
	mockAlerts := &mockAlertProvider{alerts: nil}

	m := NewModel(cfg, WithAlertProvider(mockAlerts), WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	panel := m.renderAlertsPanel(120, 3)
	if !strings.Contains(panel, "None") {
		t.Error("alerts panel with no alerts should show 'None'")
	}
}

func TestRenderAlertsPanel_WithAlerts(t *testing.T) {
	cfg := config.DefaultConfig()
	mockAlerts := &mockAlertProvider{
		alerts: []alerts.Alert{
			{
				Rule:      "CostSurge",
				Severity:  "warning",
				Message:   "Cost rate exceeds $2/hr",
				SessionID: "sess-001",
				FiredAt:   time.Now(),
			},
			{
				Rule:      "LoopDetector",
				Severity:  "critical",
				Message:   "npm test failing repeatedly",
				SessionID: "sess-002",
				FiredAt:   time.Now(),
			},
		},
	}

	m := NewModel(cfg, WithAlertProvider(mockAlerts), WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	panel := m.renderAlertsPanel(120, 5)
	if panel == "" {
		t.Error("alerts panel returned empty string")
	}
}

func TestRenderAlertsPanel_SessionSpecific(t *testing.T) {
	cfg := config.DefaultConfig()
	mockAlerts := &mockAlertProvider{
		alerts: []alerts.Alert{
			{Rule: "CostSurge", Severity: "warning", Message: "cost", SessionID: "sess-001"},
			{Rule: "ErrorStorm", Severity: "critical", Message: "errors", SessionID: "sess-002"},
			{Rule: "Global", Severity: "warning", Message: "global alert", SessionID: ""},
		},
	}

	m := NewModel(cfg, WithAlertProvider(mockAlerts), WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40
	m.selectedSession = "sess-001"

	activeAlerts := m.getActiveAlerts()
	// Should only return sess-001 and global alerts.
	if len(activeAlerts) != 2 {
		t.Errorf("session-specific alerts count = %d, want 2", len(activeAlerts))
	}
}

func TestRenderAlertLine(t *testing.T) {
	tests := []struct {
		name     string
		alert    alerts.Alert
		selected string
	}{
		{
			name: "warning alert",
			alert: alerts.Alert{
				Rule:      "CostSurge",
				Severity:  "warning",
				Message:   "Cost rate exceeds threshold",
				SessionID: "sess-001",
			},
			selected: "",
		},
		{
			name: "critical alert",
			alert: alerts.Alert{
				Rule:      "LoopDetector",
				Severity:  "critical",
				Message:   "Command loop detected",
				SessionID: "sess-002",
			},
			selected: "",
		},
		{
			name: "highlighted when selected",
			alert: alerts.Alert{
				Rule:      "CostSurge",
				Severity:  "warning",
				Message:   "Cost surge",
				SessionID: "sess-001",
			},
			selected: "sess-001",
		},
		{
			name: "global alert",
			alert: alerts.Alert{
				Rule:     "ErrorStorm",
				Severity: "critical",
				Message:  "Many errors",
			},
			selected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := renderAlertLine(tt.alert, 80, tt.selected)
			if line == "" {
				t.Error("renderAlertLine returned empty string")
			}
		})
	}
}

func TestRenderAlertsPanel_NilProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	// Should not panic.
	panel := m.renderAlertsPanel(120, 3)
	if !strings.Contains(panel, "None") {
		t.Error("alerts panel with nil provider should show 'None'")
	}
}
