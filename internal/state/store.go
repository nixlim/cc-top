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

type Store interface {
	AddMetric(sessionID string, m Metric)

	AddEvent(sessionID string, e Event)

	GetSession(sessionID string) *SessionData

	ListSessions() []SessionData

	GetAggregatedCost() float64

	UpdatePID(sessionID string, pid int)

	MarkExited(pid int)

	UpdateMetadata(sessionID string, meta SessionMetadata)

	OnEvent(fn EventListener)

	Close() error

	DroppedWrites() int64

	QueryDailySummaries(days int) []DailySummary
}

type EventListener func(sessionID string, e Event)

type MemoryStore struct {
	mu             sync.RWMutex
	sessions       map[string]*SessionData
	eventListeners []EventListener
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: make(map[string]*SessionData),
	}
}

func (ms *MemoryStore) OnEvent(fn EventListener) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.eventListeners = append(ms.eventListeners, fn)
}

func resolveSessionID(sessionID string) string {
	if sessionID == "" {
		log.Printf("WARNING: metric/event received without session.id, storing under %q", UnknownSessionID)
		return UnknownSessionID
	}
	return sessionID
}

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

func MetricKey(name string, attrs map[string]string) string {
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

func (ms *MemoryStore) AddMetric(sessionID string, m Metric) {
	sessionID = resolveSessionID(sessionID)

	ms.mu.Lock()
	defer ms.mu.Unlock()

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

	key := MetricKey(m.Name, m.Attributes)
	prev, hasPrev := s.PreviousValues[key]
	s.PreviousValues[key] = m.Value

	var delta float64
	if !hasPrev {
		delta = m.Value
	} else {
		delta = m.Value - prev
		if delta < 0 {
			delta = m.Value
		}
	}

	switch m.Name {
	case "claude_code.cost.usage":
		s.TotalCost += delta
	case "claude_code.token.usage":
		s.TotalTokens += int64(delta)
	case "claude_code.active_time.total":
		s.ActiveTime += time.Duration(delta * float64(time.Second))
	}

	if model, ok := m.Attributes["model"]; ok && model != "" {
		s.Model = model
	}

	if terminal, ok := m.Attributes["terminal.type"]; ok && terminal != "" {
		s.Terminal = terminal
	}

	if speed, ok := m.Attributes["speed"]; ok && speed != "" {
		s.FastMode = true
	}

	if orgID, ok := m.Attributes["organization.id"]; ok && orgID != "" {
		s.OrgID = orgID
	}
	if userUUID, ok := m.Attributes["user.account_uuid"]; ok && userUUID != "" {
		s.UserUUID = userUUID
	}
}

func (ms *MemoryStore) AddEvent(sessionID string, e Event) {
	sessionID = resolveSessionID(sessionID)

	ms.mu.Lock()

	s := ms.getOrCreateSession(sessionID)

	if seqStr, ok := e.Attributes["event.sequence"]; ok {
		if seq, err := strconv.ParseInt(seqStr, 10, 64); err == nil {
			e.Sequence = seq
		}
	}

	s.Events = append(s.Events, e)

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

	if model, ok := e.Attributes["model"]; ok && model != "" {
		s.Model = model
	}

	if e.Name == "claude_code.api_request" {
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

	if speed, ok := e.Attributes["speed"]; ok && speed != "" {
		s.FastMode = true
	} else {
		s.FastMode = false
	}

	if orgID, ok := e.Attributes["organization.id"]; ok && orgID != "" {
		s.OrgID = orgID
	}
	if userUUID, ok := e.Attributes["user.account_uuid"]; ok && userUUID != "" {
		s.UserUUID = userUUID
	}

	listeners := ms.eventListeners

	ms.mu.Unlock()

	for _, fn := range listeners {
		fn(sessionID, e)
	}
}

func (ms *MemoryStore) GetSession(sessionID string) *SessionData {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	s, ok := ms.sessions[sessionID]
	if !ok {
		return nil
	}
	return ms.copySession(s)
}

func (ms *MemoryStore) ListSessions() []SessionData {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	result := make([]SessionData, 0, len(ms.sessions))
	for _, s := range ms.sessions {
		result = append(result, *ms.copySession(s))
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].StartedAt.Equal(result[j].StartedAt) {
			return result[i].SessionID < result[j].SessionID
		}
		return result[i].StartedAt.Before(result[j].StartedAt)
	})
	return result
}

func (ms *MemoryStore) GetAggregatedCost() float64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var total float64
	for _, s := range ms.sessions {
		total += s.TotalCost
	}
	return total
}

func (ms *MemoryStore) UpdatePID(sessionID string, pid int) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	s := ms.getOrCreateSession(sessionID)
	s.PID = pid
}

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

func (ms *MemoryStore) Close() error {
	return nil
}

func (ms *MemoryStore) DroppedWrites() int64 {
	return 0
}

func (ms *MemoryStore) QueryDailySummaries(days int) []DailySummary {
	return nil
}

func (ms *MemoryStore) RestoreSession(session *SessionData) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.sessions[session.SessionID] = session
}

func (ms *MemoryStore) copySession(s *SessionData) *SessionData {
	cp := *s

	if len(s.Metrics) > 0 {
		cp.Metrics = make([]Metric, len(s.Metrics))
		copy(cp.Metrics, s.Metrics)
	}
	if len(s.Events) > 0 {
		cp.Events = make([]Event, len(s.Events))
		copy(cp.Events, s.Events)
	}

	if len(s.PreviousValues) > 0 {
		cp.PreviousValues = make(map[string]float64, len(s.PreviousValues))
		for k, v := range s.PreviousValues {
			cp.PreviousValues[k] = v
		}
	}

	return &cp
}
