package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nixlim/cc-top/internal/alerts"
	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/events"
	"github.com/nixlim/cc-top/internal/scanner"
	"github.com/nixlim/cc-top/internal/state"
	"github.com/nixlim/cc-top/internal/stats"
)


type mockStateProvider struct {
	sessions []state.SessionData
}

func (m *mockStateProvider) GetSession(id string) *state.SessionData {
	for i := range m.sessions {
		if m.sessions[i].SessionID == id {
			cp := m.sessions[i]
			return &cp
		}
	}
	return nil
}

func (m *mockStateProvider) ListSessions() []state.SessionData {
	return m.sessions
}

func (m *mockStateProvider) GetAggregatedCost() float64 {
	var total float64
	for _, s := range m.sessions {
		total += s.TotalCost
	}
	return total
}

func (m *mockStateProvider) QueryDailySummaries(_ int) []state.DailySummary {
	return nil
}
func (m *mockStateProvider) DroppedWrites() int64 { return 0 }

type mockBurnRateProvider struct {
	global  burnrate.BurnRate
	perSess map[string]burnrate.BurnRate
}

func (m *mockBurnRateProvider) Get(sessionID string) burnrate.BurnRate {
	if br, ok := m.perSess[sessionID]; ok {
		return br
	}
	return burnrate.BurnRate{}
}

func (m *mockBurnRateProvider) GetGlobal() burnrate.BurnRate {
	return m.global
}

type mockEventProvider struct {
	events []events.FormattedEvent
}

func (m *mockEventProvider) Recent(limit int) []events.FormattedEvent {
	if limit > len(m.events) {
		limit = len(m.events)
	}
	return m.events[:limit]
}

func (m *mockEventProvider) RecentForSession(sessionID string, limit int) []events.FormattedEvent {
	var result []events.FormattedEvent
	for _, e := range m.events {
		if e.SessionID == sessionID {
			result = append(result, e)
		}
	}
	if limit > len(result) {
		limit = len(result)
	}
	return result[:limit]
}

type mockAlertProvider struct {
	alerts []alerts.Alert
}

func (m *mockAlertProvider) Active() []alerts.Alert {
	return m.alerts
}

func (m *mockAlertProvider) ActiveForSession(sessionID string) []alerts.Alert {
	var result []alerts.Alert
	for _, a := range m.alerts {
		if a.SessionID == sessionID || a.SessionID == "" {
			result = append(result, a)
		}
	}
	return result
}

type mockStatsProvider struct {
	global  stats.DashboardStats
	perSess map[string]stats.DashboardStats
}

func (m *mockStatsProvider) Get(sessionID string) stats.DashboardStats {
	if ds, ok := m.perSess[sessionID]; ok {
		return ds
	}
	return stats.DashboardStats{}
}

func (m *mockStatsProvider) GetGlobal() stats.DashboardStats {
	return m.global
}


func TestComputeDimensions_LargeTerminal(t *testing.T) {
	dims := computeDimensions(120, 40)

	if dims.sessionListW < 40 || dims.sessionListW > 60 {
		t.Errorf("sessionListW = %d, want ~48", dims.sessionListW)
	}

	if dims.burnRateW < 50 {
		t.Errorf("burnRateW = %d, want >= 50", dims.burnRateW)
	}

	if dims.sessionListH <= 0 {
		t.Errorf("sessionListH = %d, want > 0", dims.sessionListH)
	}
	if dims.burnRateH <= 0 {
		t.Errorf("burnRateH = %d, want > 0", dims.burnRateH)
	}
	if dims.eventStreamH <= 0 {
		t.Errorf("eventStreamH = %d, want > 0", dims.eventStreamH)
	}
	if dims.alertsH <= 0 {
		t.Errorf("alertsH = %d, want > 0", dims.alertsH)
	}

	rightH := dims.burnRateH + dims.eventStreamH
	if rightH != dims.sessionListH {
		t.Errorf("burnRateH(%d) + eventStreamH(%d) = %d, want sessionListH = %d",
			dims.burnRateH, dims.eventStreamH, rightH, dims.sessionListH)
	}
	totalH := dims.headerH + dims.sessionListH + dims.alertsH
	if totalH != 40 {
		t.Errorf("headerH(%d) + sessionListH(%d) + alertsH(%d) = %d, want 40",
			dims.headerH, dims.sessionListH, dims.alertsH, totalH)
	}
}

func TestComputeDimensions_SmallTerminal(t *testing.T) {
	dims := computeDimensions(80, 24)

	if dims.sessionListW <= 0 {
		t.Errorf("sessionListW = %d, want > 0", dims.sessionListW)
	}
	if dims.burnRateW <= 0 {
		t.Errorf("burnRateW = %d, want > 0", dims.burnRateW)
	}
}

func TestComputeDimensions_MinimumTerminal(t *testing.T) {
	dims := computeDimensions(20, 8)

	if dims.sessionListW <= 0 {
		t.Errorf("sessionListW = %d, want > 0", dims.sessionListW)
	}
	if dims.eventStreamH < 3 {
		t.Errorf("eventStreamH = %d, want >= 3", dims.eventStreamH)
	}
}

func TestModel_Init(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg)

	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() returned nil cmd, want tick command")
	}
}

func TestModel_ViewStartup(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewStartup))
	m.width = 120
	m.height = 40

	view := m.View()
	if !strings.Contains(view, "cc-top") {
		t.Error("startup view should contain 'cc-top'")
	}
	if !strings.Contains(view, "No Claude Code instances found") {
		t.Error("startup view with no scanner should show 'No Claude Code instances found'")
	}
}

func TestModel_ViewDashboard(t *testing.T) {
	cfg := config.DefaultConfig()
	mockState := &mockStateProvider{
		sessions: []state.SessionData{
			{
				SessionID:   "sess-001",
				PID:         1234,
				Terminal:    "iTerm2",
				CWD:         "/Users/test/project",
				Model:       "sonnet-4.5",
				TotalCost:   1.50,
				TotalTokens: 50000,
				ActiveTime:  10 * time.Minute,
				LastEventAt: time.Now(),
				StartedAt:   time.Now().Add(-30 * time.Minute),
			},
		},
	}

	m := NewModel(cfg,
		WithStartView(ViewDashboard),
		WithStateProvider(mockState),
	)
	m.width = 120
	m.height = 40

	view := m.View()
	if !strings.Contains(view, "Sessions") {
		t.Error("dashboard view should contain 'Sessions' panel")
	}
	if !strings.Contains(view, "Burn Rate") {
		t.Error("dashboard view should contain 'Burn Rate' panel")
	}
	if !strings.Contains(view, "Events") {
		t.Error("dashboard view should contain 'Events' panel")
	}
}

func TestModel_ViewStats(t *testing.T) {
	cfg := config.DefaultConfig()
	mockStats := &mockStatsProvider{
		global: stats.DashboardStats{
			LinesAdded:   100,
			LinesRemoved: 50,
			Commits:      5,
			PRs:          1,
		},
	}

	m := NewModel(cfg,
		WithStartView(ViewStats),
		WithStatsProvider(mockStats),
	)
	m.width = 120
	m.height = 40

	view := m.View()
	if !strings.Contains(view, "Stats") {
		t.Error("stats view should contain 'Stats'")
	}
	if !strings.Contains(view, "Code Metrics") {
		t.Error("stats view should contain 'Code Metrics'")
	}
}

func TestModel_TabToggle(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2 := result.(Model)
	if m2.view != ViewStats {
		t.Errorf("after Tab, view = %d, want ViewStats (%d)", m2.view, ViewStats)
	}

	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyTab})
	m3 := result.(Model)
	if m3.view != ViewHistory {
		t.Errorf("after second Tab, view = %d, want ViewHistory (%d)", m3.view, ViewHistory)
	}

	result, _ = m3.Update(tea.KeyMsg{Type: tea.KeyTab})
	m4 := result.(Model)
	if m4.view != ViewDashboard {
		t.Errorf("after third Tab, view = %d, want ViewDashboard (%d)", m4.view, ViewDashboard)
	}
}

func TestModel_QuitKey(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m2 := result.(Model)
	if !m2.quitting {
		t.Error("after 'q', quitting should be true")
	}
	if cmd == nil {
		t.Error("after 'q', cmd should be non-nil (tea.Quit)")
	}
}

func TestModel_SessionNavigation(t *testing.T) {
	cfg := config.DefaultConfig()
	mockState := &mockStateProvider{
		sessions: []state.SessionData{
			{SessionID: "sess-001", PID: 1, LastEventAt: time.Now()},
			{SessionID: "sess-002", PID: 2, LastEventAt: time.Now()},
			{SessionID: "sess-003", PID: 3, LastEventAt: time.Now()},
		},
	}

	m := NewModel(cfg, WithStartView(ViewDashboard), WithStateProvider(mockState))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2 := result.(Model)
	if m2.sessionCursor != 1 {
		t.Errorf("after Down, sessionCursor = %d, want 1", m2.sessionCursor)
	}

	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := result.(Model)
	if m3.selectedSession != "sess-002" {
		t.Errorf("after Enter, selectedSession = %q, want %q", m3.selectedSession, "sess-002")
	}

	result, _ = m3.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m4 := result.(Model)
	if m4.selectedSession != "" {
		t.Errorf("after Esc, selectedSession = %q, want empty", m4.selectedSession)
	}
}

func TestModel_WindowResize(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewDashboard))

	result, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	m2 := result.(Model)
	if m2.width != 100 {
		t.Errorf("width = %d, want 100", m2.width)
	}
	if m2.height != 50 {
		t.Errorf("height = %d, want 50", m2.height)
	}
}

func TestModel_StartupEnterTransitions(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewStartup))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := result.(Model)
	if m2.view != ViewDashboard {
		t.Errorf("after Enter on startup, view = %d, want ViewDashboard (%d)", m2.view, ViewDashboard)
	}
}

func TestModel_FilterMenu(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m2 := result.(Model)
	if !m2.filterMenu.Active {
		t.Error("after 'f', filter menu should be active")
	}

	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyDown})
	m3 := result.(Model)
	if m3.filterMenu.Cursor != 1 {
		t.Errorf("filter cursor = %d, want 1", m3.filterMenu.Cursor)
	}

	result, _ = m3.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m4 := result.(Model)
	if m4.filterMenu.Active {
		t.Error("after Esc, filter menu should be inactive")
	}
}

func TestModel_ShutdownCallback(t *testing.T) {
	called := false
	cfg := config.DefaultConfig()
	m := NewModel(cfg,
		WithStartView(ViewDashboard),
		WithOnShutdown(func() { called = true }),
	)
	m.width = 120
	m.height = 40

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !called {
		t.Error("onShutdown callback should have been called on quit")
	}
}

func TestModel_RenderWithAllProviders(t *testing.T) {
	cfg := config.DefaultConfig()
	mockState := &mockStateProvider{
		sessions: []state.SessionData{
			{
				SessionID:   "sess-001",
				PID:         1234,
				Terminal:    "iTerm2",
				CWD:         "/Users/test/myproject",
				Model:       "sonnet-4.5",
				TotalCost:   1.50,
				TotalTokens: 50000,
				ActiveTime:  10 * time.Minute,
				LastEventAt: time.Now(),
				StartedAt:   time.Now().Add(-30 * time.Minute),
			},
		},
	}
	boolTrue := true
	mockEvents := &mockEventProvider{
		events: []events.FormattedEvent{
			{
				SessionID: "sess-001",
				EventType: "api_request",
				Formatted: "[sess-001] sonnet-4.5 -> 2.1k in / 890 out ($0.03) 4.2s",
				Timestamp: time.Now(),
				Success:   &boolTrue,
			},
		},
	}
	mockAlerts := &mockAlertProvider{
		alerts: []alerts.Alert{
			{
				Rule:      "CostSurge",
				Severity:  "warning",
				Message:   "Cost rate exceeds $2/hr",
				SessionID: "sess-001",
				FiredAt:   time.Now(),
			},
		},
	}
	mockBR := &mockBurnRateProvider{
		global: burnrate.BurnRate{
			TotalCost:     1.50,
			HourlyRate:    3.00,
			Trend:         burnrate.TrendUp,
			TokenVelocity: 5000,
		},
	}

	m := NewModel(cfg,
		WithStartView(ViewDashboard),
		WithStateProvider(mockState),
		WithEventProvider(mockEvents),
		WithAlertProvider(mockAlerts),
		WithBurnRateProvider(mockBR),
	)
	m.width = 120
	m.height = 40

	view := m.View()
	if view == "" {
		t.Error("View() returned empty string")
	}
	if !strings.Contains(view, "Sessions") {
		t.Error("dashboard should contain Sessions panel")
	}
	if !strings.Contains(view, "Burn Rate") {
		t.Error("dashboard should contain Burn Rate panel")
	}
	if !strings.Contains(view, "Events") {
		t.Error("dashboard should contain Events panel")
	}
}

func TestModel_QuittingView(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg)
	m.quitting = true

	view := m.View()
	if !strings.Contains(view, "Shutting down") {
		t.Errorf("quitting view = %q, want to contain 'Shutting down'", view)
	}
}

func TestModel_ViewZeroDimensions(t *testing.T) {
	cfg := config.DefaultConfig()

	views := []struct {
		name string
		view ViewState
	}{
		{"startup", ViewStartup},
		{"dashboard", ViewDashboard},
		{"stats", ViewStats},
		{"history", ViewHistory},
	}

	for _, v := range views {
		t.Run(v.name+"_nil_providers", func(t *testing.T) {
			m := NewModel(cfg, WithStartView(v.view))
			result := m.View()
			if result == "" && v.view != ViewStartup {
				_ = result
			}
		})

		t.Run(v.name+"_with_providers", func(t *testing.T) {
			mockScan := &mockScannerProvider{
				processes: []scanner.ProcessInfo{
					{PID: 1234, Terminal: "iTerm2", CWD: "/test", EnvReadable: true, EnvVars: map[string]string{}},
				},
				statuses: map[int]scanner.StatusInfo{
					1234: {Status: scanner.TelemetryOff, Icon: "NO", Label: "No telemetry"},
				},
			}
			mockAlerts := &mockAlertProvider{
				alerts: []alerts.Alert{
					{Rule: "CostSurge", Severity: "critical", Message: "test alert", FiredAt: time.Now()},
				},
			}

			m := NewModel(cfg,
				WithStartView(v.view),
				WithScannerProvider(mockScan),
				WithAlertProvider(mockAlerts),
				WithStateProvider(&mockStateProvider{}),
				WithStatsProvider(&mockStatsProvider{}),
			)
			_ = m.View()
		})
	}
}

func TestModel_ViewSmallDimensions(t *testing.T) {
	cfg := config.DefaultConfig()

	sizes := []struct {
		name   string
		width  int
		height int
	}{
		{"1x1", 1, 1},
		{"10x5", 10, 5},
		{"40x10", 40, 10},
	}

	views := []struct {
		name string
		view ViewState
	}{
		{"startup", ViewStartup},
		{"dashboard", ViewDashboard},
		{"stats", ViewStats},
		{"history", ViewHistory},
	}

	mockScanner := &mockScannerProvider{
		processes: []scanner.ProcessInfo{
			{PID: 99, Terminal: "tmux", CWD: "/app", EnvReadable: true, EnvVars: map[string]string{}},
		},
		statuses: map[int]scanner.StatusInfo{
			99: {Status: scanner.TelemetryOff, Icon: "NO", Label: "No telemetry"},
		},
	}

	for _, sz := range sizes {
		for _, v := range views {
			t.Run(sz.name+"_"+v.name, func(t *testing.T) {
				m := NewModel(cfg,
					WithStartView(v.view),
					WithScannerProvider(mockScanner),
					WithStateProvider(&mockStateProvider{}),
					WithStatsProvider(&mockStatsProvider{}),
				)
				m.width = sz.width
				m.height = sz.height
				_ = m.View()
			})
		}
	}
}


func TestModel_FocusEvents(t *testing.T) {
	cfg := config.DefaultConfig()
	boolTrue := true
	mockEvents := &mockEventProvider{
		events: []events.FormattedEvent{
			{SessionID: "s1", EventType: "api_request", Formatted: "event1", Timestamp: time.Now(), Success: &boolTrue},
			{SessionID: "s1", EventType: "api_request", Formatted: "event2", Timestamp: time.Now(), Success: &boolTrue},
			{SessionID: "s1", EventType: "api_request", Formatted: "event3", Timestamp: time.Now(), Success: &boolTrue},
		},
	}

	m := NewModel(cfg, WithStartView(ViewDashboard), WithEventProvider(mockEvents))
	m.width = 120
	m.height = 40

	if m.panelFocus != FocusSessions {
		t.Errorf("default panelFocus = %d, want FocusSessions (%d)", m.panelFocus, FocusSessions)
	}

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m2 := result.(Model)
	if m2.panelFocus != FocusEvents {
		t.Errorf("after 'e', panelFocus = %d, want FocusEvents (%d)", m2.panelFocus, FocusEvents)
	}
	if m2.autoScroll {
		t.Error("after focusing events, autoScroll should be false")
	}
	if m2.eventCursor != 2 {
		t.Errorf("after 'e', eventCursor = %d, want 2", m2.eventCursor)
	}

	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyUp})
	m3 := result.(Model)
	if m3.eventCursor != 1 {
		t.Errorf("after Up, eventCursor = %d, want 1", m3.eventCursor)
	}

	result, _ = m3.Update(tea.KeyMsg{Type: tea.KeyUp})
	m4 := result.(Model)
	if m4.eventCursor != 0 {
		t.Errorf("after Up, eventCursor = %d, want 0", m4.eventCursor)
	}

	result, _ = m4.Update(tea.KeyMsg{Type: tea.KeyUp})
	m5 := result.(Model)
	if m5.eventCursor != 0 {
		t.Errorf("after Up at 0, eventCursor = %d, want 0", m5.eventCursor)
	}

	result, _ = m5.Update(tea.KeyMsg{Type: tea.KeyDown})
	m6 := result.(Model)
	if m6.eventCursor != 1 {
		t.Errorf("after Down, eventCursor = %d, want 1", m6.eventCursor)
	}

	result, _ = m6.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m7 := result.(Model)
	if m7.panelFocus != FocusSessions {
		t.Errorf("after Esc, panelFocus = %d, want FocusSessions (%d)", m7.panelFocus, FocusSessions)
	}
	if !m7.autoScroll {
		t.Error("after Esc from events, autoScroll should be true")
	}
}

func TestModel_FocusAlerts(t *testing.T) {
	cfg := config.DefaultConfig()
	mockAlerts := &mockAlertProvider{
		alerts: []alerts.Alert{
			{Rule: "CostSurge", Severity: "warning", Message: "Cost rate high", SessionID: "s1", FiredAt: time.Now()},
			{Rule: "LoopDetector", Severity: "critical", Message: "Loop detected", SessionID: "s2", FiredAt: time.Now()},
		},
	}

	m := NewModel(cfg, WithStartView(ViewDashboard), WithAlertProvider(mockAlerts))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m2 := result.(Model)
	if m2.panelFocus != FocusAlerts {
		t.Errorf("after 'a', panelFocus = %d, want FocusAlerts (%d)", m2.panelFocus, FocusAlerts)
	}
	if m2.alertCursor != 0 {
		t.Errorf("after 'a', alertCursor = %d, want 0", m2.alertCursor)
	}

	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyDown})
	m3 := result.(Model)
	if m3.alertCursor != 1 {
		t.Errorf("after Down, alertCursor = %d, want 1", m3.alertCursor)
	}

	result, _ = m3.Update(tea.KeyMsg{Type: tea.KeyDown})
	m4 := result.(Model)
	if m4.alertCursor != 1 {
		t.Errorf("after Down at end, alertCursor = %d, want 1", m4.alertCursor)
	}

	result, _ = m4.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m5 := result.(Model)
	if m5.panelFocus != FocusSessions {
		t.Errorf("after Esc, panelFocus = %d, want FocusSessions (%d)", m5.panelFocus, FocusSessions)
	}
}

func TestModel_EventDetailOverlay(t *testing.T) {
	cfg := config.DefaultConfig()
	boolTrue := true
	mockEvents := &mockEventProvider{
		events: []events.FormattedEvent{
			{
				SessionID: "sess-001",
				EventType: "api_request",
				Formatted: "[sess-001] claude-opus-4-6 -> 3 in / 431 out ($1.23) 4.2s",
				Timestamp: time.Now(),
				Success:   &boolTrue,
			},
		},
	}

	m := NewModel(cfg, WithStartView(ViewDashboard), WithEventProvider(mockEvents))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m2 := result.(Model)

	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := result.(Model)
	if !m3.detailOverlay {
		t.Error("after Enter in events, detailOverlay should be true")
	}
	if m3.detailTitle != "Event Detail" {
		t.Errorf("detailTitle = %q, want %q", m3.detailTitle, "Event Detail")
	}
	if !strings.Contains(m3.detailContent, "api_request") {
		t.Error("detail content should contain event type")
	}
	if !strings.Contains(m3.detailContent, "sess-001") {
		t.Error("detail content should contain session ID")
	}
	if !strings.Contains(m3.detailContent, "claude-opus-4-6") {
		t.Error("detail content should contain the full formatted event text")
	}

	view := m3.View()
	if view == "" {
		t.Error("View() returned empty string with detail overlay")
	}

	result, _ = m3.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m4 := result.(Model)
	if m4.detailOverlay {
		t.Error("after Esc, detailOverlay should be false")
	}
	if m4.detailContent != "" {
		t.Error("after Esc, detailContent should be empty")
	}
}

func TestModel_AlertDetailOverlay(t *testing.T) {
	cfg := config.DefaultConfig()
	mockAlerts := &mockAlertProvider{
		alerts: []alerts.Alert{
			{
				Rule:      "CostSurge",
				Severity:  "critical",
				Message:   "Cost surge: $125.56/hr exceeds threshold $100.00/hr for session sess-001",
				SessionID: "sess-001",
				FiredAt:   time.Now(),
			},
		},
	}

	m := NewModel(cfg, WithStartView(ViewDashboard), WithAlertProvider(mockAlerts))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m2 := result.(Model)

	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := result.(Model)
	if !m3.detailOverlay {
		t.Error("after Enter in alerts, detailOverlay should be true")
	}
	if m3.detailTitle != "Alert Detail" {
		t.Errorf("detailTitle = %q, want %q", m3.detailTitle, "Alert Detail")
	}
	if !strings.Contains(m3.detailContent, "CostSurge") {
		t.Error("detail content should contain rule name")
	}
	if !strings.Contains(m3.detailContent, "critical") {
		t.Error("detail content should contain severity")
	}
	if !strings.Contains(m3.detailContent, "$125.56/hr") {
		t.Error("detail content should contain full message")
	}

	view := m3.View()
	if view == "" {
		t.Error("View() returned empty string with alert detail overlay")
	}

	result, _ = m3.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m4 := result.(Model)
	if m4.detailOverlay {
		t.Error("after Enter, detailOverlay should be false")
	}
}

func TestModel_DetailOverlayScroll(t *testing.T) {
	cfg := config.DefaultConfig()
	mockAlerts := &mockAlertProvider{
		alerts: []alerts.Alert{
			{
				Rule:      "LoopDetector",
				Severity:  "critical",
				Message:   "Very long alert message that spans many lines when wrapped in the detail overlay to test scrolling functionality",
				SessionID: "sess-001",
				FiredAt:   time.Now(),
			},
		},
	}

	m := NewModel(cfg, WithStartView(ViewDashboard), WithAlertProvider(mockAlerts))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m2 := result.(Model)
	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := result.(Model)

	if m3.detailScrollPos != 0 {
		t.Errorf("initial detailScrollPos = %d, want 0", m3.detailScrollPos)
	}

	result, _ = m3.Update(tea.KeyMsg{Type: tea.KeyDown})
	m4 := result.(Model)
	if m4.detailScrollPos != 1 {
		t.Errorf("after Down, detailScrollPos = %d, want 1", m4.detailScrollPos)
	}

	result, _ = m4.Update(tea.KeyMsg{Type: tea.KeyUp})
	m5 := result.(Model)
	if m5.detailScrollPos != 0 {
		t.Errorf("after Up, detailScrollPos = %d, want 0", m5.detailScrollPos)
	}

	result, _ = m5.Update(tea.KeyMsg{Type: tea.KeyUp})
	m6 := result.(Model)
	if m6.detailScrollPos != 0 {
		t.Errorf("after Up at 0, detailScrollPos = %d, want 0", m6.detailScrollPos)
	}
}

func TestModel_FocusSwitchBetweenPanels(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	if m.panelFocus != FocusSessions {
		t.Fatalf("initial focus = %d, want FocusSessions", m.panelFocus)
	}

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m2 := result.(Model)
	if m2.panelFocus != FocusEvents {
		t.Errorf("after 'e', focus = %d, want FocusEvents", m2.panelFocus)
	}

	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m3 := result.(Model)
	if m3.panelFocus != FocusAlerts {
		t.Errorf("after 'a' from events, focus = %d, want FocusAlerts", m3.panelFocus)
	}

	result, _ = m3.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m4 := result.(Model)
	if m4.panelFocus != FocusEvents {
		t.Errorf("after 'e' from alerts, focus = %d, want FocusEvents", m4.panelFocus)
	}

	result, _ = m4.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m5 := result.(Model)
	if m5.panelFocus != FocusSessions {
		t.Errorf("after Esc, focus = %d, want FocusSessions", m5.panelFocus)
	}
}

func TestModel_DetailOverlayBlocksOtherKeys(t *testing.T) {
	cfg := config.DefaultConfig()
	boolTrue := true
	mockEvents := &mockEventProvider{
		events: []events.FormattedEvent{
			{SessionID: "s1", EventType: "api_request", Formatted: "test event", Timestamp: time.Now(), Success: &boolTrue},
		},
	}

	m := NewModel(cfg, WithStartView(ViewDashboard), WithEventProvider(mockEvents))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m2 := result.(Model)
	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := result.(Model)

	result, _ = m3.Update(tea.KeyMsg{Type: tea.KeyTab})
	m4 := result.(Model)
	if m4.view != ViewDashboard {
		t.Error("Tab should not switch views while detail overlay is open")
	}
	if !m4.detailOverlay {
		t.Error("detail overlay should still be open after Tab")
	}

	result, _ = m4.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m5 := result.(Model)
	if m5.filterMenu.Active {
		t.Error("filter menu should not open while detail overlay is open")
	}
}

func TestModel_HeaderHelp(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	help := m.headerHelp()
	if !strings.Contains(help, "a:Alerts") {
		t.Error("sessions focus header should contain 'a:Alerts'")
	}
	if !strings.Contains(help, "e:Events") {
		t.Error("sessions focus header should contain 'e:Events'")
	}

	m.panelFocus = FocusEvents
	help = m.headerHelp()
	if !strings.Contains(help, "Enter:Detail") {
		t.Error("events focus header should contain 'Enter:Detail'")
	}
	if !strings.Contains(help, "Esc:Back") {
		t.Error("events focus header should contain 'Esc:Back'")
	}

	m.panelFocus = FocusAlerts
	help = m.headerHelp()
	if !strings.Contains(help, "Enter:Detail") {
		t.Error("alerts focus header should contain 'Enter:Detail'")
	}
	if !strings.Contains(help, "e:Events") {
		t.Error("alerts focus header should contain 'e:Events'")
	}
}

func TestModel_FocusEventsEmptyList(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m2 := result.(Model)
	if m2.panelFocus != FocusEvents {
		t.Errorf("should still focus events even if empty")
	}

	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := result.(Model)
	if m3.detailOverlay {
		t.Error("should not open detail overlay for empty events")
	}
}

func TestModel_FocusAlertsEmptyList(t *testing.T) {
	cfg := config.DefaultConfig()
	mockAlerts := &mockAlertProvider{alerts: nil}
	m := NewModel(cfg, WithStartView(ViewDashboard), WithAlertProvider(mockAlerts))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m2 := result.(Model)
	if m2.panelFocus != FocusAlerts {
		t.Errorf("should still focus alerts even if empty")
	}

	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := result.(Model)
	if m3.detailOverlay {
		t.Error("should not open detail overlay for empty alerts")
	}
}

func TestModel_TabResetsFocus(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m2 := result.(Model)
	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyTab})
	m3 := result.(Model)
	if m3.view != ViewStats {
		t.Error("Tab should switch to stats view")
	}
	if m3.panelFocus != FocusSessions {
		t.Errorf("Tab should reset focus to sessions, got %d", m3.panelFocus)
	}
}

func TestStripAnsi(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"with colors", "\x1b[31mred\x1b[0m text", "red text"},
		{"bold", "\x1b[1mbold\x1b[0m", "bold"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripAnsi(tt.input)
			if got != tt.want {
				t.Errorf("stripAnsi(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestModel_ViewClampedToTerminalHeight(t *testing.T) {
	cfg := config.DefaultConfig()
	mockAlerts := &mockAlertProvider{
		alerts: []alerts.Alert{
			{Rule: "CostSurge", Severity: "critical", Message: "test alert", SessionID: "s1", FiredAt: time.Now()},
		},
	}

	sizes := []struct {
		name   string
		width  int
		height int
	}{
		{"small_15", 80, 15},
		{"small_12", 80, 12},
		{"minimum_10", 40, 10},
	}

	for _, sz := range sizes {
		t.Run(sz.name, func(t *testing.T) {
			m := NewModel(cfg,
				WithStartView(ViewDashboard),
				WithAlertProvider(mockAlerts),
				WithStateProvider(&mockStateProvider{}),
			)
			m.width = sz.width
			m.height = sz.height

			view := m.View()
			lines := strings.Split(view, "\n")
			if len(lines) > sz.height {
				t.Errorf("View() output has %d lines, want at most %d", len(lines), sz.height)
			}

			if len(lines) > 0 && !strings.Contains(lines[0], "cc-top") {
				t.Errorf("first line = %q, want to contain 'cc-top'", lines[0])
			}
		})
	}
}

func TestModel_AlertsBarVisible(t *testing.T) {
	cfg := config.DefaultConfig()
	mockAlerts := &mockAlertProvider{
		alerts: []alerts.Alert{
			{Rule: "CostSurge", Severity: "critical", Message: "high burn", SessionID: "s1", FiredAt: time.Now()},
		},
	}
	mockBR := &mockBurnRateProvider{
		global: burnrate.BurnRate{TotalCost: 12345.67, HourlyRate: 5.0, TokenVelocity: 500},
	}

	sizes := []struct {
		name   string
		width  int
		height int
	}{
		{"standard_80x24", 80, 24},
		{"large_120x40", 120, 40},
		{"wide_200x24", 200, 24},
		{"short_80x15", 80, 15},
		{"short_80x12", 80, 12},
		{"minimum_40x10", 40, 10},
		{"narrow_50x30", 50, 30},
	}

	for _, sz := range sizes {
		t.Run(sz.name, func(t *testing.T) {
			m := NewModel(cfg,
				WithStartView(ViewDashboard),
				WithAlertProvider(mockAlerts),
				WithBurnRateProvider(mockBR),
				WithStateProvider(&mockStateProvider{}),
			)
			m.width = sz.width
			m.height = sz.height
			m.cachedBurnRate = mockBR.global

			view := m.View()
			lines := strings.Split(view, "\n")

			if len(lines) > sz.height {
				t.Errorf("View() has %d lines, want at most %d", len(lines), sz.height)
			}

			found := false
			for _, line := range lines {
				if strings.Contains(line, "CostSurge") || strings.Contains(line, "Alerts") || strings.Contains(line, "!!") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("alerts bar content not found in view output (%d lines at %dx%d)",
					len(lines), sz.width, sz.height)
			}
		})
	}
}

func TestRenderBorderedPanel_ClampsContent(t *testing.T) {
	tests := []struct {
		name        string
		contentRows int
		w, h        int
	}{
		{"content_fits", 4, 40, 8},
		{"content_overflows", 10, 40, 8},
		{"content_way_overflows", 20, 40, 8},
		{"small_panel", 5, 40, 4},
		{"tall_panel", 8, 60, 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lines []string
			for i := range tt.contentRows {
				lines = append(lines, fmt.Sprintf("line %d content here", i))
			}
			content := strings.Join(lines, "\n")

			result := renderBorderedPanel(content, tt.w, tt.h)
			resultLines := strings.Split(result, "\n")

			if len(resultLines) != tt.h {
				t.Errorf("renderBorderedPanel() produced %d lines, want exactly %d",
					len(resultLines), tt.h)
			}
		})
	}
}

func TestModel_RenderWithFocusedPanels(t *testing.T) {
	cfg := config.DefaultConfig()
	boolTrue := true
	mockEvents := &mockEventProvider{
		events: []events.FormattedEvent{
			{SessionID: "s1", EventType: "api_request", Formatted: "test event", Timestamp: time.Now(), Success: &boolTrue},
		},
	}
	mockAlerts := &mockAlertProvider{
		alerts: []alerts.Alert{
			{Rule: "CostSurge", Severity: "warning", Message: "test alert", SessionID: "s1", FiredAt: time.Now()},
		},
	}

	m := NewModel(cfg,
		WithStartView(ViewDashboard),
		WithEventProvider(mockEvents),
		WithAlertProvider(mockAlerts),
		WithStateProvider(&mockStateProvider{}),
	)
	m.width = 120
	m.height = 40

	m.panelFocus = FocusEvents
	m.eventCursor = 0
	view := m.View()
	if view == "" {
		t.Error("View() returned empty string with events focused")
	}

	m.panelFocus = FocusAlerts
	m.alertCursor = 0
	view = m.View()
	if view == "" {
		t.Error("View() returned empty string with alerts focused")
	}

	m.detailOverlay = true
	m.detailTitle = "Test"
	m.detailContent = "Test content"
	view = m.View()
	if view == "" {
		t.Error("View() returned empty string with detail overlay")
	}
}

func TestTabCycle_DashboardStatsHistory(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewDashboard), WithStateProvider(&mockStateProvider{}), WithPersistenceFlag(true))
	m.width = 120
	m.height = 40

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m1 := result.(Model)
	if m1.view != ViewStats {
		t.Fatalf("Dashboard Tab: got view %d, want ViewStats (%d)", m1.view, ViewStats)
	}

	result, _ = m1.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2 := result.(Model)
	if m2.view != ViewHistory {
		t.Fatalf("Stats Tab: got view %d, want ViewHistory (%d)", m2.view, ViewHistory)
	}

	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyTab})
	m3 := result.(Model)
	if m3.view != ViewDashboard {
		t.Fatalf("History Tab: got view %d, want ViewDashboard (%d)", m3.view, ViewDashboard)
	}
}

func TestNoPersistenceIndicator(t *testing.T) {
	cfg := config.DefaultConfig()

	views := []struct {
		name string
		view ViewState
	}{
		{"dashboard", ViewDashboard},
		{"stats", ViewStats},
		{"history", ViewHistory},
	}

	for _, v := range views {
		t.Run(v.name+"_no_persistence", func(t *testing.T) {
			m := NewModel(cfg, WithStartView(v.view), WithStateProvider(&mockStateProvider{}), WithPersistenceFlag(false))
			m.width = 120
			m.height = 40

			output := m.View()
			if !strings.Contains(output, "No persistence") {
				t.Errorf("%s view should contain 'No persistence' when isPersistent=false", v.name)
			}
		})

		t.Run(v.name+"_with_persistence", func(t *testing.T) {
			m := NewModel(cfg, WithStartView(v.view), WithStateProvider(&mockStateProvider{}), WithPersistenceFlag(true))
			m.width = 120
			m.height = 40

			output := m.View()
			if strings.Contains(output, "No persistence") {
				t.Errorf("%s view should NOT contain 'No persistence' when isPersistent=true", v.name)
			}
		})
	}
}
