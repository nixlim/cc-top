package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

func TestMaintenance_AggregateOldData(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	oldDate := time.Now().AddDate(0, 0, -8)
	oldTimestamp := oldDate.Format(time.RFC3339Nano)
	oldDay := oldDate.Format("2006-01-02")

	_, err = store.db.Exec(`
		INSERT INTO sessions (session_id, last_event_at) VALUES (?, ?)
		ON CONFLICT(session_id) DO UPDATE SET last_event_at=excluded.last_event_at
	`, "sess-old", oldTimestamp)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	_, err = store.db.Exec(`
		INSERT INTO metrics (session_id, name, value, timestamp) VALUES (?, ?, ?, ?)
	`, "sess-old", "claude_code.cost.usage", 5.0, oldTimestamp)
	if err != nil {
		t.Fatalf("insert metric: %v", err)
	}

	err = store.runMaintenanceCycle(7, 90)
	if err != nil {
		t.Fatalf("runMaintenanceCycle failed: %v", err)
	}

	var count int
	err = store.db.QueryRow("SELECT COUNT(*) FROM daily_summaries WHERE session_id = ? AND date = ?", "sess-old", oldDay).Scan(&count)
	if err != nil {
		t.Fatalf("query daily_summaries: %v", err)
	}
	if count == 0 {
		t.Error("old data not aggregated into daily_summaries")
	}
}

func TestMaintenance_PruneRawData(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	oldTimestamp := time.Now().AddDate(0, 0, -8).Format(time.RFC3339Nano)
	_, err = store.db.Exec(`
		INSERT INTO sessions (session_id, last_event_at) VALUES (?, ?)
		ON CONFLICT(session_id) DO UPDATE SET last_event_at=excluded.last_event_at
	`, "sess-prune", oldTimestamp)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	_, err = store.db.Exec(
		"INSERT INTO metrics (session_id, name, value, timestamp) VALUES (?, ?, ?, ?)",
		"sess-prune", "test.metric", 1.0, oldTimestamp)
	if err != nil {
		t.Fatalf("insert old metric: %v", err)
	}

	_, err = store.db.Exec(
		"INSERT INTO events (session_id, name, timestamp) VALUES (?, ?, ?)",
		"sess-prune", "test.event", oldTimestamp)
	if err != nil {
		t.Fatalf("insert old event: %v", err)
	}

	recentTimestamp := time.Now().Format(time.RFC3339Nano)
	_, err = store.db.Exec(
		"INSERT INTO metrics (session_id, name, value, timestamp) VALUES (?, ?, ?, ?)",
		"sess-prune", "test.metric", 2.0, recentTimestamp)
	if err != nil {
		t.Fatalf("insert recent metric: %v", err)
	}

	err = store.runMaintenanceCycle(7, 90)
	if err != nil {
		t.Fatalf("runMaintenanceCycle failed: %v", err)
	}

	var oldCount int
	err = store.db.QueryRow("SELECT COUNT(*) FROM metrics WHERE session_id = ? AND datetime(timestamp) < datetime('now', '-7 days')", "sess-prune").Scan(&oldCount)
	if err != nil {
		t.Fatalf("query old metrics: %v", err)
	}
	if oldCount != 0 {
		t.Errorf("old metrics not pruned: got %d rows", oldCount)
	}

	var oldEventCount int
	err = store.db.QueryRow("SELECT COUNT(*) FROM events WHERE session_id = ? AND datetime(timestamp) < datetime('now', '-7 days')", "sess-prune").Scan(&oldEventCount)
	if err != nil {
		t.Fatalf("query old events: %v", err)
	}
	if oldEventCount != 0 {
		t.Errorf("old events not pruned: got %d rows", oldEventCount)
	}

	var recentCount int
	err = store.db.QueryRow("SELECT COUNT(*) FROM metrics WHERE session_id = ? AND datetime(timestamp) >= datetime('now', '-7 days')", "sess-prune").Scan(&recentCount)
	if err != nil {
		t.Fatalf("query recent metrics: %v", err)
	}
	if recentCount == 0 {
		t.Error("recent metrics were incorrectly pruned")
	}

	_ = store.Close()
}

func TestMaintenance_PruneOldSummaries(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	oldDate := time.Now().AddDate(0, 0, -91).Format("2006-01-02")
	_, err = store.db.Exec(
		"INSERT INTO daily_summaries (session_id, date, total_cost) VALUES (?, ?, ?)",
		"sess-summary", oldDate, 10.0)
	if err != nil {
		t.Fatalf("insert old summary: %v", err)
	}

	recentDate := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	_, err = store.db.Exec(
		"INSERT INTO daily_summaries (session_id, date, total_cost) VALUES (?, ?, ?)",
		"sess-summary", recentDate, 5.0)
	if err != nil {
		t.Fatalf("insert recent summary: %v", err)
	}

	err = store.runMaintenanceCycle(7, 90)
	if err != nil {
		t.Fatalf("runMaintenanceCycle failed: %v", err)
	}

	var oldCount int
	err = store.db.QueryRow("SELECT COUNT(*) FROM daily_summaries WHERE date = ?", oldDate).Scan(&oldCount)
	if err != nil {
		t.Fatalf("query old summaries: %v", err)
	}
	if oldCount != 0 {
		t.Error("old summaries not pruned")
	}

	var recentCount int
	err = store.db.QueryRow("SELECT COUNT(*) FROM daily_summaries WHERE date = ?", recentDate).Scan(&recentCount)
	if err != nil {
		t.Fatalf("query recent summaries: %v", err)
	}
	if recentCount == 0 {
		t.Error("recent summary was incorrectly pruned")
	}

	_ = store.Close()
}

func TestMaintenance_NoDataToAggregate(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	err = store.runMaintenanceCycle(7, 90)
	if err != nil {
		t.Errorf("runMaintenanceCycle on empty DB should not error: %v", err)
	}

	_ = store.Close()
}

func TestMaintenance_FailureRetried(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	_ = store.db.Close()

	err = store.runMaintenanceCycle(7, 90)
	if err == nil {
		t.Error("expected error from maintenance with closed DB")
	}

	m := state.Metric{
		Name:      "test.metric",
		Value:     1.0,
		Timestamp: time.Now(),
	}
	store.AddMetric("sess-retry", m)

	session := store.GetSession("sess-retry")
	if session == nil {
		t.Error("memory store should still work after maintenance failure")
	}
}
