package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/nixlim/cc-top/internal/state"
)

// renderSessionListPanel renders the session list panel with columns for
// PID, Session ID, Terminal, CWD, Telemetry, Model, Status, Cost, Tokens, Active Time.
func (m Model) renderSessionListPanel(w, h int) string {
	sessions := m.getSessions()

	contentW := w - 4
	if contentW < 16 {
		contentW = 16
	}

	contentH := h - 4 // borders + title
	if contentH < 2 {
		contentH = 2
	}

	var lines []string

	// Title.
	title := panelTitleStyle.Render("Sessions")
	if m.selectedSession != "" {
		title += dimStyle.Render(" [" + truncateID(m.selectedSession, 8) + "]")
	} else {
		title += dimStyle.Render(" [Global]")
	}
	lines = append(lines, title)

	if len(sessions) == 0 {
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("No sessions found"))
		content := strings.Join(lines, "\n")
		return panelBorderStyle.
			Width(w - 2).
			Height(h - 2).
			Render(content)
	}

	// Build header row.
	header := formatSessionHeader(contentW)
	lines = append(lines, dimStyle.Render(header))
	lines = append(lines, dimStyle.Render(strings.Repeat("─", min(contentW, len(header)))))

	// Sort sessions: telemetry-enabled first, then non-telemetry greyed out.
	telemetrySessions, noTelemetrySessions := splitSessionsByTelemetry(sessions)

	// Limit done/exited sessions to most recent 5 per group.
	const maxDone = 5
	telemetrySessions, telHidden := filterDoneSessions(telemetrySessions, maxDone)
	noTelemetrySessions, noTelHidden := filterDoneSessions(noTelemetrySessions, maxDone)

	rowIdx := 0
	// Render telemetry-enabled sessions.
	for _, s := range telemetrySessions {
		line := formatSessionRow(&s, contentW)
		if rowIdx == m.sessionCursor {
			line = selectedStyle.Render(line)
		} else if s.IsNew {
			line = newBadgeStyle.Render("NEW ") + line
		}
		lines = append(lines, line)
		rowIdx++
	}

	if telHidden > 0 {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("── +%d done sessions hidden ──", telHidden)))
	}

	// Render non-telemetry sessions greyed out at bottom.
	if len(noTelemetrySessions) > 0 {
		lines = append(lines, dimStyle.Render("── no telemetry ──"))
		for _, s := range noTelemetrySessions {
			line := formatSessionRow(&s, contentW)
			if rowIdx == m.sessionCursor {
				line = selectedStyle.Render(line)
			} else {
				line = dimStyle.Render(line)
			}
			lines = append(lines, line)
			rowIdx++
		}
		if noTelHidden > 0 {
			lines = append(lines, dimStyle.Render(fmt.Sprintf("── +%d done sessions hidden ──", noTelHidden)))
		}
	}

	// Scroll viewport: keep header lines fixed, scroll data lines.
	headerCount := 3 // title + column header + separator
	if len(lines) > headerCount {
		dataLines := lines[headerCount:]
		visibleRows := contentH - headerCount
		if visibleRows > 0 && len(dataLines) > visibleRows {
			offset := m.sessionScrollOffset
			if offset < 0 {
				offset = 0
			}
			maxOffset := len(dataLines) - visibleRows
			if offset > maxOffset {
				offset = maxOffset
			}
			end := offset + visibleRows
			if end > len(dataLines) {
				end = len(dataLines)
			}
			lines = append(lines[:headerCount], dataLines[offset:end]...)
		}
	} else if len(lines) > contentH {
		lines = lines[:contentH]
	}

	content := strings.Join(lines, "\n")
	return renderBorderedPanel(content, w, h)
}

// formatSessionHeader returns the column header string.
func formatSessionHeader(maxW int) string {
	if maxW >= 90 {
		return fmt.Sprintf("%-8s %-9s %-8s %-15s %-6s %-8s %-5s %-8s %-6s",
			"Session", "Started", "Term", "CWD", "Model", "Status", "Cost", "Tokens", "Time")
	}
	if maxW >= 60 {
		return fmt.Sprintf("%-8s %-9s %-8s %-12s %-6s %-5s",
			"Session", "Started", "Term", "CWD", "Status", "Cost")
	}
	return fmt.Sprintf("%-8s %-9s %-6s %-5s",
		"Session", "Started", "Status", "Cost")
}

// formatSessionRow formats a single session row based on available width.
func formatSessionRow(s *state.SessionData, maxW int) string {
	sessionID := truncateID(s.SessionID, 8)
	started := formatStartedAt(s.StartedAt)
	terminal := truncateStr(s.Terminal, 8)
	cwd := truncateCWD(s.CWD, 15)
	model := truncateStr(s.Model, 6)
	statusStr := renderStatus(s.Status())
	cost := fmt.Sprintf("$%.2f", s.TotalCost)
	tokens := formatNumber(s.TotalTokens)
	activeTime := formatDuration(s.ActiveTime)

	if maxW >= 90 {
		return fmt.Sprintf("%-8s %-9s %-8s %-15s %-6s %-8s %5s %8s %6s",
			sessionID, started, terminal, cwd, model, statusStr, cost, tokens, activeTime)
	}
	if maxW >= 60 {
		return fmt.Sprintf("%-8s %-9s %-8s %-12s %-6s %5s",
			sessionID, started, terminal, truncateCWD(s.CWD, 12), statusStr, cost)
	}
	return fmt.Sprintf("%-8s %-9s %-6s %5s",
		sessionID, started, statusStr, cost)
}

// formatStartedAt formats a timestamp as DDMMHHMM (day, month, hour, minute).
func formatStartedAt(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("0201 1504")
}

// renderStatus returns a styled string for the session status.
func renderStatus(s state.SessionStatus) string {
	switch s {
	case state.StatusActive:
		return activeStyle.Render("active")
	case state.StatusIdle:
		return idleStyle.Render("idle")
	case state.StatusDone:
		return doneStyle.Render("done")
	case state.StatusExited:
		return exitedStyle.Render("exited")
	default:
		return string(s)
	}
}

// truncateCWD shortens a path by replacing the home directory with ~
// and using ellipsis for long paths.
func truncateCWD(cwd string, maxLen int) string {
	if cwd == "" {
		return "—"
	}

	// Replace home directory with ~.
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(cwd, home) {
		cwd = "~" + cwd[len(home):]
	}

	if len(cwd) <= maxLen {
		return cwd
	}

	if maxLen <= 4 {
		return cwd[:maxLen]
	}

	// Keep the first part and last part with ... in the middle.
	dir := filepath.Dir(cwd)
	base := filepath.Base(cwd)

	if len(base) >= maxLen-3 {
		return "..." + base[len(base)-(maxLen-3):]
	}

	available := maxLen - len(base) - 4 // for .../
	if available <= 0 {
		return "..." + string(filepath.Separator) + base
	}

	return dir[:available] + "..." + string(filepath.Separator) + base
}

// truncateStr truncates a string to maxLen characters.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "."
}

// formatDuration formats a duration into a human-readable short form.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// splitSessionsByTelemetry separates sessions into those with telemetry data
// (have events or metrics) and those without.
func splitSessionsByTelemetry(sessions []state.SessionData) (withTelemetry, withoutTelemetry []state.SessionData) {
	for _, s := range sessions {
		if hasTelemetry(&s) {
			withTelemetry = append(withTelemetry, s)
		} else {
			withoutTelemetry = append(withoutTelemetry, s)
		}
	}
	return
}

// hasTelemetry returns true if a session has received any telemetry data.
func hasTelemetry(s *state.SessionData) bool {
	return len(s.Metrics) > 0 || len(s.Events) > 0 || !s.LastEventAt.IsZero()
}

// filterDoneSessions keeps all active/idle sessions and limits done/exited
// sessions to the most recent maxDone (by LastEventAt). Returns the filtered
// list preserving original order and the count of hidden done sessions.
func filterDoneSessions(sessions []state.SessionData, maxDone int) ([]state.SessionData, int) {
	var active, done []state.SessionData
	for _, s := range sessions {
		status := s.Status()
		if status == state.StatusDone || status == state.StatusExited {
			done = append(done, s)
		} else {
			active = append(active, s)
		}
	}

	if len(done) <= maxDone {
		return sessions, 0
	}

	// Sort done sessions by LastEventAt descending to keep most recent.
	sort.Slice(done, func(i, j int) bool {
		return done[i].LastEventAt.After(done[j].LastEventAt)
	})
	hiddenCount := len(done) - maxDone
	keptDone := done[:maxDone]

	// Build a set of kept done session IDs for fast lookup.
	kept := make(map[string]bool, maxDone)
	for _, s := range keptDone {
		kept[s.SessionID] = true
	}

	// Rebuild the list preserving original order.
	var result []state.SessionData
	for _, s := range sessions {
		status := s.Status()
		if status == state.StatusDone || status == state.StatusExited {
			if kept[s.SessionID] {
				result = append(result, s)
			}
		} else {
			result = append(result, s)
		}
	}

	return result, hiddenCount
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TelemetryIcon returns the appropriate icon string for a session.
func TelemetryIcon(s *state.SessionData) string {
	if hasTelemetry(s) {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render("OK")
	}
	if s.PID > 0 && !s.Exited {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("NO")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("??")
}
