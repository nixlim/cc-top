package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nixlim/cc-top/internal/state"
)

type historyRow struct {
	label    string
	cost     float64
	tokens   int64
	sessions int
	requests int
	errors   int
}

func (m Model) renderHistory() string {
	var sb strings.Builder

	viewLabel := " [History] Global"
	indicators := m.headerIndicators()
	help := "d:Daily w:Weekly m:Monthly  Tab:Dashboard  q:Quit "
	padding := m.width - lipgloss.Width(" cc-top") - lipgloss.Width(viewLabel) - lipgloss.Width(indicators) - lipgloss.Width(help)
	if padding < 0 {
		padding = 0
	}
	headerLine := headerStyle.Width(m.width).Render(
		" cc-top" + viewLabel + indicators + strings.Repeat(" ", padding) + help)
	sb.WriteString(headerLine)
	sb.WriteByte('\n')

	if !m.isPersistent {
		sb.WriteByte('\n')
		sb.WriteString(dimStyle.Render("  persistence is disabled — run with a valid db_path to enable history"))
		sb.WriteByte('\n')
		return sb.String()
	}

	var summaries []state.DailySummary
	if m.state != nil {
		switch m.historyGranularity {
		case "weekly":
			summaries = m.state.QueryDailySummaries(28)
		case "monthly":
			summaries = m.state.QueryDailySummaries(90)
		default:
			summaries = m.state.QueryDailySummaries(7)
		}
	}

	if len(summaries) == 0 {
		sb.WriteByte('\n')
		sb.WriteString(dimStyle.Render("  No historical data available"))
		sb.WriteByte('\n')
		return sb.String()
	}

	var rows []historyRow
	switch m.historyGranularity {
	case "weekly":
		rows = aggregateWeekly(summaries)
	case "monthly":
		rows = aggregateMonthly(summaries)
	default:
		for _, ds := range summaries {
			rows = append(rows, historyRow{
				label:    ds.Date,
				cost:     ds.TotalCost,
				tokens:   ds.TotalTokens,
				sessions: ds.SessionCount,
				requests: ds.APIRequests,
				errors:   ds.APIErrors,
			})
		}
	}

	sb.WriteByte('\n')
	var dateHeader string
	switch m.historyGranularity {
	case "weekly":
		dateHeader = "Week"
	case "monthly":
		dateHeader = "Month"
	default:
		dateHeader = "Date"
	}
	sb.WriteString(fmt.Sprintf("  %-14s %10s %12s %8s %10s %8s",
		dateHeader, "Cost", "Tokens", "Sessions", "API Reqs", "Errors"))
	sb.WriteByte('\n')
	sb.WriteString(dimStyle.Render("  " + strings.Repeat("─", 68)))
	sb.WriteByte('\n')

	visibleH := m.height - 5
	if visibleH < 1 {
		visibleH = 1
	}
	startIdx := m.historyScrollPos
	if startIdx > len(rows)-visibleH {
		startIdx = len(rows) - visibleH
	}
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + visibleH
	if endIdx > len(rows) {
		endIdx = len(rows)
	}

	for i := startIdx; i < endIdx; i++ {
		r := rows[i]
		sb.WriteString(fmt.Sprintf("  %-14s   $%7.2f %12s %8d %10d %8d",
			r.label, r.cost, formatNumber(r.tokens), r.sessions, r.requests, r.errors))
		sb.WriteByte('\n')
	}

	return sb.String()
}

func aggregateWeekly(summaries []state.DailySummary) []historyRow {
	weekMap := make(map[string]*historyRow)
	var weekOrder []string

	for _, ds := range summaries {
		weekLabel := ds.Date[:7]
		if len(ds.Date) == 10 {
			t, err := time.Parse("2006-01-02", ds.Date)
			if err == nil {
				y, w := t.ISOWeek()
				weekLabel = fmt.Sprintf("Week %d-%02d", y, w)
			}
		}

		if _, exists := weekMap[weekLabel]; !exists {
			weekMap[weekLabel] = &historyRow{label: weekLabel}
			weekOrder = append(weekOrder, weekLabel)
		}
		r := weekMap[weekLabel]
		r.cost += ds.TotalCost
		r.tokens += ds.TotalTokens
		r.sessions += ds.SessionCount
		r.requests += ds.APIRequests
		r.errors += ds.APIErrors
	}

	result := make([]historyRow, 0, len(weekOrder))
	for _, key := range weekOrder {
		result = append(result, *weekMap[key])
	}
	return result
}

func aggregateMonthly(summaries []state.DailySummary) []historyRow {
	monthMap := make(map[string]*historyRow)
	var monthOrder []string

	for _, ds := range summaries {
		monthLabel := ds.Date[:7]
		if _, exists := monthMap[monthLabel]; !exists {
			monthMap[monthLabel] = &historyRow{label: monthLabel}
			monthOrder = append(monthOrder, monthLabel)
		}
		r := monthMap[monthLabel]
		r.cost += ds.TotalCost
		r.tokens += ds.TotalTokens
		r.sessions += ds.SessionCount
		r.requests += ds.APIRequests
		r.errors += ds.APIErrors
	}

	result := make([]historyRow, 0, len(monthOrder))
	for _, key := range monthOrder {
		result = append(result, *monthMap[key])
	}
	return result
}
