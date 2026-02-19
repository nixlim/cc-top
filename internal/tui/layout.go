package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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

	headerHeight = 1

	alertsHeight = 3

	burnRateMinHeight = 7

	burnRateMaxHeight = 10
)

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

	usableH := totalH - headerHeight - alertsHeight
	if usableH < 4 {
		usableH = 4
	}

	d.sessionListW = totalW * 40 / 100
	if d.sessionListW < 20 {
		d.sessionListW = 20
	}
	if d.sessionListW > totalW-20 {
		d.sessionListW = totalW - 20
	}
	d.sessionListH = usableH

	rightW := totalW - d.sessionListW
	if rightW < 20 {
		rightW = 20
	}

	d.burnRateW = rightW
	maxBR := usableH * 30 / 100
	if maxBR < burnRateMinHeight {
		maxBR = burnRateMinHeight
	}
	if maxBR > burnRateMaxHeight {
		maxBR = burnRateMaxHeight
	}
	d.burnRateH = maxBR
	if d.burnRateH > usableH/2 {
		d.burnRateH = usableH / 2
	}

	d.eventStreamW = rightW
	d.eventStreamH = usableH - d.burnRateH
	if d.eventStreamH < 3 {
		d.eventStreamH = 3
	}

	d.alertsW = totalW
	d.alertsH = alertsHeight

	return d
}

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62"))

	panelBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240"))

	panelTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("69"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	activeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82"))

	idleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("226"))

	doneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	exitedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	costGreenStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("82"))

	costYellowStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("226"))

	costRedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196"))

	alertWarningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("226"))

	alertCriticalStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("196"))

	filterMenuStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(1, 2)

	killDialogStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(1, 3).
			Bold(true)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	newBadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82")).
			Bold(true)

	focusBorderColor = lipgloss.Color("63")

	cursorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62"))

	detailOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("69")).
				Padding(1, 2)
)

func renderBorderedPanel(content string, w, h int) string {
	return renderBorderedPanelStyled(content, w, h, panelBorderStyle)
}

func renderBorderedPanelStyled(content string, w, h int, style lipgloss.Style) string {
	contentH := h - 2
	if contentH < 1 {
		contentH = 1
	}

	lines := strings.Split(content, "\n")
	if len(lines) > contentH {
		lines = lines[:contentH]
		content = strings.Join(lines, "\n")
	}

	return style.
		Width(w - 2).
		Height(contentH).
		Render(content)
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripAnsi(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func (m Model) renderDashboard() string {
	dims := computeDimensions(m.width, m.height)

	header := m.renderHeader(dims)

	sessionList := m.renderSessionListPanel(dims.sessionListW, dims.sessionListH)
	burnRatePanel := m.renderBurnRatePanel(dims.burnRateW, dims.burnRateH)
	eventStream := m.renderEventStreamPanel(dims.eventStreamW, dims.eventStreamH)
	alertsBar := m.renderAlertsPanel(dims.alertsW, dims.alertsH)

	rightCol := lipgloss.JoinVertical(lipgloss.Left, burnRatePanel, eventStream)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, sessionList, rightCol)

	usableH := m.height - dims.headerH - dims.alertsH
	if usableH < 4 {
		usableH = 4
	}
	mcLines := strings.Split(mainContent, "\n")
	if len(mcLines) > usableH {
		mcLines = mcLines[:usableH]
		mainContent = strings.Join(mcLines, "\n")
	}

	layout := lipgloss.JoinVertical(lipgloss.Left, header, mainContent, alertsBar)

	if m.killConfirm {
		layout = m.overlayKillDialog(layout)
	}

	if m.filterMenu.Active {
		layout = m.overlayFilterMenu(layout)
	}

	if m.detailOverlay {
		layout = m.overlayDetail(layout)
	}

	return layout
}

func (m Model) renderHeader(dims panelDimensions) string {
	title := " cc-top"
	viewLabel := " [Dashboard]"
	if m.selectedSession != "" {
		viewLabel += " Session: " + truncateID(m.selectedSession, 8)
	} else {
		viewLabel += " Global"
	}

	indicators := m.headerIndicators()
	help := m.headerHelp()

	padding := m.width - lipgloss.Width(title) - lipgloss.Width(viewLabel) - lipgloss.Width(indicators) - lipgloss.Width(help)
	if padding < 0 {
		padding = 0
	}
	spaces := ""
	for range padding {
		spaces += " "
	}

	return headerStyle.Width(m.width).Render(title + viewLabel + indicators + spaces + help)
}

func (m Model) headerHelp() string {
	switch m.panelFocus {
	case FocusEvents:
		return "Enter:Detail  Esc:Back  a:Alerts  Tab:Stats  q:Quit "
	case FocusAlerts:
		return "Enter:Detail  Esc:Back  e:Events  Tab:Stats  q:Quit "
	default:
		return "a:Alerts  e:Events  Tab:Stats  q:Quit  f:Filter  Ctrl+K:Kill "
	}
}

func truncateID(id string, maxLen int) string {
	if len(id) <= maxLen {
		return id
	}
	return id[:maxLen]
}

func (m Model) overlayKillDialog(base string) string {
	dialog := killDialogStyle.Render(
		"Kill session?\n\n" +
			m.killTargetInfo + "\n\n" +
			"[Y] Kill  [n/Esc] Cancel (resume)")

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

func (m Model) overlayDetail(base string) string {
	overlayW := m.width * 70 / 100
	if overlayW < 40 {
		overlayW = 40
	}
	if overlayW > m.width-4 {
		overlayW = m.width - 4
	}
	overlayH := m.height * 60 / 100
	if overlayH < 10 {
		overlayH = 10
	}
	if overlayH > m.height-4 {
		overlayH = m.height - 4
	}

	contentW := overlayW - 6
	if contentW < 10 {
		contentW = 10
	}
	contentH := overlayH - 4
	if contentH < 3 {
		contentH = 3
	}

	allLines := strings.Split(m.detailContent, "\n")

	var wrapped []string
	for _, line := range allLines {
		if len(line) <= contentW {
			wrapped = append(wrapped, line)
		} else {
			for len(line) > contentW {
				cutAt := contentW
				for i := contentW; i > 0; i-- {
					if line[i] == ' ' {
						cutAt = i
						break
					}
				}
				wrapped = append(wrapped, line[:cutAt])
				line = line[cutAt:]
				if len(line) > 0 && line[0] == ' ' {
					line = line[1:]
				}
			}
			if line != "" {
				wrapped = append(wrapped, line)
			}
		}
	}

	startIdx := m.detailScrollPos
	if startIdx > len(wrapped)-contentH {
		startIdx = len(wrapped) - contentH
	}
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + contentH
	if endIdx > len(wrapped) {
		endIdx = len(wrapped)
	}

	visibleLines := wrapped[startIdx:endIdx]
	body := strings.Join(visibleLines, "\n")

	title := panelTitleStyle.Render(m.detailTitle)
	footer := dimStyle.Render("Esc/Enter: Close")
	if len(wrapped) > contentH {
		footer += dimStyle.Render("  Up/Down: Scroll")
	}

	content := title + "\n\n" + body + "\n\n" + footer

	dialog := detailOverlayStyle.
		Width(overlayW - 2).
		Render(content)

	return placeOverlay(0, 0, dialog, base)
}

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
