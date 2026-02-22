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

func TestRetentionPruning_AllNewTables(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	// Insert old burn_rate_snapshots (>7 days)
	oldBurnTS := time.Now().AddDate(0, 0, -8).UTC().Format(time.RFC3339)
	_, err = store.db.Exec(
		"INSERT INTO burn_rate_snapshots (timestamp, total_cost, hourly_rate) VALUES (?, ?, ?)",
		oldBurnTS, 5.0, 1.0)
	if err != nil {
		t.Fatalf("insert old burn rate: %v", err)
	}

	// Insert recent burn_rate_snapshots (<7 days)
	recentBurnTS := time.Now().AddDate(0, 0, -3).UTC().Format(time.RFC3339)
	_, err = store.db.Exec(
		"INSERT INTO burn_rate_snapshots (timestamp, total_cost, hourly_rate) VALUES (?, ?, ?)",
		recentBurnTS, 3.0, 0.5)
	if err != nil {
		t.Fatalf("insert recent burn rate: %v", err)
	}

	// Insert old daily_stats (>90 days)
	oldStatsDate := time.Now().AddDate(0, 0, -91).Format("2006-01-02")
	_, err = store.db.Exec(
		"INSERT INTO daily_stats (date, total_cost) VALUES (?, ?)",
		oldStatsDate, 10.0)
	if err != nil {
		t.Fatalf("insert old daily stats: %v", err)
	}

	// Insert recent daily_stats (<90 days)
	recentStatsDate := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	_, err = store.db.Exec(
		"INSERT INTO daily_stats (date, total_cost) VALUES (?, ?)",
		recentStatsDate, 5.0)
	if err != nil {
		t.Fatalf("insert recent daily stats: %v", err)
	}

	// Insert old alert_history (>90 days)
	oldAlertTS := time.Now().AddDate(0, 0, -91).UTC().Format(time.RFC3339)
	_, err = store.db.Exec(
		"INSERT INTO alert_history (rule, severity, message, fired_at) VALUES (?, ?, ?, ?)",
		"CostSurge", "critical", "old alert", oldAlertTS)
	if err != nil {
		t.Fatalf("insert old alert: %v", err)
	}

	// Insert recent alert_history (<90 days)
	recentAlertTS := time.Now().AddDate(0, 0, -30).UTC().Format(time.RFC3339)
	_, err = store.db.Exec(
		"INSERT INTO alert_history (rule, severity, message, fired_at) VALUES (?, ?, ?, ?)",
		"ErrorStorm", "warning", "recent alert", recentAlertTS)
	if err != nil {
		t.Fatalf("insert recent alert: %v", err)
	}

	// Run maintenance
	err = store.runMaintenanceCycle(7, 90)
	if err != nil {
		t.Fatalf("runMaintenanceCycle failed: %v", err)
	}

	// Verify old burn_rate_snapshots pruned
	var oldBurnCount int
	err = store.db.QueryRow("SELECT COUNT(*) FROM burn_rate_snapshots WHERE timestamp = ?", oldBurnTS).Scan(&oldBurnCount)
	if err != nil {
		t.Fatalf("query old burn rate: %v", err)
	}
	if oldBurnCount != 0 {
		t.Error("old burn_rate_snapshots not pruned")
	}

	// Verify recent burn_rate_snapshots preserved
	var recentBurnCount int
	err = store.db.QueryRow("SELECT COUNT(*) FROM burn_rate_snapshots WHERE timestamp = ?", recentBurnTS).Scan(&recentBurnCount)
	if err != nil {
		t.Fatalf("query recent burn rate: %v", err)
	}
	if recentBurnCount != 1 {
		t.Error("recent burn_rate_snapshots incorrectly pruned")
	}

	// Verify old daily_stats pruned
	var oldStatsCount int
	err = store.db.QueryRow("SELECT COUNT(*) FROM daily_stats WHERE date = ?", oldStatsDate).Scan(&oldStatsCount)
	if err != nil {
		t.Fatalf("query old daily stats: %v", err)
	}
	if oldStatsCount != 0 {
		t.Error("old daily_stats not pruned")
	}

	// Verify recent daily_stats preserved
	var recentStatsCount int
	err = store.db.QueryRow("SELECT COUNT(*) FROM daily_stats WHERE date = ?", recentStatsDate).Scan(&recentStatsCount)
	if err != nil {
		t.Fatalf("query recent daily stats: %v", err)
	}
	if recentStatsCount != 1 {
		t.Error("recent daily_stats incorrectly pruned")
	}

	// Verify old alert_history pruned
	var oldAlertCount int
	err = store.db.QueryRow("SELECT COUNT(*) FROM alert_history WHERE fired_at = ?", oldAlertTS).Scan(&oldAlertCount)
	if err != nil {
		t.Fatalf("query old alert: %v", err)
	}
	if oldAlertCount != 0 {
		t.Error("old alert_history not pruned")
	}

	// Verify recent alert_history preserved
	var recentAlertCount int
	err = store.db.QueryRow("SELECT COUNT(*) FROM alert_history WHERE fired_at = ?", recentAlertTS).Scan(&recentAlertCount)
	if err != nil {
		t.Fatalf("query recent alert: %v", err)
	}
	if recentAlertCount != 1 {
		t.Error("recent alert_history incorrectly pruned")
	}

	_ = store.Close()
}

func TestMaintenanceCycle_PreservesExistingBehavior(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	// Insert old metrics and events data
	oldTimestamp := time.Now().AddDate(0, 0, -8).Format(time.RFC3339Nano)
	_, _ = store.db.Exec("INSERT INTO sessions (session_id) VALUES (?)", "sess-existing")
	_, _ = store.db.Exec(
		"INSERT INTO metrics (session_id, name, value, timestamp) VALUES (?, ?, ?, ?)",
		"sess-existing", "claude_code.cost.usage", 1.0, oldTimestamp)
	_, _ = store.db.Exec(
		"INSERT INTO events (session_id, name, timestamp) VALUES (?, ?, ?)",
		"sess-existing", "claude_code.api_request", oldTimestamp)

	// Insert recent data
	recentTimestamp := time.Now().Format(time.RFC3339Nano)
	_, _ = store.db.Exec(
		"INSERT INTO metrics (session_id, name, value, timestamp) VALUES (?, ?, ?, ?)",
		"sess-existing", "claude_code.cost.usage", 2.0, recentTimestamp)

	err = store.runMaintenanceCycle(7, 90)
	if err != nil {
		t.Fatalf("runMaintenanceCycle failed: %v", err)
	}

	// Old metrics should be pruned
	var oldMetrics int
	_ = store.db.QueryRow(
		"SELECT COUNT(*) FROM metrics WHERE datetime(timestamp) < datetime('now', '-7 days')").Scan(&oldMetrics)
	if oldMetrics != 0 {
		t.Error("old metrics not pruned (existing behavior broken)")
	}

	// Recent metrics should be preserved
	var recentMetrics int
	_ = store.db.QueryRow(
		"SELECT COUNT(*) FROM metrics WHERE datetime(timestamp) >= datetime('now', '-7 days')").Scan(&recentMetrics)
	if recentMetrics == 0 {
		t.Error("recent metrics incorrectly pruned (existing behavior broken)")
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
