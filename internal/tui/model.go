package tui

import (
	"strings"
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

type ViewState int

const (
	ViewStartup ViewState = iota
	ViewDashboard
	ViewStats
	ViewHistory
)

type PanelFocus int

const (
	FocusSessions PanelFocus = iota
	FocusEvents
	FocusAlerts
)

type tickMsg time.Time

type StateProvider interface {
	GetSession(sessionID string) *state.SessionData
	ListSessions() []state.SessionData
	GetAggregatedCost() float64
	QueryDailySummaries(days int) []state.DailySummary
	DroppedWrites() int64
}

type BurnRateProvider interface {
	Get(sessionID string) burnrate.BurnRate
	GetGlobal() burnrate.BurnRate
}

type EventProvider interface {
	Recent(limit int) []events.FormattedEvent
	RecentForSession(sessionID string, limit int) []events.FormattedEvent
}

type AlertProvider interface {
	Active() []alerts.Alert
	ActiveForSession(sessionID string) []alerts.Alert
}

type StatsProvider interface {
	Get(sessionID string) stats.DashboardStats
	GetGlobal() stats.DashboardStats
}

type ScannerProvider interface {
	Processes() []scanner.ProcessInfo
	GetTelemetryStatus(p scanner.ProcessInfo) scanner.StatusInfo
	Rescan()
}

type SettingsWriter interface {
	EnableTelemetry() error
	FixMisconfigured() error
}

type Model struct {
	view     ViewState
	width    int
	height   int
	keys     KeyMap
	quitting bool

	cfg config.Config

	state    StateProvider
	burnRate BurnRateProvider
	events   EventProvider
	alerts   AlertProvider
	stats    StatsProvider
	scanner  ScannerProvider
	settings SettingsWriter

	selectedSession string
	sessionCursor   int

	eventScrollPos int
	autoScroll     bool
	eventFilter    EventFilter
	filterMenu     FilterMenuState

	startupMessage string

	killConfirm    bool
	killTargetPID  int
	killTargetInfo string

	cachedBurnRate burnrate.BurnRate

	alertScrollPos int
	alertCursor    int

	panelFocus      PanelFocus
	eventCursor     int
	detailOverlay   bool
	detailContent   string
	detailTitle     string
	detailScrollPos int

	statsScrollPos int

	isPersistent bool

	historyGranularity string
	historyScrollPos   int

	refreshRate time.Duration

	onShutdown func()
}

func NewModel(cfg config.Config, opts ...ModelOption) Model {
	m := Model{
		view:               ViewStartup,
		keys:               DefaultKeyMap(),
		cfg:                cfg,
		autoScroll:         true,
		eventFilter:        NewEventFilter(),
		filterMenu:         NewFilterMenu(),
		historyGranularity: "daily",
		refreshRate:        time.Duration(cfg.Display.RefreshRateMS) * time.Millisecond,
	}

	for _, opt := range opts {
		opt(&m)
	}

	return m
}

type ModelOption func(*Model)

func WithStateProvider(s StateProvider) ModelOption {
	return func(m *Model) { m.state = s }
}

func WithBurnRateProvider(b BurnRateProvider) ModelOption {
	return func(m *Model) { m.burnRate = b }
}

func WithEventProvider(e EventProvider) ModelOption {
	return func(m *Model) { m.events = e }
}

func WithAlertProvider(a AlertProvider) ModelOption {
	return func(m *Model) { m.alerts = a }
}

func WithStatsProvider(s StatsProvider) ModelOption {
	return func(m *Model) { m.stats = s }
}

func WithScannerProvider(s ScannerProvider) ModelOption {
	return func(m *Model) { m.scanner = s }
}

func WithSettingsWriter(s SettingsWriter) ModelOption {
	return func(m *Model) { m.settings = s }
}

func WithStartView(v ViewState) ModelOption {
	return func(m *Model) { m.view = v }
}

func WithOnShutdown(fn func()) ModelOption {
	return func(m *Model) { m.onShutdown = fn }
}

func WithPersistenceFlag(isPersistent bool) ModelOption {
	return func(m *Model) { m.isPersistent = isPersistent }
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.tickCmd(),
	)
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(m.refreshRate, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.cachedBurnRate = m.computeBurnRate()
		return m, m.tickCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.killConfirm {
		return m.handleKillConfirmKey(msg)
	}

	if m.detailOverlay {
		return m.handleDetailOverlayKey(msg)
	}

	if m.filterMenu.Active {
		return m.handleFilterMenuKey(msg)
	}

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

	switch m.view {
	case ViewStartup:
		return m.handleStartupKey(msg)
	case ViewDashboard:
		return m.handleDashboardKey(msg)
	case ViewStats:
		return m.handleStatsKey(msg)
	case ViewHistory:
		return m.handleHistoryKey(msg)
	}

	return m, nil
}

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

func (m Model) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Tab):
		m.panelFocus = FocusSessions
		m.view = ViewStats
		return m, nil

	case key.Matches(msg, m.keys.Filter):
		m.filterMenu.Active = true
		m.filterMenu.Cursor = 0
		return m, nil

	case key.Matches(msg, m.keys.FocusAlerts):
		if m.panelFocus != FocusAlerts {
			m.panelFocus = FocusAlerts
			m.alertCursor = 0
		}
		return m, nil

	case key.Matches(msg, m.keys.FocusEvents):
		if m.panelFocus != FocusEvents {
			m.panelFocus = FocusEvents
			m.autoScroll = false
			evts := m.getFilteredEvents(m.cfg.Display.EventBufferSize)
			if len(evts) > 0 {
				m.eventCursor = len(evts) - 1
			}
		}
		return m, nil
	}

	switch m.panelFocus {
	case FocusEvents:
		return m.handleEventsPanelKey(msg)
	case FocusAlerts:
		return m.handleAlertsPanelKey(msg)
	default:
		return m.handleSessionsPanelKey(msg)
	}
}

func (m Model) handleSessionsPanelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
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

func (m Model) handleEventsPanelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	evts := m.getFilteredEvents(m.cfg.Display.EventBufferSize)

	switch {
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.ScrollUp):
		if m.eventCursor > 0 {
			m.eventCursor--
		}
		return m, nil

	case key.Matches(msg, m.keys.Down), key.Matches(msg, m.keys.ScrollDown):
		if m.eventCursor < len(evts)-1 {
			m.eventCursor++
		}
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		if m.eventCursor >= 0 && m.eventCursor < len(evts) {
			e := evts[m.eventCursor]
			m.detailOverlay = true
			m.detailTitle = "Event Detail"
			m.detailContent = m.formatEventDetail(e)
			m.detailScrollPos = 0
		}
		return m, nil

	case key.Matches(msg, m.keys.Escape):
		m.panelFocus = FocusSessions
		m.autoScroll = true
		return m, nil
	}

	return m, nil
}

func (m Model) handleAlertsPanelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	activeAlerts := m.getActiveAlerts()

	switch {
	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.ScrollUp):
		if m.alertCursor > 0 {
			m.alertCursor--
		}
		return m, nil

	case key.Matches(msg, m.keys.Down), key.Matches(msg, m.keys.ScrollDown):
		if m.alertCursor < len(activeAlerts)-1 {
			m.alertCursor++
		}
		return m, nil

	case key.Matches(msg, m.keys.Enter):
		if m.alertCursor >= 0 && m.alertCursor < len(activeAlerts) {
			a := activeAlerts[m.alertCursor]
			m.detailOverlay = true
			m.detailTitle = "Alert Detail"
			m.detailContent = m.formatAlertDetail(a)
			m.detailScrollPos = 0
		}
		return m, nil

	case key.Matches(msg, m.keys.Escape):
		m.panelFocus = FocusSessions
		return m, nil
	}

	return m, nil
}

func (m Model) handleDetailOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Escape), key.Matches(msg, m.keys.Enter):
		m.detailOverlay = false
		m.detailContent = ""
		m.detailTitle = ""
		m.detailScrollPos = 0
		return m, nil

	case key.Matches(msg, m.keys.Up), key.Matches(msg, m.keys.ScrollUp):
		if m.detailScrollPos > 0 {
			m.detailScrollPos--
		}
		return m, nil

	case key.Matches(msg, m.keys.Down), key.Matches(msg, m.keys.ScrollDown):
		m.detailScrollPos++
		return m, nil
	}

	return m, nil
}

func (m Model) formatEventDetail(e events.FormattedEvent) string {
	var lines []string
	lines = append(lines, "Type:      "+e.EventType)
	lines = append(lines, "Session:   "+e.SessionID)
	lines = append(lines, "Timestamp: "+e.Timestamp.Format("2006-01-02 15:04:05"))
	if e.Success != nil {
		if *e.Success {
			lines = append(lines, "Status:    success")
		} else {
			lines = append(lines, "Status:    failure")
		}
	}
	lines = append(lines, "")
	lines = append(lines, "Content:")
	lines = append(lines, e.Formatted)
	return strings.Join(lines, "\n")
}

func (m Model) formatAlertDetail(a alerts.Alert) string {
	var lines []string
	lines = append(lines, "Rule:      "+a.Rule)
	lines = append(lines, "Severity:  "+a.Severity)
	if a.SessionID != "" {
		lines = append(lines, "Session:   "+a.SessionID)
	} else {
		lines = append(lines, "Session:   (global)")
	}
	lines = append(lines, "Fired at:  "+a.FiredAt.Format("2006-01-02 15:04:05"))
	lines = append(lines, "")
	lines = append(lines, "Message:")
	lines = append(lines, a.Message)
	return strings.Join(lines, "\n")
}

func (m Model) handleStatsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Tab):
		m.view = ViewHistory
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

func (m Model) handleHistoryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Tab):
		m.view = ViewDashboard
		return m, nil
	case key.Matches(msg, m.keys.Up):
		if m.historyScrollPos > 0 {
			m.historyScrollPos--
		}
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.historyScrollPos++
		return m, nil
	}

	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case 'd':
			m.historyGranularity = "daily"
			m.historyScrollPos = 0
			return m, nil
		case 'w':
			m.historyGranularity = "weekly"
			m.historyScrollPos = 0
			return m, nil
		case 'm':
			m.historyGranularity = "monthly"
			m.historyScrollPos = 0
			return m, nil
		}
	}

	return m, nil
}

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

func (m Model) getSessions() []state.SessionData {
	if m.state == nil {
		return nil
	}
	return m.state.ListSessions()
}

func (m Model) headerIndicators() string {
	var parts []string
	if !m.isPersistent {
		parts = append(parts, "[No persistence]")
	}
	if m.state != nil && m.state.DroppedWrites() > 0 {
		parts = append(parts, "[!] Writes dropped")
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + dimStyle.Render(strings.Join(parts, " "))
}

func (m Model) View() string {
	if m.quitting {
		return "Shutting down...\n"
	}

	var output string
	switch m.view {
	case ViewStartup:
		output = m.renderStartup()
	case ViewDashboard:
		output = m.renderDashboard()
	case ViewStats:
		output = m.renderStats()
	case ViewHistory:
		output = m.renderHistory()
	}

	if m.height > 0 {
		lines := strings.Split(output, "\n")
		if len(lines) > m.height {
			lines = lines[:m.height]
			output = strings.Join(lines, "\n")
		}
	}

	return output
}
