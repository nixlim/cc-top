package storage

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/alerts"
	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/state"
	"github.com/nixlim/cc-top/internal/stats"
)

func TestFullLifecycle_IngestShutdownRestart(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "lifecycle.db")

	store1, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore (phase 1) failed: %v", err)
	}

	store1.AddMetric("sess-A", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     3.50,
		Timestamp: time.Now(),
	})
	store1.AddMetric("sess-A", state.Metric{
		Name:      "claude_code.token.usage",
		Value:     15000,
		Timestamp: time.Now(),
	})
	store1.AddEvent("sess-A", state.Event{
		Name:      "claude_code.api_request",
		Timestamp: time.Now(),
		Attributes: map[string]string{
			"status": "success",
		},
	})
	store1.UpdatePID("sess-A", 11111)

	store1.AddMetric("sess-B", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     1.25,
		Timestamp: time.Now(),
	})
	store1.UpdatePID("sess-B", 22222)

	time.Sleep(200 * time.Millisecond)

	sessions := store1.ListSessions()
	if len(sessions) != 2 {
		t.Fatalf("phase 1: want 2 sessions in memory, got %d", len(sessions))
	}

	if err := store1.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	verifyDB, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB for verification failed: %v", err)
	}
	var summaryCount int
	err = verifyDB.QueryRow("SELECT COUNT(*) FROM daily_summaries").Scan(&summaryCount)
	if err != nil {
		t.Fatalf("query daily_summaries: %v", err)
	}
	if summaryCount == 0 {
		t.Error("Close did not create daily summaries")
	}

	var metricCount int
	err = verifyDB.QueryRow("SELECT COUNT(*) FROM metrics").Scan(&metricCount)
	if err != nil {
		t.Fatalf("query metrics count: %v", err)
	}
	if metricCount < 3 {
		t.Errorf("not all metrics flushed to DB: want >=3, got %d", metricCount)
	}
	_ = verifyDB.Close()

	store2, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore (phase 3) failed: %v", err)
	}
	defer func() { _ = store2.Close() }()

	recoveredSessions := store2.ListSessions()
	if len(recoveredSessions) != 2 {
		t.Fatalf("recovery: want 2 sessions, got %d", len(recoveredSessions))
	}

	sessA := store2.GetSession("sess-A")
	if sessA == nil {
		t.Fatal("session A not recovered")
	}
	if sessA.PID != 11111 {
		t.Errorf("session A PID: want 11111, got %d", sessA.PID)
	}

	sessB := store2.GetSession("sess-B")
	if sessB == nil {
		t.Fatal("session B not recovered")
	}
	if sessB.PID != 22222 {
		t.Errorf("session B PID: want 22222, got %d", sessB.PID)
	}

	if sessA.PreviousValues == nil {
		t.Fatal("session A PreviousValues not restored")
	}
	prevCost, ok := sessA.PreviousValues["claude_code.cost.usage"]
	if !ok {
		t.Error("session A counter state missing for claude_code.cost.usage")
	} else if prevCost != 3.50 {
		t.Errorf("session A counter state: want 3.50, got %f", prevCost)
	}

	store2.AddMetric("sess-A", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     5.00,
		Timestamp: time.Now(),
	})

	sessAUpdated := store2.GetSession("sess-A")
	if sessAUpdated == nil {
		t.Fatal("session A lost after post-recovery AddMetric")
	}
	if sessAUpdated.TotalCost != 5.00 {
		t.Errorf("post-recovery TotalCost: want 5.00 (recovered 3.50 + delta 1.50), got %f", sessAUpdated.TotalCost)
	}
}

func TestFullLifecycle_MaintenanceCycle(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "maintenance.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	now := time.Now()
	db := store.db

	for day := 1; day <= 10; day++ {
		daysAgo := 11 - day
		ts := now.AddDate(0, 0, -daysAgo)
		tsStr := ts.Format(time.RFC3339Nano)
		sessID := fmt.Sprintf("sess-day%d", day)

		_, err := db.Exec(`
			INSERT INTO sessions (session_id, last_event_at)
			VALUES (?, ?)
			ON CONFLICT(session_id) DO UPDATE SET last_event_at=excluded.last_event_at
		`, sessID, tsStr)
		if err != nil {
			t.Fatalf("insert session for day %d: %v", day, err)
		}

		for j := 0; j < 3; j++ {
			_, err = db.Exec(
				"INSERT INTO metrics (session_id, name, value, timestamp) VALUES (?, ?, ?, ?)",
				sessID, "claude_code.cost.usage", float64(day)*0.5+float64(j)*0.1, tsStr)
			if err != nil {
				t.Fatalf("insert metric for day %d: %v", day, err)
			}
		}

		_, err = db.Exec(
			"INSERT INTO events (session_id, name, timestamp) VALUES (?, ?, ?)",
			sessID, "claude_code.api_request", tsStr)
		if err != nil {
			t.Fatalf("insert event for day %d: %v", day, err)
		}
	}

	var totalMetrics int
	err = db.QueryRow("SELECT COUNT(*) FROM metrics").Scan(&totalMetrics)
	if err != nil {
		t.Fatalf("count metrics: %v", err)
	}
	if totalMetrics != 30 {
		t.Fatalf("seeded metrics: want 30, got %d", totalMetrics)
	}

	err = store.runMaintenanceCycle(7, 90)
	if err != nil {
		t.Fatalf("runMaintenanceCycle failed: %v", err)
	}

	for day := 1; day <= 3; day++ {
		daysAgo := 11 - day
		dateStr := now.AddDate(0, 0, -daysAgo).Format("2006-01-02")
		sessID := fmt.Sprintf("sess-day%d", day)

		var count int
		err = db.QueryRow(
			"SELECT COUNT(*) FROM daily_summaries WHERE session_id = ? AND date = ?",
			sessID, dateStr).Scan(&count)
		if err != nil {
			t.Fatalf("query summary for day %d: %v", day, err)
		}
		if count == 0 {
			t.Errorf("daily summary missing for day %d (date %s)", day, dateStr)
		}
	}

	var oldMetricCount int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM metrics WHERE datetime(timestamp) < datetime('now', '-7 days')").
		Scan(&oldMetricCount)
	if err != nil {
		t.Fatalf("count old metrics: %v", err)
	}
	if oldMetricCount != 0 {
		t.Errorf("old raw metrics not pruned: got %d rows", oldMetricCount)
	}

	var oldEventCount int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM events WHERE datetime(timestamp) < datetime('now', '-7 days')").
		Scan(&oldEventCount)
	if err != nil {
		t.Fatalf("count old events: %v", err)
	}
	if oldEventCount != 0 {
		t.Errorf("old raw events not pruned: got %d rows", oldEventCount)
	}

	var recentMetricCount int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM metrics WHERE datetime(timestamp) >= datetime('now', '-7 days')").
		Scan(&recentMetricCount)
	if err != nil {
		t.Fatalf("count recent metrics: %v", err)
	}
	if recentMetricCount == 0 {
		t.Error("recent metrics were incorrectly pruned")
	}

	_ = store.Close()
}

// TestFullLifecycle_PersistAndRecover verifies that daily stats, burn rate snapshots,
// and alert history survive a close/reopen cycle and are queryable (spec test #53).
func TestFullLifecycle_PersistAndRecover(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "persist-recover.db")

	// Phase 1: Create store and write data.
	store1, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore (phase 1): %v", err)
	}

	today := time.Now().Format("2006-01-02")

	// Write daily stats via the public API.
	store1.WriteDailyStats(today, stats.DashboardStats{
		LinesAdded:   42,
		LinesRemoved: 10,
		Commits:      3,
		PRs:          1,
		CacheEfficiency: 0.85,
		AvgAPILatency:   1.2,
		ErrorRate:        0.02,
		RetryRate:        0.01,
		CacheSavingsUSD:  0.55,
		ModelBreakdown: []stats.ModelStats{
			{Model: "claude-opus-4", TotalCost: 5.00, TotalTokens: 50000},
		},
		TopTools: []stats.ToolUsage{
			{ToolName: "Read", Count: 20},
		},
		ErrorCategories:   map[string]int{"rate_limit": 2},
		LanguageBreakdown: map[string]int{"go": 15},
		DecisionSources:   map[string]int{"user": 5},
		MCPToolUsage:      map[string]int{"pal:chat": 3},
		LatencyPercentiles: stats.LatencyPercentiles{
			P50: 0.8,
			P95: 2.5,
			P99: 4.0,
		},
		TokenBreakdown: map[string]int64{
			"input":         30000,
			"output":        15000,
			"cacheRead":     5000,
			"cacheCreation": 2000,
		},
	})

	// Write burn rate snapshot.
	store1.WriteBurnRateSnapshot(burnrate.BurnRate{
		TotalCost:         12.50,
		HourlyRate:        2.10,
		Trend:             burnrate.TrendUp,
		TokenVelocity:     500.0,
		DailyProjection:   50.40,
		MonthlyProjection: 1512.00,
		PerModel: []burnrate.ModelBurnRate{
			{Model: "claude-opus-4", HourlyRate: 2.10, TotalCost: 12.50},
		},
	})

	// Write alert history.
	store1.PersistAlert(alerts.Alert{
		Rule:      "CostSurge",
		Severity:  "critical",
		Message:   "Cost exceeded $10/hr",
		SessionID: "sess-test-1",
		FiredAt:   time.Now(),
	})

	// Write some raw metrics/events to verify those survive too.
	store1.AddMetric("sess-test-1", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     7.25,
		Timestamp: time.Now(),
	})
	store1.AddEvent("sess-test-1", state.Event{
		Name:      "claude_code.api_request",
		Timestamp: time.Now(),
		Attributes: map[string]string{
			"model": "claude-opus-4",
		},
	})
	store1.UpdatePID("sess-test-1", 99999)

	// Allow async writes to flush.
	time.Sleep(300 * time.Millisecond)

	// Close store (triggers final flush + daily summaries).
	if err := store1.Close(); err != nil {
		t.Fatalf("Close (phase 1): %v", err)
	}

	// Phase 2: Reopen and verify all data is queryable.
	store2, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore (phase 2): %v", err)
	}
	defer func() { _ = store2.Close() }()

	// Verify daily stats query.
	dailyStats := store2.QueryDailyStats(7)
	if len(dailyStats) == 0 {
		t.Fatal("QueryDailyStats returned no rows after reopen")
	}
	found := false
	for _, ds := range dailyStats {
		if ds.Date == today {
			found = true
			if ds.LinesAdded != 42 {
				t.Errorf("LinesAdded: want 42, got %d", ds.LinesAdded)
			}
			if ds.Commits != 3 {
				t.Errorf("Commits: want 3, got %d", ds.Commits)
			}
			if ds.PRsOpened != 1 {
				t.Errorf("PRsOpened: want 1, got %d", ds.PRsOpened)
			}
			if ds.ModelBreakdown == "" {
				t.Error("ModelBreakdown JSON is empty")
			}
			if ds.TopTools == "" {
				t.Error("TopTools JSON is empty")
			}
			if ds.ErrorCategories == "" {
				t.Error("ErrorCategories JSON is empty")
			}
			break
		}
	}
	if !found {
		t.Errorf("QueryDailyStats did not return today's date %s", today)
	}

	// Verify burn rate daily summary.
	brSummary := store2.QueryBurnRateDailySummary(7)
	if len(brSummary) == 0 {
		t.Fatal("QueryBurnRateDailySummary returned no rows after reopen")
	}
	brFound := false
	for _, bs := range brSummary {
		if bs.Date == today {
			brFound = true
			if bs.AvgHourlyRate < 2.0 {
				t.Errorf("AvgHourlyRate: want >=2.0, got %f", bs.AvgHourlyRate)
			}
			break
		}
	}
	if !brFound {
		t.Errorf("QueryBurnRateDailySummary did not return today's date %s", today)
	}

	// Verify burn rate snapshots for today.
	brSnapshots := store2.QueryBurnRateSnapshotsForDate(today)
	if len(brSnapshots) == 0 {
		t.Fatal("QueryBurnRateSnapshotsForDate returned no rows after reopen")
	}
	snap := brSnapshots[0]
	if snap.TotalCost != 12.50 {
		t.Errorf("snapshot TotalCost: want 12.50, got %f", snap.TotalCost)
	}
	if snap.Trend != int(burnrate.TrendUp) {
		t.Errorf("snapshot Trend: want %d (TrendUp), got %d", int(burnrate.TrendUp), snap.Trend)
	}
	if snap.PerModel == "" {
		t.Error("snapshot PerModel JSON is empty")
	}

	// Verify alert history.
	alertRows := store2.QueryAlertHistory(7, "")
	if len(alertRows) == 0 {
		t.Fatal("QueryAlertHistory returned no rows after reopen")
	}
	alertFound := false
	for _, a := range alertRows {
		if a.Rule == "CostSurge" && a.SessionID == "sess-test-1" {
			alertFound = true
			if a.Severity != "critical" {
				t.Errorf("alert Severity: want critical, got %s", a.Severity)
			}
			if a.Message != "Cost exceeded $10/hr" {
				t.Errorf("alert Message: want 'Cost exceeded $10/hr', got %q", a.Message)
			}
			break
		}
	}
	if !alertFound {
		t.Error("QueryAlertHistory did not return the CostSurge alert")
	}

	// Verify alert history filter works.
	filteredAlerts := store2.QueryAlertHistory(7, "CostSurge")
	if len(filteredAlerts) != 1 {
		t.Errorf("filtered alert count: want 1, got %d", len(filteredAlerts))
	}
	noMatch := store2.QueryAlertHistory(7, "NonExistentRule")
	if len(noMatch) != 0 {
		t.Errorf("non-matching filter: want 0, got %d", len(noMatch))
	}

	// Verify distinct alert rules.
	rules := store2.QueryDistinctAlertRules()
	if len(rules) == 0 {
		t.Error("QueryDistinctAlertRules returned no rules")
	}

	// Verify session recovery.
	sess := store2.GetSession("sess-test-1")
	if sess == nil {
		t.Fatal("session sess-test-1 not recovered after reopen")
	}
	if sess.PID != 99999 {
		t.Errorf("recovered PID: want 99999, got %d", sess.PID)
	}
}
