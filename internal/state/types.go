package state

import "time"

const UnknownSessionID = "unknown"

type SessionData struct {
	SessionID           string
	PID                 int
	Terminal            string
	CWD                 string
	Model               string
	TotalCost           float64
	TotalTokens         int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	ActiveTime          time.Duration
	LastEventAt         time.Time
	StartedAt           time.Time
	Exited              bool
	IsNew               bool
	FastMode            bool
	OrgID               string
	UserUUID            string

	Metrics []Metric
	Events  []Event

	Metadata SessionMetadata

	PreviousValues map[string]float64
}

type SessionMetadata struct {
	ServiceVersion string
	OSType         string
	OSVersion      string
	HostArch       string
}

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

type Metric struct {
	Name       string
	Value      float64
	Attributes map[string]string
	Timestamp  time.Time
}

type Event struct {
	Name       string
	Attributes map[string]string
	Timestamp  time.Time
	Sequence   int64
}

type SessionStatus string

const (
	StatusActive SessionStatus = "active"
	StatusIdle   SessionStatus = "idle"
	StatusDone   SessionStatus = "done"
	StatusExited SessionStatus = "exited"
)

type DailySummary struct {
	Date         string
	TotalCost    float64
	TotalTokens  int64
	SessionCount int
	APIRequests  int
	APIErrors    int
}
