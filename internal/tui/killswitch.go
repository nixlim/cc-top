package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nixlim/cc-top/internal/process"
	"github.com/nixlim/cc-top/internal/state"
)

// initiateKillSwitch begins the kill sequence. If a session is selected,
// it sends SIGSTOP and shows the confirmation dialog. If no session is
// selected, it selects the first active session.
func (m Model) initiateKillSwitch() (tea.Model, tea.Cmd) {
	sessions := m.getSessions()
	if len(sessions) == 0 {
		return m, nil
	}

	var target *state.SessionData

	if m.selectedSession != "" {
		// Use the selected session.
		for i := range sessions {
			if sessions[i].SessionID == m.selectedSession {
				target = &sessions[i]
				break
			}
		}
	} else {
		// No session selected: use the session under the cursor.
		if m.sessionCursor >= 0 && m.sessionCursor < len(sessions) {
			target = &sessions[m.sessionCursor]
		}
	}

	if target == nil {
		return m, nil
	}

	// Check if session already exited.
	if target.Exited {
		m.startupMessage = "Session already exited"
		return m, nil
	}

	// Check if we have a PID.
	if target.PID <= 0 {
		m.startupMessage = "No PID available for this session"
		return m, nil
	}

	// Send SIGSTOP to freeze the process.
	err := process.SendSignal(target.PID, process.SignalStop)
	if err != nil {
		if process.IsNoSuchProcess(err) {
			m.startupMessage = "Session already exited"
			return m, nil
		}
		m.startupMessage = fmt.Sprintf("Error stopping process: %v", err)
		return m, nil
	}

	// Show confirmation dialog.
	m.killConfirm = true
	m.killTargetPID = target.PID
	m.killTargetInfo = fmt.Sprintf("Session: %s\nPID: %d\nCWD: %s",
		truncateID(target.SessionID, 12),
		target.PID,
		target.CWD)

	return m, nil
}

// handleKillConfirmKey handles Y/N/Esc in the kill confirmation dialog.
func (m Model) handleKillConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Confirm):
		// User confirmed: send SIGKILL.
		err := process.SendSignal(m.killTargetPID, process.SignalKill)
		if err != nil && !process.IsNoSuchProcess(err) {
			m.startupMessage = fmt.Sprintf("Error killing process: %v", err)
		}
		m.killConfirm = false
		m.killTargetPID = 0
		m.killTargetInfo = ""
		return m, nil

	case key.Matches(msg, m.keys.Deny), key.Matches(msg, m.keys.Escape):
		// User cancelled: send SIGCONT to resume.
		err := process.SendSignal(m.killTargetPID, process.SignalContinue)
		if err != nil && !process.IsNoSuchProcess(err) {
			m.startupMessage = fmt.Sprintf("Error resuming process: %v", err)
		}
		m.killConfirm = false
		m.killTargetPID = 0
		m.killTargetInfo = ""
		return m, nil
	}

	return m, nil
}
