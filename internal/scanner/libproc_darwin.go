//go:build darwin

package scanner

/*
#include <libproc.h>
#include <sys/sysctl.h>
#include <sys/proc_info.h>
#include <sys/socket.h>
#include <arpa/inet.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

// sysctl_procargs2 wraps sysctl for KERN_PROCARGS2 and returns the errno.
static int sysctl_procargs2(int pid, void *buf, size_t *size) {
	int mib[3] = { CTL_KERN, KERN_PROCARGS2, pid };
	return sysctl(mib, 3, buf, size, NULL, 0);
}

// port_from_sockinfo extracts local and remote ports from socket info.
// This avoids dealing with the union in Go.
static void get_tcp_ports(struct socket_info *si, int *local_port, int *remote_port) {
	struct in_sockinfo *insi = &si->soi_proto.pri_in;
	*local_port = ntohs((uint16_t)insi->insi_lport);
	*remote_port = ntohs((uint16_t)insi->insi_fport);
}
*/
import "C"

import (
	"bytes"
	"fmt"
	"os"
	"unsafe"
)

// darwinProcessAPI implements ProcessAPI using macOS libproc and sysctl.
type darwinProcessAPI struct{}

// newDarwinProcessAPI returns a ProcessAPI backed by macOS system calls.
func newDarwinProcessAPI() ProcessAPI {
	return &darwinProcessAPI{}
}

// ListAllPIDs returns all PIDs visible to the current user via proc_listallpids.
func (d *darwinProcessAPI) ListAllPIDs() ([]int, error) {
	// First call with nil to get count.
	n := C.proc_listallpids(nil, 0)
	if n <= 0 {
		return nil, fmt.Errorf("proc_listallpids count failed: %d", int(n))
	}

	// Allocate buffer with some extra room for new processes.
	bufSize := int(n) + 64
	buf := make([]C.int, bufSize)
	n = C.proc_listallpids(unsafe.Pointer(&buf[0]), C.int(bufSize*C.sizeof_int))
	if n <= 0 {
		return nil, fmt.Errorf("proc_listallpids failed: %d", int(n))
	}

	// Filter to current user's PIDs only.
	currentUID := os.Getuid()
	pids := make([]int, 0, int(n))
	for i := 0; i < int(n); i++ {
		pid := int(buf[i])
		if pid <= 0 {
			continue
		}
		// Check ownership via proc_pidinfo with PROC_PIDTASKALLINFO.
		var info C.struct_proc_taskallinfo
		ret := C.proc_pidinfo(C.int(pid), C.PROC_PIDTASKALLINFO, 0,
			unsafe.Pointer(&info), C.int(C.sizeof_struct_proc_taskallinfo))
		if ret <= 0 {
			continue // can't read -- skip
		}
		if int(info.pbsd.pbi_uid) == currentUID {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// GetProcessInfo retrieves task info for a single PID using PROC_PIDTASKALLINFO.
func (d *darwinProcessAPI) GetProcessInfo(pid int) (*RawProcessInfo, error) {
	var info C.struct_proc_taskallinfo
	ret := C.proc_pidinfo(C.int(pid), C.PROC_PIDTASKALLINFO, 0,
		unsafe.Pointer(&info), C.int(C.sizeof_struct_proc_taskallinfo))
	if ret <= 0 {
		return nil, fmt.Errorf("proc_pidinfo PROC_PIDTASKALLINFO failed for pid %d", pid)
	}

	nameBytes := info.pbsd.pbi_comm
	var nameBuf []byte
	for i := 0; i < len(nameBytes); i++ {
		if nameBytes[i] == 0 {
			break
		}
		nameBuf = append(nameBuf, byte(nameBytes[i]))
	}

	return &RawProcessInfo{
		PID:        pid,
		BinaryName: string(nameBuf),
	}, nil
}

// GetProcessArgs reads the full argv + env block for a PID via sysctl KERN_PROCARGS2.
// Returns (args, envVars, error). envVars is a map of KEY=VALUE pairs from the
// process environment. The format of KERN_PROCARGS2 is:
//
//	[argc (4 bytes)] [exec_path\0] [padding\0...] [argv[0]\0] ... [argv[argc-1]\0] [\0...] [env[0]\0] [env[1]\0] ...
func (d *darwinProcessAPI) GetProcessArgs(pid int) (args []string, envVars map[string]string, err error) {
	var size C.size_t

	// Get required buffer size.
	ret := C.sysctl_procargs2(C.int(pid), nil, &size)
	if ret != 0 {
		return nil, nil, fmt.Errorf("sysctl KERN_PROCARGS2 size for pid %d failed", pid)
	}

	buf := make([]byte, int(size))
	ret = C.sysctl_procargs2(C.int(pid), unsafe.Pointer(&buf[0]), &size)
	if ret != 0 {
		return nil, nil, fmt.Errorf("sysctl KERN_PROCARGS2 for pid %d failed", pid)
	}

	data := buf[:int(size)]
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("KERN_PROCARGS2 data too short for pid %d", pid)
	}

	// First 4 bytes are argc.
	argc := int(*(*C.int)(unsafe.Pointer(&data[0])))
	rest := data[4:]

	// Skip the exec path (null-terminated).
	idx := bytes.IndexByte(rest, 0)
	if idx < 0 {
		return nil, nil, fmt.Errorf("no null terminator in exec path for pid %d", pid)
	}
	rest = rest[idx:]

	// Skip padding null bytes.
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}

	// Parse argc null-terminated argument strings.
	args = make([]string, 0, argc)
	for i := 0; i < argc && len(rest) > 0; i++ {
		idx = bytes.IndexByte(rest, 0)
		if idx < 0 {
			args = append(args, string(rest))
			rest = nil
			break
		}
		args = append(args, string(rest[:idx]))
		rest = rest[idx+1:]
	}

	// Remaining null-terminated strings are environment variables.
	envVars = make(map[string]string)
	for len(rest) > 0 {
		// Skip any leading null bytes between argv and env.
		if rest[0] == 0 {
			rest = rest[1:]
			continue
		}
		idx = bytes.IndexByte(rest, 0)
		var entry string
		if idx < 0 {
			entry = string(rest)
			rest = nil
		} else {
			entry = string(rest[:idx])
			rest = rest[idx+1:]
		}
		if eqIdx := bytes.IndexByte([]byte(entry), '='); eqIdx >= 0 {
			envVars[entry[:eqIdx]] = entry[eqIdx+1:]
		}
	}

	return args, envVars, nil
}

// GetProcessCWD retrieves the current working directory for a PID
// using PROC_PIDVNODEPATHINFO.
func (d *darwinProcessAPI) GetProcessCWD(pid int) (string, error) {
	var pathinfo C.struct_proc_vnodepathinfo
	ret := C.proc_pidinfo(C.int(pid), C.PROC_PIDVNODEPATHINFO, 0,
		unsafe.Pointer(&pathinfo), C.int(C.sizeof_struct_proc_vnodepathinfo))
	if ret <= 0 {
		return "", fmt.Errorf("proc_pidinfo PROC_PIDVNODEPATHINFO failed for pid %d", pid)
	}

	cwd := C.GoString(&pathinfo.pvi_cdir.vip_path[0])
	return cwd, nil
}

// PgrepClaude uses pgrep as a fallback to find Claude Code PIDs.
func (d *darwinProcessAPI) PgrepClaude() []int {
	return pgrepClaude()
}

// GetOpenPorts returns local and remote port pairs for TCP sockets owned by pid.
// Each entry is [localPort, remotePort].
func (d *darwinProcessAPI) GetOpenPorts(pid int) ([][2]int, error) {
	// Get the buffer size for file descriptors.
	bufSize := C.proc_pidinfo(C.int(pid), C.PROC_PIDLISTFDS, 0, nil, 0)
	if bufSize <= 0 {
		return nil, fmt.Errorf("proc_pidinfo PROC_PIDLISTFDS size failed for pid %d", pid)
	}

	buf := make([]byte, int(bufSize))
	ret := C.proc_pidinfo(C.int(pid), C.PROC_PIDLISTFDS, 0,
		unsafe.Pointer(&buf[0]), bufSize)
	if ret <= 0 {
		return nil, fmt.Errorf("proc_pidinfo PROC_PIDLISTFDS failed for pid %d", pid)
	}

	fdInfoSize := int(C.sizeof_struct_proc_fdinfo)
	nfds := int(ret) / fdInfoSize
	var ports [][2]int

	for i := 0; i < nfds; i++ {
		fdInfo := (*C.struct_proc_fdinfo)(unsafe.Pointer(&buf[i*fdInfoSize]))
		if fdInfo.proc_fdtype != C.PROX_FDTYPE_SOCKET {
			continue
		}

		var socketInfo C.struct_socket_fdinfo
		sret := C.proc_pidfdinfo(C.int(pid), fdInfo.proc_fd,
			C.PROC_PIDFDSOCKETINFO,
			unsafe.Pointer(&socketInfo), C.int(C.sizeof_struct_socket_fdinfo))
		if sret <= 0 {
			continue
		}

		// Only interested in TCP (IPPROTO_TCP = 6).
		si := &socketInfo.psi
		if si.soi_protocol != 6 {
			continue
		}

		// Extract ports from the in-socket info.
		// For AF_INET (2) and AF_INET6 (30).
		family := si.soi_family
		if family != 2 && family != 30 {
			continue
		}

		var localPort, remotePort C.int
		C.get_tcp_ports(si, &localPort, &remotePort)

		lp := int(localPort)
		rp := int(remotePort)
		if lp > 0 || rp > 0 {
			ports = append(ports, [2]int{lp, rp})
		}
	}

	return ports, nil
}
