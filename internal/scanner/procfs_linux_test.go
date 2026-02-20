//go:build linux

package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestLinuxProcessAPI_ListAllPIDs_IncludesSelf(t *testing.T) {
	api := newLinuxProcessAPI()
	pids, err := api.ListAllPIDs()
	if err != nil {
		t.Fatalf("ListAllPIDs() error: %v", err)
	}

	myPID := os.Getpid()
	found := false
	for _, pid := range pids {
		if pid == myPID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListAllPIDs() did not include own PID %d", myPID)
	}
}

func TestLinuxProcessAPI_GetProcessInfo_Self(t *testing.T) {
	api := newLinuxProcessAPI()
	info, err := api.GetProcessInfo(os.Getpid())
	if err != nil {
		t.Fatalf("GetProcessInfo() error: %v", err)
	}
	if info.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", info.PID, os.Getpid())
	}
	if info.BinaryName == "" {
		t.Error("BinaryName should not be empty for current process")
	}
}

func TestLinuxProcessAPI_GetProcessArgs_Self(t *testing.T) {
	api := newLinuxProcessAPI()
	args, envVars, err := api.GetProcessArgs(os.Getpid())
	if err != nil {
		t.Fatalf("GetProcessArgs() error: %v", err)
	}
	if len(args) == 0 {
		t.Error("args should not be empty for current process")
	}
	// HOME should be in envVars for most environments.
	if _, ok := envVars["HOME"]; !ok {
		t.Log("HOME not found in envVars (may be expected in some CI environments)")
	}
}

func TestLinuxProcessAPI_GetProcessCWD_Self(t *testing.T) {
	api := newLinuxProcessAPI()
	cwd, err := api.GetProcessCWD(os.Getpid())
	if err != nil {
		t.Fatalf("GetProcessCWD() error: %v", err)
	}
	if cwd == "" {
		t.Error("CWD should not be empty")
	}
	// It should match os.Getwd().
	expected, err := os.Getwd()
	if err == nil && cwd != expected {
		t.Errorf("CWD = %q, want %q", cwd, expected)
	}
}

func TestLinuxProcessAPI_GetOpenPorts_Self(t *testing.T) {
	api := newLinuxProcessAPI()
	// Should not error; may return empty slice.
	_, err := api.GetOpenPorts(os.Getpid())
	if err != nil {
		t.Fatalf("GetOpenPorts() error: %v", err)
	}
}

func TestLinuxProcessAPI_GetProcessInfo_NonexistentPID(t *testing.T) {
	api := newLinuxProcessAPI()
	_, err := api.GetProcessInfo(999999999)
	if err == nil {
		t.Error("expected error for nonexistent PID")
	}
}

func TestReadProcUID(t *testing.T) {
	uid, err := readProcUID(os.Getpid())
	if err != nil {
		t.Fatalf("readProcUID() error: %v", err)
	}
	if uid != os.Getuid() {
		t.Errorf("UID = %d, want %d", uid, os.Getuid())
	}
}

func TestCollectSocketInodes(t *testing.T) {
	// Should not error for the current process.
	inodes, err := collectSocketInodes(os.Getpid())
	if err != nil {
		t.Fatalf("collectSocketInodes() error: %v", err)
	}
	// Just verify it returns a valid map (may be empty if no sockets open).
	if inodes == nil {
		t.Error("expected non-nil map")
	}
}

func TestParseHexPort(t *testing.T) {
	tests := []struct {
		input    string
		wantPort int
		wantErr  bool
	}{
		{"0100007F:1A0B", 0x1A0B, false}, // 127.0.0.1:6667
		{"00000000:10E1", 0x10E1, false}, // 0.0.0.0:4321
		{"0100007F:0000", 0, false},
		{"invalid", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			port, err := parseHexPort(tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("parseHexPort(%q) error = %v, wantErr = %v", tc.input, err, tc.wantErr)
				return
			}
			if !tc.wantErr && port != tc.wantPort {
				t.Errorf("parseHexPort(%q) = %d, want %d", tc.input, port, tc.wantPort)
			}
		})
	}
}

func TestParseProcNetTCP(t *testing.T) {
	// Create a temporary file mimicking /proc/net/tcp format.
	dir := t.TempDir()
	tcpFile := filepath.Join(dir, "tcp")

	// Inode 12345 with local port 0x10E1 (4321) and remote port 0x1A0B (6667).
	content := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:10E1 0100007F:1A0B 01 00000000:00000000 00:00000000 00000000  1000        0 12345 1 0000000000000000 100 0 0 10 0
   1: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 99999 1 0000000000000000 100 0 0 10 0
`
	if err := os.WriteFile(tcpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	inodes := map[uint64]bool{12345: true}
	ports, err := parseProcNetTCP(tcpFile, inodes)
	if err != nil {
		t.Fatalf("parseProcNetTCP() error: %v", err)
	}

	if len(ports) != 1 {
		t.Fatalf("got %d port pairs, want 1", len(ports))
	}
	if ports[0][0] != 0x10E1 {
		t.Errorf("local port = %d, want %d", ports[0][0], 0x10E1)
	}
	if ports[0][1] != 0x1A0B {
		t.Errorf("remote port = %d, want %d", ports[0][1], 0x1A0B)
	}
}

func TestNewDefaultScanner_Linux(t *testing.T) {
	s := NewDefaultScanner(5)
	if s == nil {
		t.Fatal("NewDefaultScanner returned nil")
	}
	if s.api == nil {
		t.Error("Scanner API should not be nil")
	}
	if len(s.globalConfigPaths) == 0 {
		t.Error("globalConfigPaths should not be empty")
	}

	// Should contain the Linux managed settings path.
	foundLinux := false
	for _, p := range s.globalConfigPaths {
		if p == filepath.Join("/etc", "claude-code", "managed-settings.json") {
			foundLinux = true
		}
	}
	if !foundLinux {
		t.Errorf("expected Linux managed settings path, got %v", s.globalConfigPaths)
	}

	// The PID is just used to close the stop channel; use a simple int.
	_ = fmt.Sprintf("scanner interval: %v", s.interval)
	_ = strconv.Itoa(len(s.globalConfigPaths))
}
