package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

func TestSQLiteStore_RecoveryLoadsSessions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store1, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	m := state.Metric{
		Name:      "test.metric",
		Value:     42.0,
		Timestamp: time.Now(),
	}
	store1.AddMetric("sess-001", m)
	store1.UpdatePID("sess-001", 12345)

	time.Sleep(150 * time.Millisecond)
	_ = store1.Close()

	store2, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore (recovery) failed: %v", err)
	}
	defer func() { _ = store2.Close() }()

	session := store2.GetSession("sess-001")
	if session == nil {
		t.Fatal("session not recovered from SQLite")
	}

	if session.PID != 12345 {
		t.Errorf("PID not recovered: want 12345, got %d", session.PID)
	}

	if len(session.Metrics) != 1 {
		t.Errorf("metrics not recovered: want 1, got %d", len(session.Metrics))
	}
}

func TestSQLiteStore_RecoveryExcludesOldSessions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}

	oldTimestamp := time.Now().Add(-25 * time.Hour).Format(time.RFC3339Nano)
	_, err = db.Exec(`
		INSERT INTO sessions (session_id, started_at, last_event_at)
		VALUES (?, ?, ?)
	`, "sess-old", oldTimestamp, oldTimestamp)
	if err != nil {
		t.Fatalf("failed to insert old session: %v", err)
	}

	recentTimestamp := time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano)
	_, err = db.Exec(`
		INSERT INTO sessions (session_id, started_at, last_event_at)
		VALUES (?, ?, ?)
	`, "sess-recent", recentTimestamp, recentTimestamp)
	if err != nil {
		t.Fatalf("failed to insert recent session: %v", err)
	}

	_ = db.Close()

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	oldSession := store.GetSession("sess-old")
	if oldSession != nil {
		t.Error("old session should NOT be recovered (outside 24h window)")
	}

	recentSession := store.GetSession("sess-recent")
	if recentSession == nil {
		t.Error("recent session should be recovered (within 24h window)")
	}
}

func TestSQLiteStore_RecoveryRestoresCounterState(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store1, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	m := state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     1.5,
		Timestamp: time.Now(),
	}
	store1.AddMetric("sess-002", m)

	time.Sleep(150 * time.Millisecond)
	_ = store1.Close()

	store2, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore (recovery) failed: %v", err)
	}
	defer func() { _ = store2.Close() }()

	session := store2.GetSession("sess-002")
	if session == nil {
		t.Fatal("session not recovered")
	}

	if session.PreviousValues == nil {
		t.Fatal("PreviousValues not restored")
	}

	prevCost, ok := session.PreviousValues["claude_code.cost.usage"]
	if !ok {
		t.Error("counter state not restored for claude_code.cost.usage")
	} else if prevCost != 1.5 {
		t.Errorf("counter state value: want 1.5, got %f", prevCost)
	}
}

func TestSQLiteStore_RecoveryEmptyDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore on empty DB failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	sessions := store.ListSessions()
	if len(sessions) != 0 {
		t.Errorf("empty DB should have 0 sessions, got %d", len(sessions))
	}
}

func TestSQLiteStore_RecoveryExitedFlag(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store1, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	m := state.Metric{
		Name:      "test.metric",
		Value:     1.0,
		Timestamp: time.Now(),
	}
	store1.AddMetric("sess-003", m)
	store1.UpdatePID("sess-003", 99999)
	store1.MarkExited(99999)

	time.Sleep(150 * time.Millisecond)
	_ = store1.Close()

	store2, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore (recovery) failed: %v", err)
	}
	defer func() { _ = store2.Close() }()

	session := store2.GetSession("sess-003")
	if session == nil {
		t.Fatal("session not recovered")
	}

	if !session.Exited {
		t.Error("exited flag not preserved on recovery")
	}
}

func TestSQLiteStore_RecoveryCompletesBeforeReturn(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store1, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	m := state.Metric{
		Name:      "test.metric",
		Value:     100.0,
		Timestamp: time.Now(),
	}
	store1.AddMetric("sess-sync", m)

	time.Sleep(150 * time.Millisecond)
	_ = store1.Close()

	store2, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore (recovery) failed: %v", err)
	}
	defer func() { _ = store2.Close() }()

	session := store2.GetSession("sess-sync")
	if session == nil {
		t.Fatal("session not available immediately after NewSQLiteStore (recovery not synchronous)")
	}

	if len(session.Metrics) != 1 {
		t.Errorf("metrics not recovered synchronously: want 1, got %d", len(session.Metrics))
	}
}
