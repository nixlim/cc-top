package correlator

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockPortMapper is a test double for PortMapper.
type mockPortMapper struct {
	mu    sync.Mutex
	ports map[int][][2]int // PID -> list of [localPort, remotePort]
}

func newMockPortMapper() *mockPortMapper {
	return &mockPortMapper{
		ports: make(map[int][][2]int),
	}
}

func (m *mockPortMapper) SetPorts(pid int, ports [][2]int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ports[pid] = ports
}

func (m *mockPortMapper) GetOpenPorts(pid int) ([][2]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ports, ok := m.ports[pid]
	if !ok {
		return nil, fmt.Errorf("no such process: %d", pid)
	}
	return ports, nil
}

func TestCorrelator_PortFingerprint(t *testing.T) {
	pm := newMockPortMapper()
	c := NewCorrelator(pm, 4317)

	// PID 4821 has a TCP connection to the receiver (remote port 4317)
	// with local ephemeral port 52345.
	pm.SetPorts(4821, [][2]int{
		{52345, 4317}, // local:52345 -> remote:4317
	})

	// An OTLP request arrives from source port 52345 with session "sess-abc".
	c.RecordConnection(52345, "sess-abc")

	// Run correlation with PID 4821 as an active PID.
	c.Correlate([]int{4821})

	// Verify the correlation.
	correlations := c.GetCorrelation()
	if sid, ok := correlations[4821]; !ok || sid != "sess-abc" {
		t.Errorf("GetCorrelation()[4821] = %q, want %q", sid, "sess-abc")
	}

	// Also verify the accessor methods.
	if sid := c.GetSessionForPID(4821); sid != "sess-abc" {
		t.Errorf("GetSessionForPID(4821) = %q, want %q", sid, "sess-abc")
	}
	if pid := c.GetPIDForSession("sess-abc"); pid != 4821 {
		t.Errorf("GetPIDForSession(%q) = %d, want %d", "sess-abc", pid, 4821)
	}
}

func TestCorrelator_TimingHeuristic(t *testing.T) {
	pm := newMockPortMapper()
	c := NewCorrelator(pm, 4317)

	// PID 6200 appears but has no socket to the receiver (connection may
	// have already closed or port fingerprinting fails).
	pm.SetPorts(6200, [][2]int{
		{50000, 443}, // unrelated connection
	})

	// Record the new PID.
	c.RecordPID(6200)

	// Within 10 seconds, a new session arrives (no matching port).
	c.RecordConnection(99999, "sess-xyz")

	// Run correlation. Port fingerprinting won't match (no socket to 4317),
	// so timing heuristic should kick in.
	c.Correlate([]int{6200})

	correlations := c.GetCorrelation()
	if sid, ok := correlations[6200]; !ok || sid != "sess-xyz" {
		t.Errorf("GetCorrelation()[6200] = %q, want %q (timing heuristic)", sid, "sess-xyz")
	}
}

func TestCorrelator_TimingHeuristic_Expired(t *testing.T) {
	pm := newMockPortMapper()
	c := NewCorrelator(pm, 4317)

	// Manually set the PID timestamp to be well outside the timing window.
	c.mu.Lock()
	c.newPIDs[6200] = time.Now().Add(-30 * time.Second) // 30s ago
	c.mu.Unlock()

	pm.SetPorts(6200, [][2]int{})

	// New session arrives now.
	c.RecordConnection(99999, "sess-late")

	// Run correlation. The PID is too old for the timing heuristic.
	c.Correlate([]int{6200})

	correlations := c.GetCorrelation()
	if _, ok := correlations[6200]; ok {
		t.Error("should not correlate PID 6200 (timing expired)")
	}
}

func TestCorrelator_NoMatch(t *testing.T) {
	pm := newMockPortMapper()
	c := NewCorrelator(pm, 4317)

	// PID exists but has no connection to the receiver.
	pm.SetPorts(5000, [][2]int{
		{40000, 8080}, // connected to something else
	})

	// No session recorded.
	c.Correlate([]int{5000})

	correlations := c.GetCorrelation()
	if sid, ok := correlations[5000]; ok {
		t.Errorf("GetCorrelation()[5000] = %q, want no correlation", sid)
	}

	// Uncorrelated session should show PID 0.
	if pid := c.GetPIDForSession("sess-orphan"); pid != 0 {
		t.Errorf("GetPIDForSession(%q) = %d, want 0", "sess-orphan", pid)
	}
}

func TestCorrelator_TwoSessions(t *testing.T) {
	pm := newMockPortMapper()
	c := NewCorrelator(pm, 4317)

	// PID 4821 connects via port 52345.
	pm.SetPorts(4821, [][2]int{
		{52345, 4317},
	})

	// PID 5102 connects via port 52346.
	pm.SetPorts(5102, [][2]int{
		{52346, 4317},
	})

	// Record OTLP connections.
	c.RecordConnection(52345, "sess-abc")
	c.RecordConnection(52346, "sess-def")

	// Run correlation.
	c.Correlate([]int{4821, 5102})

	correlations := c.GetCorrelation()

	if sid := correlations[4821]; sid != "sess-abc" {
		t.Errorf("PID 4821 correlated to %q, want %q", sid, "sess-abc")
	}
	if sid := correlations[5102]; sid != "sess-def" {
		t.Errorf("PID 5102 correlated to %q, want %q", sid, "sess-def")
	}

	// Verify no cross-contamination.
	if pid := c.GetPIDForSession("sess-abc"); pid != 4821 {
		t.Errorf("sess-abc PID = %d, want 4821", pid)
	}
	if pid := c.GetPIDForSession("sess-def"); pid != 5102 {
		t.Errorf("sess-def PID = %d, want 5102", pid)
	}
}

func TestCorrelator_ProcessExitPreservesCorrelation(t *testing.T) {
	pm := newMockPortMapper()
	c := NewCorrelator(pm, 4317)

	pm.SetPorts(4821, [][2]int{{52345, 4317}})
	c.RecordConnection(52345, "sess-abc")
	c.Correlate([]int{4821})

	// Verify initial correlation.
	if sid := c.GetSessionForPID(4821); sid != "sess-abc" {
		t.Fatalf("initial correlation failed: %q", sid)
	}

	// Process exits.
	c.RemovePID(4821)

	// Correlation should still be queryable.
	if sid := c.GetSessionForPID(4821); sid != "sess-abc" {
		t.Errorf("after exit, GetSessionForPID(4821) = %q, want %q (should be preserved)", sid, "sess-abc")
	}
}

func TestCorrelator_ConcurrentAccess(t *testing.T) {
	pm := newMockPortMapper()
	c := NewCorrelator(pm, 4317)

	pm.SetPorts(1000, [][2]int{{50000, 4317}})

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			c.RecordConnection(50000+i, fmt.Sprintf("sess-%d", i))
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			c.RecordPID(1000 + i)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			c.Correlate([]int{1000 + i})
			c.GetCorrelation()
		}
	}()

	wg.Wait()
}
