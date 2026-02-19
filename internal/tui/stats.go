package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nixlim/cc-top/internal/stats"
)

func (m Model) renderStats() string {
	var sb strings.Builder

	viewLabel := " [Stats]"
	if m.selectedSession != "" {
		viewLabel += " Session: " + truncateID(m.selectedSession, 8)
	} else {
		viewLabel += " Global"
	}
	indicators := m.headerIndicators()
	help := "Tab:History  q:Quit "
	padding := m.width - lipgloss.Width(" cc-top") - lipgloss.Width(viewLabel) - lipgloss.Width(indicators) - lipgloss.Width(help)
	if padding < 0 {
		padding = 0
	}
	headerLine := headerStyle.Width(m.width).Render(
		" cc-top" + viewLabel + indicators + strings.Repeat(" ", padding) + help)
	sb.WriteString(headerLine)
	sb.WriteByte('\n')

	ds := m.getStats()

	contentW := m.width - 4
	if contentW < 20 {
		contentW = 20
	}

	sections := []string{
		m.renderCodeSection(ds),
		m.renderToolsSection(ds),
		m.renderAPISection(ds),
		m.renderTokenBreakdownSection(ds),
		m.renderModelBreakdown(ds),
		m.renderTopTools(ds),
	}

	allLines := []string{}
	for _, section := range sections {
		allLines = append(allLines, section)
		allLines = append(allLines, "")
	}

	visibleH := m.height - 3
	if visibleH < 1 {
		visibleH = 1
	}
	startIdx := m.statsScrollPos
	if startIdx > len(allLines)-visibleH {
		startIdx = len(allLines) - visibleH
	}
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + visibleH
	if endIdx > len(allLines) {
		endIdx = len(allLines)
	}

	for i := startIdx; i < endIdx; i++ {
		sb.WriteString(allLines[i])
		sb.WriteByte('\n')
	}

	return sb.String()
}

func (m Model) getStats() stats.DashboardStats {
	if m.stats == nil {
		return stats.DashboardStats{}
	}
	if m.selectedSession != "" {
		return m.stats.Get(m.selectedSession)
	}
	return m.stats.GetGlobal()
}

func (m Model) renderCodeSection(ds stats.DashboardStats) string {
	title := panelTitleStyle.Render("Code Metrics")
	lines := []string{
		title,
		fmt.Sprintf("  Lines added:   %s", formatNumber(int64(ds.LinesAdded))),
		fmt.Sprintf("  Lines removed: %s", formatNumber(int64(ds.LinesRemoved))),
		fmt.Sprintf("  Commits:       %s", formatNumber(int64(ds.Commits))),
		fmt.Sprintf("  Pull Requests: %s", formatNumber(int64(ds.PRs))),
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderToolsSection(ds stats.DashboardStats) string {
	title := panelTitleStyle.Render("Tool Acceptance")
	lines := []string{title}

	if len(ds.ToolAcceptance) == 0 {
		lines = append(lines, dimStyle.Render("  No tool data"))
	} else {
		for tool, rate := range ds.ToolAcceptance {
			bar := renderProgressBar(rate, 20)
			lines = append(lines, fmt.Sprintf("  %-15s %s %.0f%%", tool, bar, rate*100))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderAPISection(ds stats.DashboardStats) string {
	title := panelTitleStyle.Render("API Performance")
	lines := []string{
		title,
		fmt.Sprintf("  Cache efficiency: %s %.0f%%",
			renderProgressBar(ds.CacheEfficiency, 20), ds.CacheEfficiency*100),
		fmt.Sprintf("  Avg API latency:  %.1fs", ds.AvgAPILatency),
		fmt.Sprintf("  Error rate:       %s %.1f%%",
			renderProgressBar(ds.ErrorRate, 20), ds.ErrorRate*100),
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderTokenBreakdownSection(ds stats.DashboardStats) string {
	title := panelTitleStyle.Render("Token Breakdown")
	if len(ds.TokenBreakdown) == 0 {
		return title + "\n" + dimStyle.Render("  No token data")
	}
	lines := []string{
		title,
		fmt.Sprintf("  Input:          %s", formatNumber(ds.TokenBreakdown["input"])),
		fmt.Sprintf("  Output:         %s", formatNumber(ds.TokenBreakdown["output"])),
		fmt.Sprintf("  Cache Read:     %s", formatNumber(ds.TokenBreakdown["cacheRead"])),
		fmt.Sprintf("  Cache Creation: %s", formatNumber(ds.TokenBreakdown["cacheCreation"])),
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderModelBreakdown(ds stats.DashboardStats) string {
	title := panelTitleStyle.Render("Model Breakdown")
	lines := []string{title}

	if len(ds.ModelBreakdown) == 0 {
		lines = append(lines, dimStyle.Render("  No model data"))
	} else {
		lines = append(lines, fmt.Sprintf("  %-25s %10s %12s", "Model", "Cost", "Tokens"))
		lines = append(lines, dimStyle.Render("  "+strings.Repeat("─", 50)))
		for _, ms := range ds.ModelBreakdown {
			lines = append(lines, fmt.Sprintf("  %-25s $%9.2f %12s",
				truncateStr(ms.Model, 25), ms.TotalCost, formatNumber(ms.TotalTokens)))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderTopTools(ds stats.DashboardStats) string {
	title := panelTitleStyle.Render("Top Tools")
	lines := []string{title}

	if len(ds.TopTools) == 0 {
		lines = append(lines, dimStyle.Render("  No tool data"))
	} else {
		maxCount := 0
		for _, t := range ds.TopTools {
			if t.Count > maxCount {
				maxCount = t.Count
			}
		}

		for _, t := range ds.TopTools {
			ratio := 0.0
			if maxCount > 0 {
				ratio = float64(t.Count) / float64(maxCount)
			}
			bar := renderProgressBar(ratio, 20)
			lines = append(lines, fmt.Sprintf("  %-15s %s %d", t.ToolName, bar, t.Count))
		}
	}
	return strings.Join(lines, "\n")
}

func renderProgressBar(ratio float64, width int) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	filled := int(ratio * float64(width))
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	if ratio >= 0.8 {
		return costRedStyle.Render(bar)
	}
	if ratio >= 0.5 {
		return costYellowStyle.Render(bar)
	}
	return costGreenStyle.Render(bar)
}
