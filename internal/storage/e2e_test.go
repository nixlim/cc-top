package storage

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/state"
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
