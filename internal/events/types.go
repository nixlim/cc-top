package events

import "time"

// FormattedEvent holds a display-ready event with metadata.
type FormattedEvent struct {
	SessionID string
	EventType string // user_prompt, tool_result, api_request, api_error, tool_decision
	Formatted string // display-ready string
	Timestamp time.Time
	Success   *bool  // nil if not applicable
}
