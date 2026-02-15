package state

import "time"

// SessionAge returns the duration since the session started.
func SessionAge(s *SessionData) time.Duration {
	if s.StartedAt.IsZero() {
		return 0
	}
	return time.Since(s.StartedAt)
}

// SessionIdleDuration returns the duration since the last event.
// Returns 0 if no events have been received.
func SessionIdleDuration(s *SessionData) time.Duration {
	if s.LastEventAt.IsZero() {
		return 0
	}
	return time.Since(s.LastEventAt)
}

// TruncateSessionID returns a truncated session ID suitable for display.
// If the ID is longer than maxLen, it is truncated and suffixed with "...".
func TruncateSessionID(id string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(id) <= maxLen {
		return id
	}
	if maxLen <= 3 {
		return id[:maxLen]
	}
	return id[:maxLen-3] + "..."
}

// FilterSessionsByStatus returns sessions matching the given status.
func FilterSessionsByStatus(sessions []SessionData, status SessionStatus) []SessionData {
	var result []SessionData
	for i := range sessions {
		if sessions[i].Status() == status {
			result = append(result, sessions[i])
		}
	}
	return result
}

// ActiveSessions returns sessions that are not exited.
func ActiveSessions(sessions []SessionData) []SessionData {
	var result []SessionData
	for i := range sessions {
		if !sessions[i].Exited {
			result = append(result, sessions[i])
		}
	}
	return result
}

// MetricsByName filters a session's metrics by metric name.
func MetricsByName(s *SessionData, name string) []Metric {
	var result []Metric
	for _, m := range s.Metrics {
		if m.Name == name {
			result = append(result, m)
		}
	}
	return result
}

// EventsByName filters a session's events by event name.
func EventsByName(s *SessionData, name string) []Event {
	var result []Event
	for _, e := range s.Events {
		if e.Name == name {
			result = append(result, e)
		}
	}
	return result
}
