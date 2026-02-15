package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nixlim/cc-top/internal/alerts"
	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/events"
	"github.com/nixlim/cc-top/internal/state"
	"github.com/nixlim/cc-top/internal/stats"
)

// --- Mock providers for testing ---

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

// --- Tests ---

func TestComputeDimensions_LargeTerminal(t *testing.T) {
	dims := computeDimensions(120, 40)

	// Session list should be ~40% of 120 = 48.
	if dims.sessionListW < 40 || dims.sessionListW > 60 {
		t.Errorf("sessionListW = %d, want ~48", dims.sessionListW)
	}

	// Burn rate should fill right side.
	if dims.burnRateW < 50 {
		t.Errorf("burnRateW = %d, want >= 50", dims.burnRateW)
	}

	// All heights should be positive.
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
}

func TestComputeDimensions_SmallTerminal(t *testing.T) {
	dims := computeDimensions(80, 24)

	// All dimensions should be positive.
	if dims.sessionListW <= 0 {
		t.Errorf("sessionListW = %d, want > 0", dims.sessionListW)
	}
	if dims.burnRateW <= 0 {
		t.Errorf("burnRateW = %d, want > 0", dims.burnRateW)
	}
}

func TestComputeDimensions_MinimumTerminal(t *testing.T) {
	dims := computeDimensions(20, 8)

	// Should not panic with very small sizes.
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

	// Tab should switch to stats.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2 := result.(Model)
	if m2.view != ViewStats {
		t.Errorf("after Tab, view = %d, want ViewStats (%d)", m2.view, ViewStats)
	}

	// Tab again should switch back to dashboard.
	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyTab})
	m3 := result.(Model)
	if m3.view != ViewDashboard {
		t.Errorf("after second Tab, view = %d, want ViewDashboard (%d)", m3.view, ViewDashboard)
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

	// Navigate down.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m2 := result.(Model)
	if m2.sessionCursor != 1 {
		t.Errorf("after Down, sessionCursor = %d, want 1", m2.sessionCursor)
	}

	// Select with Enter.
	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := result.(Model)
	if m3.selectedSession != "sess-002" {
		t.Errorf("after Enter, selectedSession = %q, want %q", m3.selectedSession, "sess-002")
	}

	// Escape returns to global.
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

	// Open filter menu.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m2 := result.(Model)
	if !m2.filterMenu.Active {
		t.Error("after 'f', filter menu should be active")
	}

	// Navigate and toggle.
	result, _ = m2.Update(tea.KeyMsg{Type: tea.KeyDown})
	m3 := result.(Model)
	if m3.filterMenu.Cursor != 1 {
		t.Errorf("filter cursor = %d, want 1", m3.filterMenu.Cursor)
	}

	// Close with Escape.
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
