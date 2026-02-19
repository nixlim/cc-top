package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

func TestSQLiteStore_Close_FlushesWrites(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	for range 10 {
		m := state.Metric{
			Name:      "test.metric",
			Value:     1.0,
			Timestamp: time.Now(),
		}
		store.AddMetric("sess-flush", m)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM metrics WHERE session_id = ?", "sess-flush").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 10 {
		t.Errorf("not all writes flushed: want 10, got %d", count)
	}
}

func TestSQLiteStore_Close_RunsAggregation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	m := state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     2.5,
		Timestamp: time.Now(),
	}
	store.AddMetric("sess-agg", m)

	time.Sleep(150 * time.Millisecond)

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM daily_summaries WHERE session_id = ?", "sess-agg").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count == 0 {
		t.Error("daily aggregation not run on Close — no daily_summaries row found")
	}
}

func TestSQLiteStore_Close_EmptyChannel(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	start := time.Now()
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("empty channel close too slow: %v (want <2s)", elapsed)
	}
}

func TestSQLiteStore_AddMetric_AfterClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("AddMetric after Close panicked: %v", r)
		}
	}()

	m := state.Metric{
		Name:      "test.metric",
		Value:     1.0,
		Timestamp: time.Now(),
	}
	store.AddMetric("sess-post-close", m)

	session := store.GetSession("sess-post-close")
	if session == nil {
		t.Error("session not in memory after post-close AddMetric")
	}
}

func TestSQLiteStore_Close_TimesOutDrain(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := newSQLiteStoreWithChannelSize(dbPath, 5, 7, 90)
	if err != nil {
		t.Fatalf("newSQLiteStoreWithChannelSize failed: %v", err)
	}

	for range 3 {
		m := state.Metric{
			Name:      "test.metric",
			Value:     1.0,
			Timestamp: time.Now(),
		}
		store.AddMetric("sess-timeout", m)
	}

	start := time.Now()
	err = store.Close()
	elapsed := time.Since(start)

	if err != nil {
		t.Logf("Close returned error (may be expected): %v", err)
	}

	if elapsed > 15*time.Second {
		t.Errorf("Close took too long: %v (drain timeout should be 10s max)", elapsed)
	}
}
