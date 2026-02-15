package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"
)

func TestTruncateCWD(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name   string
		cwd    string
		maxLen int
		want   string
	}{
		{
			name:   "empty",
			cwd:    "",
			maxLen: 20,
			want:   "\u2014", // em dash
		},
		{
			name:   "short path",
			cwd:    "/usr/bin",
			maxLen: 20,
			want:   "/usr/bin",
		},
		{
			name:   "home directory replacement",
			cwd:    filepath.Join(home, "projects"),
			maxLen: 20,
			want:   "~/projects",
		},
		{
			name:   "long path truncated",
			cwd:    "/very/long/path/to/some/deeply/nested/directory",
			maxLen: 20,
			want:   "", // just check it fits
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateCWD(tt.cwd, tt.maxLen)
			if len(got) > tt.maxLen+10 { // some tolerance for unicode
				t.Errorf("truncateCWD(%q, %d) = %q (len %d), exceeds maxLen",
					tt.cwd, tt.maxLen, got, len(got))
			}
			if tt.want != "" && got != tt.want {
				t.Errorf("truncateCWD(%q, %d) = %q, want %q",
					tt.cwd, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hell."},
		{"hi", 3, "hi"},
		{"abcd", 3, "abc"},
	}

	for _, tt := range tests {
		got := truncateStr(tt.s, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
		}
	}
}

func TestTruncateID(t *testing.T) {
	tests := []struct {
		id     string
		maxLen int
		want   string
	}{
		{"abcdefgh", 8, "abcdefgh"},
		{"abcdefghij", 8, "abcdefgh"},
		{"abc", 8, "abc"},
	}

	for _, tt := range tests {
		got := truncateID(tt.id, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateID(%q, %d) = %q, want %q", tt.id, tt.maxLen, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{3600 * time.Second, "1h0m"},
		{3661 * time.Second, "1h1m"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestSplitSessionsByTelemetry(t *testing.T) {
	sessions := []state.SessionData{
		{SessionID: "with-data", LastEventAt: time.Now()},
		{SessionID: "no-data"},
		{SessionID: "with-metrics", Metrics: []state.Metric{{Name: "test"}}},
	}

	withTel, withoutTel := splitSessionsByTelemetry(sessions)

	if len(withTel) != 2 {
		t.Errorf("withTelemetry count = %d, want 2", len(withTel))
	}
	if len(withoutTel) != 1 {
		t.Errorf("withoutTelemetry count = %d, want 1", len(withoutTel))
	}
	if withoutTel[0].SessionID != "no-data" {
		t.Errorf("withoutTelemetry[0].SessionID = %q, want %q", withoutTel[0].SessionID, "no-data")
	}
}

func TestRenderSessionListPanel_Empty(t *testing.T) {
	cfg := config.DefaultConfig()
	mockState := &mockStateProvider{sessions: nil}

	m := NewModel(cfg, WithStateProvider(mockState), WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	panel := m.renderSessionListPanel(48, 30)
	if !strings.Contains(panel, "No sessions found") {
		t.Error("empty session list should show 'No sessions found'")
	}
}

func TestRenderSessionListPanel_WithSessions(t *testing.T) {
	cfg := config.DefaultConfig()
	mockState := &mockStateProvider{
		sessions: []state.SessionData{
			{
				SessionID:   "sess-001-abc",
				PID:         1234,
				Terminal:    "iTerm2",
				CWD:         "/Users/test/project",
				Model:       "sonnet-4.5",
				TotalCost:   1.50,
				TotalTokens: 50000,
				LastEventAt: time.Now(),
				StartedAt:   time.Now().Add(-30 * time.Minute),
			},
			{
				SessionID: "no-tel-session",
				PID:       5678,
				Terminal:  "VS Code",
				CWD:       "/Users/test/other",
				StartedAt: time.Now(),
			},
		},
	}

	m := NewModel(cfg, WithStateProvider(mockState), WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	panel := m.renderSessionListPanel(48, 30)
	if !strings.Contains(panel, "Sessions") {
		t.Error("session list should contain 'Sessions' title")
	}
	if !strings.Contains(panel, "1234") {
		t.Error("session list should contain PID 1234")
	}
}

func TestRenderSessionListPanel_SelectedSession(t *testing.T) {
	cfg := config.DefaultConfig()
	mockState := &mockStateProvider{
		sessions: []state.SessionData{
			{SessionID: "sess-001", PID: 1, LastEventAt: time.Now()},
		},
	}

	m := NewModel(cfg, WithStateProvider(mockState), WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40
	m.selectedSession = "sess-001"

	panel := m.renderSessionListPanel(48, 30)
	if !strings.Contains(panel, "sess-001") {
		t.Error("selected session should be shown in panel title")
	}
}

func TestRenderStatus(t *testing.T) {
	tests := []struct {
		status state.SessionStatus
	}{
		{state.StatusActive},
		{state.StatusIdle},
		{state.StatusDone},
		{state.StatusExited},
	}

	for _, tt := range tests {
		got := renderStatus(tt.status)
		if got == "" {
			t.Errorf("renderStatus(%v) returned empty string", tt.status)
		}
	}
}

func TestFormatSessionRow_Widths(t *testing.T) {
	s := &state.SessionData{
		SessionID:   "sess-001-abcdef",
		PID:         1234,
		Terminal:    "iTerm2",
		CWD:         "/Users/test/project",
		Model:       "sonnet",
		TotalCost:   1.50,
		TotalTokens: 50000,
		ActiveTime:  10 * time.Minute,
		LastEventAt: time.Now(),
	}

	// Test different widths.
	for _, w := range []int{100, 70, 40} {
		row := formatSessionRow(s, w)
		if row == "" {
			t.Errorf("formatSessionRow at width %d returned empty", w)
		}
	}
}

func TestFormatSessionRow_NoPID(t *testing.T) {
	s := &state.SessionData{
		SessionID: "sess-001",
		PID:       0,
	}

	row := formatSessionRow(s, 100)
	if !strings.Contains(row, "\u2014") { // em dash
		t.Error("session with PID 0 should show em-dash")
	}
}
