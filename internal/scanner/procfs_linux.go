//go:build linux

package scanner

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// linuxProcessAPI implements ProcessAPI using the Linux /proc filesystem.
// No CGO is required.
type linuxProcessAPI struct{}

// newLinuxProcessAPI returns a ProcessAPI backed by procfs.
func newLinuxProcessAPI() ProcessAPI {
	return &linuxProcessAPI{}
}

// ListAllPIDs returns all PIDs owned by the current user by scanning /proc.
func (l *linuxProcessAPI) ListAllPIDs() ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read /proc: %w", err)
	}

	currentUID := os.Getuid()
	var pids []int

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}

		// Check ownership via /proc/[pid]/status Uid field.
		uid, err := readProcUID(pid)
		if err != nil {
			continue
		}
		if uid == currentUID {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// GetProcessInfo returns the binary name for a PID from /proc/[pid]/comm.
func (l *linuxProcessAPI) GetProcessInfo(pid int) (*RawProcessInfo, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return nil, fmt.Errorf("read comm for pid %d: %w", pid, err)
	}
	name := strings.TrimSpace(string(data))
	return &RawProcessInfo{
		PID:        pid,
		BinaryName: name,
	}, nil
}

// GetProcessArgs reads argv from /proc/[pid]/cmdline and environment
// variables from /proc/[pid]/environ. Both are null-byte separated.
func (l *linuxProcessAPI) GetProcessArgs(pid int) (args []string, envVars map[string]string, err error) {
	// Read cmdline (null-separated argv).
	cmdlineData, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil, nil, fmt.Errorf("read cmdline for pid %d: %w", pid, err)
	}

	// Split on null bytes, dropping trailing empty string.
	if len(cmdlineData) > 0 {
		trimmed := bytes.TrimRight(cmdlineData, "\x00")
		if len(trimmed) > 0 {
			parts := bytes.Split(trimmed, []byte{0})
			args = make([]string, len(parts))
			for i, p := range parts {
				args[i] = string(p)
			}
		}
	}

	// Read environ (null-separated KEY=VALUE pairs).
	envData, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		// Env may be unreadable for some processes; return args without env.
		return args, nil, fmt.Errorf("read environ for pid %d: %w", pid, err)
	}

	envVars = make(map[string]string)
	if len(envData) > 0 {
		trimmed := bytes.TrimRight(envData, "\x00")
		if len(trimmed) > 0 {
			entries := bytes.Split(trimmed, []byte{0})
			for _, entry := range entries {
				if eqIdx := bytes.IndexByte(entry, '='); eqIdx >= 0 {
					envVars[string(entry[:eqIdx])] = string(entry[eqIdx+1:])
				}
			}
		}
	}

	return args, envVars, nil
}

// GetProcessCWD returns the current working directory for a PID
// by reading the /proc/[pid]/cwd symlink.
func (l *linuxProcessAPI) GetProcessCWD(pid int) (string, error) {
	cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		return "", fmt.Errorf("readlink cwd for pid %d: %w", pid, err)
	}
	return cwd, nil
}

// GetOpenPorts returns local/remote port pairs for TCP sockets owned by pid.
// Parses /proc/[pid]/net/tcp and /proc/[pid]/net/tcp6.
func (l *linuxProcessAPI) GetOpenPorts(pid int) ([][2]int, error) {
	// Collect all socket inodes owned by this pid from /proc/[pid]/fd.
	inodes, err := collectSocketInodes(pid)
	if err != nil {
		return nil, err
	}
	if len(inodes) == 0 {
		return nil, nil
	}

	var ports [][2]int

	// Parse both TCP and TCP6 tables.
	for _, proto := range []string{"tcp", "tcp6"} {
		path := fmt.Sprintf("/proc/%d/net/%s", pid, proto)
		parsed, err := parseProcNetTCP(path, inodes)
		if err != nil {
			continue // File may not exist (e.g., no IPv6).
		}
		ports = append(ports, parsed...)
	}

	return ports, nil
}

// PgrepClaude uses pgrep as a fallback to find Claude Code PIDs.
func (l *linuxProcessAPI) PgrepClaude() []int {
	return pgrepClaude()
}

// readProcUID reads the real UID from /proc/[pid]/status.
func readProcUID(pid int) (int, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return -1, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				uid, err := strconv.Atoi(fields[1])
				if err != nil {
					return -1, err
				}
				return uid, nil
			}
		}
	}
	return -1, fmt.Errorf("Uid not found in /proc/%d/status", pid)
}

// collectSocketInodes finds all socket inodes referenced by /proc/[pid]/fd.
func collectSocketInodes(pid int) (map[uint64]bool, error) {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil, fmt.Errorf("read fd dir for pid %d: %w", pid, err)
	}

	inodes := make(map[uint64]bool)
	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
		if err != nil {
			continue
		}
		// Socket links look like "socket:[12345]".
		if strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
			inoStr := link[len("socket:[") : len(link)-1]
			ino, err := strconv.ParseUint(inoStr, 10, 64)
			if err != nil {
				continue
			}
			inodes[ino] = true
		}
	}
	return inodes, nil
}

// parseProcNetTCP parses /proc/[pid]/net/tcp or tcp6 and returns port pairs
// for sockets whose inode matches the given set.
// Format: sl local_address rem_address st tx_queue:rx_queue ... inode
func parseProcNetTCP(path string, inodes map[uint64]bool) ([][2]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var ports [][2]int
	sc := bufio.NewScanner(f)

	// Skip header line.
	if !sc.Scan() {
		return nil, nil
	}

	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}

		// Field 9 is the inode.
		ino, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			continue
		}
		if !inodes[ino] {
			continue
		}

		localPort, err := parseHexPort(fields[1])
		if err != nil {
			continue
		}
		remotePort, err := parseHexPort(fields[2])
		if err != nil {
			continue
		}

		if localPort > 0 || remotePort > 0 {
			ports = append(ports, [2]int{localPort, remotePort})
		}
	}

	return ports, nil
}

// parseHexPort extracts the port from a hex-encoded addr:port string
// like "0100007F:1A0B" (IPv4) or longer (IPv6). The port is the part after ":".
func parseHexPort(addrPort string) (int, error) {
	parts := strings.SplitN(addrPort, ":", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid addr:port %q", addrPort)
	}
	portBytes, err := hex.DecodeString(parts[1])
	if err != nil {
		return 0, err
	}
	if len(portBytes) != 2 {
		return 0, fmt.Errorf("unexpected port hex length: %d", len(portBytes))
	}
	return int(portBytes[0])<<8 | int(portBytes[1]), nil
}
