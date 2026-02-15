package state

import "time"

// UnknownSessionID is the bucket used for metrics/events that arrive
// without a session.id attribute.
const UnknownSessionID = "unknown"

// SessionData holds all data for a single Claude Code session.
type SessionData struct {
	SessionID   string
	PID         int // 0 if uncorrelated
	Terminal    string
	CWD         string
	Model       string
	TotalCost           float64
	TotalTokens         int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	ActiveTime          time.Duration
	LastEventAt time.Time
	StartedAt   time.Time
	Exited      bool
	IsNew       bool // "New" badge for one scan cycle
	FastMode    bool
	OrgID       string
	UserUUID    string

	Metrics []Metric
	Events  []Event

	Metadata SessionMetadata

	// PreviousValues tracks the last-seen counter value for each metric key
	// to support delta computation and counter reset detection.
	// Key format: "metric_name|attr1=val1,attr2=val2"
	PreviousValues map[string]float64
}

// SessionMetadata holds metadata extracted from OTLP resource attributes.
type SessionMetadata struct {
	ServiceVersion string
	OSType         string
	OSVersion      string
	HostArch       string
}

// Status returns the current activity status of the session based on
// the time elapsed since the last event. If the process has exited,
// StatusExited is always returned.
func (s *SessionData) Status() SessionStatus {
	if s.Exited {
		return StatusExited
	}
	if s.LastEventAt.IsZero() {
		return StatusDone
	}
	elapsed := time.Since(s.LastEventAt)
	switch {
	case elapsed <= 30*time.Second:
		return StatusActive
	case elapsed <= 5*time.Minute:
		return StatusIdle
	default:
		return StatusDone
	}
}

// Metric represents a received OTLP metric data point.
type Metric struct {
	Name       string
	Value      float64
	Attributes map[string]string
	Timestamp  time.Time
}

// Event represents a received OTLP log event.
type Event struct {
	Name       string
	Attributes map[string]string
	Timestamp  time.Time
	Sequence   int64
}

// SessionStatus represents the activity status of a session.
type SessionStatus string

const (
	StatusActive SessionStatus = "active"  // events within 30s
	StatusIdle   SessionStatus = "idle"    // 30s-5min since last event
	StatusDone   SessionStatus = "done"    // >5min since last event
	StatusExited SessionStatus = "exited"  // process gone
)
