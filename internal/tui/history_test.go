package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/stats"
)

// --- Mock HistoryProvider ---

type mockHistoryProvider struct {
	dailyStats     []DailyStatsRow
	burnSummaries  []BurnRateDailySummary
	burnSnapshots  []BurnRateSnapshotRow
	alertHistory   []AlertHistoryRow
	callLog        []string // tracks method calls for verification
}

func (m *mockHistoryProvider) QueryDailyStats(days int) []DailyStatsRow {
	m.callLog = append(m.callLog, "QueryDailyStats")
	return m.dailyStats
}

func (m *mockHistoryProvider) QueryBurnRateDailySummary(days int) []BurnRateDailySummary {
	m.callLog = append(m.callLog, "QueryBurnRateDailySummary")
	return m.burnSummaries
}

func (m *mockHistoryProvider) QueryBurnRateSnapshots(date string) []BurnRateSnapshotRow {
	m.callLog = append(m.callLog, "QueryBurnRateSnapshots")
	return m.burnSnapshots
}

func (m *mockHistoryProvider) QueryAlertHistory(days int, ruleFilter string) []AlertHistoryRow {
	m.callLog = append(m.callLog, "QueryAlertHistory")
	// Respect ruleFilter the same way the real impl would.
	if ruleFilter == "" {
		return m.alertHistory
	}
	var filtered []AlertHistoryRow
	for _, a := range m.alertHistory {
		if a.Rule == ruleFilter {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

// --- Helpers ---

func newHistoryModel(opts ...ModelOption) Model {
	cfg := config.DefaultConfig()
	defaults := []ModelOption{WithStartView(ViewHistory)}
	defaults = append(defaults, opts...)
	m := NewModel(cfg, defaults...)
	m.width = 120
	m.height = 40
	return m
}

func sampleDailyStats() []DailyStatsRow {
	return []DailyStatsRow{
		{
			Date: "2026-02-20", TotalCost: 12.50,
			TokenInput: 50000, TokenOutput: 20000,
			TokenCacheRead: 10000, TokenCacheWrite: 5000,
			SessionCount: 5, APIRequests: 100, APIErrors: 3,
			LinesAdded: 200, LinesRemoved: 50, Commits: 4, PRsOpened: 1,
			CacheEfficiency: 0.85, CacheSavingsUSD: 2.10,
			ErrorRate: 0.03, RetryRate: 0.01,
			AvgAPILatency: 0.5, LatencyP50: 0.3, LatencyP95: 1.2, LatencyP99: 2.5,
			ModelBreakdown: []stats.ModelStats{
				{Model: "claude-opus-4", TotalCost: 10.0, TotalTokens: 60000},
			},
			TopTools: []stats.ToolUsage{
				{ToolName: "Read", Count: 50},
			},
			ToolPerformance: []stats.ToolPerf{
				{ToolName: "Read", AvgDurationMS: 15.0, P95DurationMS: 45.0},
			},
			ErrorCategories:   map[string]int{"rate_limit": 2, "timeout": 1},
			LanguageBreakdown: map[string]int{"Go": 100, "Python": 50},
			DecisionSources:   map[string]int{"user": 5},
			MCPToolUsage:      map[string]int{"pal:chat": 3},
		},
		{
			Date: "2026-02-19", TotalCost: 8.00,
			TokenInput: 30000, TokenOutput: 15000,
			SessionCount: 3, APIRequests: 60, APIErrors: 0,
			LinesAdded: 100, LinesRemoved: 30, Commits: 2,
			CacheEfficiency: 0.90, ErrorRate: 0.0,
			AvgAPILatency: 0.4, LatencyP50: 0.25, LatencyP95: 0.9, LatencyP99: 1.8,
		},
		{
			Date: "2026-02-18", TotalCost: 5.25,
			TokenInput: 20000, TokenOutput: 10000,
			SessionCount: 2, APIRequests: 40, APIErrors: 1,
			LinesAdded: 80, LinesRemoved: 20, Commits: 1,
			IsLegacy: true, // legacy row with no performance data
		},
	}
}

func sampleBurnSummaries() []BurnRateDailySummary {
	return []BurnRateDailySummary{
		{Date: "2026-02-20", AvgHourlyRate: 1.50, PeakHourlyRate: 3.00, AvgTokenVelocity: 500, DailyProjection: 36.00, MonthlyProjection: 1080.00, SnapshotCount: 48},
		{Date: "2026-02-19", AvgHourlyRate: 1.20, PeakHourlyRate: 2.50, AvgTokenVelocity: 400, DailyProjection: 28.80, MonthlyProjection: 864.00, SnapshotCount: 40},
	}
}

func sampleAlerts() []AlertHistoryRow {
	return []AlertHistoryRow{
		{Rule: "cost_spike", Severity: "warning", Message: "Cost spike detected: $5.00 in 1hr", SessionID: "sess-1", FiredAt: time.Date(2026, 2, 20, 14, 30, 0, 0, time.UTC)},
		{Rule: "error_rate", Severity: "critical", Message: "Error rate exceeded 10%", SessionID: "sess-2", FiredAt: time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)},
		{Rule: "cost_spike", Severity: "warning", Message: "Another cost spike", SessionID: "", FiredAt: time.Date(2026, 2, 19, 16, 0, 0, 0, time.UTC)},
	}
}

func sendKey(m Model, k string) Model {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	result, _ := m.Update(msg)
	return result.(Model)
}

func sendSpecialKey(m Model, k tea.KeyType) Model {
	msg := tea.KeyMsg{Type: k}
	result, _ := m.Update(msg)
	return result.(Model)
}

// --- v63.9: HistoryProvider interface + Model fields ---

func TestHistoryProvider_Interface(t *testing.T) {
	mock := &mockHistoryProvider{}
	m := newHistoryModel(WithHistoryProvider(mock))

	if m.history == nil {
		t.Fatal("history provider should be set via WithHistoryProvider")
	}
	if m.historySection != 0 {
		t.Errorf("default historySection should be 0, got %d", m.historySection)
	}
	if m.historyCursor != 0 {
		t.Errorf("default historyCursor should be 0, got %d", m.historyCursor)
	}
	if m.historyGranularity != "daily" {
		t.Errorf("default historyGranularity should be 'daily', got %q", m.historyGranularity)
	}
	if m.historyAlertFilter != "" {
		t.Errorf("default historyAlertFilter should be empty, got %q", m.historyAlertFilter)
	}
}

// --- v63.10: Sub-tab navigation ---

func TestHistorySubTabNavigation(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	tests := []struct {
		key      string
		expected int
	}{
		{"1", 0},
		{"2", 1},
		{"3", 2},
		{"4", 3},
		{"1", 0}, // back to first
	}
	for _, tt := range tests {
		m = sendKey(m, tt.key)
		if m.historySection != tt.expected {
			t.Errorf("key %q: expected historySection=%d, got %d", tt.key, tt.expected, m.historySection)
		}
		if m.historyCursor != 0 {
			t.Errorf("key %q: cursor should reset to 0 after tab switch, got %d", tt.key, m.historyCursor)
		}
	}
}

func TestHistoryNumberKeys_IgnoredOutsideHistory(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.view = ViewDashboard

	m = sendKey(m, "1")
	if m.historySection != 0 {
		t.Error("number keys should not change historySection outside ViewHistory")
	}
}

func TestHistoryGranularity_PersistsAcrossTabs(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	// Set weekly granularity
	m = sendKey(m, "w")
	if m.historyGranularity != "weekly" {
		t.Fatalf("expected weekly, got %q", m.historyGranularity)
	}

	// Switch to Performance tab and back
	m = sendKey(m, "2")
	m = sendKey(m, "1")

	if m.historyGranularity != "weekly" {
		t.Errorf("granularity should persist across tab switches, got %q", m.historyGranularity)
	}
}

func TestHistoryGranularity_IgnoredOnAlerts(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats(), alertHistory: sampleAlerts()}
	m := newHistoryModel(WithHistoryProvider(mock))

	m = sendKey(m, "4") // switch to Alerts
	m = sendKey(m, "w") // try to change granularity

	if m.historyGranularity != "daily" {
		t.Errorf("granularity changes should be ignored on Alerts tab, got %q", m.historyGranularity)
	}
}

func TestHistoryGranularity_AllModes(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	tests := []struct {
		key      string
		expected string
	}{
		{"d", "daily"},
		{"w", "weekly"},
		{"m", "monthly"},
		{"d", "daily"}, // back to daily
		{"W", "weekly"},  // uppercase
		{"M", "monthly"}, // uppercase
		{"D", "daily"},   // uppercase
	}
	for _, tt := range tests {
		m = sendKey(m, tt.key)
		if m.historyGranularity != tt.expected {
			t.Errorf("key %q: expected granularity=%q, got %q", tt.key, tt.expected, m.historyGranularity)
		}
	}
}

func TestHistoryCursor_UpDown(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	m = sendSpecialKey(m, tea.KeyDown)
	if m.historyCursor != 1 {
		t.Errorf("down: expected cursor=1, got %d", m.historyCursor)
	}

	m = sendSpecialKey(m, tea.KeyDown)
	if m.historyCursor != 2 {
		t.Errorf("down: expected cursor=2, got %d", m.historyCursor)
	}

	m = sendSpecialKey(m, tea.KeyUp)
	if m.historyCursor != 1 {
		t.Errorf("up: expected cursor=1, got %d", m.historyCursor)
	}

	// Cursor should not go below 0.
	m = sendSpecialKey(m, tea.KeyUp)
	m = sendSpecialKey(m, tea.KeyUp)
	if m.historyCursor != 0 {
		t.Errorf("up below 0: expected cursor=0, got %d", m.historyCursor)
	}
}

// --- v63.12: Header rendering ---

func TestHistoryHeader_SubTabLabels(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	header := m.renderHistoryHeader()

	for _, label := range []string{"Overview", "Performance", "Burn Rate", "Alerts"} {
		if !strings.Contains(header, label) {
			t.Errorf("header should contain %q", label)
		}
	}
	for _, key := range []string{"[1]", "[2]", "[3]", "[4]"} {
		if !strings.Contains(header, key) {
			t.Errorf("header should contain %q", key)
		}
	}
}

func TestHistoryHeader_GranularityIndicators(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	header := m.renderHistoryHeader()

	// On tabs 0-2, granularity indicators should be visible.
	if !strings.Contains(header, "[D]") {
		t.Error("header should contain daily granularity indicator [D]")
	}
	if !strings.Contains(header, "[W]") {
		t.Error("header should contain weekly granularity indicator [W]")
	}
	if !strings.Contains(header, "[M]") {
		t.Error("header should contain monthly granularity indicator [M]")
	}
}

func TestHistoryHeader_AlertsTabShowsFilter(t *testing.T) {
	mock := &mockHistoryProvider{alertHistory: sampleAlerts()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 3

	header := m.renderHistoryHeader()

	if !strings.Contains(header, "/:Filter") {
		t.Error("Alerts tab header should show '/:Filter'")
	}
	// Should NOT show granularity on Alerts tab.
	if strings.Contains(header, "[D]aily") {
		t.Error("Alerts tab header should not show granularity indicators")
	}
}

// --- v63.13: Table rendering ---

func TestHistoryOverview_RendersColumns(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	view := m.renderHistoryOverview()

	expectedColumns := []string{"Date", "Cost", "Tokens", "Sessions", "API Reqs", "Errors", "Lines+", "Lines-", "Commits"}
	for _, col := range expectedColumns {
		if !strings.Contains(view, col) {
			t.Errorf("Overview should contain column %q", col)
		}
	}
	if !strings.Contains(view, "2026-02-20") {
		t.Error("Overview should contain date '2026-02-20'")
	}
	if !strings.Contains(view, "12.50") {
		t.Errorf("Overview should contain cost '12.50', got:\n%s", view)
	}
}

func TestHistoryPerformance_RendersColumns(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 1

	view := m.renderHistoryPerformance()

	expectedColumns := []string{"Cache%", "Err Rate", "Avg Lat", "P50", "P95", "P99", "Retries", "Cache $"}
	for _, col := range expectedColumns {
		if !strings.Contains(view, col) {
			t.Errorf("Performance should contain column %q", col)
		}
	}
}

func TestHistoryPerformance_LegacyShowsDashes(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 1

	view := m.renderHistoryPerformance()

	// Row for 2026-02-18 is legacy, should show "--".
	if !strings.Contains(view, "--") {
		t.Error("Legacy rows should display '--' for unavailable performance metrics")
	}
}

func TestHistoryBurnRate_RendersColumns(t *testing.T) {
	mock := &mockHistoryProvider{burnSummaries: sampleBurnSummaries()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 2

	view := m.renderHistoryBurnRate()

	expectedColumns := []string{"Avg $/hr", "Peak $/hr", "Tokens/min", "Daily $", "Monthly $"}
	for _, col := range expectedColumns {
		if !strings.Contains(view, col) {
			t.Errorf("Burn Rate should contain column %q", col)
		}
	}
	if !strings.Contains(view, "2026-02-20") {
		t.Error("Burn Rate should contain date '2026-02-20'")
	}
	if !strings.Contains(view, "1.50") {
		t.Errorf("Burn Rate should contain avg rate '1.50', got:\n%s", view)
	}
}

func TestHistoryAlerts_RendersColumns(t *testing.T) {
	mock := &mockHistoryProvider{alertHistory: sampleAlerts()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 3

	view := m.renderHistoryAlerts()

	expectedColumns := []string{"Time", "Rule", "Severity", "Session", "Message"}
	for _, col := range expectedColumns {
		if !strings.Contains(view, col) {
			t.Errorf("Alerts should contain column %q", col)
		}
	}
	if !strings.Contains(view, "cost_spike") {
		t.Error("Alerts should contain rule 'cost_spike'")
	}
	if !strings.Contains(view, "warning") {
		t.Error("Alerts should contain severity 'warning'")
	}
}

func TestHistoryAlerts_GlobalSession(t *testing.T) {
	mock := &mockHistoryProvider{alertHistory: sampleAlerts()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 3

	view := m.renderHistoryAlerts()

	if !strings.Contains(view, "(global)") {
		t.Error("Alerts with empty session should display '(global)'")
	}
}

// --- v63.13: Empty state ---

func TestHistoryNilProvider(t *testing.T) {
	m := newHistoryModel() // no HistoryProvider
	view := m.renderHistory()

	if !strings.Contains(view, "persistence is disabled") {
		t.Errorf("nil provider should show 'persistence is disabled', got:\n%s", view)
	}
}

func TestHistoryEmptyState_Overview(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: nil}
	m := newHistoryModel(WithHistoryProvider(mock))

	view := m.renderHistoryOverview()

	if !strings.Contains(view, "No daily statistics yet") {
		t.Errorf("empty overview should show empty state message, got:\n%s", view)
	}
}

func TestHistoryEmptyState_BurnRate(t *testing.T) {
	mock := &mockHistoryProvider{burnSummaries: nil}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 2

	view := m.renderHistoryBurnRate()

	if !strings.Contains(view, "No burn rate data yet") {
		t.Errorf("empty burn rate should show empty state message, got:\n%s", view)
	}
}

func TestHistoryEmptyState_Alerts(t *testing.T) {
	mock := &mockHistoryProvider{alertHistory: nil}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 3

	view := m.renderHistoryAlerts()

	if !strings.Contains(view, "No alerts recorded yet") {
		t.Errorf("empty alerts should show empty state message, got:\n%s", view)
	}
}

// --- v63.13: Aggregation (weekly/monthly) ---

func TestHistoryOverview_WeeklyAggregation(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historyGranularity = "weekly"

	view := m.renderHistoryOverview()

	if !strings.Contains(view, "Week") {
		t.Error("weekly view should contain 'Week' column header")
	}
}

func TestHistoryOverview_MonthlyAggregation(t *testing.T) {
	stats := []DailyStatsRow{
		{Date: "2026-02-20", TotalCost: 10.0, TokenInput: 50000, TokenOutput: 20000, SessionCount: 5, APIRequests: 100, APIErrors: 3, LinesAdded: 200, LinesRemoved: 50, Commits: 4},
		{Date: "2026-01-15", TotalCost: 8.0, TokenInput: 40000, TokenOutput: 15000, SessionCount: 3, APIRequests: 60, APIErrors: 1, LinesAdded: 100, LinesRemoved: 30, Commits: 2},
	}
	mock := &mockHistoryProvider{dailyStats: stats}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historyGranularity = "monthly"

	view := m.renderHistoryOverview()

	if !strings.Contains(view, "Month") {
		t.Error("monthly view should contain 'Month' column header")
	}
	if !strings.Contains(view, "2026-02") {
		t.Error("monthly view should contain '2026-02'")
	}
	if !strings.Contains(view, "2026-01") {
		t.Error("monthly view should contain '2026-01'")
	}
}

func TestHistoryPerf_WeeklyAggregatesAverages(t *testing.T) {
	stats := []DailyStatsRow{
		{Date: "2026-02-16", CacheEfficiency: 0.80, ErrorRate: 0.02, AvgAPILatency: 0.5, LatencyP50: 0.3, LatencyP95: 1.0, LatencyP99: 2.0, RetryRate: 0.01, CacheSavingsUSD: 1.00},
		{Date: "2026-02-17", CacheEfficiency: 0.90, ErrorRate: 0.04, AvgAPILatency: 0.7, LatencyP50: 0.4, LatencyP95: 1.4, LatencyP99: 2.8, RetryRate: 0.03, CacheSavingsUSD: 2.00},
	}
	result := aggregatePerfWeekly(stats)

	if len(result) != 1 {
		t.Fatalf("expected 1 weekly group, got %d", len(result))
	}
	// Cache efficiency should be averaged: (0.80 + 0.90) / 2 = 0.85
	if result[0].cacheEff < 0.849 || result[0].cacheEff > 0.851 {
		t.Errorf("expected avg cacheEff ~0.85, got %f", result[0].cacheEff)
	}
	// Cache savings should be summed (total savings for the period), not averaged.
	// 1.00 + 2.00 = 3.00
	if result[0].cacheSave < 2.99 || result[0].cacheSave > 3.01 {
		t.Errorf("expected summed cacheSave ~3.00, got %f", result[0].cacheSave)
	}
}

func TestHistoryPerf_LegacyExcludedFromAggregation(t *testing.T) {
	stats := []DailyStatsRow{
		{Date: "2026-02-16", CacheEfficiency: 0.80, IsLegacy: false},
		{Date: "2026-02-17", IsLegacy: true}, // should be excluded from averages
	}
	result := aggregatePerfWeekly(stats)

	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	if result[0].isLegacy {
		t.Error("group with at least one non-legacy row should not be marked legacy")
	}
	// Only the non-legacy row counts: cacheEff should be 0.80, not 0.40.
	if result[0].cacheEff < 0.799 || result[0].cacheEff > 0.801 {
		t.Errorf("expected cacheEff=0.80 (legacy excluded), got %f", result[0].cacheEff)
	}
}

func TestHistoryPerf_AllLegacyGroupMarkedLegacy(t *testing.T) {
	stats := []DailyStatsRow{
		{Date: "2026-02-16", IsLegacy: true},
		{Date: "2026-02-17", IsLegacy: true},
	}
	result := aggregatePerfWeekly(stats)

	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	if !result[0].isLegacy {
		t.Error("group with all legacy rows should be marked legacy")
	}
}

func TestHistoryBurn_WeeklyAggregation(t *testing.T) {
	summaries := []BurnRateDailySummary{
		{Date: "2026-02-16", AvgHourlyRate: 1.00, PeakHourlyRate: 2.00, AvgTokenVelocity: 400, DailyProjection: 24.00, MonthlyProjection: 720.00, SnapshotCount: 48},
		{Date: "2026-02-17", AvgHourlyRate: 2.00, PeakHourlyRate: 5.00, AvgTokenVelocity: 600, DailyProjection: 48.00, MonthlyProjection: 1440.00, SnapshotCount: 48},
	}
	result := aggregateBurnWeekly(summaries)

	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	// avgRate should be averaged: (1.00 + 2.00) / 2 = 1.50
	if result[0].avgRate < 1.49 || result[0].avgRate > 1.51 {
		t.Errorf("expected avg rate ~1.50, got %f", result[0].avgRate)
	}
	// peakRate should be max: 5.00
	if result[0].peakRate != 5.00 {
		t.Errorf("expected peak rate 5.00, got %f", result[0].peakRate)
	}
	// dailyProj should be summed: 24.00 + 48.00 = 72.00
	if result[0].dailyProj != 72.00 {
		t.Errorf("expected dailyProj=72.00, got %f", result[0].dailyProj)
	}
}

func TestHistoryBurn_ZeroSnapshotDaysExcluded(t *testing.T) {
	summaries := []BurnRateDailySummary{
		{Date: "2026-02-16", AvgHourlyRate: 2.00, PeakHourlyRate: 4.00, AvgTokenVelocity: 500, DailyProjection: 48.00, MonthlyProjection: 1440.00, SnapshotCount: 48},
		{Date: "2026-02-17", AvgHourlyRate: 0.00, PeakHourlyRate: 0.00, AvgTokenVelocity: 0, DailyProjection: 0, MonthlyProjection: 0, SnapshotCount: 0},
	}
	result := aggregateBurnWeekly(summaries)

	if len(result) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result))
	}
	// Only the active day should count: avgRate should be 2.00, not 1.00.
	if result[0].avgRate < 1.99 || result[0].avgRate > 2.01 {
		t.Errorf("expected avg rate 2.00 (zero-snapshot excluded), got %f", result[0].avgRate)
	}
}

// --- v63.14: Detail overlays ---

func TestHistoryOverviewDetail_Daily(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	m, _ = m.openOverviewDetail()

	if !m.detailOverlay {
		t.Fatal("detail overlay should be active")
	}
	if !strings.Contains(m.detailTitle, "Overview Detail") {
		t.Errorf("detail title should contain 'Overview Detail', got %q", m.detailTitle)
	}
	if !strings.Contains(m.detailContent, "2026-02-20") {
		t.Error("detail should contain date")
	}
	if !strings.Contains(m.detailContent, "12.50") {
		t.Error("detail should contain cost")
	}
	if !strings.Contains(m.detailContent, "Model Breakdown") {
		t.Error("detail should contain Model Breakdown section")
	}
	if !strings.Contains(m.detailContent, "claude-opus-4") {
		t.Error("detail should contain model name")
	}
	if !strings.Contains(m.detailContent, "Tool Usage") {
		t.Error("detail should contain Tool Usage section")
	}
	if !strings.Contains(m.detailContent, "Error Categories") {
		t.Error("detail should contain Error Categories section")
	}
	if !strings.Contains(m.detailContent, "Language Breakdown") {
		t.Error("detail should contain Language Breakdown section")
	}
	if !strings.Contains(m.detailContent, "Decision Sources") {
		t.Error("detail should contain Decision Sources section")
	}
}

func TestHistoryOverviewDetail_WeeklyShowsMiniTable(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historyGranularity = "weekly"

	m, _ = m.openOverviewDetail()

	if !m.detailOverlay {
		t.Fatal("detail overlay should be active")
	}
	if !strings.Contains(m.detailContent, "Period:") {
		t.Error("weekly detail should show 'Period:' header")
	}
}

func TestHistoryPerformanceDetail(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 1

	m, _ = m.openPerformanceDetail()

	if !m.detailOverlay {
		t.Fatal("detail overlay should be active")
	}
	if !strings.Contains(m.detailTitle, "Performance Detail") {
		t.Errorf("detail title should contain 'Performance Detail', got %q", m.detailTitle)
	}
	if !strings.Contains(m.detailContent, "Model Breakdown") {
		t.Error("detail should contain Model Breakdown")
	}
	if !strings.Contains(m.detailContent, "Top Tools") {
		t.Error("detail should contain Top Tools section")
	}
	if !strings.Contains(m.detailContent, "Error Categories") {
		t.Error("detail should contain Error Categories")
	}
	if !strings.Contains(m.detailContent, "MCP Tool Usage") {
		t.Error("detail should contain MCP Tool Usage")
	}
}

func TestHistoryBurnRateDetail(t *testing.T) {
	snapshots := []BurnRateSnapshotRow{
		{Timestamp: time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC), TotalCost: 5.00, HourlyRate: 1.50, Trend: burnrate.TrendUp, TokenVelocity: 500},
		{Timestamp: time.Date(2026, 2, 20, 10, 5, 0, 0, time.UTC), TotalCost: 5.10, HourlyRate: 1.40, Trend: burnrate.TrendDown, TokenVelocity: 480},
	}
	mock := &mockHistoryProvider{
		burnSummaries: sampleBurnSummaries(),
		burnSnapshots: snapshots,
	}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 2

	m, _ = m.openBurnRateDetail()

	if !m.detailOverlay {
		t.Fatal("detail overlay should be active")
	}
	if !strings.Contains(m.detailTitle, "Burn Rate Detail") {
		t.Errorf("detail title should contain 'Burn Rate Detail', got %q", m.detailTitle)
	}
	if !strings.Contains(m.detailContent, "Intra-day Snapshots") {
		t.Error("detail should contain 'Intra-day Snapshots'")
	}
	if !strings.Contains(m.detailContent, "up") {
		t.Error("detail should contain trend direction 'up'")
	}
}

func TestHistoryBurnRateDetail_NoSnapshots(t *testing.T) {
	mock := &mockHistoryProvider{
		burnSummaries: sampleBurnSummaries(),
		burnSnapshots: nil,
	}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 2

	m, _ = m.openBurnRateDetail()

	if !strings.Contains(m.detailContent, "No intra-day snapshots") {
		t.Error("detail should show 'No intra-day snapshots' when none available")
	}
}

func TestHistoryAlertDetail(t *testing.T) {
	mock := &mockHistoryProvider{alertHistory: sampleAlerts()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 3

	m, _ = m.openAlertDetail()

	if !m.detailOverlay {
		t.Fatal("detail overlay should be active")
	}
	if !strings.Contains(m.detailTitle, "Alert Detail") {
		t.Errorf("detail title should contain 'Alert Detail', got %q", m.detailTitle)
	}
	if !strings.Contains(m.detailContent, "cost_spike") {
		t.Error("detail should contain rule name")
	}
	if !strings.Contains(m.detailContent, "warning") {
		t.Error("detail should contain severity")
	}
	if !strings.Contains(m.detailContent, "Cost spike detected") {
		t.Error("detail should contain full message")
	}
}

func TestHistoryDetail_EmptyData(t *testing.T) {
	mock := &mockHistoryProvider{}
	m := newHistoryModel(WithHistoryProvider(mock))

	m, _ = m.openHistoryDetail()

	if m.detailOverlay {
		t.Error("detail overlay should not open when there is no data")
	}
}

func TestHistoryDetail_NilProvider(t *testing.T) {
	m := newHistoryModel()

	m, _ = m.openHistoryDetail()

	if m.detailOverlay {
		t.Error("detail overlay should not open with nil provider")
	}
}

func TestBackspace_ClosesDetailOverlay(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	// Open detail.
	m, _ = m.openOverviewDetail()
	if !m.detailOverlay {
		t.Fatal("detail should be open")
	}

	// Press backspace.
	m = sendSpecialKey(m, tea.KeyBackspace)

	if m.detailOverlay {
		t.Error("backspace should close detail overlay")
	}
}

func TestEscape_ClosesDetailOverlay(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	m, _ = m.openOverviewDetail()
	if !m.detailOverlay {
		t.Fatal("detail should be open")
	}

	m = sendSpecialKey(m, tea.KeyEscape)

	if m.detailOverlay {
		t.Error("escape should close detail overlay")
	}
}

func TestEnter_OpensAndClosesDetail(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	// Open via Enter key.
	m = sendSpecialKey(m, tea.KeyEnter)
	if !m.detailOverlay {
		t.Fatal("enter should open detail overlay")
	}

	// Close via Enter again (while in detail view).
	m = sendSpecialKey(m, tea.KeyEnter)
	if m.detailOverlay {
		t.Error("enter should close detail overlay when already open")
	}
}

// --- v63.15: Alert filter menu ---

func TestHistorySlashKey_OpensFilterMenu(t *testing.T) {
	mock := &mockHistoryProvider{alertHistory: sampleAlerts()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 3 // Alerts tab

	m = sendKey(m, "/")

	if !m.historyFilterMenu.Active {
		t.Fatal("/ should open the alert filter menu on Alerts tab")
	}
	// Should have "All" + distinct rules.
	if len(m.historyFilterMenu.Options) < 2 {
		t.Errorf("filter menu should have at least 2 options (All + rules), got %d", len(m.historyFilterMenu.Options))
	}
	if m.historyFilterMenu.Options[0].Label != "All" {
		t.Errorf("first option should be 'All', got %q", m.historyFilterMenu.Options[0].Label)
	}
}

func TestHistorySlashKey_IgnoredOnNonAlertsTabs(t *testing.T) {
	mock := &mockHistoryProvider{dailyStats: sampleDailyStats()}
	m := newHistoryModel(WithHistoryProvider(mock))

	for _, section := range []int{0, 1, 2} {
		m.historySection = section
		m = sendKey(m, "/")
		if m.historyFilterMenu.Active {
			t.Errorf("/ should not open filter menu on section %d", section)
		}
	}
}

func TestHistoryFilterMenu_SelectRule(t *testing.T) {
	mock := &mockHistoryProvider{alertHistory: sampleAlerts()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 3

	// Open filter menu.
	m = sendKey(m, "/")
	if !m.historyFilterMenu.Active {
		t.Fatal("filter menu should be active")
	}

	// Move down to select a specific rule.
	m = sendSpecialKey(m, tea.KeyDown)
	m = sendSpecialKey(m, tea.KeyEnter)

	if m.historyFilterMenu.Active {
		t.Error("filter menu should close after selection")
	}
	if m.historyAlertFilter == "" {
		t.Error("alert filter should be set after selection")
	}
	if m.historyCursor != 0 {
		t.Error("cursor should reset to 0 after filter change")
	}
}

func TestHistoryFilterMenu_EscapeDismisses(t *testing.T) {
	mock := &mockHistoryProvider{alertHistory: sampleAlerts()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 3

	m = sendKey(m, "/")
	if !m.historyFilterMenu.Active {
		t.Fatal("filter menu should be active")
	}

	m = sendSpecialKey(m, tea.KeyEscape)

	if m.historyFilterMenu.Active {
		t.Error("escape should dismiss the filter menu")
	}
}

func TestHistoryFilterMenu_SelectAll(t *testing.T) {
	mock := &mockHistoryProvider{alertHistory: sampleAlerts()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 3
	m.historyAlertFilter = "cost_spike" // start with a filter active

	m = sendKey(m, "/")
	// First option is "All" with key="".
	m = sendSpecialKey(m, tea.KeyEnter)

	if m.historyAlertFilter != "" {
		t.Errorf("selecting 'All' should clear filter, got %q", m.historyAlertFilter)
	}
}

func TestHistoryFilterMenu_OverlayRendered(t *testing.T) {
	mock := &mockHistoryProvider{alertHistory: sampleAlerts()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 3
	m.openHistoryAlertFilterMenu()

	view := m.renderHistory()

	if !strings.Contains(view, "Alert Rule Filter") {
		t.Error("filter menu overlay should contain 'Alert Rule Filter' title")
	}
}

func TestHistoryFilterMenu_DistinctRules(t *testing.T) {
	mock := &mockHistoryProvider{alertHistory: sampleAlerts()}
	m := newHistoryModel(WithHistoryProvider(mock))
	m.historySection = 3

	m.openHistoryAlertFilterMenu()

	// sampleAlerts has 2 distinct rules: cost_spike and error_rate.
	// Options should be: All + cost_spike + error_rate = 3.
	if len(m.historyFilterMenu.Options) != 3 {
		t.Errorf("expected 3 filter options, got %d", len(m.historyFilterMenu.Options))
	}
}

// --- Tab cycling ---

func TestHistoryTab_CyclesToDashboard(t *testing.T) {
	m := newHistoryModel()

	m = sendSpecialKey(m, tea.KeyTab)

	if m.view != ViewDashboard {
		t.Errorf("Tab from History should cycle to Dashboard, got view=%d", m.view)
	}
}

// --- Cursor clamping ---

func TestClampHistoryCursor(t *testing.T) {
	m := newHistoryModel()

	m.historyCursor = 100
	m.clampHistoryCursor(3)
	if m.historyCursor != 2 {
		t.Errorf("cursor should be clamped to 2, got %d", m.historyCursor)
	}

	m.historyCursor = -1
	m.clampHistoryCursor(3)
	if m.historyCursor != 0 {
		t.Errorf("negative cursor should be clamped to 0, got %d", m.historyCursor)
	}

	m.historyCursor = 5
	m.clampHistoryCursor(0)
	if m.historyCursor != 0 {
		t.Errorf("cursor should be 0 for empty list, got %d", m.historyCursor)
	}
}

// --- Week label helper ---

func TestWeekLabelForDate(t *testing.T) {
	label := weekLabelForDate("2026-02-16")
	if !strings.HasPrefix(label, "Week 2026-") {
		t.Errorf("expected 'Week 2026-XX', got %q", label)
	}

	// Invalid date should fallback to first 7 chars.
	label = weekLabelForDate("not-a-date-at-all")
	if label != "not-a-d" {
		t.Errorf("expected 'not-a-d' fallback, got %q", label)
	}
}

// --- Query days mapping ---

func TestHistoryQueryDays(t *testing.T) {
	m := newHistoryModel()

	m.historyGranularity = "daily"
	if d := m.historyQueryDays(); d != 7 {
		t.Errorf("daily: expected 7, got %d", d)
	}

	m.historyGranularity = "weekly"
	if d := m.historyQueryDays(); d != 28 {
		t.Errorf("weekly: expected 28, got %d", d)
	}

	m.historyGranularity = "monthly"
	if d := m.historyQueryDays(); d != 90 {
		t.Errorf("monthly: expected 90, got %d", d)
	}
}

// --- Visible range ---

func TestVisibleRange(t *testing.T) {
	m := newHistoryModel()
	m.height = 20 // visibleH = 20 - 6 = 14

	start, end := m.visibleRange(5)
	if start != 0 || end != 5 {
		t.Errorf("5 rows in 14-height: expected range [0,5), got [%d,%d)", start, end)
	}

	m.historyCursor = 3
	start, end = m.visibleRange(50)
	if start > 3 || end <= 3 {
		t.Errorf("cursor=3: should be within [%d,%d)", start, end)
	}
}
