package events

import (
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

func TestEventFormat_UserPrompt(t *testing.T) {
	e := state.Event{
		Name: "claude_code.user_prompt",
		Attributes: map[string]string{
			"prompt_length": "342",
		},
		Timestamp: time.Now(),
	}

	fe := FormatEvent("sess-abc", e)

	expected := "[sess-abc] Prompt (342 chars)"
	if fe.Formatted != expected {
		t.Errorf("expected %q, got %q", expected, fe.Formatted)
	}
	if fe.EventType != "user_prompt" {
		t.Errorf("expected EventType='user_prompt', got %q", fe.EventType)
	}
	if fe.SessionID != "sess-abc" {
		t.Errorf("expected SessionID='sess-abc', got %q", fe.SessionID)
	}
	if fe.Success != nil {
		t.Error("expected Success to be nil for user_prompt")
	}
}

func TestEventFormat_UserPrompt_WithContent(t *testing.T) {
	e := state.Event{
		Name: "claude_code.user_prompt",
		Attributes: map[string]string{
			"prompt_length": "20",
			"prompt":        "Fix the login bug",
		},
		Timestamp: time.Now(),
	}

	fe := FormatEvent("sess-abc", e)

	expected := "[sess-abc] Prompt (20 chars): Fix the login bug"
	if fe.Formatted != expected {
		t.Errorf("expected %q, got %q", expected, fe.Formatted)
	}
}

func TestEventFormat_ToolResultSuccess(t *testing.T) {
	e := state.Event{
		Name: "claude_code.tool_result",
		Attributes: map[string]string{
			"tool_name":   "Bash",
			"success":     "true",
			"duration_ms": "1200",
		},
		Timestamp: time.Now(),
	}

	fe := FormatEvent("session", e)

	expected := "[session] Bash \u2713 (1.2s)"
	if fe.Formatted != expected {
		t.Errorf("expected %q, got %q", expected, fe.Formatted)
	}
	if fe.Success == nil || !*fe.Success {
		t.Error("expected Success=true for successful tool_result")
	}
}

func TestEventFormat_ToolResultReject(t *testing.T) {
	e := state.Event{
		Name: "claude_code.tool_result",
		Attributes: map[string]string{
			"tool_name":   "Edit",
			"success":     "false",
			"decision":    "reject",
			"duration_ms": "0",
		},
		Timestamp: time.Now(),
	}

	fe := FormatEvent("session", e)

	expected := "[session] Edit \u2717 rejected by user"
	if fe.Formatted != expected {
		t.Errorf("expected %q, got %q", expected, fe.Formatted)
	}
	if fe.Success == nil || *fe.Success {
		t.Error("expected Success=false for rejected tool_result")
	}
}

func TestEventFormat_APIRequest(t *testing.T) {
	e := state.Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"model":         "sonnet-4.5",
			"input_tokens":  "2100",
			"output_tokens": "890",
			"cost_usd":      "0.03",
			"duration_ms":   "4200",
		},
		Timestamp: time.Now(),
	}

	fe := FormatEvent("session", e)

	expected := "[session] sonnet-4.5 \u2192 2.1k in / 890 out ($0.03) 4.2s"
	if fe.Formatted != expected {
		t.Errorf("expected %q, got %q", expected, fe.Formatted)
	}
	if fe.Success == nil || !*fe.Success {
		t.Error("expected Success=true for api_request")
	}
	if fe.EventType != "api_request" {
		t.Errorf("expected EventType='api_request', got %q", fe.EventType)
	}
}

func TestEventFormat_APIError(t *testing.T) {
	e := state.Event{
		Name: "claude_code.api_error",
		Attributes: map[string]string{
			"status_code": "529",
			"error":       "overloaded",
			"attempt":     "2",
		},
		Timestamp: time.Now(),
	}

	fe := FormatEvent("session", e)

	expected := "[session] 529 overloaded (attempt 2)"
	if fe.Formatted != expected {
		t.Errorf("expected %q, got %q", expected, fe.Formatted)
	}
	if fe.Success == nil || *fe.Success {
		t.Error("expected Success=false for api_error")
	}
}

func TestEventFormat_ToolDecision(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		e := state.Event{
			Name: "claude_code.tool_decision",
			Attributes: map[string]string{
				"tool_name": "Write",
				"decision":  "accept",
				"source":    "config",
			},
			Timestamp: time.Now(),
		}

		fe := FormatEvent("session", e)

		expected := "[session] Write accepted (config)"
		if fe.Formatted != expected {
			t.Errorf("expected %q, got %q", expected, fe.Formatted)
		}
		if fe.Success == nil || !*fe.Success {
			t.Error("expected Success=true for accepted tool_decision")
		}
	})

	t.Run("rejected", func(t *testing.T) {
		e := state.Event{
			Name: "claude_code.tool_decision",
			Attributes: map[string]string{
				"tool_name": "Bash",
				"decision":  "reject",
				"source":    "user",
			},
			Timestamp: time.Now(),
		}

		fe := FormatEvent("session", e)

		expected := "[session] Bash rejected (user)"
		if fe.Formatted != expected {
			t.Errorf("expected %q, got %q", expected, fe.Formatted)
		}
		if fe.Success == nil || *fe.Success {
			t.Error("expected Success=false for rejected tool_decision")
		}
	})
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2100", "2.1k"},
		{"890", "890"},
		{"1000", "1.0k"},
		{"999", "999"},
		{"50000", "50.0k"},
		{"0", "0"},
		{"invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := formatTokenCount(tt.input)
			if got != tt.expected {
				t.Errorf("formatTokenCount(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1200", "1.2s"},
		{"4200", "4.2s"},
		{"500", "0.5s"},
		{"0", "0.0s"},
		{"invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := formatDuration(tt.input)
			if got != tt.expected {
				t.Errorf("formatDuration(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0.03", "0.03"},
		{"1.5", "1.50"},
		{"0.001", "0.00"},
		{"invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := formatCost(tt.input)
			if got != tt.expected {
				t.Errorf("formatCost(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}
