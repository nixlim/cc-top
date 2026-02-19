package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"
)

type mockHistoryStateProvider struct {
	summaries []state.DailySummary
}

func (m *mockHistoryStateProvider) GetSession(_ string) *state.SessionData { return nil }
func (m *mockHistoryStateProvider) ListSessions() []state.SessionData      { return nil }
func (m *mockHistoryStateProvider) GetAggregatedCost() float64             { return 0 }
func (m *mockHistoryStateProvider) QueryDailySummaries(_ int) []state.DailySummary {
	return m.summaries
}
func (m *mockHistoryStateProvider) DroppedWrites() int64 { return 0 }

func TestHistoryView_DailyGranularity(t *testing.T) {
	mock := &mockHistoryStateProvider{
		summaries: []state.DailySummary{
			{Date: "2026-02-17", TotalCost: 5.50, TotalTokens: 100000, SessionCount: 3, APIRequests: 50, APIErrors: 2},
			{Date: "2026-02-16", TotalCost: 3.25, TotalTokens: 75000, SessionCount: 2, APIRequests: 30, APIErrors: 0},
			{Date: "2026-02-15", TotalCost: 8.10, TotalTokens: 200000, SessionCount: 5, APIRequests: 80, APIErrors: 5},
		},
	}

	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewHistory), WithStateProvider(mock), WithPersistenceFlag(true))
	m.width = 100
	m.height = 30

	view := m.renderHistory()

	if !strings.Contains(view, "History") {
		t.Error("history view should contain 'History' in header")
	}
	if !strings.Contains(view, "2026-02-17") {
		t.Error("history view should contain date '2026-02-17'")
	}
	if !strings.Contains(view, "2026-02-16") {
		t.Error("history view should contain date '2026-02-16'")
	}
	if !strings.Contains(view, "5.50") {
		t.Errorf("history view should contain cost '5.50', got:\n%s", view)
	}
}

func TestHistoryView_WeeklyGranularity(t *testing.T) {
	var summaries []state.DailySummary
	for i := range 14 {
		summaries = append(summaries, state.DailySummary{
			Date:         fmt.Sprintf("2026-02-%02d", 17-i),
			TotalCost:    1.0,
			TotalTokens:  10000,
			SessionCount: 1,
			APIRequests:  5,
			APIErrors:    0,
		})
	}

	mock := &mockHistoryStateProvider{summaries: summaries}

	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewHistory), WithStateProvider(mock), WithPersistenceFlag(true))
	m.width = 100
	m.height = 30
	m.historyGranularity = "weekly"

	view := m.renderHistory()

	if !strings.Contains(view, "Week") {
		t.Error("weekly view should contain 'Week' label")
	}
}

func TestHistoryView_MonthlyGranularity(t *testing.T) {
	summaries := []state.DailySummary{
		{Date: "2026-02-15", TotalCost: 5.0, TotalTokens: 100000, SessionCount: 3, APIRequests: 50, APIErrors: 2},
		{Date: "2026-01-20", TotalCost: 10.0, TotalTokens: 200000, SessionCount: 5, APIRequests: 80, APIErrors: 3},
		{Date: "2025-12-10", TotalCost: 8.0, TotalTokens: 150000, SessionCount: 4, APIRequests: 60, APIErrors: 1},
	}

	mock := &mockHistoryStateProvider{summaries: summaries}

	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewHistory), WithStateProvider(mock), WithPersistenceFlag(true))
	m.width = 100
	m.height = 30
	m.historyGranularity = "monthly"

	view := m.renderHistory()

	if !strings.Contains(view, "2026-02") {
		t.Error("monthly view should contain '2026-02'")
	}
	if !strings.Contains(view, "2026-01") {
		t.Error("monthly view should contain '2026-01'")
	}
}

func TestHistoryView_NoData(t *testing.T) {
	mock := &mockHistoryStateProvider{summaries: nil}

	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewHistory), WithStateProvider(mock), WithPersistenceFlag(true))
	m.width = 100
	m.height = 30

	view := m.renderHistory()

	if !strings.Contains(view, "No historical data") {
		t.Errorf("empty history should show 'No historical data', got:\n%s", view)
	}
}

func TestHistoryView_PartialData(t *testing.T) {
	mock := &mockHistoryStateProvider{
		summaries: []state.DailySummary{
			{Date: "2026-02-17", TotalCost: 0, TotalTokens: 0, SessionCount: 1, APIRequests: 0, APIErrors: 0},
		},
	}

	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewHistory), WithStateProvider(mock), WithPersistenceFlag(true))
	m.width = 100
	m.height = 30

	view := m.renderHistory()

	if !strings.Contains(view, "2026-02-17") {
		t.Error("partial data should still render the date")
	}
	if !strings.Contains(view, "0.00") {
		t.Errorf("partial data should show 0.00, got:\n%s", view)
	}
}

func TestHistoryView_MemoryOnlyMode(t *testing.T) {
	mock := &mockHistoryStateProvider{summaries: nil}

	cfg := config.DefaultConfig()
	m := NewModel(cfg, WithStartView(ViewHistory), WithStateProvider(mock), WithPersistenceFlag(false))
	m.width = 100
	m.height = 30

	view := m.renderHistory()

	if !strings.Contains(view, "persistence is disabled") {
		t.Errorf("memory-only mode should show 'persistence is disabled', got:\n%s", view)
	}
}
