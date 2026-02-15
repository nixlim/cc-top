package state

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Store is the interface for the in-memory state store.
// All methods must be thread-safe.
type Store interface {
	// AddMetric indexes a metric data point under the given session ID.
	// If sessionID is empty, the metric is stored under the "unknown" bucket
	// and a warning is logged.
	AddMetric(sessionID string, m Metric)

	// AddEvent indexes an event under the given session ID.
	// If sessionID is empty, the event is stored under the "unknown" bucket
	// and a warning is logged.
	AddEvent(sessionID string, e Event)

	// GetSession returns a snapshot of the session data for the given ID,
	// or nil if the session does not exist.
	GetSession(sessionID string) *SessionData

	// ListSessions returns a snapshot of all sessions sorted by start time.
	ListSessions() []SessionData

	// GetAggregatedCost returns the sum of TotalCost across all sessions.
	GetAggregatedCost() float64

	// UpdatePID associates a PID with the given session.
	UpdatePID(sessionID string, pid int)

	// MarkExited marks all sessions associated with the given PID as exited.
	MarkExited(pid int)

	// UpdateMetadata updates the session metadata for the given session.
	UpdateMetadata(sessionID string, meta SessionMetadata)
}

// EventListener is a callback invoked after a new event is stored.
// It receives the resolved session ID and the event. Listeners are
// called outside the store lock and must not call back into the store
// in a way that acquires a write lock to avoid deadlocks.
type EventListener func(sessionID string, e Event)

// MemoryStore is a thread-safe in-memory implementation of Store.
// It indexes metrics and events by session.id using a sync.RWMutex
// for safe concurrent access.
type MemoryStore struct {
	mu             sync.RWMutex
	sessions       map[string]*SessionData
	eventListeners []EventListener
}

// NewMemoryStore creates a new empty MemoryStore ready for use.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]*SessionData),
	}
}

// OnEvent registers a listener that is called after every AddEvent.
// Listeners are invoked synchronously outside the store lock.
func (ms *MemoryStore) OnEvent(fn EventListener) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.eventListeners = append(ms.eventListeners, fn)
}

// resolveSessionID returns the provided sessionID if non-empty, or
// UnknownSessionID with a warning log if empty.
func resolveSessionID(sessionID string) string {
	if sessionID == "" {
		log.Printf("WARNING: metric/event received without session.id, storing under %q", UnknownSessionID)
		return UnknownSessionID
	}
	return sessionID
}

// getOrCreateSession returns the existing session or creates a new one.
// Caller must hold ms.mu (write lock).
func (ms *MemoryStore) getOrCreateSession(sessionID string) *SessionData {
	s, ok := ms.sessions[sessionID]
	if !ok {
		s = &SessionData{
			SessionID:      sessionID,
			StartedAt:      time.Now(),
			PreviousValues: make(map[string]float64),
		}
		ms.sessions[sessionID] = s
	}
	return s
}

// metricKey builds a deterministic key for counter reset tracking from a
// metric name and its attributes. The key format is:
// "metric_name|attr1=val1,attr2=val2" with attributes sorted by key.
func metricKey(name string, attrs map[string]string) string {
	if len(attrs) == 0 {
		return name
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, attrs[k]))
	}
	return name + "|" + strings.Join(parts, ",")
}

// AddMetric indexes a metric data point under the given session ID.
// Counter resets (negative deltas) are handled by treating the previous
// value as 0 and computing the delta from there.
func (ms *MemoryStore) AddMetric(sessionID string, m Metric) {
	sessionID = resolveSessionID(sessionID)

	ms.mu.Lock()
	defer ms.mu.Unlock()

	// For session.count metrics, create the session with the metric timestamp
	// instead of time.Now() so StartedAt reflects the actual session start.
	if m.Name == "claude_code.session.count" {
		if _, exists := ms.sessions[sessionID]; !exists {
			ts := m.Timestamp
			if ts.IsZero() {
				ts = time.Now()
			}
			ms.sessions[sessionID] = &SessionData{
				SessionID:      sessionID,
				StartedAt:      ts,
				PreviousValues: make(map[string]float64),
			}
		}
	}

	s := ms.getOrCreateSession(sessionID)
	s.Metrics = append(s.Metrics, m)

	if !m.Timestamp.IsZero() {
		s.LastEventAt = m.Timestamp
	} else {
		s.LastEventAt = time.Now()
	}

	// Compute delta for cumulative counters with counter reset handling.
	key := metricKey(m.Name, m.Attributes)
	prev, hasPrev := s.PreviousValues[key]
	s.PreviousValues[key] = m.Value

	var delta float64
	if !hasPrev {
		delta = m.Value
	} else {
		delta = m.Value - prev
		if delta < 0 {
			// Counter reset: treat previous as 0.
			delta = m.Value
		}
	}

	// Update aggregated session fields based on metric type.
	switch m.Name {
	case "claude_code.cost.usage":
		s.TotalCost += delta
	case "claude_code.token.usage":
		s.TotalTokens += int64(delta)
	case "claude_code.active_time.total":
		s.ActiveTime += time.Duration(delta * float64(time.Second))
	}

	// Track model from api_request-related attributes if present.
	if model, ok := m.Attributes["model"]; ok && model != "" {
		s.Model = model
	}

	// Track terminal from attributes if present.
	if terminal, ok := m.Attributes["terminal.type"]; ok && terminal != "" {
		s.Terminal = terminal
	}

	// Track fast mode from speed attribute.
	if speed, ok := m.Attributes["speed"]; ok && speed != "" {
		s.FastMode = true
	}

	// Extract org and user identity attributes.
	if orgID, ok := m.Attributes["organization.id"]; ok && orgID != "" {
		s.OrgID = orgID
	}
	if userUUID, ok := m.Attributes["user.account_uuid"]; ok && userUUID != "" {
		s.UserUUID = userUUID
	}
}

// AddEvent indexes an event under the given session ID.
func (ms *MemoryStore) AddEvent(sessionID string, e Event) {
	sessionID = resolveSessionID(sessionID)

	ms.mu.Lock()

	s := ms.getOrCreateSession(sessionID)

	// Extract sequence number from event attributes.
	if seqStr, ok := e.Attributes["event.sequence"]; ok {
		if seq, err := strconv.ParseInt(seqStr, 10, 64); err == nil {
			e.Sequence = seq
		}
	}

	s.Events = append(s.Events, e)

	// Keep events sorted by sequence (primary) then timestamp (secondary).
	sort.SliceStable(s.Events, func(i, j int) bool {
		ei, ej := s.Events[i], s.Events[j]
		if ei.Sequence != 0 && ej.Sequence != 0 {
			return ei.Sequence < ej.Sequence
		}
		if ei.Sequence != 0 && ej.Sequence == 0 {
			return true
		}
		if ei.Sequence == 0 && ej.Sequence != 0 {
			return false
		}
		return ei.Timestamp.Before(ej.Timestamp)
	})

	if !e.Timestamp.IsZero() {
		s.LastEventAt = e.Timestamp
	} else {
		s.LastEventAt = time.Now()
	}

	// Extract model and cache tokens from api_request events.
	if e.Name == "claude_code.api_request" {
		if model, ok := e.Attributes["model"]; ok && model != "" {
			s.Model = model
		}
		if v, ok := e.Attributes["cache_read_tokens"]; ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				s.CacheReadTokens += n
			}
		}
		if v, ok := e.Attributes["cache_creation_tokens"]; ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				s.CacheCreationTokens += n
			}
		}
	}

	// Track fast mode from speed attribute.
	if speed, ok := e.Attributes["speed"]; ok && speed != "" {
		s.FastMode = true
	} else {
		s.FastMode = false
	}

	// Extract org and user identity attributes.
	if orgID, ok := e.Attributes["organization.id"]; ok && orgID != "" {
		s.OrgID = orgID
	}
	if userUUID, ok := e.Attributes["user.account_uuid"]; ok && userUUID != "" {
		s.UserUUID = userUUID
	}

	// Snapshot listeners while holding the lock.
	listeners := ms.eventListeners

	ms.mu.Unlock()

	// Notify listeners outside the lock to prevent deadlocks.
	for _, fn := range listeners {
		fn(sessionID, e)
	}
}

// GetSession returns a deep copy of the session data for the given ID,
// or nil if the session does not exist.
func (ms *MemoryStore) GetSession(sessionID string) *SessionData {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	s, ok := ms.sessions[sessionID]
	if !ok {
		return nil
	}
	return ms.copySession(s)
}

// ListSessions returns a snapshot of all sessions sorted by start time
// (oldest first).
func (ms *MemoryStore) ListSessions() []SessionData {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	result := make([]SessionData, 0, len(ms.sessions))
	for _, s := range ms.sessions {
		result = append(result, *ms.copySession(s))
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].StartedAt.Before(result[j].StartedAt)
	})
	return result
}

// GetAggregatedCost returns the sum of TotalCost across all sessions.
func (ms *MemoryStore) GetAggregatedCost() float64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var total float64
	for _, s := range ms.sessions {
		total += s.TotalCost
	}
	return total
}

// UpdatePID associates a PID with the given session.
func (ms *MemoryStore) UpdatePID(sessionID string, pid int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	s := ms.getOrCreateSession(sessionID)
	s.PID = pid
}

// MarkExited marks all sessions associated with the given PID as exited.
// Sessions with PID 0 (uncorrelated) are never marked by this method.
func (ms *MemoryStore) MarkExited(pid int) {
	if pid == 0 {
		return
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	for _, s := range ms.sessions {
		if s.PID == pid {
			s.Exited = true
		}
	}
}

// UpdateMetadata updates the session metadata for the given session.
func (ms *MemoryStore) UpdateMetadata(sessionID string, meta SessionMetadata) {
	sessionID = resolveSessionID(sessionID)

	ms.mu.Lock()
	defer ms.mu.Unlock()

	s := ms.getOrCreateSession(sessionID)
	if meta.ServiceVersion != "" {
		s.Metadata.ServiceVersion = meta.ServiceVersion
	}
	if meta.OSType != "" {
		s.Metadata.OSType = meta.OSType
	}
	if meta.OSVersion != "" {
		s.Metadata.OSVersion = meta.OSVersion
	}
	if meta.HostArch != "" {
		s.Metadata.HostArch = meta.HostArch
	}
}

// copySession returns a deep copy of a SessionData to prevent callers
// from mutating internal state.
func (ms *MemoryStore) copySession(s *SessionData) *SessionData {
	cp := *s

	// Deep copy slices.
	if len(s.Metrics) > 0 {
		cp.Metrics = make([]Metric, len(s.Metrics))
		copy(cp.Metrics, s.Metrics)
	}
	if len(s.Events) > 0 {
		cp.Events = make([]Event, len(s.Events))
		copy(cp.Events, s.Events)
	}

	// Deep copy maps.
	if len(s.PreviousValues) > 0 {
		cp.PreviousValues = make(map[string]float64, len(s.PreviousValues))
		for k, v := range s.PreviousValues {
			cp.PreviousValues[k] = v
		}
	}

	return &cp
}
