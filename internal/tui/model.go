// Package tui implements the Bubble Tea TUI for cc-top.
//
// The TUI has three top-level views: Startup, Dashboard, and Stats.
// The Dashboard view arranges four panels: Session List (left),
// Burn Rate (top right), Event Stream (center right), and Alerts (bottom).
// The Stats view is a full-screen display of aggregate statistics.
package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nixlim/cc-top/internal/alerts"
	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/events"
	"github.com/nixlim/cc-top/internal/scanner"
	"github.com/nixlim/cc-top/internal/state"
	"github.com/nixlim/cc-top/internal/stats"
)

// ViewState represents which top-level view is active.
type ViewState int

const (
	// ViewStartup shows the startup screen with process discovery.
	ViewStartup ViewState = iota
	// ViewDashboard shows the main dashboard with four panels.
	ViewDashboard
	// ViewStats shows the full-screen stats dashboard.
	ViewStats
)

// tickMsg is sent periodically to trigger TUI refresh.
type tickMsg time.Time

// StateProvider is the interface for reading application state.
// This decouples the TUI from the concrete state store implementation.
type StateProvider interface {
	GetSession(sessionID string) *state.SessionData
	ListSessions() []state.SessionData
	GetAggregatedCost() float64
}

// BurnRateProvider is the interface for reading burn rate data.
type BurnRateProvider interface {
	Get(sessionID string) burnrate.BurnRate
	GetGlobal() burnrate.BurnRate
}

// EventProvider is the interface for reading formatted events.
type EventProvider interface {
	Recent(limit int) []events.FormattedEvent
	RecentForSession(sessionID string, limit int) []events.FormattedEvent
}

// AlertProvider is the interface for reading active alerts.
type AlertProvider interface {
	Active() []alerts.Alert
	ActiveForSession(sessionID string) []alerts.Alert
}

// StatsProvider is the interface for reading dashboard statistics.
type StatsProvider interface {
	Get(sessionID string) stats.DashboardStats
	GetGlobal() stats.DashboardStats
}

// ScannerProvider is the interface for reading process scan results.
type ScannerProvider interface {
	Processes() []scanner.ProcessInfo
	GetTelemetryStatus(p scanner.ProcessInfo) scanner.StatusInfo
	Rescan()
}

// SettingsWriter is the interface for writing Claude Code settings.
type SettingsWriter interface {
	EnableTelemetry() error
	FixMisconfigured() error
}

// Model is the top-level Bubble Tea model for cc-top.
type Model struct {
	// View state.
	view     ViewState
	width    int
	height   int
	keys     KeyMap
	quitting bool

	// Configuration.
	cfg config.Config

	// Providers (dependency-injected, may be nil during tests).
	state    StateProvider
	burnRate BurnRateProvider
	events   EventProvider
	alerts   AlertProvider
	stats    StatsProvider
	scanner  ScannerProvider
	settings SettingsWriter

	// Session selection.
	selectedSession string // empty = global view
	sessionCursor   int    // cursor position in session list

	// Event stream state.
	eventScrollPos int
	autoScroll     bool
	eventFilter    EventFilter
	filterMenu     FilterMenuState

	// Startup screen state.
	startupMessage string

	// Kill switch state.
	killConfirm    bool
	killTargetPID  int
	killTargetInfo string

	// Alert scroll state.
	alertScrollPos int

	// Stats view scroll.
	statsScrollPos int

	// Refresh rate.
	refreshRate time.Duration

	// Shutdown callback, if set.
	onShutdown func()
}

// NewModel creates a new TUI model with the given configuration and providers.
func NewModel(cfg config.Config, opts ...ModelOption) Model {
	m := Model{
		view:        ViewStartup,
		keys:        DefaultKeyMap(),
		cfg:         cfg,
		autoScroll:  true,
		eventFilter: NewEventFilter(),
		filterMenu:  NewFilterMenu(),
		refreshRate: time.Duration(cfg.Display.RefreshRateMS) * time.Millisecond,
	}

	for _, opt := range opts {
		opt(&m)
	}

	return m
}

// ModelOption is a functional option for configuring the Model.
type ModelOption func(*Model)

// WithStateProvider sets the state provider.
func WithStateProvider(s StateProvider) ModelOption {
	return func(m *Model) { m.state = s }
}

// WithBurnRateProvider sets the burn rate provider.
func WithBurnRateProvider(b BurnRateProvider) ModelOption {
	return func(m *Model) { m.burnRate = b }
}

// WithEventProvider sets the event provider.
func WithEventProvider(e EventProvider) ModelOption {
	return func(m *Model) { m.events = e }
}

// WithAlertProvider sets the alert provider.
func WithAlertProvider(a AlertProvider) ModelOption {
	return func(m *Model) { m.alerts = a }
}

// WithStatsProvider sets the stats provider.
func WithStatsProvider(s StatsProvider) ModelOption {
	return func(m *Model) { m.stats = s }
}

// WithScannerProvider sets the scanner provider.
func WithScannerProvider(s ScannerProvider) ModelOption {
	return func(m *Model) { m.scanner = s }
}

// WithSettingsWriter sets the settings writer.
func WithSettingsWriter(s SettingsWriter) ModelOption {
	return func(m *Model) { m.settings = s }
}

// WithStartView sets the initial view state.
func WithStartView(v ViewState) ModelOption {
	return func(m *Model) { m.view = v }
}

// WithOnShutdown sets a callback to invoke during graceful shutdown.
func WithOnShutdown(fn func()) ModelOption {
	return func(m *Model) { m.onShutdown = fn }
}

// Init returns the initial command for the Bubble Tea program.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.tickCmd(),
	)
}

// tickCmd returns a command that sends a tickMsg after the refresh interval.
func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(m.refreshRate, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles all incoming messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		return m, m.tickCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey routes key presses to the appropriate handler based on current view.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Kill confirmation dialog takes priority.
	if m.killConfirm {
		return m.handleKillConfirmKey(msg)
	}

	// Filter menu takes priority when active.
	if m.filterMenu.Active {
		return m.handleFilterMenuKey(msg)
	}

	// Global key bindings (available in all views).
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		if m.onShutdown != nil {
			m.onShutdown()
		}
		return m, tea.Quit

	case key.Matches(msg, m.keys.KillSwitch):
		if m.view == ViewDashboard || m.view == ViewStats {
			return m.initiateKillSwitch()
		}
	}

	// View-specific key handling.
	switch m.view {
	case ViewStartup:
		return m.handleStartupKey(msg)
	case ViewDashboard:
		return m.handleDashboardKey(msg)
	case ViewStats:
		return m.handleStatsKey(msg)
	}

	return m, nil
}

// handleStartupKey handles keys on the startup screen.
func (m Model) handleStartupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Enter):
		m.view = ViewDashboard
		return m, nil

	case key.Matches(msg, m.keys.Enable):
		if m.settings != nil {
			if err := m.settings.EnableTelemetry(); err != nil {
				m.startupMessage = "Error: " + err.Error()
			} else {
				m.startupMessage = "Settings written. New Claude Code sessions will auto-connect. Existing sessions need restart."
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Fix):
		if m.settings != nil {
			if err := m.settings.FixMisconfigured(); err != nil {
				m.startupMessage = "Error: " + err.Error()
			} else {
				m.startupMessage = "Misconfigured sessions fixed. Restart affected sessions."
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.Rescan):
		if m.scanner != nil {
			m.scanner.Rescan()
			m.startupMessage = "Rescanning..."
		}
		return m, nil
	}

	return m, nil
}

// handleDashboardKey handles keys on the main dashboard.
func (m Model) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Tab):
		m.view = ViewStats
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.sessionCursor > 0 {
			m.sessionCursor--
		}
		return m, nil

	case key.Matches(msg, m.keys.Down):
		sessions := m.getSessions()
		if m.sessionCursor < len(sessions)-1 {
			m.sessionCursor++
		}
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		sessions := m.getSessions()
		if m.sessionCursor >= 0 && m.sessionCursor < len(sessions) {
			m.selectedSession = sessions[m.sessionCursor].SessionID
			m.eventFilter.SessionID = m.selectedSession
		}
		return m, nil

	case key.Matches(msg, m.keys.Escape):
		m.selectedSession = ""
		m.eventFilter.SessionID = ""
		return m, nil

	case key.Matches(msg, m.keys.Filter):
		m.filterMenu.Active = true
		m.filterMenu.Cursor = 0
		return m, nil

	case key.Matches(msg, m.keys.ScrollDown):
		m.autoScroll = false
		m.eventScrollPos++
		return m, nil

	case key.Matches(msg, m.keys.ScrollUp):
		m.autoScroll = false
		if m.eventScrollPos > 0 {
			m.eventScrollPos--
		}
		return m, nil
	}

	return m, nil
}

// handleStatsKey handles keys on the stats dashboard.
func (m Model) handleStatsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Tab):
		m.view = ViewDashboard
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.statsScrollPos > 0 {
			m.statsScrollPos--
		}
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.statsScrollPos++
		return m, nil
	}
	return m, nil
}

// handleFilterMenuKey handles keys in the filter menu overlay.
func (m Model) handleFilterMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Escape):
		m.filterMenu.Active = false
		return m, nil

	case key.Matches(msg, m.keys.Up):
		if m.filterMenu.Cursor > 0 {
			m.filterMenu.Cursor--
		}
		return m, nil

	case key.Matches(msg, m.keys.Down):
		if m.filterMenu.Cursor < len(m.filterMenu.Options)-1 {
			m.filterMenu.Cursor++
		}
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		if m.filterMenu.Cursor >= 0 && m.filterMenu.Cursor < len(m.filterMenu.Options) {
			opt := &m.filterMenu.Options[m.filterMenu.Cursor]
			opt.Enabled = !opt.Enabled
			m.applyFilter()
		}
		return m, nil
	}
	return m, nil
}

// applyFilter updates the event filter from the filter menu state.
func (m *Model) applyFilter() {
	m.eventFilter.EventTypes = make(map[string]bool)
	m.eventFilter.SuccessOnly = false
	m.eventFilter.FailureOnly = false

	for _, opt := range m.filterMenu.Options {
		switch opt.Key {
		case "success_only":
			m.eventFilter.SuccessOnly = opt.Enabled
		case "failure_only":
			m.eventFilter.FailureOnly = opt.Enabled
		default:
			m.eventFilter.EventTypes[opt.Key] = opt.Enabled
		}
	}
}

// getSessions returns the current session list from the state provider.
func (m Model) getSessions() []state.SessionData {
	if m.state == nil {
		return nil
	}
	return m.state.ListSessions()
}

// View renders the TUI based on the current view state.
func (m Model) View() string {
	if m.quitting {
		return "Shutting down...\n"
	}

	switch m.view {
	case ViewStartup:
		return m.renderStartup()
	case ViewDashboard:
		return m.renderDashboard()
	case ViewStats:
		return m.renderStats()
	}

	return ""
}
