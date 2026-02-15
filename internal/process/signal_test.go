package process

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestSendSignal_InvalidPID(t *testing.T) {
	err := SendSignal(0, SignalKill)
	if err == nil {
		t.Error("SendSignal(0) should return error")
	}

	err = SendSignal(-1, SignalKill)
	if err == nil {
		t.Error("SendSignal(-1) should return error")
	}
}

func TestSendSignal_NonExistentProcess(t *testing.T) {
	// Use a very high PID that is unlikely to exist.
	err := SendSignal(4999999, SignalKill)
	if err == nil {
		t.Error("SendSignal to non-existent PID should return error")
	}
	if !IsNoSuchProcess(err) {
		t.Errorf("error should be 'no such process', got: %v", err)
	}
}

func TestIsNoSuchProcess(t *testing.T) {
	if !IsNoSuchProcess(errNoSuchProcess) {
		t.Error("IsNoSuchProcess(errNoSuchProcess) should return true")
	}

	if IsNoSuchProcess(nil) {
		t.Error("IsNoSuchProcess(nil) should return false")
	}

	if IsNoSuchProcess(os.ErrNotExist) {
		t.Error("IsNoSuchProcess(ErrNotExist) should return false")
	}
}

func TestToOSSignal(t *testing.T) {
	tests := []struct {
		sig  SignalType
		want os.Signal
	}{
		{SignalStop, syscall.SIGSTOP},
		{SignalKill, syscall.SIGKILL},
		{SignalContinue, syscall.SIGCONT},
		{SignalTerminate, syscall.SIGTERM},
	}

	for _, tt := range tests {
		got := toOSSignal(tt.sig)
		if got != tt.want {
			t.Errorf("toOSSignal(%d) = %v, want %v", tt.sig, got, tt.want)
		}
	}
}

func TestToOSSignal_Unknown(t *testing.T) {
	got := toOSSignal(SignalType(99))
	if got != nil {
		t.Errorf("toOSSignal(99) = %v, want nil", got)
	}
}

func TestSendSignal_UnknownSignalType(t *testing.T) {
	err := SendSignal(1, SignalType(99))
	if err == nil {
		t.Error("SendSignal with unknown signal type should return error")
	}
}

func TestCheckProcess_Self(t *testing.T) {
	// Our own process should exist.
	err := CheckProcess(os.Getpid())
	if err != nil {
		t.Errorf("CheckProcess(self) should succeed, got: %v", err)
	}
}

func TestCheckProcess_NonExistent(t *testing.T) {
	err := CheckProcess(4999999)
	if err == nil {
		t.Error("CheckProcess non-existent PID should return error")
	}
	if !IsNoSuchProcess(err) {
		t.Errorf("error should be 'no such process', got: %v", err)
	}
}

func TestCheckProcess_InvalidPID(t *testing.T) {
	err := CheckProcess(0)
	if err == nil {
		t.Error("CheckProcess(0) should return error")
	}

	err = CheckProcess(-1)
	if err == nil {
		t.Error("CheckProcess(-1) should return error")
	}
}

func TestSendSignal_RealProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real process test in short mode")
	}

	// Start a sleep process that we can safely signal.
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sleep process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	pid := cmd.Process.Pid

	// Give the process a moment to start.
	time.Sleep(100 * time.Millisecond)

	// Verify the process exists.
	err := CheckProcess(pid)
	if err != nil {
		t.Fatalf("sleep process should exist, got: %v", err)
	}

	// Send SIGSTOP.
	err = SendSignal(pid, SignalStop)
	if err != nil {
		t.Errorf("SendSignal(SIGSTOP) failed: %v", err)
	}

	// Send SIGCONT to resume.
	err = SendSignal(pid, SignalContinue)
	if err != nil {
		t.Errorf("SendSignal(SIGCONT) failed: %v", err)
	}

	// Send SIGTERM.
	err = SendSignal(pid, SignalTerminate)
	if err != nil {
		t.Errorf("SendSignal(SIGTERM) failed: %v", err)
	}

	// Wait for process to exit.
	_ = cmd.Wait()

	// Now the process should not exist.
	time.Sleep(100 * time.Millisecond)
	err = CheckProcess(pid)
	if err == nil {
		// May still exist briefly; try SIGKILL.
		_ = SendSignal(pid, SignalKill)
	}
}
