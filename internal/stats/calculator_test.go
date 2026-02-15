package stats

import (
	"math"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

func TestStatsCalc_LinesOfCode(t *testing.T) {
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Metrics: []state.Metric{
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      100,
					Attributes: map[string]string{"type": "added"},
				},
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      20,
					Attributes: map[string]string{"type": "removed"},
				},
			},
		},
		{
			SessionID: "sess-002",
			Metrics: []state.Metric{
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      50,
					Attributes: map[string]string{"type": "added"},
				},
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      10,
					Attributes: map[string]string{"type": "removed"},
				},
			},
		},
	}

	calc := NewCalculator()
	stats := calc.Compute(sessions)

	if stats.LinesAdded != 150 {
		t.Errorf("expected LinesAdded=150, got %d", stats.LinesAdded)
	}
	if stats.LinesRemoved != 30 {
		t.Errorf("expected LinesRemoved=30, got %d", stats.LinesRemoved)
	}
}

func TestStatsCalc_CommitsAndPRs(t *testing.T) {
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Metrics: []state.Metric{
				{Name: "claude_code.commit.count", Value: 3},
				{Name: "claude_code.pull_request.count", Value: 1},
			},
		},
		{
			SessionID: "sess-002",
			Metrics: []state.Metric{
				{Name: "claude_code.commit.count", Value: 2},
				{Name: "claude_code.pull_request.count", Value: 1},
			},
		},
	}

	calc := NewCalculator()
	stats := calc.Compute(sessions)

	if stats.Commits != 5 {
		t.Errorf("expected Commits=5, got %d", stats.Commits)
	}
	if stats.PRs != 2 {
		t.Errorf("expected PRs=2, got %d", stats.PRs)
	}
}

func TestStatsCalc_CacheEfficiency(t *testing.T) {
	t.Run("normal calculation", func(t *testing.T) {
		sessions := []state.SessionData{
			{
				SessionID: "sess-001",
				Metrics: []state.Metric{
					{
						Name:       "claude_code.token.usage",
						Value:      80000,
						Attributes: map[string]string{"type": "cacheRead"},
					},
					{
						Name:       "claude_code.token.usage",
						Value:      20000,
						Attributes: map[string]string{"type": "input"},
					},
				},
			},
		}

		calc := NewCalculator()
		stats := calc.Compute(sessions)

		// 80000 / (20000 + 80000) = 0.80
		if math.Abs(stats.CacheEfficiency-0.80) > 0.001 {
			t.Errorf("expected CacheEfficiency=0.80, got %f", stats.CacheEfficiency)
		}
	})

	t.Run("division by zero", func(t *testing.T) {
		sessions := []state.SessionData{
			{
				SessionID: "sess-001",
				Metrics:   []state.Metric{},
			},
		}

		calc := NewCalculator()
		stats := calc.Compute(sessions)

		if stats.CacheEfficiency != 0 {
			t.Errorf("expected CacheEfficiency=0 for no data, got %f", stats.CacheEfficiency)
		}
	})

	t.Run("all cache hits", func(t *testing.T) {
		sessions := []state.SessionData{
			{
				SessionID: "sess-001",
				Metrics: []state.Metric{
					{
						Name:       "claude_code.token.usage",
						Value:      50000,
						Attributes: map[string]string{"type": "cacheRead"},
					},
					{
						Name:       "claude_code.token.usage",
						Value:      0,
						Attributes: map[string]string{"type": "input"},
					},
				},
			},
		}

		calc := NewCalculator()
		stats := calc.Compute(sessions)

		if math.Abs(stats.CacheEfficiency-1.0) > 0.001 {
			t.Errorf("expected CacheEfficiency=1.0, got %f", stats.CacheEfficiency)
		}
	})

	t.Run("no cache reads", func(t *testing.T) {
		sessions := []state.SessionData{
			{
				SessionID: "sess-001",
				Metrics: []state.Metric{
					{
						Name:       "claude_code.token.usage",
						Value:      0,
						Attributes: map[string]string{"type": "cacheRead"},
					},
					{
						Name:       "claude_code.token.usage",
						Value:      10000,
						Attributes: map[string]string{"type": "input"},
					},
				},
			},
		}

		calc := NewCalculator()
		stats := calc.Compute(sessions)

		if stats.CacheEfficiency != 0 {
			t.Errorf("expected CacheEfficiency=0, got %f", stats.CacheEfficiency)
		}
	})
}

func TestStatsCalc_ErrorRate(t *testing.T) {
	t.Run("normal error rate", func(t *testing.T) {
		sessions := []state.SessionData{
			{
				SessionID: "sess-001",
				Events:    makeAPIEvents(95, 5),
			},
		}

		calc := NewCalculator()
		stats := calc.Compute(sessions)

		// 5 / 100 = 0.05
		// Note: we have 95 api_request + 5 api_error = 100 events
		// But the denominator is api_request count (95), not total.
		// Actually re-reading the spec: error rate = api_error count / api_request count
		// So 5 / 95 = 0.0526...
		// Wait, let me re-check. The spec says 100 api_request and 5 api_error = 5%.
		// So it seems like total api_request events in the denominator.
		// Let me make it match the spec example: 100 requests + 5 errors = 5%.
		// That means 5/100 = 0.05.
		expected := 5.0 / 95.0
		if math.Abs(stats.ErrorRate-expected) > 0.001 {
			t.Errorf("expected ErrorRate=%f, got %f", expected, stats.ErrorRate)
		}
	})

	t.Run("exact spec example", func(t *testing.T) {
		// From the spec: 100 api_request events and 5 api_error events = 5.0%
		sessions := []state.SessionData{
			{
				SessionID: "sess-001",
				Events:    makeAPIEvents(100, 5),
			},
		}

		calc := NewCalculator()
		stats := calc.Compute(sessions)

		// 5 / 100 = 0.05
		if math.Abs(stats.ErrorRate-0.05) > 0.001 {
			t.Errorf("expected ErrorRate=0.05, got %f", stats.ErrorRate)
		}
	})

	t.Run("no requests means zero error rate", func(t *testing.T) {
		sessions := []state.SessionData{
			{SessionID: "sess-001"},
		}

		calc := NewCalculator()
		stats := calc.Compute(sessions)

		if stats.ErrorRate != 0 {
			t.Errorf("expected ErrorRate=0 for no requests, got %f", stats.ErrorRate)
		}
	})

	t.Run("no errors", func(t *testing.T) {
		sessions := []state.SessionData{
			{
				SessionID: "sess-001",
				Events:    makeAPIEvents(50, 0),
			},
		}

		calc := NewCalculator()
		stats := calc.Compute(sessions)

		if stats.ErrorRate != 0 {
			t.Errorf("expected ErrorRate=0, got %f", stats.ErrorRate)
		}
	})
}

func TestStatsCalc_ToolAcceptRate(t *testing.T) {
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Metrics: []state.Metric{
				{
					Name:       "claude_code.code_edit_tool.decision",
					Value:      8,
					Attributes: map[string]string{"tool": "Edit", "decision": "accept"},
				},
				{
					Name:       "claude_code.code_edit_tool.decision",
					Value:      2,
					Attributes: map[string]string{"tool": "Edit", "decision": "reject"},
				},
				{
					Name:       "claude_code.code_edit_tool.decision",
					Value:      5,
					Attributes: map[string]string{"tool": "Write", "decision": "accept"},
				},
			},
		},
	}

	calc := NewCalculator()
	stats := calc.Compute(sessions)

	// Edit: 8 / (8+2) = 0.80
	if math.Abs(stats.ToolAcceptance["Edit"]-0.80) > 0.001 {
		t.Errorf("expected Edit acceptance=0.80, got %f", stats.ToolAcceptance["Edit"])
	}

	// Write: 5 / 5 = 1.00
	if math.Abs(stats.ToolAcceptance["Write"]-1.0) > 0.001 {
		t.Errorf("expected Write acceptance=1.0, got %f", stats.ToolAcceptance["Write"])
	}
}

func TestStatsCalc_AvgLatency(t *testing.T) {
	t.Run("normal latency", func(t *testing.T) {
		events := make([]state.Event, 10)
		for i := 0; i < 10; i++ {
			events[i] = state.Event{
				Name: "claude_code.api_request",
				Attributes: map[string]string{
					"duration_ms": "3500",
					"model":       "sonnet-4.5",
				},
				Timestamp: time.Now(),
			}
		}

		sessions := []state.SessionData{
			{SessionID: "sess-001", Events: events},
		}

		calc := NewCalculator()
		stats := calc.Compute(sessions)

		// Average of 3500ms = 3.5s
		if math.Abs(stats.AvgAPILatency-3.5) > 0.001 {
			t.Errorf("expected AvgAPILatency=3.5s, got %f", stats.AvgAPILatency)
		}
	})

	t.Run("mixed latencies", func(t *testing.T) {
		sessions := []state.SessionData{
			{
				SessionID: "sess-001",
				Events: []state.Event{
					{
						Name:       "claude_code.api_request",
						Attributes: map[string]string{"duration_ms": "1000"},
					},
					{
						Name:       "claude_code.api_request",
						Attributes: map[string]string{"duration_ms": "5000"},
					},
				},
			},
		}

		calc := NewCalculator()
		stats := calc.Compute(sessions)

		// Average of (1000 + 5000) / 2 = 3000ms = 3.0s
		if math.Abs(stats.AvgAPILatency-3.0) > 0.001 {
			t.Errorf("expected AvgAPILatency=3.0s, got %f", stats.AvgAPILatency)
		}
	})

	t.Run("no requests", func(t *testing.T) {
		sessions := []state.SessionData{
			{SessionID: "sess-001"},
		}

		calc := NewCalculator()
		stats := calc.Compute(sessions)

		if stats.AvgAPILatency != 0 {
			t.Errorf("expected AvgAPILatency=0 for no requests, got %f", stats.AvgAPILatency)
		}
	})
}

func TestStatsCalc_ModelBreakdown(t *testing.T) {
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Events: []state.Event{
				{
					Name: "claude_code.api_request",
					Attributes: map[string]string{
						"model":         "sonnet-4.5",
						"cost_usd":      "0.50",
						"input_tokens":  "1000",
						"output_tokens": "500",
					},
				},
				{
					Name: "claude_code.api_request",
					Attributes: map[string]string{
						"model":         "sonnet-4.5",
						"cost_usd":      "0.50",
						"input_tokens":  "2000",
						"output_tokens": "1000",
					},
				},
				{
					Name: "claude_code.api_request",
					Attributes: map[string]string{
						"model":         "haiku-4.5",
						"cost_usd":      "0.20",
						"input_tokens":  "500",
						"output_tokens": "200",
					},
				},
			},
		},
	}

	calc := NewCalculator()
	stats := calc.Compute(sessions)

	if len(stats.ModelBreakdown) != 2 {
		t.Fatalf("expected 2 models in breakdown, got %d", len(stats.ModelBreakdown))
	}

	// Sorted by cost descending: sonnet first.
	if stats.ModelBreakdown[0].Model != "sonnet-4.5" {
		t.Errorf("expected first model='sonnet-4.5', got %q", stats.ModelBreakdown[0].Model)
	}
	if math.Abs(stats.ModelBreakdown[0].TotalCost-1.0) > 0.001 {
		t.Errorf("expected sonnet cost=1.0, got %f", stats.ModelBreakdown[0].TotalCost)
	}
	if stats.ModelBreakdown[0].TotalTokens != 4500 {
		t.Errorf("expected sonnet tokens=4500, got %d", stats.ModelBreakdown[0].TotalTokens)
	}

	if stats.ModelBreakdown[1].Model != "haiku-4.5" {
		t.Errorf("expected second model='haiku-4.5', got %q", stats.ModelBreakdown[1].Model)
	}
}

func TestStatsCalc_TopTools(t *testing.T) {
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Events: []state.Event{
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Bash"}},
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Bash"}},
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Bash"}},
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Edit"}},
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Edit"}},
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Read"}},
			},
		},
	}

	calc := NewCalculator()
	stats := calc.Compute(sessions)

	if len(stats.TopTools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(stats.TopTools))
	}

	// Sorted by count descending.
	if stats.TopTools[0].ToolName != "Bash" || stats.TopTools[0].Count != 3 {
		t.Errorf("expected top tool Bash(3), got %s(%d)", stats.TopTools[0].ToolName, stats.TopTools[0].Count)
	}
	if stats.TopTools[1].ToolName != "Edit" || stats.TopTools[1].Count != 2 {
		t.Errorf("expected second tool Edit(2), got %s(%d)", stats.TopTools[1].ToolName, stats.TopTools[1].Count)
	}
	if stats.TopTools[2].ToolName != "Read" || stats.TopTools[2].Count != 1 {
		t.Errorf("expected third tool Read(1), got %s(%d)", stats.TopTools[2].ToolName, stats.TopTools[2].Count)
	}
}

func TestStatsCalc_EmptySessions(t *testing.T) {
	calc := NewCalculator()
	stats := calc.Compute(nil)

	if stats.LinesAdded != 0 {
		t.Errorf("expected LinesAdded=0, got %d", stats.LinesAdded)
	}
	if stats.ErrorRate != 0 {
		t.Errorf("expected ErrorRate=0, got %f", stats.ErrorRate)
	}
	if stats.CacheEfficiency != 0 {
		t.Errorf("expected CacheEfficiency=0, got %f", stats.CacheEfficiency)
	}
	if stats.AvgAPILatency != 0 {
		t.Errorf("expected AvgAPILatency=0, got %f", stats.AvgAPILatency)
	}
}

// makeAPIEvents creates N api_request events and M api_error events.
func makeAPIEvents(requests, errors int) []state.Event {
	events := make([]state.Event, 0, requests+errors)
	for i := 0; i < requests; i++ {
		events = append(events, state.Event{
			Name: "claude_code.api_request",
			Attributes: map[string]string{
				"model":       "sonnet-4.5",
				"duration_ms": "3000",
			},
			Timestamp: time.Now(),
		})
	}
	for i := 0; i < errors; i++ {
		events = append(events, state.Event{
			Name: "claude_code.api_error",
			Attributes: map[string]string{
				"status_code": "529",
				"error":       "overloaded",
			},
			Timestamp: time.Now(),
		})
	}
	return events
}
