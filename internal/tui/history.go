package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// overviewAggRow is an aggregated row for the Overview sub-tab.
type overviewAggRow struct {
	label    string
	cost     float64
	tokens   int64
	sessions int
	requests int
	errors   int
	linesAdd int
	linesDel int
	commits  int
	days     []DailyStatsRow
}

// perfAggRow is an aggregated row for the Performance sub-tab.
type perfAggRow struct {
	label     string
	cacheEff  float64
	errRate   float64
	avgLat    float64
	p50       float64
	p95       float64
	p99       float64
	retryRate float64
	cacheSave float64
	isLegacy  bool
	days      []DailyStatsRow
}

// burnAggRow is an aggregated row for the Burn Rate sub-tab.
type burnAggRow struct {
	label     string
	avgRate   float64
	peakRate  float64
	tokenVel  float64
	dailyProj float64
	monthProj float64
	days      []BurnRateDailySummary
}

// dateGroup holds a label and its constituent daily rows.
type dateGroup struct {
	label string
	days  []DailyStatsRow
}

// --- Header rendering (v63.12) ---

func (m Model) renderHistoryHeader() string {
	title := " cc-top"

	tabs := []struct {
		key   string
		label string
	}{
		{"1", "Overview"},
		{"2", "Performance"},
		{"3", "Burn Rate"},
		{"4", "Alerts"},
	}
	var tabParts []string
	for i, t := range tabs {
		label := fmt.Sprintf("[%s] %s", t.key, t.label)
		if i == m.historySection {
			label = selectedStyle.Render(label)
		}
		tabParts = append(tabParts, label)
	}
	tabSection := "  " + strings.Join(tabParts, "  ")

	var modeSection string
	if m.historySection == 3 {
		modeSection = "  |  /:Filter"
	} else {
		granularities := []struct {
			key   string
			label string
			value string
		}{
			{"D", "aily", "daily"},
			{"W", "eekly", "weekly"},
			{"M", "onthly", "monthly"},
		}
		var gParts []string
		for _, g := range granularities {
			label := fmt.Sprintf("[%s]%s", g.key, g.label)
			if m.historyGranularity == g.value {
				label = selectedStyle.Render(label)
			}
			gParts = append(gParts, label)
		}
		modeSection = "  |  " + strings.Join(gParts, " / ")
	}

	indicators := m.headerIndicators()
	help := "  |  Tab:Dashboard  q:Quit "

	rawContent := title + tabSection + modeSection + indicators + help
	padding := m.width - lipgloss.Width(rawContent)
	if padding < 0 {
		padding = 0
	}

	return headerStyle.Width(m.width).Render(
		title + tabSection + modeSection + indicators + strings.Repeat(" ", padding) + help)
}

// --- Main render entry point ---

func (m Model) renderHistory() string {
	var sb strings.Builder

	sb.WriteString(m.renderHistoryHeader())
	sb.WriteByte('\n')

	if m.history == nil {
		sb.WriteByte('\n')
		sb.WriteString(dimStyle.Render("  persistence is disabled — run with a valid db_path to enable history"))
		sb.WriteByte('\n')
		return sb.String()
	}

	switch m.historySection {
	case 0:
		sb.WriteString(m.renderHistoryOverview())
	case 1:
		sb.WriteString(m.renderHistoryPerformance())
	case 2:
		sb.WriteString(m.renderHistoryBurnRate())
	case 3:
		sb.WriteString(m.renderHistoryAlerts())
	}

	if m.historyFilterMenu.Active {
		result := sb.String()
		return m.overlayHistoryFilterMenu(result)
	}

	return sb.String()
}

func (m Model) historyQueryDays() int {
	switch m.historyGranularity {
	case "weekly":
		return 28
	case "monthly":
		return 90
	default:
		return 7
	}
}

func (m Model) historyDateHeader() string {
	switch m.historyGranularity {
	case "weekly":
		return "Week"
	case "monthly":
		return "Month"
	default:
		return "Date"
	}
}

// --- Overview sub-tab (v63.13) ---

func (m Model) renderHistoryOverview() string {
	days := m.historyQueryDays()
	rows := m.history.QueryDailyStats(days)

	if len(rows) == 0 {
		return "\n" + dimStyle.Render("  No daily statistics yet. Data will appear after the first maintenance cycle.") + "\n"
	}

	var aggRows []overviewAggRow
	switch m.historyGranularity {
	case "weekly":
		aggRows = aggregateOverviewWeekly(rows)
	case "monthly":
		aggRows = aggregateOverviewMonthly(rows)
	default:
		for _, r := range rows {
			aggRows = append(aggRows, overviewAggRow{
				label: r.Date, cost: r.TotalCost,
				tokens:   r.TokenInput + r.TokenOutput,
				sessions: r.SessionCount, requests: r.APIRequests,
				errors: r.APIErrors, linesAdd: r.LinesAdded,
				linesDel: r.LinesRemoved, commits: r.Commits,
				days: []DailyStatsRow{r},
			})
		}
	}

	var sb strings.Builder
	sb.WriteByte('\n')
	dateH := m.historyDateHeader()
	sb.WriteString(fmt.Sprintf("  %-14s %10s %12s %8s %8s %6s %7s %7s %7s",
		dateH, "Cost", "Tokens", "Sessions", "API Reqs", "Errors", "Lines+", "Lines-", "Commits"))
	sb.WriteByte('\n')
	sb.WriteString(dimStyle.Render("  " + strings.Repeat("─", 90)))
	sb.WriteByte('\n')

	m.clampHistoryCursor(len(aggRows))
	startIdx, endIdx := m.visibleRange(len(aggRows))

	for i := startIdx; i < endIdx; i++ {
		r := aggRows[i]
		line := fmt.Sprintf("  %-14s   $%7.2f %12s %8d %8d %6d %7d %7d %7d",
			r.label, r.cost, formatNumber(r.tokens),
			r.sessions, r.requests, r.errors,
			r.linesAdd, r.linesDel, r.commits)
		if i == m.historyCursor {
			line = cursorStyle.Render(line)
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	return sb.String()
}

// --- Performance sub-tab (v63.13) ---

func (m Model) renderHistoryPerformance() string {
	days := m.historyQueryDays()
	rows := m.history.QueryDailyStats(days)

	if len(rows) == 0 {
		return "\n" + dimStyle.Render("  No daily statistics yet. Data will appear after the first maintenance cycle.") + "\n"
	}

	var aggRows []perfAggRow
	switch m.historyGranularity {
	case "weekly":
		aggRows = aggregatePerfWeekly(rows)
	case "monthly":
		aggRows = aggregatePerfMonthly(rows)
	default:
		for _, r := range rows {
			aggRows = append(aggRows, perfAggRow{
				label: r.Date, cacheEff: r.CacheEfficiency,
				errRate: r.ErrorRate, avgLat: r.AvgAPILatency,
				p50: r.LatencyP50, p95: r.LatencyP95, p99: r.LatencyP99,
				retryRate: r.RetryRate, cacheSave: r.CacheSavingsUSD,
				isLegacy: r.IsLegacy, days: []DailyStatsRow{r},
			})
		}
	}

	var sb strings.Builder
	sb.WriteByte('\n')
	dateH := m.historyDateHeader()
	sb.WriteString(fmt.Sprintf("  %-14s %7s %8s %8s %7s %7s %7s %8s %9s",
		dateH, "Cache%", "Err Rate", "Avg Lat", "P50", "P95", "P99", "Retries", "Cache $"))
	sb.WriteByte('\n')
	sb.WriteString(dimStyle.Render("  " + strings.Repeat("─", 85)))
	sb.WriteByte('\n')

	m.clampHistoryCursor(len(aggRows))
	startIdx, endIdx := m.visibleRange(len(aggRows))

	for i := startIdx; i < endIdx; i++ {
		r := aggRows[i]
		var line string
		if r.isLegacy {
			line = fmt.Sprintf("  %-14s %7s %8s %8s %7s %7s %7s %8s %9s",
				r.label, "--", "--", "--", "--", "--", "--", "--", "--")
		} else {
			line = fmt.Sprintf("  %-14s %6.0f%% %7.1f%% %7.1fs %6.1fs %6.1fs %6.1fs %7.1f%% $%7.2f",
				r.label, r.cacheEff*100, r.errRate*100, r.avgLat,
				r.p50, r.p95, r.p99, r.retryRate*100, r.cacheSave)
		}
		if i == m.historyCursor {
			line = cursorStyle.Render(line)
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	return sb.String()
}

// --- Burn Rate sub-tab (v63.13) ---

func (m Model) renderHistoryBurnRate() string {
	days := m.historyQueryDays()
	rows := m.history.QueryBurnRateDailySummary(days)

	if len(rows) == 0 {
		return "\n" + dimStyle.Render("  No burn rate data yet. Snapshots are captured every 5 minutes.") + "\n"
	}

	var aggRows []burnAggRow
	switch m.historyGranularity {
	case "weekly":
		aggRows = aggregateBurnWeekly(rows)
	case "monthly":
		aggRows = aggregateBurnMonthly(rows)
	default:
		for _, r := range rows {
			aggRows = append(aggRows, burnAggRow{
				label: r.Date, avgRate: r.AvgHourlyRate,
				peakRate: r.PeakHourlyRate, tokenVel: r.AvgTokenVelocity,
				dailyProj: r.DailyProjection, monthProj: r.MonthlyProjection,
				days: []BurnRateDailySummary{r},
			})
		}
	}

	var sb strings.Builder
	sb.WriteByte('\n')
	dateH := m.historyDateHeader()
	sb.WriteString(fmt.Sprintf("  %-14s %10s %10s %12s %10s %10s",
		dateH, "Avg $/hr", "Peak $/hr", "Tokens/min", "Daily $", "Monthly $"))
	sb.WriteByte('\n')
	sb.WriteString(dimStyle.Render("  " + strings.Repeat("─", 72)))
	sb.WriteByte('\n')

	m.clampHistoryCursor(len(aggRows))
	startIdx, endIdx := m.visibleRange(len(aggRows))

	for i := startIdx; i < endIdx; i++ {
		r := aggRows[i]
		line := fmt.Sprintf("  %-14s   $%7.2f   $%7.2f %12.1f   $%7.2f   $%7.2f",
			r.label, r.avgRate, r.peakRate, r.tokenVel, r.dailyProj, r.monthProj)
		if i == m.historyCursor {
			line = cursorStyle.Render(line)
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	return sb.String()
}

// --- Alerts sub-tab (v63.13) ---

func (m Model) renderHistoryAlerts() string {
	alerts := m.history.QueryAlertHistory(90, m.historyAlertFilter)

	if len(alerts) == 0 {
		return "\n" + dimStyle.Render("  No alerts recorded yet. Alerts will appear here when triggered.") + "\n"
	}

	var sb strings.Builder
	sb.WriteByte('\n')
	sb.WriteString(fmt.Sprintf("  %-19s %-20s %-10s %-12s %s",
		"Time", "Rule", "Severity", "Session", "Message"))
	sb.WriteByte('\n')
	sb.WriteString(dimStyle.Render("  " + strings.Repeat("─", 85)))
	sb.WriteByte('\n')

	m.clampHistoryCursor(len(alerts))
	startIdx, endIdx := m.visibleRange(len(alerts))

	for i := startIdx; i < endIdx; i++ {
		a := alerts[i]
		sess := a.SessionID
		if sess == "" {
			sess = "(global)"
		}
		msg := a.Message
		if len(msg) > 30 {
			msg = msg[:30] + "..."
		}
		line := fmt.Sprintf("  %-19s %-20s %-10s %-12s %s",
			a.FiredAt.Local().Format("2006-01-02 15:04:05"),
			truncateStr(a.Rule, 20),
			a.Severity,
			truncateStr(sess, 12),
			msg)
		if i == m.historyCursor {
			line = cursorStyle.Render(line)
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}

	return sb.String()
}

// --- Detail overlays (v63.14) ---

func (m Model) openHistoryDetail() (Model, tea.Cmd) {
	if m.history == nil {
		return m, nil
	}

	switch m.historySection {
	case 0:
		return m.openOverviewDetail()
	case 1:
		return m.openPerformanceDetail()
	case 2:
		return m.openBurnRateDetail()
	case 3:
		return m.openAlertDetail()
	}
	return m, nil
}

func (m Model) openOverviewDetail() (Model, tea.Cmd) {
	days := m.historyQueryDays()
	rows := m.history.QueryDailyStats(days)

	// For weekly/monthly, use aggregate groups.
	if m.historyGranularity == "weekly" || m.historyGranularity == "monthly" {
		groups := groupByDate(rows, m.historyGranularity)
		if len(groups) == 0 || m.historyCursor >= len(groups) {
			return m, nil
		}
		g := groups[m.historyCursor]
		var lines []string
		lines = append(lines, fmt.Sprintf("Period: %s (%d days)", g.label, len(g.days)))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %-12s %10s %12s %8s %8s %6s %7s %7s %7s",
			"Date", "Cost", "Tokens", "Sessions", "API Reqs", "Errors", "Lines+", "Lines-", "Commits"))
		lines = append(lines, "  "+strings.Repeat("─", 90))
		for _, r := range g.days {
			lines = append(lines, fmt.Sprintf("  %-12s   $%7.2f %12s %8d %8d %6d %7d %7d %7d",
				r.Date, r.TotalCost, formatNumber(r.TokenInput+r.TokenOutput),
				r.SessionCount, r.APIRequests, r.APIErrors,
				r.LinesAdded, r.LinesRemoved, r.Commits))
		}
		m.detailOverlay = true
		m.detailTitle = "Overview Detail — " + g.label
		m.detailContent = strings.Join(lines, "\n")
		m.detailScrollPos = 0
		return m, nil
	}

	if len(rows) == 0 || m.historyCursor >= len(rows) {
		return m, nil
	}

	r := rows[m.historyCursor]
	var lines []string
	lines = append(lines, fmt.Sprintf("Date:             %s", r.Date))
	lines = append(lines, fmt.Sprintf("Total Cost:       $%.2f", r.TotalCost))
	lines = append(lines, fmt.Sprintf("Tokens (in/out):  %s / %s", formatNumber(r.TokenInput), formatNumber(r.TokenOutput)))
	lines = append(lines, fmt.Sprintf("Cache (R/W):      %s / %s", formatNumber(r.TokenCacheRead), formatNumber(r.TokenCacheWrite)))
	lines = append(lines, fmt.Sprintf("Sessions:         %d", r.SessionCount))
	lines = append(lines, fmt.Sprintf("API Requests:     %d", r.APIRequests))
	lines = append(lines, fmt.Sprintf("API Errors:       %d", r.APIErrors))
	lines = append(lines, fmt.Sprintf("Lines +/-:        %d / %d", r.LinesAdded, r.LinesRemoved))
	lines = append(lines, fmt.Sprintf("Commits:          %d", r.Commits))
	lines = append(lines, fmt.Sprintf("PRs Opened:       %d", r.PRsOpened))

	if !r.IsLegacy {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("Cache Efficiency: %.0f%%", r.CacheEfficiency*100))
		lines = append(lines, fmt.Sprintf("Error Rate:       %.1f%%", r.ErrorRate*100))
		lines = append(lines, fmt.Sprintf("Retry Rate:       %.1f%%", r.RetryRate*100))
		lines = append(lines, fmt.Sprintf("Cache Savings:    $%.2f", r.CacheSavingsUSD))
	}

	if len(r.ModelBreakdown) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Model Breakdown:")
		lines = append(lines, fmt.Sprintf("  %-25s %10s %12s", "Model", "Cost", "Tokens"))
		lines = append(lines, "  "+strings.Repeat("─", 50))
		for _, mb := range r.ModelBreakdown {
			lines = append(lines, fmt.Sprintf("  %-25s $%9.2f %12s",
				truncateStr(mb.Model, 25), mb.TotalCost, formatNumber(mb.TotalTokens)))
		}
	}

	if len(r.TopTools) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Tool Usage:")
		lines = append(lines, fmt.Sprintf("  %-20s %8s %12s %12s", "Tool", "Count", "Avg Dur(ms)", "P95 Dur(ms)"))
		lines = append(lines, "  "+strings.Repeat("─", 55))
		for j, t := range r.TopTools {
			avgDur := 0.0
			p95Dur := 0.0
			if j < len(r.ToolPerformance) {
				avgDur = r.ToolPerformance[j].AvgDurationMS
				p95Dur = r.ToolPerformance[j].P95DurationMS
			}
			lines = append(lines, fmt.Sprintf("  %-20s %8d %12.1f %12.1f",
				truncateStr(t.ToolName, 20), t.Count, avgDur, p95Dur))
		}
	}

	if len(r.ErrorCategories) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Error Categories:")
		lines = append(lines, fmt.Sprintf("  %-20s %8s", "Category", "Count"))
		lines = append(lines, "  "+strings.Repeat("─", 30))
		for cat, cnt := range r.ErrorCategories {
			lines = append(lines, fmt.Sprintf("  %-20s %8d", cat, cnt))
		}
	}

	if len(r.LanguageBreakdown) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Language Breakdown:")
		for lang, cnt := range r.LanguageBreakdown {
			lines = append(lines, fmt.Sprintf("  %-20s %8d", lang, cnt))
		}
	}

	if len(r.DecisionSources) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Decision Sources:")
		for src, cnt := range r.DecisionSources {
			lines = append(lines, fmt.Sprintf("  %-20s %8d", src, cnt))
		}
	}

	m.detailOverlay = true
	m.detailTitle = "Overview Detail — " + r.Date
	m.detailContent = strings.Join(lines, "\n")
	m.detailScrollPos = 0
	return m, nil
}

func (m Model) openPerformanceDetail() (Model, tea.Cmd) {
	days := m.historyQueryDays()
	rows := m.history.QueryDailyStats(days)

	// For weekly/monthly, show mini-table of daily performance stats.
	if m.historyGranularity == "weekly" || m.historyGranularity == "monthly" {
		var aggRows []perfAggRow
		if m.historyGranularity == "weekly" {
			aggRows = aggregatePerfWeekly(rows)
		} else {
			aggRows = aggregatePerfMonthly(rows)
		}
		if len(aggRows) == 0 || m.historyCursor >= len(aggRows) {
			return m, nil
		}
		g := aggRows[m.historyCursor]
		var lines []string
		lines = append(lines, fmt.Sprintf("Period: %s (%d days)", g.label, len(g.days)))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %-12s %7s %8s %8s %7s %7s %7s %8s %9s",
			"Date", "Cache%", "Err Rate", "Avg Lat", "P50", "P95", "P99", "Retries", "Cache $"))
		lines = append(lines, "  "+strings.Repeat("─", 85))
		for _, r := range g.days {
			if r.IsLegacy {
				lines = append(lines, fmt.Sprintf("  %-12s %7s %8s %8s %7s %7s %7s %8s %9s",
					r.Date, "--", "--", "--", "--", "--", "--", "--", "--"))
			} else {
				lines = append(lines, fmt.Sprintf("  %-12s %6.0f%% %7.1f%% %7.1fs %6.1fs %6.1fs %6.1fs %7.1f%% $%7.2f",
					r.Date, r.CacheEfficiency*100, r.ErrorRate*100, r.AvgAPILatency,
					r.LatencyP50, r.LatencyP95, r.LatencyP99, r.RetryRate*100, r.CacheSavingsUSD))
			}
		}
		m.detailOverlay = true
		m.detailTitle = "Performance Detail — " + g.label
		m.detailContent = strings.Join(lines, "\n")
		m.detailScrollPos = 0
		return m, nil
	}

	if len(rows) == 0 || m.historyCursor >= len(rows) {
		return m, nil
	}

	r := rows[m.historyCursor]
	var lines []string
	lines = append(lines, fmt.Sprintf("Date: %s", r.Date))

	if len(r.ModelBreakdown) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Model Breakdown:")
		lines = append(lines, fmt.Sprintf("  %-25s %10s %12s", "Model", "Cost", "Tokens"))
		lines = append(lines, "  "+strings.Repeat("─", 50))
		for _, mb := range r.ModelBreakdown {
			lines = append(lines, fmt.Sprintf("  %-25s $%9.2f %12s",
				truncateStr(mb.Model, 25), mb.TotalCost, formatNumber(mb.TotalTokens)))
		}
	}

	if len(r.TopTools) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Top Tools + Performance:")
		lines = append(lines, fmt.Sprintf("  %-20s %8s %15s %15s", "Tool", "Count", "Avg Duration(ms)", "P95 Duration(ms)"))
		lines = append(lines, "  "+strings.Repeat("─", 65))
		for j, t := range r.TopTools {
			avgDur := 0.0
			p95Dur := 0.0
			if j < len(r.ToolPerformance) {
				avgDur = r.ToolPerformance[j].AvgDurationMS
				p95Dur = r.ToolPerformance[j].P95DurationMS
			}
			lines = append(lines, fmt.Sprintf("  %-20s %8d %15.1f %15.1f",
				truncateStr(t.ToolName, 20), t.Count, avgDur, p95Dur))
		}
	}

	if len(r.ErrorCategories) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Error Categories:")
		lines = append(lines, fmt.Sprintf("  %-20s %8s", "Category", "Count"))
		lines = append(lines, "  "+strings.Repeat("─", 30))
		for cat, cnt := range r.ErrorCategories {
			lines = append(lines, fmt.Sprintf("  %-20s %8d", cat, cnt))
		}
	}

	if len(r.MCPToolUsage) > 0 {
		lines = append(lines, "")
		lines = append(lines, "MCP Tool Usage:")
		lines = append(lines, fmt.Sprintf("  %-30s %8s", "Server:Tool", "Count"))
		lines = append(lines, "  "+strings.Repeat("─", 40))
		for tool, cnt := range r.MCPToolUsage {
			lines = append(lines, fmt.Sprintf("  %-30s %8d", truncateStr(tool, 30), cnt))
		}
	}

	m.detailOverlay = true
	m.detailTitle = "Performance Detail — " + r.Date
	m.detailContent = strings.Join(lines, "\n")
	m.detailScrollPos = 0
	return m, nil
}

func (m Model) openBurnRateDetail() (Model, tea.Cmd) {
	days := m.historyQueryDays()
	summaries := m.history.QueryBurnRateDailySummary(days)

	// For weekly/monthly, show mini-table of daily burn rate summaries.
	if m.historyGranularity == "weekly" || m.historyGranularity == "monthly" {
		var aggRows []burnAggRow
		if m.historyGranularity == "weekly" {
			aggRows = aggregateBurnWeekly(summaries)
		} else {
			aggRows = aggregateBurnMonthly(summaries)
		}
		if len(aggRows) == 0 || m.historyCursor >= len(aggRows) {
			return m, nil
		}
		g := aggRows[m.historyCursor]
		var lines []string
		lines = append(lines, fmt.Sprintf("Period: %s (%d days)", g.label, len(g.days)))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %-12s %10s %10s %12s %10s %10s",
			"Date", "Avg $/hr", "Peak $/hr", "Tokens/min", "Daily $", "Monthly $"))
		lines = append(lines, "  "+strings.Repeat("─", 72))
		for _, d := range g.days {
			lines = append(lines, fmt.Sprintf("  %-12s   $%7.2f   $%7.2f %12.1f   $%7.2f   $%7.2f",
				d.Date, d.AvgHourlyRate, d.PeakHourlyRate,
				d.AvgTokenVelocity, d.DailyProjection, d.MonthlyProjection))
		}
		m.detailOverlay = true
		m.detailTitle = "Burn Rate Detail — " + g.label
		m.detailContent = strings.Join(lines, "\n")
		m.detailScrollPos = 0
		return m, nil
	}

	if len(summaries) == 0 || m.historyCursor >= len(summaries) {
		return m, nil
	}

	s := summaries[m.historyCursor]
	snapshots := m.history.QueryBurnRateSnapshots(s.Date)

	var lines []string
	lines = append(lines, fmt.Sprintf("Date: %s", s.Date))
	lines = append(lines, fmt.Sprintf("Avg $/hr: $%.2f  Peak $/hr: $%.2f  Snapshots: %d",
		s.AvgHourlyRate, s.PeakHourlyRate, s.SnapshotCount))
	lines = append(lines, "")

	if len(snapshots) > 0 {
		lines = append(lines, "Intra-day Snapshots:")
		lines = append(lines, fmt.Sprintf("  %-8s %10s %10s %8s %12s",
			"Time", "Cost", "$/hr", "Trend", "Tokens/min"))
		lines = append(lines, "  "+strings.Repeat("─", 55))
		for _, snap := range snapshots {
			lines = append(lines, fmt.Sprintf("  %-8s   $%7.2f   $%7.2f %8s %12.1f",
				snap.Timestamp.Local().Format("15:04"),
				snap.TotalCost, snap.HourlyRate,
				snap.Trend.String(), snap.TokenVelocity))
		}
	} else {
		lines = append(lines, dimStyle.Render("  No intra-day snapshots available"))
	}

	m.detailOverlay = true
	m.detailTitle = "Burn Rate Detail — " + s.Date
	m.detailContent = strings.Join(lines, "\n")
	m.detailScrollPos = 0
	return m, nil
}

func (m Model) openAlertDetail() (Model, tea.Cmd) {
	alerts := m.history.QueryAlertHistory(90, m.historyAlertFilter)
	if len(alerts) == 0 || m.historyCursor >= len(alerts) {
		return m, nil
	}

	a := alerts[m.historyCursor]
	var lines []string
	lines = append(lines, "Rule:      "+a.Rule)
	lines = append(lines, "Severity:  "+a.Severity)
	sess := a.SessionID
	if sess == "" {
		sess = "(global)"
	}
	lines = append(lines, "Session:   "+sess)
	lines = append(lines, "Fired at:  "+a.FiredAt.Local().Format("2006-01-02 15:04:05"))
	lines = append(lines, "")
	lines = append(lines, "Message:")
	lines = append(lines, a.Message)

	m.detailOverlay = true
	m.detailTitle = "Alert Detail"
	m.detailContent = strings.Join(lines, "\n")
	m.detailScrollPos = 0
	return m, nil
}

// --- Alert filter menu (v63.15) ---

func (m *Model) openHistoryAlertFilterMenu() {
	var options []FilterOption
	options = append(options, FilterOption{Label: "All", Key: "", Enabled: m.historyAlertFilter == ""})

	if m.history != nil {
		alerts := m.history.QueryAlertHistory(90, "")
		seen := make(map[string]bool)
		for _, a := range alerts {
			if !seen[a.Rule] {
				seen[a.Rule] = true
				options = append(options, FilterOption{
					Label:   a.Rule,
					Key:     a.Rule,
					Enabled: m.historyAlertFilter == a.Rule,
				})
			}
		}
	}

	m.historyFilterMenu = FilterMenuState{
		Active:  true,
		Cursor:  0,
		Options: options,
	}
}

func (m Model) handleHistoryFilterMenuKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Escape):
		m.historyFilterMenu.Active = false
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.historyFilterMenu.Cursor > 0 {
			m.historyFilterMenu.Cursor--
		}
		return m, nil

	case key.Matches(msg, m.keys.Down):
		if m.historyFilterMenu.Cursor < len(m.historyFilterMenu.Options)-1 {
			m.historyFilterMenu.Cursor++
		}
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		if m.historyFilterMenu.Cursor >= 0 && m.historyFilterMenu.Cursor < len(m.historyFilterMenu.Options) {
			opt := m.historyFilterMenu.Options[m.historyFilterMenu.Cursor]
			m.historyAlertFilter = opt.Key
			m.historyCursor = 0
			m.historyFilterMenu.Active = false
		}
		return m, nil
	}
	return m, nil
}

func (m Model) overlayHistoryFilterMenu(base string) string {
	content := panelTitleStyle.Render("Alert Rule Filter") + "\n\n"
	for i, opt := range m.historyFilterMenu.Options {
		cursor := "  "
		if i == m.historyFilterMenu.Cursor {
			cursor = "> "
		}
		check := "  "
		if opt.Enabled {
			check = "* "
		}
		line := cursor + check + opt.Label
		if i == m.historyFilterMenu.Cursor {
			line = selectedStyle.Render(line)
		}
		content += line + "\n"
	}
	content += "\nEnter: Select  Esc: Close"

	dialog := filterMenuStyle.Render(content)
	return placeOverlay(0, 0, dialog, base)
}

// --- Cursor and scroll helpers ---

func (m *Model) clampHistoryCursor(rowCount int) {
	if rowCount == 0 {
		m.historyCursor = 0
		return
	}
	if m.historyCursor >= rowCount {
		m.historyCursor = rowCount - 1
	}
	if m.historyCursor < 0 {
		m.historyCursor = 0
	}
}

func (m Model) visibleRange(rowCount int) (start, end int) {
	visibleH := m.height - 6
	if visibleH < 1 {
		visibleH = 1
	}

	start = m.historyScrollPos
	if m.historyCursor < start {
		start = m.historyCursor
	}
	if m.historyCursor >= start+visibleH {
		start = m.historyCursor - visibleH + 1
	}
	if start > rowCount-visibleH {
		start = rowCount - visibleH
	}
	if start < 0 {
		start = 0
	}
	end = start + visibleH
	if end > rowCount {
		end = rowCount
	}
	return start, end
}

// --- Aggregation helpers ---

func groupByDate(rows []DailyStatsRow, granularity string) []dateGroup {
	labelFn := weekLabelForDate
	if granularity == "monthly" {
		labelFn = func(date string) string { return date[:7] }
	}

	groupMap := make(map[string]*dateGroup)
	var order []string
	for _, r := range rows {
		lbl := labelFn(r.Date)
		if _, ok := groupMap[lbl]; !ok {
			groupMap[lbl] = &dateGroup{label: lbl}
			order = append(order, lbl)
		}
		groupMap[lbl].days = append(groupMap[lbl].days, r)
	}
	result := make([]dateGroup, 0, len(order))
	for _, k := range order {
		result = append(result, *groupMap[k])
	}
	return result
}

func aggregateOverviewWeekly(rows []DailyStatsRow) []overviewAggRow {
	return aggregateOverviewByGroup(rows, weekLabelForDate)
}

func aggregateOverviewMonthly(rows []DailyStatsRow) []overviewAggRow {
	return aggregateOverviewByGroup(rows, func(date string) string { return date[:7] })
}

func aggregateOverviewByGroup(rows []DailyStatsRow, labelFn func(string) string) []overviewAggRow {
	groups := make(map[string]*overviewAggRow)
	var order []string
	for _, r := range rows {
		lbl := labelFn(r.Date)
		if _, ok := groups[lbl]; !ok {
			groups[lbl] = &overviewAggRow{label: lbl}
			order = append(order, lbl)
		}
		g := groups[lbl]
		g.cost += r.TotalCost
		g.tokens += r.TokenInput + r.TokenOutput
		g.sessions += r.SessionCount
		g.requests += r.APIRequests
		g.errors += r.APIErrors
		g.linesAdd += r.LinesAdded
		g.linesDel += r.LinesRemoved
		g.commits += r.Commits
		g.days = append(g.days, r)
	}
	result := make([]overviewAggRow, 0, len(order))
	for _, k := range order {
		result = append(result, *groups[k])
	}
	return result
}

func aggregatePerfWeekly(rows []DailyStatsRow) []perfAggRow {
	return aggregatePerfByGroup(rows, weekLabelForDate)
}

func aggregatePerfMonthly(rows []DailyStatsRow) []perfAggRow {
	return aggregatePerfByGroup(rows, func(date string) string { return date[:7] })
}

func aggregatePerfByGroup(rows []DailyStatsRow, labelFn func(string) string) []perfAggRow {
	type accumulator struct {
		perfAggRow
		nonLegacy int
	}
	groups := make(map[string]*accumulator)
	var order []string
	for _, r := range rows {
		lbl := labelFn(r.Date)
		if _, ok := groups[lbl]; !ok {
			groups[lbl] = &accumulator{perfAggRow: perfAggRow{label: lbl}}
			order = append(order, lbl)
		}
		g := groups[lbl]
		g.days = append(g.days, r)
		if r.IsLegacy {
			continue
		}
		g.nonLegacy++
		g.cacheEff += r.CacheEfficiency
		g.errRate += r.ErrorRate
		g.avgLat += r.AvgAPILatency
		g.p50 += r.LatencyP50
		g.p95 += r.LatencyP95
		g.p99 += r.LatencyP99
		g.retryRate += r.RetryRate
		g.cacheSave += r.CacheSavingsUSD
	}
	result := make([]perfAggRow, 0, len(order))
	for _, k := range order {
		g := groups[k]
		if g.nonLegacy > 0 {
			n := float64(g.nonLegacy)
			g.cacheEff /= n
			g.errRate /= n
			g.avgLat /= n
			g.p50 /= n
			g.p95 /= n
			g.p99 /= n
			g.retryRate /= n
		} else {
			g.isLegacy = true
		}
		result = append(result, g.perfAggRow)
	}
	return result
}

func aggregateBurnWeekly(rows []BurnRateDailySummary) []burnAggRow {
	return aggregateBurnByGroup(rows, weekLabelForDate)
}

func aggregateBurnMonthly(rows []BurnRateDailySummary) []burnAggRow {
	return aggregateBurnByGroup(rows, func(date string) string { return date[:7] })
}

func aggregateBurnByGroup(rows []BurnRateDailySummary, labelFn func(string) string) []burnAggRow {
	groups := make(map[string]*burnAggRow)
	var order []string
	for _, r := range rows {
		lbl := labelFn(r.Date)
		if _, ok := groups[lbl]; !ok {
			groups[lbl] = &burnAggRow{label: lbl}
			order = append(order, lbl)
		}
		g := groups[lbl]
		g.days = append(g.days, r)
	}

	result := make([]burnAggRow, 0, len(order))
	for _, k := range order {
		g := groups[k]
		activeDays := 0
		for _, d := range g.days {
			if d.SnapshotCount == 0 {
				continue
			}
			activeDays++
			g.avgRate += d.AvgHourlyRate
			g.tokenVel += d.AvgTokenVelocity
			g.monthProj += d.MonthlyProjection
			if d.PeakHourlyRate > g.peakRate {
				g.peakRate = d.PeakHourlyRate
			}
			g.dailyProj += d.DailyProjection
		}
		if activeDays > 0 {
			g.avgRate /= float64(activeDays)
			g.tokenVel /= float64(activeDays)
			g.monthProj /= float64(activeDays)
		}
		result = append(result, *g)
	}
	return result
}

func weekLabelForDate(date string) string {
	if len(date) >= 10 {
		t, err := time.Parse("2006-01-02", date[:10])
		if err == nil {
			y, w := t.ISOWeek()
			return fmt.Sprintf("Week %d-%02d", y, w)
		}
	}
	return date[:7]
}
