package tui

import (
	"strings"
	"testing"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/scanner"
)

// Mock scanner provider for startup tests.
type mockScannerProvider struct {
	processes []scanner.ProcessInfo
	statuses  map[int]scanner.StatusInfo
}

func (m *mockScannerProvider) Processes() []scanner.ProcessInfo {
	return m.processes
}

func (m *mockScannerProvider) GetTelemetryStatus(p scanner.ProcessInfo) scanner.StatusInfo {
	if s, ok := m.statuses[p.PID]; ok {
		return s
	}
	return scanner.StatusInfo{Status: scanner.TelemetryUnknown, Icon: "??", Label: "Unknown"}
}

func (m *mockScannerProvider) Rescan() {}

func TestRenderStartup_NoProcesses(t *testing.T) {
	cfg := config.DefaultConfig()
	mockScanner := &mockScannerProvider{processes: nil}

	m := NewModel(cfg, WithStartView(ViewStartup), WithScannerProvider(mockScanner))
	m.width = 120
	m.height = 40

	view := m.renderStartup()
	if !strings.Contains(view, "No Claude Code instances found") {
		t.Error("startup with no processes should show 'No Claude Code instances found'")
	}
	if !strings.Contains(view, "[E]") {
		t.Error("startup should show [E] action key")
	}
	if !strings.Contains(view, "[Enter]") {
		t.Error("startup should show [Enter] action key")
	}
}

func TestRenderStartup_WithProcesses(t *testing.T) {
	cfg := config.DefaultConfig()
	mockScanner := &mockScannerProvider{
		processes: []scanner.ProcessInfo{
			{
				PID:         4821,
				Terminal:    "iTerm2",
				CWD:         "/Users/test/myapp",
				EnvReadable: true,
				EnvVars: map[string]string{
					"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
					"OTEL_EXPORTER_OTLP_ENDPOINT":  "http://localhost:4317",
				},
			},
			{
				PID:         5102,
				Terminal:    "VS Code",
				CWD:         "/Users/test/api",
				EnvReadable: true,
				EnvVars: map[string]string{
					"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
					"OTEL_EXPORTER_OTLP_ENDPOINT":  "http://localhost:9090",
				},
			},
			{
				PID:         6017,
				Terminal:    "tmux",
				CWD:         "/Users/test/tools",
				EnvReadable: true,
				EnvVars:     map[string]string{},
			},
		},
		statuses: map[int]scanner.StatusInfo{
			4821: {Status: scanner.TelemetryConnected, Icon: "OK", Label: "Connected"},
			5102: {Status: scanner.TelemetryWrongPort, Icon: "!!", Label: "Wrong port"},
			6017: {Status: scanner.TelemetryOff, Icon: "NO", Label: "No telemetry"},
		},
	}

	m := NewModel(cfg, WithStartView(ViewStartup), WithScannerProvider(mockScanner))
	m.width = 120
	m.height = 40

	view := m.renderStartup()
	if !strings.Contains(view, "4821") {
		t.Error("startup should show PID 4821")
	}
	if !strings.Contains(view, "5102") {
		t.Error("startup should show PID 5102")
	}
	if !strings.Contains(view, "6017") {
		t.Error("startup should show PID 6017")
	}
	if !strings.Contains(view, "1 connected") {
		t.Error("startup summary should show '1 connected'")
	}
	if !strings.Contains(view, "1 misconfigured") {
		t.Error("startup summary should show '1 misconfigured'")
	}
	if !strings.Contains(view, "1 no telemetry") {
		t.Error("startup summary should show '1 no telemetry'")
	}
}

func TestRenderStartup_NilScanner(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewStartup))
	m.width = 120
	m.height = 40

	// Should not panic.
	view := m.renderStartup()
	if !strings.Contains(view, "No Claude Code instances found") {
		t.Error("startup with nil scanner should show 'No Claude Code instances found'")
	}
}

func TestRenderStartup_Message(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewStartup))
	m.width = 120
	m.height = 40
	m.startupMessage = "Settings written successfully"

	view := m.renderStartup()
	if !strings.Contains(view, "Settings written successfully") {
		t.Error("startup should display the startupMessage")
	}
}

func TestFormatTelemetryIcon(t *testing.T) {
	tests := []struct {
		status scanner.TelemetryStatus
		want   string
	}{
		{scanner.TelemetryConnected, "OK ON"},
		{scanner.TelemetryWaiting, "OK ON"},
		{scanner.TelemetryWrongPort, "!! ON"},
		{scanner.TelemetryConsoleOnly, "!! ON"},
		{scanner.TelemetryOff, "NO OFF"},
		{scanner.TelemetryUnknown, "?? ???"},
	}

	for _, tt := range tests {
		got := formatTelemetryIcon(tt.status)
		if got != tt.want {
			t.Errorf("formatTelemetryIcon(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestFormatOTLPDest(t *testing.T) {
	tests := []struct {
		name string
		p    scanner.ProcessInfo
		want string
	}{
		{
			name: "no endpoint",
			p:    scanner.ProcessInfo{EnvVars: map[string]string{}},
			want: "--",
		},
		{
			name: "with port",
			p:    scanner.ProcessInfo{EnvVars: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4317"}},
			want: ":4317",
		},
		{
			name: "nil env vars",
			p:    scanner.ProcessInfo{},
			want: "--",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatOTLPDest(tt.p)
			if got != tt.want {
				t.Errorf("formatOTLPDest() = %q, want %q", got, tt.want)
			}
		})
	}
}
