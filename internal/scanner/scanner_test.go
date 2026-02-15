package scanner

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockProcessAPI is a test double for ProcessAPI that allows configuring
// per-PID responses for all process inspection methods.
type mockProcessAPI struct {
	mu        sync.Mutex
	processes map[int]*mockProcess
}

type mockProcess struct {
	info    *RawProcessInfo
	args    []string
	env     map[string]string
	cwd     string
	envErr  error
	infoErr error
	ports   [][2]int
}

func newMockAPI() *mockProcessAPI {
	return &mockProcessAPI{
		processes: make(map[int]*mockProcess),
	}
}

func (m *mockProcessAPI) addProcess(p *mockProcess) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processes[p.info.PID] = p
}

func (m *mockProcessAPI) removeProcess(pid int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.processes, pid)
}

func (m *mockProcessAPI) ListAllPIDs() ([]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pids := make([]int, 0, len(m.processes))
	for pid := range m.processes {
		pids = append(pids, pid)
	}
	return pids, nil
}

func (m *mockProcessAPI) GetProcessInfo(pid int) (*RawProcessInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.processes[pid]
	if !ok {
		return nil, fmt.Errorf("no such process: %d", pid)
	}
	if p.infoErr != nil {
		return nil, p.infoErr
	}
	return p.info, nil
}

func (m *mockProcessAPI) GetProcessArgs(pid int) ([]string, map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.processes[pid]
	if !ok {
		return nil, nil, fmt.Errorf("no such process: %d", pid)
	}
	if p.envErr != nil {
		return nil, nil, p.envErr
	}
	return p.args, p.env, nil
}

func (m *mockProcessAPI) GetProcessCWD(pid int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.processes[pid]
	if !ok {
		return "", fmt.Errorf("no such process: %d", pid)
	}
	return p.cwd, nil
}

func (m *mockProcessAPI) GetOpenPorts(pid int) ([][2]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.processes[pid]
	if !ok {
		return nil, fmt.Errorf("no such process: %d", pid)
	}
	return p.ports, nil
}

func TestProcessScanner_DetectClaude(t *testing.T) {
	api := newMockAPI()
	api.addProcess(&mockProcess{
		info: &RawProcessInfo{PID: 4821, BinaryName: "claude"},
		args: []string{"/usr/local/bin/claude"},
		env: map[string]string{
			"CLAUDE_CODE_ENABLE_TELEMETRY":  "1",
			"OTEL_EXPORTER_OTLP_ENDPOINT":  "http://localhost:4317",
			"TERM_PROGRAM":                 "iTerm.app",
		},
		cwd: "/Users/testuser/myapp",
	})

	// Also add a non-Claude process that should be ignored.
	api.addProcess(&mockProcess{
		info: &RawProcessInfo{PID: 1234, BinaryName: "bash"},
		args: []string{"/bin/bash"},
		env:  map[string]string{},
		cwd:  "/tmp",
	})

	s := NewScanner(api, 5*time.Second)
	results := s.Scan()

	if len(results) != 1 {
		t.Fatalf("got %d processes, want 1 (only claude)", len(results))
	}

	p := results[0]
	if p.PID != 4821 {
		t.Errorf("PID = %d, want 4821", p.PID)
	}
	if p.BinaryName != "claude" {
		t.Errorf("BinaryName = %q, want %q", p.BinaryName, "claude")
	}
	if !p.EnvReadable {
		t.Error("EnvReadable should be true")
	}
	if p.Terminal != "iTerm2" {
		t.Errorf("Terminal = %q, want %q", p.Terminal, "iTerm2")
	}
	if p.EnvVars["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Error("CLAUDE_CODE_ENABLE_TELEMETRY should be '1'")
	}
}

func TestProcessScanner_DetectClaude_FiltersFalsePositives(t *testing.T) {
	api := newMockAPI()
	// "claude-helper" should NOT be detected.
	api.addProcess(&mockProcess{
		info: &RawProcessInfo{PID: 5000, BinaryName: "claude-helper"},
		args: []string{"/usr/local/bin/claude-helper"},
		env:  map[string]string{},
		cwd:  "/tmp",
	})

	s := NewScanner(api, 5*time.Second)
	results := s.Scan()

	if len(results) != 0 {
		t.Errorf("got %d processes, want 0 (claude-helper is a false positive)", len(results))
	}
}

func TestProcessScanner_DetectNodeClaude(t *testing.T) {
	api := newMockAPI()
	api.addProcess(&mockProcess{
		info: &RawProcessInfo{PID: 5102, BinaryName: "node"},
		args: []string{
			"/usr/local/bin/node",
			"/Users/testuser/.npm/_npx/abcdef/node_modules/@anthropic-ai/claude-code/cli.js",
		},
		env: map[string]string{
			"CLAUDE_CODE_ENABLE_TELEMETRY":  "1",
			"OTEL_EXPORTER_OTLP_ENDPOINT":  "http://localhost:4317",
			"VSCODE_PID":                   "1234",
		},
		cwd: "/Users/testuser/api",
	})

	// Node process without claude-code should be ignored.
	api.addProcess(&mockProcess{
		info: &RawProcessInfo{PID: 9999, BinaryName: "node"},
		args: []string{"/usr/local/bin/node", "/some/other/script.js"},
		env:  map[string]string{},
		cwd:  "/tmp",
	})

	s := NewScanner(api, 5*time.Second)
	results := s.Scan()

	if len(results) != 1 {
		t.Fatalf("got %d processes, want 1", len(results))
	}

	p := results[0]
	if p.PID != 5102 {
		t.Errorf("PID = %d, want 5102", p.PID)
	}
	if p.BinaryName != "node" {
		t.Errorf("BinaryName = %q, want %q", p.BinaryName, "node")
	}
	if p.Terminal != "VS Code" {
		t.Errorf("Terminal = %q, want %q", p.Terminal, "VS Code")
	}
}

func TestProcessScanner_NewProcess(t *testing.T) {
	api := newMockAPI()
	api.addProcess(&mockProcess{
		info: &RawProcessInfo{PID: 4821, BinaryName: "claude"},
		args: []string{"/usr/local/bin/claude"},
		env:  map[string]string{},
		cwd:  "/tmp",
	})

	s := NewScanner(api, 5*time.Second)

	// First scan: PID 4821 should be new.
	results := s.Scan()
	found := findPID(results, 4821)
	if found == nil {
		t.Fatal("PID 4821 not found in results")
	}
	if !found.IsNew {
		t.Error("PID 4821 should be IsNew=true on first scan")
	}

	// Second scan: PID 4821 should no longer be new.
	results = s.Scan()
	found = findPID(results, 4821)
	if found == nil {
		t.Fatal("PID 4821 not found in second scan")
	}
	if found.IsNew {
		t.Error("PID 4821 should be IsNew=false on second scan")
	}

	// Add a new process.
	api.addProcess(&mockProcess{
		info: &RawProcessInfo{PID: 7001, BinaryName: "claude"},
		args: []string{"/usr/local/bin/claude"},
		env:  map[string]string{},
		cwd:  "/tmp",
	})

	// Third scan: PID 7001 should be new, PID 4821 should not.
	results = s.Scan()
	found7001 := findPID(results, 7001)
	if found7001 == nil {
		t.Fatal("PID 7001 not found in results")
	}
	if !found7001.IsNew {
		t.Error("PID 7001 should be IsNew=true")
	}

	found4821 := findPID(results, 4821)
	if found4821 == nil {
		t.Fatal("PID 4821 not found in third scan")
	}
	if found4821.IsNew {
		t.Error("PID 4821 should be IsNew=false")
	}
}

func TestProcessScanner_ExitedProcess(t *testing.T) {
	api := newMockAPI()
	api.addProcess(&mockProcess{
		info: &RawProcessInfo{PID: 4821, BinaryName: "claude"},
		args: []string{"/usr/local/bin/claude"},
		env:  map[string]string{"CLAUDE_CODE_ENABLE_TELEMETRY": "1"},
		cwd:  "/Users/testuser/myapp",
	})

	s := NewScanner(api, 5*time.Second)

	// First scan: process is alive.
	results := s.Scan()
	found := findPID(results, 4821)
	if found == nil {
		t.Fatal("PID 4821 not found")
	}
	if found.Exited {
		t.Error("PID 4821 should not be exited on first scan")
	}

	// Second scan to clear IsNew.
	s.Scan()

	// Remove the process (simulates exit).
	api.removeProcess(4821)

	// Third scan: process should appear as exited with data preserved.
	results = s.Scan()
	found = findPID(results, 4821)
	if found == nil {
		t.Fatal("exited PID 4821 should still appear in results")
	}
	if !found.Exited {
		t.Error("PID 4821 should have Exited=true")
	}
	if found.BinaryName != "claude" {
		t.Errorf("BinaryName = %q, want %q (should be preserved)", found.BinaryName, "claude")
	}
	if found.EnvVars["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Error("env vars should be preserved for exited process")
	}
}

func TestProcessScanner_ZombiePermissionDenied(t *testing.T) {
	api := newMockAPI()
	api.addProcess(&mockProcess{
		info:   &RawProcessInfo{PID: 6600, BinaryName: "claude"},
		args:   nil,
		env:    nil,
		envErr: fmt.Errorf("operation not permitted"),
		cwd:    "",
	})

	s := NewScanner(api, 5*time.Second)
	results := s.Scan()

	if len(results) != 1 {
		t.Fatalf("got %d processes, want 1", len(results))
	}

	p := results[0]
	if p.EnvReadable {
		t.Error("EnvReadable should be false when env read fails")
	}
	if p.PID != 6600 {
		t.Errorf("PID = %d, want 6600", p.PID)
	}
}

func TestProcessScanner_TerminalDetection(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantTerm string
	}{
		{
			name:     "iTerm2",
			env:      map[string]string{"TERM_PROGRAM": "iTerm.app"},
			wantTerm: "iTerm2",
		},
		{
			name:     "VS Code via TERM_PROGRAM",
			env:      map[string]string{"TERM_PROGRAM": "vscode"},
			wantTerm: "VS Code",
		},
		{
			name:     "VS Code via VSCODE_PID",
			env:      map[string]string{"VSCODE_PID": "1234"},
			wantTerm: "VS Code",
		},
		{
			name:     "Cursor",
			env:      map[string]string{"TERM_PROGRAM": "cursor"},
			wantTerm: "Cursor",
		},
		{
			name:     "tmux",
			env:      map[string]string{"TMUX": "/tmp/tmux-501/default,12345,0"},
			wantTerm: "tmux",
		},
		{
			name:     "Apple Terminal",
			env:      map[string]string{"TERM_PROGRAM": "Apple_Terminal"},
			wantTerm: "Terminal",
		},
		{
			name:     "unknown terminal",
			env:      map[string]string{},
			wantTerm: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectTerminal(tt.env)
			if got != tt.wantTerm {
				t.Errorf("detectTerminal() = %q, want %q", got, tt.wantTerm)
			}
		})
	}
}

func TestIsClaude(t *testing.T) {
	tests := []struct {
		name       string
		binaryName string
		args       []string
		want       bool
	}{
		{"claude binary", "claude", nil, true},
		{"claude-helper", "claude-helper", nil, false},
		{"node with claude-code", "node", []string{"node", "/path/@anthropic-ai/claude-code/cli.js"}, true},
		{"node without claude-code", "node", []string{"node", "/path/server.js"}, false},
		{"nodejs with claude-code", "nodejs", []string{"nodejs", "/path/@anthropic-ai/claude-code/cli.js"}, true},
		{"random binary", "vim", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isClaude(tt.binaryName, tt.args)
			if got != tt.want {
				t.Errorf("isClaude(%q, %v) = %v, want %v", tt.binaryName, tt.args, got, tt.want)
			}
		})
	}
}

// findPID searches a slice of ProcessInfo for a specific PID.
func findPID(procs []ProcessInfo, pid int) *ProcessInfo {
	for i := range procs {
		if procs[i].PID == pid {
			return &procs[i]
		}
	}
	return nil
}
