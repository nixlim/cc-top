package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/events"
)

func TestRenderEventStreamPanel_Empty(t *testing.T) {
	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewDashboard))
	m.width = 120
	m.height = 40

	panel := m.renderEventStreamPanel(60, 20)
	if !strings.Contains(panel, "No data received yet") {
		t.Error("empty event stream should show 'No data received yet'")
	}
}

func TestRenderEventStreamPanel_WithEvents(t *testing.T) {
	cfg := config.DefaultConfig()
	boolTrue := true
	boolFalse := false
	mockEvents := &mockEventProvider{
		events: []events.FormattedEvent{
			{
				SessionID: "sess-001",
				EventType: "user_prompt",
				Formatted: "[sess-001] Prompt (342 chars)",
				Timestamp: time.Now().Add(-2 * time.Second),
				Success:   nil,
			},
			{
				SessionID: "sess-001",
				EventType: "tool_result",
				Formatted: "[sess-001] Bash OK (1.2s)",
				Timestamp: time.Now().Add(-1 * time.Second),
				Success:   &boolTrue,
			},
			{
				SessionID: "sess-001",
				EventType: "api_error",
				Formatted: "[sess-001] 529 overloaded (attempt 2)",
				Timestamp: time.Now(),
				Success:   &boolFalse,
			},
		},
	}

	m := NewModel(cfg, WithStartView(ViewDashboard), WithEventProvider(mockEvents))
	m.width = 120
	m.height = 40

	panel := m.renderEventStreamPanel(60, 20)
	if !strings.Contains(panel, "Events") {
		t.Error("event stream should contain 'Events' title")
	}
}

func TestRenderEventStreamPanel_FilteredBySession(t *testing.T) {
	cfg := config.DefaultConfig()
	mockEvents := &mockEventProvider{
		events: []events.FormattedEvent{
			{SessionID: "sess-001", EventType: "api_request", Formatted: "event1"},
			{SessionID: "sess-002", EventType: "api_request", Formatted: "event2"},
		},
	}

	m := NewModel(cfg, WithStartView(ViewDashboard), WithEventProvider(mockEvents))
	m.width = 120
	m.height = 40
	m.eventFilter.SessionID = "sess-001"

	evts := m.getFilteredEvents(100)
	if len(evts) != 1 {
		t.Errorf("filtered events count = %d, want 1", len(evts))
	}
	if evts[0].SessionID != "sess-001" {
		t.Errorf("filtered event sessionID = %q, want %q", evts[0].SessionID, "sess-001")
	}
}

func TestRenderEventLine(t *testing.T) {
	boolTrue := true
	tests := []struct {
		name  string
		event events.FormattedEvent
	}{
		{
			name: "user_prompt",
			event: events.FormattedEvent{
				SessionID: "s1",
				EventType: "user_prompt",
				Formatted: "Prompt (100 chars)",
			},
		},
		{
			name: "tool_result",
			event: events.FormattedEvent{
				SessionID: "s1",
				EventType: "tool_result",
				Formatted: "Bash OK (1.2s)",
				Success:   &boolTrue,
			},
		},
		{
			name: "api_request",
			event: events.FormattedEvent{
				SessionID: "s1",
				EventType: "api_request",
				Formatted: "sonnet-4.5 -> 2.1k/890 ($0.03)",
			},
		},
		{
			name: "api_error",
			event: events.FormattedEvent{
				SessionID: "s1",
				EventType: "api_error",
				Formatted: "529 overloaded",
			},
		},
		{
			name: "tool_decision",
			event: events.FormattedEvent{
				SessionID: "s1",
				EventType: "tool_decision",
				Formatted: "Write accepted",
			},
		},
		{
			name: "unknown type",
			event: events.FormattedEvent{
				SessionID: "s1",
				EventType: "unknown_type",
				Formatted: "unknown event",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := renderEventLine(tt.event, 80)
			if line == "" {
				t.Error("renderEventLine returned empty string")
			}
		})
	}
}

func TestFormatScrollPos(t *testing.T) {
	result := formatScrollPos(10, 20, 100)
	if !strings.Contains(result, "10") {
		t.Error("formatScrollPos should contain start position")
	}
	if !strings.Contains(result, "100") {
		t.Error("formatScrollPos should contain total")
	}
}

func TestRenderEventStreamPanel_AutoScroll(t *testing.T) {
	cfg := config.DefaultConfig()
	var evts []events.FormattedEvent
	for i := 0; i < 50; i++ {
		evts = append(evts, events.FormattedEvent{
			SessionID: "s1",
			EventType: "api_request",
			Formatted: "event",
			Timestamp: time.Now(),
		})
	}
	mockEvents := &mockEventProvider{events: evts}

	m := NewModel(cfg, WithStartView(ViewDashboard), WithEventProvider(mockEvents))
	m.width = 120
	m.height = 40
	m.autoScroll = true

	// Should not panic.
	panel := m.renderEventStreamPanel(60, 20)
	if panel == "" {
		t.Error("renderEventStreamPanel returned empty string")
	}
}

func TestEventFilter_Matches(t *testing.T) {
	boolTrue := true
	boolFalse := false

	tests := []struct {
		name      string
		filter    EventFilter
		sessionID string
		eventType string
		success   *bool
		want      bool
	}{
		{
			name:      "all pass default filter",
			filter:    NewEventFilter(),
			sessionID: "s1",
			eventType: "api_request",
			success:   &boolTrue,
			want:      true,
		},
		{
			name: "session filter excludes",
			filter: EventFilter{
				SessionID:  "s2",
				EventTypes: AllEventTypes(),
			},
			sessionID: "s1",
			eventType: "api_request",
			want:      false,
		},
		{
			name: "event type filter excludes",
			filter: EventFilter{
				EventTypes: map[string]bool{
					"api_request": false,
				},
			},
			sessionID: "s1",
			eventType: "api_request",
			want:      false,
		},
		{
			name: "success only filter",
			filter: EventFilter{
				EventTypes:  AllEventTypes(),
				SuccessOnly: true,
			},
			sessionID: "s1",
			eventType: "tool_result",
			success:   &boolFalse,
			want:      false,
		},
		{
			name: "failure only filter",
			filter: EventFilter{
				EventTypes:  AllEventTypes(),
				FailureOnly: true,
			},
			sessionID: "s1",
			eventType: "tool_result",
			success:   &boolTrue,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.Matches(tt.sessionID, tt.eventType, tt.success)
			if got != tt.want {
				t.Errorf("filter.Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}
