package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nixlim/cc-top/internal/events"
)

// eventTypeIcons maps event types to their display icons.
var eventTypeIcons = map[string]string{
	"user_prompt":   ">>",
	"tool_result":   "T:",
	"api_request":   "AI",
	"api_error":     "!!",
	"tool_decision": "TD",
}

// eventTypeStyles maps event types to their display styles.
var eventTypeStyles = map[string]lipgloss.Style{
	"user_prompt":   lipgloss.NewStyle().Foreground(lipgloss.Color("117")),
	"tool_result":   lipgloss.NewStyle().Foreground(lipgloss.Color("222")),
	"api_request":   lipgloss.NewStyle().Foreground(lipgloss.Color("114")),
	"api_error":     lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
	"tool_decision": lipgloss.NewStyle().Foreground(lipgloss.Color("183")),
}

// renderEventStreamPanel renders the scrolling event stream panel.
func (m Model) renderEventStreamPanel(w, h int) string {
	contentW := w - 4
	if contentW < 10 {
		contentW = 10
	}
	contentH := h - 4 // borders + title
	if contentH < 1 {
		contentH = 1
	}

	focused := m.panelFocus == FocusEvents

	var lines []string

	// Title.
	title := panelTitleStyle.Render("Events")
	if m.eventFilter.SessionID != "" {
		title += dimStyle.Render(" [" + truncateID(m.eventFilter.SessionID, 8) + "]")
	}
	if focused {
		title += dimStyle.Render(" (Enter:detail Esc:back)")
	}
	lines = append(lines, title)

	// Get events from provider.
	evts := m.getFilteredEvents(m.cfg.Display.EventBufferSize)

	if len(evts) == 0 {
		lines = append(lines, "")
		lines = append(lines, dimStyle.Render("No data received yet"))
		content := strings.Join(lines, "\n")
		borderStyle := panelBorderStyle
		if focused {
			borderStyle = borderStyle.BorderForeground(focusBorderColor)
		}
		return renderBorderedPanelStyled(content, w, h, borderStyle)
	}

	// Apply scroll position.
	visibleLines := contentH - 1 // subtract title line
	if visibleLines < 1 {
		visibleLines = 1
	}

	// When focused, scroll to keep cursor visible.
	startIdx := 0
	if focused {
		// Ensure the event cursor stays within bounds.
		if m.eventCursor >= len(evts) {
			m.eventCursor = len(evts) - 1
		}
		if m.eventCursor < 0 {
			m.eventCursor = 0
		}
		// Scroll to keep cursor in view.
		startIdx = m.eventCursor - visibleLines + 1
		if startIdx < 0 {
			startIdx = 0
		}
		if m.eventCursor < startIdx {
			startIdx = m.eventCursor
		}
	} else if m.autoScroll {
		startIdx = len(evts) - visibleLines
		if startIdx < 0 {
			startIdx = 0
		}
	} else {
		startIdx = m.eventScrollPos
		if startIdx > len(evts)-visibleLines {
			startIdx = len(evts) - visibleLines
		}
		if startIdx < 0 {
			startIdx = 0
		}
	}

	endIdx := startIdx + visibleLines
	if endIdx > len(evts) {
		endIdx = len(evts)
	}

	for i := startIdx; i < endIdx; i++ {
		line := renderEventLine(evts[i], contentW)
		if focused && i == m.eventCursor {
			line = cursorStyle.Width(contentW).Render(stripAnsi(line))
		}
		lines = append(lines, line)
	}

	// Scroll indicator.
	if len(evts) > visibleLines {
		scrollInfo := dimStyle.Render(
			strings.Repeat(" ", contentW-20) +
				formatScrollPos(startIdx+1, endIdx, len(evts)))
		lines = append(lines, scrollInfo)
	}

	content := strings.Join(lines, "\n")
	borderStyle := panelBorderStyle
	if focused {
		borderStyle = borderStyle.BorderForeground(focusBorderColor)
	}
	return renderBorderedPanelStyled(content, w, h, borderStyle)
}

// getFilteredEvents returns events matching the current filter.
func (m Model) getFilteredEvents(limit int) []events.FormattedEvent {
	if m.events == nil {
		return nil
	}

	var evts []events.FormattedEvent
	if m.eventFilter.SessionID != "" {
		evts = m.events.RecentForSession(m.eventFilter.SessionID, limit)
	} else {
		evts = m.events.Recent(limit)
	}

	// Apply event type and success/failure filters.
	var filtered []events.FormattedEvent
	for _, e := range evts {
		if m.eventFilter.Matches(e.SessionID, e.EventType, e.Success) {
			filtered = append(filtered, e)
		}
	}

	return filtered
}

// renderEventLine formats a single event for display.
func renderEventLine(e events.FormattedEvent, maxW int) string {
	icon := eventTypeIcons[e.EventType]
	if icon == "" {
		icon = "??"
	}

	style, ok := eventTypeStyles[e.EventType]
	if !ok {
		style = dimStyle
	}

	// Truncate the formatted string if needed.
	formatted := e.Formatted
	maxFormatted := maxW - len(icon) - 2 // icon + space
	if len(formatted) > maxFormatted && maxFormatted > 3 {
		formatted = formatted[:maxFormatted-3] + "..."
	}

	return style.Render(icon + " " + formatted)
}

// formatScrollPos returns a string like "[10-20/100]".
func formatScrollPos(start, end, total int) string {
	return strings.Join([]string{
		"[",
		formatNumber(int64(start)),
		"-",
		formatNumber(int64(end)),
		"/",
		formatNumber(int64(total)),
		"]",
	}, "")
}
