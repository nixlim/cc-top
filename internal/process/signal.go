// Package process provides signal-sending utilities for the kill switch.
// On macOS/Linux, it sends POSIX signals to process groups.
package process

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
)

// SignalType represents the type of signal to send to a process.
type SignalType int

const (
	// SignalStop sends SIGSTOP to freeze a process.
	SignalStop SignalType = iota
	// SignalKill sends SIGKILL to terminate a process.
	SignalKill
	// SignalContinue sends SIGCONT to resume a frozen process.
	SignalContinue
	// SignalTerminate sends SIGTERM for graceful termination.
	SignalTerminate
)

// errNoSuchProcess is returned when the target process does not exist.
var errNoSuchProcess = errors.New("no such process")

// SendSignal sends the specified signal to the process group of the given PID.
// It returns errNoSuchProcess if the process has already exited (ESRCH).
// The signal is first sent to the negative PID (process group) so that child
// processes are also affected. If the process group signal fails (e.g., process
// is not a group leader), it falls back to sending to the individual PID.
func SendSignal(pid int, sig SignalType) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID: %d", pid)
	}

	osSig := toOSSignal(sig)
	if osSig == nil {
		return fmt.Errorf("unknown signal type: %d", sig)
	}

	sysSig := osSig.(syscall.Signal)

	// Try sending to process group first (negative PID).
	pgErr := syscall.Kill(-pid, sysSig)
	if pgErr == nil {
		return nil
	}

	// If ESRCH on process group, try the individual PID.
	// The process might not be a process group leader.
	if errors.Is(pgErr, syscall.ESRCH) || errors.Is(pgErr, syscall.EPERM) {
		pidErr := syscall.Kill(pid, sysSig)
		if pidErr == nil {
			return nil
		}
		if isProcessGone(pidErr) {
			return errNoSuchProcess
		}
		return fmt.Errorf("sending signal to PID %d: %w", pid, pidErr)
	}

	return fmt.Errorf("sending signal to process group %d: %w", pid, pgErr)
}

// IsNoSuchProcess returns true if the error indicates the process does not exist.
func IsNoSuchProcess(err error) bool {
	return errors.Is(err, errNoSuchProcess)
}

// toOSSignal converts a SignalType to an os.Signal.
func toOSSignal(sig SignalType) os.Signal {
	switch sig {
	case SignalStop:
		return syscall.SIGSTOP
	case SignalKill:
		return syscall.SIGKILL
	case SignalContinue:
		return syscall.SIGCONT
	case SignalTerminate:
		return syscall.SIGTERM
	default:
		return nil
	}
}

// isProcessGone returns true if the error indicates the process does not exist.
// It checks for ESRCH errno as well as the "os: process already finished" string
// returned by os.Process.Signal.
func isProcessGone(err error) bool {
	if err == nil {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.ESRCH
	}
	// os.Process.Signal returns "os: process already finished" which is not
	// a syscall.Errno, so we check the error string as a fallback.
	return strings.Contains(err.Error(), "process already finished") ||
		strings.Contains(err.Error(), "no such process")
}

// CheckProcess checks if a process with the given PID exists and is alive.
// Returns nil if the process exists, errNoSuchProcess if it doesn't.
func CheckProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid PID: %d", pid)
	}

	// Use syscall.Kill with signal 0 to check process existence.
	// This avoids the os.Process wrapper which can return confusing errors.
	err := syscall.Kill(pid, 0)
	if err == nil {
		return nil
	}

	if errors.Is(err, syscall.ESRCH) {
		return errNoSuchProcess
	}

	// EPERM means process exists but we don't have permission to signal it.
	if errors.Is(err, syscall.EPERM) {
		return nil
	}

	return errNoSuchProcess
}
