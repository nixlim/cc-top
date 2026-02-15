package tui

import (
	"fmt"
	"strings"

	"github.com/nixlim/cc-top/internal/scanner"
)

// renderStartup renders the startup screen showing discovered processes
// and action keys.
func (m Model) renderStartup() string {
	var sb strings.Builder

	// Title bar.
	titleLine := headerStyle.Width(m.width).Render(
		" cc-top -- Scanning for Claude Code instances...")
	sb.WriteString(titleLine)
	sb.WriteByte('\n')

	processes := m.getProcesses()

	if len(processes) == 0 {
		sb.WriteByte('\n')
		sb.WriteString(dimStyle.Render("  No Claude Code instances found."))
		sb.WriteByte('\n')
		sb.WriteString(dimStyle.Render("  Start a Claude Code session and press [R] to rescan."))
		sb.WriteByte('\n')
	} else {
		// Process table.
		sb.WriteByte('\n')

		// Table header.
		header := fmt.Sprintf("  %-6s %-10s %-20s %-12s %-12s %-12s",
			"PID", "Terminal", "CWD", "Telemetry", "OTLP Dest", "Status")
		sb.WriteString(dimStyle.Render(header))
		sb.WriteByte('\n')
		sb.WriteString(dimStyle.Render("  " + strings.Repeat("─", min(m.width-4, 80))))
		sb.WriteByte('\n')

		var connected, misconfigured, noTelemetry int

		for _, p := range processes {
			statusInfo := m.getProcessStatus(p)
			row := formatProcessRow(p, statusInfo)
			sb.WriteString(row)
			sb.WriteByte('\n')

			// Count by status.
			switch statusInfo.Status {
			case scanner.TelemetryConnected, scanner.TelemetryWaiting:
				connected++
			case scanner.TelemetryWrongPort, scanner.TelemetryConsoleOnly:
				misconfigured++
			case scanner.TelemetryOff:
				noTelemetry++
			}
		}

		sb.WriteByte('\n')

		// Summary line.
		summary := fmt.Sprintf("  %d connected . %d misconfigured . %d no telemetry",
			connected, misconfigured, noTelemetry)
		sb.WriteString(dimStyle.Render(summary))
		sb.WriteByte('\n')
	}

	sb.WriteByte('\n')

	// Action keys.
	sb.WriteString("  [E] Enable telemetry for all  [F] Fix misconfigured  [Enter] Continue  [R] Rescan")
	sb.WriteByte('\n')

	// Status message.
	if m.startupMessage != "" {
		sb.WriteByte('\n')
		sb.WriteString("  " + m.startupMessage)
		sb.WriteByte('\n')
	}

	return sb.String()
}

// getProcesses returns the current process list from the scanner.
func (m Model) getProcesses() []scanner.ProcessInfo {
	if m.scanner == nil {
		return nil
	}
	return m.scanner.Processes()
}

// getProcessStatus returns the telemetry status for a process.
func (m Model) getProcessStatus(p scanner.ProcessInfo) scanner.StatusInfo {
	if m.scanner == nil {
		return scanner.StatusInfo{
			Status: scanner.TelemetryUnknown,
			Icon:   "??",
			Label:  "Unknown",
		}
	}
	return m.scanner.GetTelemetryStatus(p)
}

// formatProcessRow formats a single process for the startup screen table.
func formatProcessRow(p scanner.ProcessInfo, status scanner.StatusInfo) string {
	cwd := truncateCWD(p.CWD, 20)
	terminal := truncateStr(p.Terminal, 10)
	if terminal == "" {
		terminal = "(headless)"
	}

	telIcon := formatTelemetryIcon(status.Status)
	otlpDest := formatOTLPDest(p)
	statusLabel := status.Label

	var style = dimStyle
	switch status.Status {
	case scanner.TelemetryConnected, scanner.TelemetryWaiting:
		style = activeStyle
	case scanner.TelemetryWrongPort, scanner.TelemetryConsoleOnly:
		style = idleStyle
	case scanner.TelemetryOff:
		style = dimStyle
	}

	row := fmt.Sprintf("  %-6d %-10s %-20s %-12s %-12s %-12s",
		p.PID, terminal, cwd, telIcon, otlpDest, statusLabel)

	return style.Render(row)
}

// formatTelemetryIcon returns the text icon for a telemetry status.
func formatTelemetryIcon(status scanner.TelemetryStatus) string {
	switch status {
	case scanner.TelemetryConnected, scanner.TelemetryWaiting:
		return "OK ON"
	case scanner.TelemetryWrongPort:
		return "!! ON"
	case scanner.TelemetryConsoleOnly:
		return "!! ON"
	case scanner.TelemetryOff:
		return "NO OFF"
	default:
		return "?? ???"
	}
}

// formatOTLPDest returns the OTLP destination display string.
func formatOTLPDest(p scanner.ProcessInfo) string {
	endpoint, ok := p.EnvVars["OTEL_EXPORTER_OTLP_ENDPOINT"]
	if !ok || endpoint == "" {
		return "--"
	}
	// Show just the port portion.
	if strings.Contains(endpoint, ":") {
		parts := strings.Split(endpoint, ":")
		port := parts[len(parts)-1]
		return ":" + port
	}
	return endpoint
}
