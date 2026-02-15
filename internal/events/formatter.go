// Package events provides formatting and buffering for OTLP events
// received from Claude Code instances.
package events

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

// FormatEvent converts a raw OTLP event into a display-ready FormattedEvent.
// It applies type-specific formatting rules for each of the 5 event types:
//   - user_prompt:   "[session] Prompt (N chars)"
//   - tool_result:   "[session] ToolName check_or_x (duration)" or reject message
//   - api_request:   "[session] model -> input in / output out ($cost) duration"
//   - api_error:     "[session] status_code error (attempt N)"
//   - tool_decision: "[session] ToolName accepted/rejected (source)"
func FormatEvent(sessionID string, e state.Event) FormattedEvent {
	fe := FormattedEvent{
		SessionID: sessionID,
		EventType: stripPrefix(e.Name),
		Timestamp: e.Timestamp,
	}
	if fe.Timestamp.IsZero() {
		fe.Timestamp = time.Now()
	}

	// Deep copy raw attributes.
	if len(e.Attributes) > 0 {
		fe.RawAttributes = make(map[string]string, len(e.Attributes))
		for k, v := range e.Attributes {
			fe.RawAttributes[k] = v
		}
	}

	shortSession := shortID(sessionID)

	switch e.Name {
	case "claude_code.user_prompt":
		fe.Formatted = formatUserPrompt(shortSession, e)
	case "claude_code.tool_result":
		fe.Formatted = formatToolResult(shortSession, e, &fe)
	case "claude_code.api_request":
		fe.Formatted = formatAPIRequest(shortSession, e)
		trueVal := true
		fe.Success = &trueVal
	case "claude_code.api_error":
		fe.Formatted = formatAPIError(shortSession, e)
		falseVal := false
		fe.Success = &falseVal
	case "claude_code.tool_decision":
		fe.Formatted = formatToolDecision(shortSession, e, &fe)
	default:
		fe.Formatted = fmt.Sprintf("[%s] %s", shortSession, e.Name)
	}

	return fe
}

// formatUserPrompt formats: [session] Prompt (N chars)
func formatUserPrompt(session string, e state.Event) string {
	length := attrStr(e, "prompt_length")
	prompt := attrStr(e, "prompt")

	if prompt != "" {
		return fmt.Sprintf("[%s] Prompt (%s chars): %s", session, length, truncatePrompt(prompt, 80))
	}
	return fmt.Sprintf("[%s] Prompt (%s chars)", session, length)
}

// formatToolResult formats tool results with success/failure indicators.
func formatToolResult(session string, e state.Event, fe *FormattedEvent) string {
	toolName := attrStr(e, "tool_name")
	successStr := attrStr(e, "success")
	durationMS := attrStr(e, "duration_ms")
	decision := attrStr(e, "decision")

	// Check for MCP tool details in tool_parameters JSON.
	toolParams := attrStr(e, "tool_parameters")
	if toolParams != "" {
		var params map[string]any
		if err := json.Unmarshal([]byte(toolParams), &params); err == nil {
			if mcpServer, ok := params["mcp_server_name"].(string); ok && mcpServer != "" {
				if mcpTool, ok := params["mcp_tool_name"].(string); ok && mcpTool != "" {
					toolName = fmt.Sprintf("%s:%s", mcpServer, mcpTool)
				}
			}
		}
	}

	success := strings.EqualFold(successStr, "true")
	successPtr := success
	fe.Success = &successPtr

	if !success && strings.EqualFold(decision, "reject") {
		return fmt.Sprintf("[%s] %s \u2717 rejected by user", session, toolName)
	}

	if success {
		return fmt.Sprintf("[%s] %s \u2713 (%s)", session, toolName, formatDuration(durationMS))
	}

	// Failed but not rejected.
	errMsg := attrStr(e, "error")
	if errMsg != "" {
		return fmt.Sprintf("[%s] %s \u2717 %s (%s)", session, toolName, errMsg, formatDuration(durationMS))
	}
	return fmt.Sprintf("[%s] %s \u2717 (%s)", session, toolName, formatDuration(durationMS))
}

// formatAPIRequest formats: [session] model -> input in / output out ($cost) duration
func formatAPIRequest(session string, e state.Event) string {
	model := attrStr(e, "model")
	inputTokens := attrStr(e, "input_tokens")
	outputTokens := attrStr(e, "output_tokens")
	costUSD := attrStr(e, "cost_usd")
	durationMS := attrStr(e, "duration_ms")

	return fmt.Sprintf("[%s] %s \u2192 %s in / %s out ($%s) %s",
		session,
		model,
		formatTokenCount(inputTokens),
		formatTokenCount(outputTokens),
		formatCost(costUSD),
		formatDuration(durationMS),
	)
}

// formatAPIError formats: [session] status_code error (attempt N)
func formatAPIError(session string, e state.Event) string {
	statusCode := attrStr(e, "status_code")
	errMsg := attrStr(e, "error")
	attempt := attrStr(e, "attempt")

	return fmt.Sprintf("[%s] %s %s (attempt %s)", session, statusCode, errMsg, attempt)
}

// formatToolDecision formats: [session] ToolName accepted/rejected (source)
func formatToolDecision(session string, e state.Event, fe *FormattedEvent) string {
	toolName := attrStr(e, "tool_name")
	decision := attrStr(e, "decision")
	source := attrStr(e, "source")

	accepted := strings.EqualFold(decision, "accept")
	fe.Success = &accepted

	decisionWord := "rejected"
	if accepted {
		decisionWord = "accepted"
	}

	return fmt.Sprintf("[%s] %s %s (%s)", session, toolName, decisionWord, source)
}

// formatTokenCount converts a token count string to human-readable format.
// Tokens > 1000 display as Xk (e.g., 2100 -> "2.1k").
func formatTokenCount(s string) string {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", n/1000)
	}
	return fmt.Sprintf("%.0f", n)
}

// FormatTokenCount is an exported version for use by other packages.
func FormatTokenCount(count int64) string {
	if count >= 1000 {
		return fmt.Sprintf("%.1fk", float64(count)/1000)
	}
	return fmt.Sprintf("%d", count)
}

// formatDuration converts a duration_ms string to seconds with 1 decimal.
// E.g., "1200" -> "1.2s".
func formatDuration(ms string) string {
	n, err := strconv.ParseFloat(ms, 64)
	if err != nil {
		return ms
	}
	return fmt.Sprintf("%.1fs", n/1000)
}

// FormatDurationMS is an exported version converting milliseconds to display string.
func FormatDurationMS(ms float64) string {
	return fmt.Sprintf("%.1fs", ms/1000)
}

// formatCost formats a cost string with 2 decimal places.
func formatCost(s string) string {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%.2f", n)
}

// FormatCost is an exported version for use by other packages.
func FormatCost(cost float64) string {
	return fmt.Sprintf("$%.2f", cost)
}

// attrStr returns the attribute value for the given key, or "".
func attrStr(e state.Event, key string) string {
	if e.Attributes == nil {
		return ""
	}
	return e.Attributes[key]
}

// shortID returns a shortened session ID for display. If the ID contains
// a dash, it shows the first segment plus 3 chars after the dash.
// Otherwise it truncates to 8 characters.
func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

// stripPrefix removes the "claude_code." prefix from event names.
func stripPrefix(name string) string {
	return strings.TrimPrefix(name, "claude_code.")
}

// truncatePrompt shortens a prompt string to maxLen with ellipsis.
func truncatePrompt(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
