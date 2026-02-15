package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// panelDimensions computes the width and height for each panel based on
// the total terminal size. The layout places:
//   - Session List on the left (40% width)
//   - Burn Rate top right (60% width, 8 rows)
//   - Event Stream center right (60% width, remaining rows minus alert bar)
//   - Alerts bar at the bottom (full width, 3 rows)
//
// At small terminal sizes (< 80 cols), panels shrink to fit.
type panelDimensions struct {
	sessionListW, sessionListH int
	burnRateW, burnRateH       int
	eventStreamW, eventStreamH int
	alertsW, alertsH           int
	headerH                    int
}

const (
	minWidth  = 40
	minHeight = 10

	// headerHeight is the height of the top status bar.
	headerHeight = 1

	// alertsHeight is the height of the bottom alerts bar.
	alertsHeight = 3

	// burnRateHeight is the fixed height of the burn rate panel.
	burnRateMinHeight = 8
)

// computeDimensions calculates panel sizes from terminal dimensions.
func computeDimensions(totalW, totalH int) panelDimensions {
	if totalW < minWidth {
		totalW = minWidth
	}
	if totalH < minHeight {
		totalH = minHeight
	}

	d := panelDimensions{
		headerH: headerHeight,
	}

	// Usable area after header and alerts bar.
	usableH := totalH - headerHeight - alertsHeight
	if usableH < 4 {
		usableH = 4
	}

	// Session list takes 40% width (minimum 20 columns).
	d.sessionListW = totalW * 40 / 100
	if d.sessionListW < 20 {
		d.sessionListW = 20
	}
	if d.sessionListW > totalW-20 {
		d.sessionListW = totalW - 20
	}
	d.sessionListH = usableH

	// Right side takes remaining width.
	rightW := totalW - d.sessionListW
	if rightW < 20 {
		rightW = 20
	}

	// Burn rate panel: fixed height at top right.
	d.burnRateW = rightW
	d.burnRateH = burnRateMinHeight
	if d.burnRateH > usableH/2 {
		d.burnRateH = usableH / 2
	}

	// Event stream: remaining right-side height.
	d.eventStreamW = rightW
	d.eventStreamH = usableH - d.burnRateH
	if d.eventStreamH < 3 {
		d.eventStreamH = 3
	}

	// Alerts bar: full width.
	d.alertsW = totalW
	d.alertsH = alertsHeight

	return d
}

// Style definitions for the TUI panels.
var (
	// headerStyle is used for the top status bar.
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62"))

	// panelBorderStyle wraps a panel with a border.
	panelBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240"))

	// panelTitleStyle is used for panel titles.
	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("69"))

	// selectedStyle highlights the selected session.
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62"))

	// dimStyle is used for greyed-out items.
	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	// activeStyle is used for active session status.
	activeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82"))

	// idleStyle is used for idle session status.
	idleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("226"))

	// doneStyle is used for done/exited session status.
	doneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	// exitedStyle is used for exited session status.
	exitedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	// costGreenStyle is used for low burn rate.
	costGreenStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("82"))

	// costYellowStyle is used for medium burn rate.
	costYellowStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("226"))

	// costRedStyle is used for high burn rate.
	costRedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196"))

	// alertWarningStyle is used for warning alerts.
	alertWarningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("226"))

	// alertCriticalStyle is used for critical alerts.
	alertCriticalStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("196"))

	// filterMenuStyle wraps the filter overlay.
	filterMenuStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 2)

	// killDialogStyle wraps the kill confirmation dialog.
	killDialogStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(1, 3).
			Bold(true)

	// statusBarStyle is used for the bottom status line.
	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	// newBadgeStyle is used for the "New" badge on recently discovered sessions.
	newBadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82")).
			Bold(true)
)

// renderDashboard composes the main dashboard view from four panels.
func (m Model) renderDashboard() string {
	dims := computeDimensions(m.width, m.height)

	// Build header.
	header := m.renderHeader(dims)

	// Build the four panels.
	sessionList := m.renderSessionListPanel(dims.sessionListW, dims.sessionListH)
	burnRatePanel := m.renderBurnRatePanel(dims.burnRateW, dims.burnRateH)
	eventStream := m.renderEventStreamPanel(dims.eventStreamW, dims.eventStreamH)
	alertsBar := m.renderAlertsPanel(dims.alertsW, dims.alertsH)

	// Right column: burn rate on top, event stream below.
	rightCol := lipgloss.JoinVertical(lipgloss.Left, burnRatePanel, eventStream)

	// Main content: session list left, right column right.
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, sessionList, rightCol)

	// Compose full layout: header, main content, alerts.
	layout := lipgloss.JoinVertical(lipgloss.Left, header, mainContent, alertsBar)

	// Overlay kill dialog if active.
	if m.killConfirm {
		layout = m.overlayKillDialog(layout)
	}

	// Overlay filter menu if active.
	if m.filterMenu.Active {
		layout = m.overlayFilterMenu(layout)
	}

	return layout
}

// renderHeader renders the top status bar.
func (m Model) renderHeader(dims panelDimensions) string {
	title := " cc-top"
	viewLabel := " [Dashboard]"
	if m.selectedSession != "" {
		viewLabel += " Session: " + truncateID(m.selectedSession, 8)
	} else {
		viewLabel += " Global"
	}

	help := "Tab:Stats  q:Quit  f:Filter  Ctrl+K:Kill "

	// Pad to fill width.
	padding := m.width - lipgloss.Width(title) - lipgloss.Width(viewLabel) - lipgloss.Width(help)
	if padding < 0 {
		padding = 0
	}
	spaces := ""
	for range padding {
		spaces += " "
	}

	return headerStyle.Width(m.width).Render(title + viewLabel + spaces + help)
}

// truncateID returns a truncated identifier for display.
func truncateID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen]
}

// overlayKillDialog renders the kill confirmation over the layout.
func (m Model) overlayKillDialog(base string) string {
	dialog := killDialogStyle.Render(
		"Kill session?\n\n" +
			m.killTargetInfo + "\n\n" +
			"[Y] Kill  [n/Esc] Cancel (resume)")

	// Center the dialog.
	dialogW := lipgloss.Width(dialog)
	dialogH := lipgloss.Height(dialog)
	x := (m.width - dialogW) / 2
	y := (m.height - dialogH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	return placeOverlay(x, y, dialog, base)
}

// overlayFilterMenu renders the filter menu over the layout.
func (m Model) overlayFilterMenu(base string) string {
	content := panelTitleStyle.Render("Event Filter") + "\n\n"
	for i, opt := range m.filterMenu.Options {
		cursor := "  "
		if i == m.filterMenu.Cursor {
			cursor = "> "
		}
		check := "[ ]"
		if opt.Enabled {
			check = "[x]"
		}
		line := cursor + check + " " + opt.Label
		if i == m.filterMenu.Cursor {
			line = selectedStyle.Render(line)
		}
		content += line + "\n"
	}
	content += "\nEnter: Toggle  Esc: Close"

	dialog := filterMenuStyle.Render(content)
	dialogW := lipgloss.Width(dialog)
	dialogH := lipgloss.Height(dialog)
	x := (m.width - dialogW) / 2
	y := (m.height - dialogH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	return placeOverlay(x, y, dialog, base)
}

// placeOverlay places fg on top of bg at position (x, y).
// This is a simplified overlay that replaces characters in the base string.
func placeOverlay(x, y int, fg, bg string) string {
	return lipgloss.Place(
		lipgloss.Width(bg),
		lipgloss.Height(bg),
		lipgloss.Center,
		lipgloss.Center,
		fg,
		lipgloss.WithWhitespaceChars(" "),
	)
}
