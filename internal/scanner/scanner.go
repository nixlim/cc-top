// Package scanner discovers Claude Code processes on macOS using libproc APIs.
// It periodically scans for processes named "claude" or node processes with
// "@anthropic-ai/claude-code" in their argv, extracting PID, binary name,
// CWD, terminal type, and environment variables for telemetry classification.
package scanner

import (
	"os"
	"strings"
	"sync"
	"time"
)

// RawProcessInfo holds data returned by the low-level process API before
// enrichment with argv/env/CWD.
type RawProcessInfo struct {
	PID        int
	BinaryName string
}

// ProcessAPI abstracts the low-level OS process inspection calls.
// Production code uses darwinProcessAPI (cgo/libproc); tests use mocks.
type ProcessAPI interface {
	// ListAllPIDs returns all PIDs visible to the current user.
	ListAllPIDs() ([]int, error)

	// GetProcessInfo returns basic info (PID, binary name) for a given PID.
	GetProcessInfo(pid int) (*RawProcessInfo, error)

	// GetProcessArgs reads the full argv and environment variables for a PID.
	// Returns (args, envVars, error). envVars maps KEY to VALUE.
	GetProcessArgs(pid int) (args []string, envVars map[string]string, err error)

	// GetProcessCWD returns the current working directory for a PID.
	GetProcessCWD(pid int) (string, error)

	// GetOpenPorts returns local/remote port pairs for TCP sockets owned by pid.
	GetOpenPorts(pid int) ([][2]int, error)
}

// Scanner discovers and tracks Claude Code processes.
type Scanner struct {
	api      ProcessAPI
	interval time.Duration

	mu      sync.RWMutex
	current map[int]*ProcessInfo // currently known live processes
	seen    map[int]bool         // PIDs seen in any previous scan (for IsNew tracking)
	exited  map[int]*ProcessInfo // exited processes preserved for display

	stopCh chan struct{}
	done   chan struct{}
}

// NewScanner creates a Scanner with the given ProcessAPI and scan interval.
func NewScanner(api ProcessAPI, interval time.Duration) *Scanner {
	return &Scanner{
		api:      api,
		interval: interval,
		current:  make(map[int]*ProcessInfo),
		seen:     make(map[int]bool),
		exited:   make(map[int]*ProcessInfo),
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// NewDefaultScanner creates a Scanner using the real macOS libproc API
// and the given scan interval in seconds. This is the production constructor.
func NewDefaultScanner(intervalSeconds int) *Scanner {
	return NewScanner(newDarwinProcessAPI(), time.Duration(intervalSeconds)*time.Second)
}

// Scan performs a single scan cycle: discovers Claude Code processes,
// enriches them with argv/env/CWD, and tracks new/exited state.
func (s *Scanner) Scan() []ProcessInfo {
	pids, err := s.api.ListAllPIDs()
	if err != nil {
		// If we can't list PIDs at all, return whatever we have.
		return s.listAll()
	}

	discovered := make(map[int]*ProcessInfo)

	for _, pid := range pids {
		raw, err := s.api.GetProcessInfo(pid)
		if err != nil {
			continue
		}

		args, envVars, envErr := s.api.GetProcessArgs(pid)
		envReadable := envErr == nil

		if !isClaude(raw.BinaryName, args) {
			continue
		}

		cwd, _ := s.api.GetProcessCWD(pid)

		info := &ProcessInfo{
			PID:         pid,
			BinaryName:  raw.BinaryName,
			Args:        args,
			CWD:         shortenHome(cwd),
			Terminal:    detectTerminal(envVars),
			EnvVars:     filterTelemetryEnvVars(envVars),
			EnvReadable: envReadable,
		}

		discovered[pid] = info
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Mark new PIDs: a PID is new if it has never been seen before.
	for pid, info := range discovered {
		if !s.seen[pid] {
			info.IsNew = true
		}
	}

	// Detect exited PIDs (present in current but not in discovered).
	for pid, prev := range s.current {
		if _, stillAlive := discovered[pid]; !stillAlive {
			exited := *prev
			exited.Exited = true
			exited.IsNew = false
			s.exited[pid] = &exited
		}
	}

	// Record all discovered PIDs as seen.
	for pid := range discovered {
		s.seen[pid] = true
	}

	s.current = discovered

	return s.listAllLocked()
}

// StartPeriodicScan starts background periodic scanning at the configured
// interval. Call Stop() to halt. The initial scan runs immediately.
func (s *Scanner) StartPeriodicScan() {
	go func() {
		defer close(s.done)
		s.Scan()

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.Scan()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop halts periodic scanning and waits for the goroutine to exit.
func (s *Scanner) Stop() {
	close(s.stopCh)
	<-s.done
}

// GetProcesses returns all currently tracked processes (alive + exited).
func (s *Scanner) GetProcesses() []ProcessInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listAllLocked()
}

// listAll returns all processes under the read lock.
func (s *Scanner) listAll() []ProcessInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listAllLocked()
}

// listAllLocked returns all processes; caller must hold the lock.
func (s *Scanner) listAllLocked() []ProcessInfo {
	result := make([]ProcessInfo, 0, len(s.current)+len(s.exited))
	for _, info := range s.current {
		result = append(result, *info)
	}
	for _, info := range s.exited {
		// Don't include exited PIDs that reappeared (shouldn't happen, but be safe).
		if _, alive := s.current[info.PID]; !alive {
			result = append(result, *info)
		}
	}
	return result
}

// isClaude returns true if the process is a Claude Code instance.
// Detection: process name is "claude" (but not "claude-helper" or similar),
// or it's a node process with "@anthropic-ai/claude-code" in argv.
func isClaude(binaryName string, args []string) bool {
	name := strings.ToLower(binaryName)

	// Direct "claude" binary match. Reject false positives like "claude-helper".
	if name == "claude" {
		return true
	}

	// Node process with Claude Code module path in arguments.
	if name == "node" || name == "nodejs" {
		for _, arg := range args {
			if strings.Contains(arg, "@anthropic-ai/claude-code") {
				return true
			}
		}
	}

	return false
}

// detectTerminal guesses the terminal type from environment variables.
func detectTerminal(envVars map[string]string) string {
	if envVars == nil {
		return ""
	}

	// Check TERM_PROGRAM first (most reliable).
	if tp := envVars["TERM_PROGRAM"]; tp != "" {
		switch strings.ToLower(tp) {
		case "iterm.app":
			return "iTerm2"
		case "apple_terminal":
			return "Terminal"
		case "vscode":
			return "VS Code"
		case "cursor":
			return "Cursor"
		default:
			return tp
		}
	}

	// Check for tmux.
	if envVars["TMUX"] != "" {
		return "tmux"
	}

	// Check for VS Code via VSCODE_PID.
	if envVars["VSCODE_PID"] != "" {
		return "VS Code"
	}

	// Check for Cursor editor.
	if envVars["CURSOR_CHANNEL"] != "" {
		return "Cursor"
	}

	return ""
}

// filterTelemetryEnvVars extracts only the telemetry-related env vars
// we care about for classification.
func filterTelemetryEnvVars(envVars map[string]string) map[string]string {
	if envVars == nil {
		return make(map[string]string)
	}

	keys := []string{
		"CLAUDE_CODE_ENABLE_TELEMETRY",
		"OTEL_METRICS_EXPORTER",
		"OTEL_LOGS_EXPORTER",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
	}

	result := make(map[string]string)
	for _, k := range keys {
		if v, ok := envVars[k]; ok {
			result[k] = v
		}
	}
	return result
}

// shortenHome replaces the user's home directory prefix with ~.
func shortenHome(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
