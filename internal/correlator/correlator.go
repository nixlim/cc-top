// Package correlator maps Claude Code PIDs to OTLP session IDs
// using port fingerprinting and timing heuristics.
//
// Primary method: Port fingerprinting tracks the ephemeral source port on
// each inbound OTLP connection. PIDs are mapped to open sockets via
// proc_pidfdinfo() (macOS). When an OTLP request arrives from source port Y
// carrying session.id Z, and PID X has a socket with local port Y connected
// to the OTLP receiver's port, PID X is correlated to session Z.
//
// Fallback: Timing heuristic. When a new PID appears in the process scanner
// and a new session.id starts sending within 10 seconds, they are assumed
// to match.
package correlator

import (
	"sync"
	"time"
)

// PortMapper abstracts the ability to retrieve open TCP ports for a PID.
// Production code delegates to the scanner's ProcessAPI; tests use mocks.
type PortMapper interface {
	// GetOpenPorts returns local/remote port pairs for TCP sockets owned by pid.
	// Each entry is [localPort, remotePort].
	GetOpenPorts(pid int) ([][2]int, error)
}

// timingWindow is the maximum delay between a new PID appearing and a new
// session arriving for the timing heuristic to match them.
const timingWindow = 10 * time.Second

// Correlator links Claude Code PIDs to OTLP session IDs.
type Correlator struct {
	mu sync.RWMutex

	// portMapper queries open sockets for a PID.
	portMapper PortMapper

	// receiverPort is the OTLP receiver's gRPC port (e.g. 4317).
	receiverPort int

	// portToSession maps source port -> session ID, populated when an OTLP
	// connection arrives and the session.id is known.
	portToSession map[int]string

	// pidToSession is the final correlation result.
	pidToSession map[int]string

	// sessionToPID is the reverse index.
	sessionToPID map[string]int

	// newPIDs tracks recently discovered PIDs with their first-seen timestamp,
	// used for the timing heuristic fallback.
	newPIDs map[int]time.Time

	// newSessions tracks recently seen session IDs with their first-seen
	// timestamp, used for the timing heuristic fallback.
	newSessions map[string]time.Time
}

// NewCorrelator creates a Correlator with the given port mapper and
// OTLP receiver port.
func NewCorrelator(portMapper PortMapper, receiverPort int) *Correlator {
	return &Correlator{
		portMapper:    portMapper,
		receiverPort:  receiverPort,
		portToSession: make(map[int]string),
		pidToSession:  make(map[int]string),
		sessionToPID:  make(map[string]int),
		newPIDs:       make(map[int]time.Time),
		newSessions:   make(map[string]time.Time),
	}
}

// RecordConnection records that an OTLP request arrived from the given
// source port carrying the given session ID. This is called by the OTLP
// receiver when it processes an inbound request.
func (c *Correlator) RecordConnection(sourcePort int, sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.portToSession[sourcePort] = sessionID

	// Track new session for timing heuristic.
	if _, exists := c.sessionToPID[sessionID]; !exists {
		if _, tracked := c.newSessions[sessionID]; !tracked {
			c.newSessions[sessionID] = time.Now()
		}
	}
}

// RecordPID records that a new Claude Code PID was discovered by the
// process scanner. This is called when the scanner finds a new PID.
func (c *Correlator) RecordPID(pid int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.pidToSession[pid]; !exists {
		c.newPIDs[pid] = time.Now()
	}
}

// RemovePID removes a PID that has exited. The correlation is preserved
// for display but the PID is no longer actively matched.
func (c *Correlator) RemovePID(pid int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.newPIDs, pid)
	// Note: we keep pidToSession entries for exited PIDs so that exited
	// processes can still display their session info.
}

// Correlate runs the correlation logic: attempts port fingerprinting for
// all known PIDs, then falls back to the timing heuristic for uncorrelated
// PIDs. This should be called periodically (e.g. after each scan cycle).
func (c *Correlator) Correlate(activePIDs []int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// Phase 1: Port fingerprinting.
	for _, pid := range activePIDs {
		if _, already := c.pidToSession[pid]; already {
			continue
		}

		ports, err := c.portMapper.GetOpenPorts(pid)
		if err != nil {
			continue
		}

		for _, portPair := range ports {
			localPort := portPair[0]
			remotePort := portPair[1]

			// The process connects TO the receiver port with a local
			// ephemeral port. The receiver sees this ephemeral port as
			// the source port. So we look for sockets where the remote
			// port matches our receiver port.
			if remotePort == c.receiverPort {
				if sessionID, ok := c.portToSession[localPort]; ok {
					c.pidToSession[pid] = sessionID
					c.sessionToPID[sessionID] = pid
					delete(c.newPIDs, pid)
					delete(c.newSessions, sessionID)
					break
				}
			}
		}
	}

	// Phase 2: Timing heuristic fallback.
	// Match uncorrelated new PIDs with uncorrelated new sessions
	// that appeared within the timing window.
	for pid, pidTime := range c.newPIDs {
		if _, already := c.pidToSession[pid]; already {
			continue
		}
		for sessionID, sessTime := range c.newSessions {
			if _, already := c.sessionToPID[sessionID]; already {
				continue
			}
			// Both must have appeared within the timing window of each other.
			diff := pidTime.Sub(sessTime)
			if diff < 0 {
				diff = -diff
			}
			if diff <= timingWindow {
				c.pidToSession[pid] = sessionID
				c.sessionToPID[sessionID] = pid
				delete(c.newPIDs, pid)
				delete(c.newSessions, sessionID)
				break
			}
		}
	}

	// Clean up stale entries from the timing heuristic tracker
	// (entries older than 2x the timing window).
	staleThreshold := now.Add(-2 * timingWindow)
	for pid, t := range c.newPIDs {
		if t.Before(staleThreshold) {
			delete(c.newPIDs, pid)
		}
	}
	for sid, t := range c.newSessions {
		if t.Before(staleThreshold) {
			delete(c.newSessions, sid)
		}
	}
}

// GetCorrelation returns the current PID-to-session ID mapping.
// The returned map is a snapshot (safe to read concurrently).
func (c *Correlator) GetCorrelation() map[int]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[int]string, len(c.pidToSession))
	for pid, sid := range c.pidToSession {
		result[pid] = sid
	}
	return result
}

// GetSessionForPID returns the session ID correlated to the given PID,
// or empty string if uncorrelated.
func (c *Correlator) GetSessionForPID(pid int) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pidToSession[pid]
}

// GetPIDForSession returns the PID correlated to the given session ID,
// or 0 if uncorrelated.
func (c *Correlator) GetPIDForSession(sessionID string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionToPID[sessionID]
}
