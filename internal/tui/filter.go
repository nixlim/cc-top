package tui

// EventFilter holds the current filter state for the event stream panel.
type EventFilter struct {
	// SessionID filters events to a specific session. Empty means all sessions.
	SessionID string

	// EventTypes is the set of event types to display. If empty, all types are shown.
	EventTypes map[string]bool

	// SuccessOnly when true shows only successful events.
	SuccessOnly bool

	// FailureOnly when true shows only failed events.
	FailureOnly bool
}

// AllEventTypes returns a map of all event types set to true (no filtering).
func AllEventTypes() map[string]bool {
	return map[string]bool{
		"user_prompt":   true,
		"tool_result":   true,
		"api_request":   true,
		"api_error":     true,
		"tool_decision": true,
	}
}

// NewEventFilter returns a filter that shows all events.
func NewEventFilter() EventFilter {
	return EventFilter{
		EventTypes: AllEventTypes(),
	}
}

// Matches returns true if the given event passes this filter.
func (f *EventFilter) Matches(sessionID, eventType string, success *bool) bool {
	// Session filter.
	if f.SessionID != "" && sessionID != f.SessionID {
		return false
	}

	// Event type filter.
	if len(f.EventTypes) > 0 {
		if !f.EventTypes[eventType] {
			return false
		}
	}

	// Success/failure filter.
	if f.SuccessOnly && success != nil && !*success {
		return false
	}
	if f.FailureOnly && success != nil && *success {
		return false
	}

	return true
}

// FilterMenuState tracks the interactive filter menu.
type FilterMenuState struct {
	Active   bool
	Cursor   int
	Options  []FilterOption
}

// FilterOption represents one toggleable filter option in the filter menu.
type FilterOption struct {
	Label   string
	Key     string
	Enabled bool
}

// NewFilterMenu creates a filter menu with default options.
func NewFilterMenu() FilterMenuState {
	return FilterMenuState{
		Options: []FilterOption{
			{Label: "User Prompts", Key: "user_prompt", Enabled: true},
			{Label: "Tool Results", Key: "tool_result", Enabled: true},
			{Label: "API Requests", Key: "api_request", Enabled: true},
			{Label: "API Errors", Key: "api_error", Enabled: true},
			{Label: "Tool Decisions", Key: "tool_decision", Enabled: true},
			{Label: "Success Only", Key: "success_only", Enabled: false},
			{Label: "Failure Only", Key: "failure_only", Enabled: false},
		},
	}
}
