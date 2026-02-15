package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nixlim/cc-top/internal/alerts"
)

// severityIcons maps alert severity to display icons.
var severityIcons = map[string]string{
	"critical": "!!",
	"warning":  "!?",
}

// renderAlertsPanel renders the bottom alerts bar.
func (m Model) renderAlertsPanel(w, h int) string {
	contentW := w - 4
	if contentW < 10 {
		contentW = 10
	}

	activeAlerts := m.getActiveAlerts()

	if len(activeAlerts) == 0 {
		statusLine := statusBarStyle.Render(" Alerts: None ")
		return panelBorderStyle.
			Width(w - 2).
			Height(h - 2).
			Render(statusLine)
	}

	var lines []string

	// Show alerts, scrollable if many.
	visibleH := h - 2 // borders
	if visibleH < 1 {
		visibleH = 1
	}

	startIdx := m.alertScrollPos
	if startIdx > len(activeAlerts)-visibleH {
		startIdx = len(activeAlerts) - visibleH
	}
	if startIdx < 0 {
		startIdx = 0
	}

	endIdx := startIdx + visibleH
	if endIdx > len(activeAlerts) {
		endIdx = len(activeAlerts)
	}

	for i := startIdx; i < endIdx; i++ {
		a := activeAlerts[i]
		line := renderAlertLine(a, contentW, m.selectedSession)
		lines = append(lines, line)
	}

	// If there are more alerts than visible, show a count.
	if len(activeAlerts) > visibleH {
		countLine := dimStyle.Render(fmt.Sprintf(" [%d alerts total]", len(activeAlerts)))
		lines = append(lines, countLine)
	}

	content := strings.Join(lines, "\n")
	return panelBorderStyle.
		Width(w - 2).
		Height(h - 2).
		BorderForeground(lipgloss.Color("196")).
		Render(content)
}

// getActiveAlerts retrieves alerts from the provider.
func (m Model) getActiveAlerts() []alerts.Alert {
	if m.alerts == nil {
		return nil
	}
	if m.selectedSession != "" {
		return m.alerts.ActiveForSession(m.selectedSession)
	}
	return m.alerts.Active()
}

// renderAlertLine formats a single alert for display in the bottom bar.
func renderAlertLine(a alerts.Alert, maxW int, selectedSession string) string {
	icon := severityIcons[a.Severity]
	if icon == "" {
		icon = "!?"
	}

	var style lipgloss.Style
	switch a.Severity {
	case "critical":
		style = alertCriticalStyle
	default:
		style = alertWarningStyle
	}

	// Highlight alerts for the selected session.
	sessionTag := ""
	if a.SessionID != "" {
		sessionTag = "[" + truncateID(a.SessionID, 8) + "] "
		if a.SessionID == selectedSession {
			style = style.Bold(true).Underline(true)
		}
	}

	msg := icon + " " + sessionTag + a.Rule + ": " + a.Message

	if len(msg) > maxW && maxW > 3 {
		msg = msg[:maxW-3] + "..."
	}

	return style.Render(msg)
}
