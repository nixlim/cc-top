package stats

import (
	"math"
	"strconv"
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

	calc := NewCalculator(nil)
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

	calc := NewCalculator(nil)
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

		calc := NewCalculator(nil)
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

		calc := NewCalculator(nil)
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

		calc := NewCalculator(nil)
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

		calc := NewCalculator(nil)
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

		calc := NewCalculator(nil)
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

		calc := NewCalculator(nil)
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

		calc := NewCalculator(nil)
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

		calc := NewCalculator(nil)
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

	calc := NewCalculator(nil)
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

		calc := NewCalculator(nil)
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

		calc := NewCalculator(nil)
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

		calc := NewCalculator(nil)
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

	calc := NewCalculator(nil)
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

	calc := NewCalculator(nil)
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
	calc := NewCalculator(nil)
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

func TestStatsCalc_LinesOfCode_CumulativeCounter(t *testing.T) {
	// Cumulative counters report running totals. If a session reports
	// values 10, 30, 50 over time for "added", only the latest (50)
	// should be used, not the sum (90).
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Metrics: []state.Metric{
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      10,
					Attributes: map[string]string{"type": "added"},
				},
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      30,
					Attributes: map[string]string{"type": "added"},
				},
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      50,
					Attributes: map[string]string{"type": "added"},
				},
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      5,
					Attributes: map[string]string{"type": "removed"},
				},
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      15,
					Attributes: map[string]string{"type": "removed"},
				},
			},
		},
		{
			SessionID: "sess-002",
			Metrics: []state.Metric{
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      1,
					Attributes: map[string]string{"type": "added"},
				},
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      2,
					Attributes: map[string]string{"type": "added"},
				},
				{
					Name:       "claude_code.lines_of_code.count",
					Value:      3,
					Attributes: map[string]string{"type": "added"},
				},
			},
		},
	}

	calc := NewCalculator(nil)
	stats := calc.Compute(sessions)

	// sess-001: latest added=50, latest removed=15
	// sess-002: latest added=3, removed=0
	// Total: added=53, removed=15
	if stats.LinesAdded != 53 {
		t.Errorf("expected LinesAdded=53, got %d", stats.LinesAdded)
	}
	if stats.LinesRemoved != 15 {
		t.Errorf("expected LinesRemoved=15, got %d", stats.LinesRemoved)
	}
}

func TestStatsCalc_CommitsAndPRs_CumulativeCounter(t *testing.T) {
	// Multiple cumulative data points: 1, 2, 3 should yield 3, not 6.
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Metrics: []state.Metric{
				{Name: "claude_code.commit.count", Value: 1},
				{Name: "claude_code.commit.count", Value: 2},
				{Name: "claude_code.commit.count", Value: 3},
				{Name: "claude_code.pull_request.count", Value: 1},
			},
		},
		{
			SessionID: "sess-002",
			Metrics: []state.Metric{
				{Name: "claude_code.commit.count", Value: 5},
				{Name: "claude_code.commit.count", Value: 7},
				{Name: "claude_code.pull_request.count", Value: 1},
				{Name: "claude_code.pull_request.count", Value: 2},
			},
		},
	}

	calc := NewCalculator(nil)
	stats := calc.Compute(sessions)

	// sess-001: latest commit=3, latest PR=1
	// sess-002: latest commit=7, latest PR=2
	// Total: commits=10, PRs=3
	if stats.Commits != 10 {
		t.Errorf("expected Commits=10, got %d", stats.Commits)
	}
	if stats.PRs != 3 {
		t.Errorf("expected PRs=3, got %d", stats.PRs)
	}
}

func TestStatsCalc_CacheEfficiency_CumulativeCounter(t *testing.T) {
	// Multiple cumulative data points per session+type: use latest only.
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Metrics: []state.Metric{
				{
					Name:       "claude_code.token.usage",
					Value:      10000,
					Attributes: map[string]string{"type": "cacheRead"},
				},
				{
					Name:       "claude_code.token.usage",
					Value:      5000,
					Attributes: map[string]string{"type": "input"},
				},
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

	calc := NewCalculator(nil)
	stats := calc.Compute(sessions)

	// Latest cacheRead=80000, latest input=20000
	// Efficiency = 80000 / (20000 + 80000) = 0.80
	if math.Abs(stats.CacheEfficiency-0.80) > 0.001 {
		t.Errorf("expected CacheEfficiency=0.80, got %f", stats.CacheEfficiency)
	}
}

func TestStatsCalc_ToolAcceptance_CumulativeCounter(t *testing.T) {
	// Multiple cumulative data points per session+tool+decision: use latest only.
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Metrics: []state.Metric{
				{
					Name:       "claude_code.code_edit_tool.decision",
					Value:      2,
					Attributes: map[string]string{"tool": "Edit", "decision": "accept"},
				},
				{
					Name:       "claude_code.code_edit_tool.decision",
					Value:      1,
					Attributes: map[string]string{"tool": "Edit", "decision": "reject"},
				},
				// Later cumulative update: accept grew to 8, reject grew to 2.
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
			},
		},
	}

	calc := NewCalculator(nil)
	stats := calc.Compute(sessions)

	// Latest accept=8, latest reject=2, total=10
	// Rate = 8/10 = 0.80
	if math.Abs(stats.ToolAcceptance["Edit"]-0.80) > 0.001 {
		t.Errorf("expected Edit acceptance=0.80, got %f", stats.ToolAcceptance["Edit"])
	}
}

func TestStatsCalc_LanguageBreakdown(t *testing.T) {
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Metrics: []state.Metric{
				{
					Name:       "claude_code.code_edit_tool.decision",
					Value:      1,
					Attributes: map[string]string{"tool": "Edit", "decision": "accept", "language": "go"},
				},
				{
					Name:       "claude_code.code_edit_tool.decision",
					Value:      1,
					Attributes: map[string]string{"tool": "Edit", "decision": "accept", "language": "go"},
				},
				{
					Name:       "claude_code.code_edit_tool.decision",
					Value:      1,
					Attributes: map[string]string{"tool": "Edit", "decision": "reject", "language": "python"},
				},
				{
					Name:       "claude_code.code_edit_tool.decision",
					Value:      1,
					Attributes: map[string]string{"tool": "Edit", "decision": "accept"},
				},
			},
		},
	}

	calc := NewCalculator(nil)
	stats := calc.Compute(sessions)

	if stats.LanguageBreakdown["go"] != 2 {
		t.Errorf("expected go=2, got %d", stats.LanguageBreakdown["go"])
	}
	if stats.LanguageBreakdown["python"] != 1 {
		t.Errorf("expected python=1, got %d", stats.LanguageBreakdown["python"])
	}
	if _, ok := stats.LanguageBreakdown[""]; ok {
		t.Error("empty language should not be in breakdown")
	}
}

func TestStatsCalc_LanguageBreakdown_Empty(t *testing.T) {
	calc := NewCalculator(nil)
	stats := calc.Compute(nil)

	if len(stats.LanguageBreakdown) != 0 {
		t.Errorf("expected empty language breakdown, got %v", stats.LanguageBreakdown)
	}
}

func TestStatsCalc_DecisionSources(t *testing.T) {
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Events: []state.Event{
				{
					Name:       "claude_code.tool_decision",
					Attributes: map[string]string{"tool_name": "Bash", "decision": "accept", "source": "config"},
				},
				{
					Name:       "claude_code.tool_decision",
					Attributes: map[string]string{"tool_name": "Edit", "decision": "accept", "source": "config"},
				},
				{
					Name:       "claude_code.tool_decision",
					Attributes: map[string]string{"tool_name": "Write", "decision": "reject", "source": "user_temporary"},
				},
				{
					Name:       "claude_code.tool_decision",
					Attributes: map[string]string{"tool_name": "Bash", "decision": "reject", "source": "user_abort"},
				},
				{
					Name:       "claude_code.tool_decision",
					Attributes: map[string]string{"tool_name": "Bash", "decision": "accept"},
				},
			},
		},
	}

	calc := NewCalculator(nil)
	stats := calc.Compute(sessions)

	if stats.DecisionSources["config"] != 2 {
		t.Errorf("expected config=2, got %d", stats.DecisionSources["config"])
	}
	if stats.DecisionSources["user_temporary"] != 1 {
		t.Errorf("expected user_temporary=1, got %d", stats.DecisionSources["user_temporary"])
	}
	if stats.DecisionSources["user_abort"] != 1 {
		t.Errorf("expected user_abort=1, got %d", stats.DecisionSources["user_abort"])
	}
	if _, ok := stats.DecisionSources[""]; ok {
		t.Error("empty source should not be in breakdown")
	}
}

func TestStatsCalc_ErrorCategories(t *testing.T) {
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Events: []state.Event{
				{Name: "claude_code.api_error", Attributes: map[string]string{"status_code": "429"}},
				{Name: "claude_code.api_error", Attributes: map[string]string{"status_code": "429"}},
				{Name: "claude_code.api_error", Attributes: map[string]string{"status_code": "401"}},
				{Name: "claude_code.api_error", Attributes: map[string]string{"status_code": "403"}},
				{Name: "claude_code.api_error", Attributes: map[string]string{"status_code": "500"}},
				{Name: "claude_code.api_error", Attributes: map[string]string{"status_code": "502"}},
				{Name: "claude_code.api_error", Attributes: map[string]string{"status_code": "599"}},
				{Name: "claude_code.api_error", Attributes: map[string]string{"status_code": "400"}},
				{Name: "claude_code.api_error", Attributes: map[string]string{"status_code": ""}},
				{Name: "claude_code.api_error", Attributes: map[string]string{"status_code": "abc"}},
				{Name: "claude_code.api_error", Attributes: map[string]string{}},
			},
		},
	}

	calc := NewCalculator(nil)
	stats := calc.Compute(sessions)

	if stats.ErrorCategories["rate_limit"] != 2 {
		t.Errorf("expected rate_limit=2, got %d", stats.ErrorCategories["rate_limit"])
	}
	if stats.ErrorCategories["auth_failure"] != 2 {
		t.Errorf("expected auth_failure=2, got %d", stats.ErrorCategories["auth_failure"])
	}
	if stats.ErrorCategories["server_error"] != 3 {
		t.Errorf("expected server_error=3, got %d", stats.ErrorCategories["server_error"])
	}
	if stats.ErrorCategories["other"] != 4 {
		t.Errorf("expected other=4, got %d", stats.ErrorCategories["other"])
	}
}

func TestStatsCalc_RetryRate(t *testing.T) {
	t.Run("mixed retries", func(t *testing.T) {
		sessions := []state.SessionData{
			{
				SessionID: "sess-001",
				Events: []state.Event{
					{Name: "claude_code.api_error", Attributes: map[string]string{"attempt": "1"}},
					{Name: "claude_code.api_error", Attributes: map[string]string{"attempt": "2"}},
					{Name: "claude_code.api_error", Attributes: map[string]string{"attempt": "3"}},
					{Name: "claude_code.api_error", Attributes: map[string]string{"attempt": "1"}},
				},
			},
		}

		calc := NewCalculator(nil)
		stats := calc.Compute(sessions)

		// 2 retries out of 4 = 0.5
		if math.Abs(stats.RetryRate-0.5) > 0.001 {
			t.Errorf("expected RetryRate=0.5, got %f", stats.RetryRate)
		}
	})

	t.Run("no errors", func(t *testing.T) {
		calc := NewCalculator(nil)
		stats := calc.Compute(nil)

		if stats.RetryRate != 0 {
			t.Errorf("expected RetryRate=0, got %f", stats.RetryRate)
		}
	})

	t.Run("missing attempt", func(t *testing.T) {
		sessions := []state.SessionData{
			{
				SessionID: "sess-001",
				Events: []state.Event{
					{Name: "claude_code.api_error", Attributes: map[string]string{}},
					{Name: "claude_code.api_error", Attributes: map[string]string{"attempt": "bad"}},
				},
			},
		}

		calc := NewCalculator(nil)
		stats := calc.Compute(sessions)

		if stats.RetryRate != 0 {
			t.Errorf("expected RetryRate=0 with malformed attempts, got %f", stats.RetryRate)
		}
	})
}

func TestStatsCalc_ToolPerformance(t *testing.T) {
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Events: []state.Event{
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Bash", "duration_ms": "100"}},
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Bash", "duration_ms": "200"}},
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Bash", "duration_ms": "300"}},
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Edit", "duration_ms": "50"}},
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Edit", "duration_ms": "150"}},
				{Name: "claude_code.tool_result", Attributes: map[string]string{"tool_name": "Read", "success": "true"}}, // no duration_ms
			},
		},
	}

	calc := NewCalculator(nil)
	stats := calc.Compute(sessions)

	if len(stats.ToolPerformance) != 2 {
		t.Fatalf("expected 2 tools in performance, got %d", len(stats.ToolPerformance))
	}

	// Sorted by avg descending: Bash (200) then Edit (100).
	if stats.ToolPerformance[0].ToolName != "Bash" {
		t.Errorf("expected first tool=Bash, got %s", stats.ToolPerformance[0].ToolName)
	}
	if math.Abs(stats.ToolPerformance[0].AvgDurationMS-200) > 0.001 {
		t.Errorf("expected Bash avg=200, got %f", stats.ToolPerformance[0].AvgDurationMS)
	}

	if stats.ToolPerformance[1].ToolName != "Edit" {
		t.Errorf("expected second tool=Edit, got %s", stats.ToolPerformance[1].ToolName)
	}
	if math.Abs(stats.ToolPerformance[1].AvgDurationMS-100) > 0.001 {
		t.Errorf("expected Edit avg=100, got %f", stats.ToolPerformance[1].AvgDurationMS)
	}
}

func TestStatsCalc_LatencyPercentiles(t *testing.T) {
	t.Run("normal percentiles", func(t *testing.T) {
		// Create 100 events with durations 1000..100000 ms (1s..100s).
		events := make([]state.Event, 100)
		for i := range 100 {
			events[i] = state.Event{
				Name: "claude_code.api_request",
				Attributes: map[string]string{
					"duration_ms": strconv.Itoa((i + 1) * 1000),
				},
			}
		}

		sessions := []state.SessionData{{SessionID: "sess-001", Events: events}}
		calc := NewCalculator(nil)
		stats := calc.Compute(sessions)

		// P50 ~50s, P95 ~95s, P99 ~99s (in seconds).
		if stats.LatencyPercentiles.P50 < 40 || stats.LatencyPercentiles.P50 > 60 {
			t.Errorf("expected P50 ~50s, got %f", stats.LatencyPercentiles.P50)
		}
		if stats.LatencyPercentiles.P95 < 90 || stats.LatencyPercentiles.P95 > 100 {
			t.Errorf("expected P95 ~95s, got %f", stats.LatencyPercentiles.P95)
		}
		if stats.LatencyPercentiles.P99 < 95 {
			t.Errorf("expected P99 >= 95s, got %f", stats.LatencyPercentiles.P99)
		}
		// P95 >= P50, P99 >= P95.
		if stats.LatencyPercentiles.P95 < stats.LatencyPercentiles.P50 {
			t.Error("P95 should be >= P50")
		}
		if stats.LatencyPercentiles.P99 < stats.LatencyPercentiles.P95 {
			t.Error("P99 should be >= P95")
		}
	})

	t.Run("empty", func(t *testing.T) {
		calc := NewCalculator(nil)
		stats := calc.Compute(nil)

		if stats.LatencyPercentiles.P50 != 0 || stats.LatencyPercentiles.P95 != 0 || stats.LatencyPercentiles.P99 != 0 {
			t.Errorf("expected all zeros, got %+v", stats.LatencyPercentiles)
		}
	})
}

func TestStatsCalc_TokenBreakdown(t *testing.T) {
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Metrics: []state.Metric{
				{Name: "claude_code.token.usage", Value: 1000, Attributes: map[string]string{"type": "input"}},
				{Name: "claude_code.token.usage", Value: 500, Attributes: map[string]string{"type": "output"}},
				{Name: "claude_code.token.usage", Value: 8000, Attributes: map[string]string{"type": "cacheRead"}},
				{Name: "claude_code.token.usage", Value: 200, Attributes: map[string]string{"type": "cacheCreation"}},
				// Later cumulative update.
				{Name: "claude_code.token.usage", Value: 2000, Attributes: map[string]string{"type": "input"}},
				{Name: "claude_code.token.usage", Value: 1000, Attributes: map[string]string{"type": "output"}},
			},
		},
		{
			SessionID: "sess-002",
			Metrics: []state.Metric{
				{Name: "claude_code.token.usage", Value: 500, Attributes: map[string]string{"type": "input"}},
				{Name: "claude_code.token.usage", Value: 300, Attributes: map[string]string{"type": "output"}},
			},
		},
	}

	calc := NewCalculator(nil)
	stats := calc.Compute(sessions)

	// sess-001: latest input=2000, output=1000, cacheRead=8000, cacheCreation=200
	// sess-002: input=500, output=300
	if stats.TokenBreakdown["input"] != 2500 {
		t.Errorf("expected input=2500, got %d", stats.TokenBreakdown["input"])
	}
	if stats.TokenBreakdown["output"] != 1300 {
		t.Errorf("expected output=1300, got %d", stats.TokenBreakdown["output"])
	}
	if stats.TokenBreakdown["cacheRead"] != 8000 {
		t.Errorf("expected cacheRead=8000, got %d", stats.TokenBreakdown["cacheRead"])
	}
	if stats.TokenBreakdown["cacheCreation"] != 200 {
		t.Errorf("expected cacheCreation=200, got %d", stats.TokenBreakdown["cacheCreation"])
	}
}

func TestStatsCalc_CacheSavings(t *testing.T) {
	t.Run("with pricing", func(t *testing.T) {
		pricing := map[string][4]float64{
			// [input, output, cacheRead, cacheCreation] per 1M tokens.
			"sonnet-4.5": {3.0, 15.0, 0.3, 3.75},
		}
		sessions := []state.SessionData{
			{
				SessionID:       "sess-001",
				Model:           "sonnet-4.5",
				CacheReadTokens: 1_000_000,
			},
		}

		calc := NewCalculator(pricing)
		stats := calc.Compute(sessions)

		// Savings = 1_000_000 * (3.0 - 0.3) / 1_000_000 = 2.70
		if math.Abs(stats.CacheSavingsUSD-2.70) > 0.001 {
			t.Errorf("expected CacheSavingsUSD=2.70, got %f", stats.CacheSavingsUSD)
		}
	})

	t.Run("no pricing", func(t *testing.T) {
		sessions := []state.SessionData{
			{SessionID: "sess-001", Model: "sonnet-4.5", CacheReadTokens: 1_000_000},
		}

		calc := NewCalculator(nil)
		stats := calc.Compute(sessions)

		if stats.CacheSavingsUSD != 0 {
			t.Errorf("expected 0 with nil pricing, got %f", stats.CacheSavingsUSD)
		}
	})

	t.Run("unknown model", func(t *testing.T) {
		pricing := map[string][4]float64{
			"sonnet-4.5": {3.0, 15.0, 0.3, 3.75},
		}
		sessions := []state.SessionData{
			{SessionID: "sess-001", Model: "unknown-model", CacheReadTokens: 1_000_000},
		}

		calc := NewCalculator(pricing)
		stats := calc.Compute(sessions)

		if stats.CacheSavingsUSD != 0 {
			t.Errorf("expected 0 for unknown model, got %f", stats.CacheSavingsUSD)
		}
	})

	t.Run("zero tokens", func(t *testing.T) {
		pricing := map[string][4]float64{
			"sonnet-4.5": {3.0, 15.0, 0.3, 3.75},
		}
		sessions := []state.SessionData{
			{SessionID: "sess-001", Model: "sonnet-4.5", CacheReadTokens: 0},
		}

		calc := NewCalculator(pricing)
		stats := calc.Compute(sessions)

		if stats.CacheSavingsUSD != 0 {
			t.Errorf("expected 0 for zero tokens, got %f", stats.CacheSavingsUSD)
		}
	})
}

func TestStatsCalc_MCPToolUsage(t *testing.T) {
	sessions := []state.SessionData{
		{
			SessionID: "sess-001",
			Events: []state.Event{
				{
					Name: "claude_code.tool_result",
					Attributes: map[string]string{
						"tool_name":       "mcp_tool",
						"tool_parameters": `{"mcp_server_name":"github","mcp_tool_name":"create_issue"}`,
					},
				},
				{
					Name: "claude_code.tool_result",
					Attributes: map[string]string{
						"tool_name":       "mcp_tool",
						"tool_parameters": `{"mcp_server_name":"github","mcp_tool_name":"create_issue"}`,
					},
				},
				{
					Name: "claude_code.tool_result",
					Attributes: map[string]string{
						"tool_name":       "mcp_tool",
						"tool_parameters": `{"mcp_server_name":"slack","mcp_tool_name":"send_message"}`,
					},
				},
				{
					Name: "claude_code.tool_result",
					Attributes: map[string]string{
						"tool_name": "Bash",
					},
				},
			},
		},
	}

	calc := NewCalculator(nil)
	stats := calc.Compute(sessions)

	if stats.MCPToolUsage["github:create_issue"] != 2 {
		t.Errorf("expected github:create_issue=2, got %d", stats.MCPToolUsage["github:create_issue"])
	}
	if stats.MCPToolUsage["slack:send_message"] != 1 {
		t.Errorf("expected slack:send_message=1, got %d", stats.MCPToolUsage["slack:send_message"])
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
