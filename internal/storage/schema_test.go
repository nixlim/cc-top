package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestSchema_CreateFresh(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	var version int
	err = db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if err != nil {
		t.Fatalf("failed to read schema_version: %v", err)
	}
	if version != 1 {
		t.Errorf("schema version: want 1, got %d", version)
	}

	tables := []string{"schema_version", "sessions", "metrics", "events", "counter_state", "daily_summaries"}
	for _, tableName := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("table %q not found", tableName)
		} else if err != nil {
			t.Fatalf("error checking table %q: %v", tableName, err)
		}
	}

	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to read journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode: want wal, got %s", journalMode)
	}

	var foreignKeys int
	err = db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys)
	if err != nil {
		t.Fatalf("failed to read foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys: want 1, got %d", foreignKeys)
	}
}

func TestSchema_MigrateV0ToV1(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	emptyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to create empty DB: %v", err)
	}
	_ = emptyDB.Close()

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed during migration: %v", err)
	}
	defer func() { _ = db.Close() }()

	var version int
	err = db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if err != nil {
		t.Fatalf("failed to read schema_version after migration: %v", err)
	}
	if version != 1 {
		t.Errorf("schema version after migration: want 1, got %d", version)
	}

	tables := []string{"sessions", "metrics", "events", "counter_state", "daily_summaries"}
	for _, tableName := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("table %q not found after migration", tableName)
		} else if err != nil {
			t.Fatalf("error checking table %q: %v", tableName, err)
		}
	}
}

func TestSchema_NoMigrationAtCurrentVersion(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db1, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("first OpenDB failed: %v", err)
	}

	_, err = db1.Exec("INSERT INTO sessions (session_id, started_at) VALUES (?, ?)", "test-001", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("failed to insert test row: %v", err)
	}
	_ = db1.Close()

	db2, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("second OpenDB failed: %v", err)
	}
	defer func() { _ = db2.Close() }()

	var version int
	err = db2.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if err != nil {
		t.Fatalf("failed to read schema_version: %v", err)
	}
	if version != 1 {
		t.Errorf("schema version: want 1, got %d", version)
	}

	var sessionID string
	err = db2.QueryRow("SELECT session_id FROM sessions WHERE session_id = ?", "test-001").Scan(&sessionID)
	if err == sql.ErrNoRows {
		t.Error("test row was lost — migration may have re-run destructively")
	} else if err != nil {
		t.Fatalf("error reading test row: %v", err)
	}
}

func TestSchema_CreateParentDirs(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir1", "subdir2", "test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed with nested path: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file was not created")
	}

	parentDir := filepath.Dir(dbPath)
	if _, err := os.Stat(parentDir); os.IsNotExist(err) {
		t.Error("parent directories were not created")
	}
}

func TestSchema_ForwardVersionRejected(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	futureDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to create future DB: %v", err)
	}
	_, err = futureDB.Exec("CREATE TABLE schema_version (version INTEGER)")
	if err != nil {
		t.Fatalf("failed to create schema_version table: %v", err)
	}
	_, err = futureDB.Exec("INSERT INTO schema_version (version) VALUES (999)")
	if err != nil {
		t.Fatalf("failed to insert future version: %v", err)
	}
	_ = futureDB.Close()

	_, err = OpenDB(dbPath)
	if err == nil {
		t.Fatal("OpenDB should have failed for forward schema version, but succeeded")
	}

	errMsg := err.Error()
	expectedPhrases := []string{"999", "newer", "upgrade cc-top", dbPath}
	for _, phrase := range expectedPhrases {
		if !contains(errMsg, phrase) {
			t.Errorf("error message missing %q: %s", phrase, errMsg)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
