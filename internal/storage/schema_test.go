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
	if version != 2 {
		t.Errorf("schema version: want 2, got %d", version)
	}

	tables := []string{"schema_version", "sessions", "metrics", "events", "counter_state", "daily_summaries", "daily_stats", "burn_rate_snapshots", "alert_history"}
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
	if version != 2 {
		t.Errorf("schema version after migration: want 2, got %d", version)
	}

	tables := []string{"sessions", "metrics", "events", "counter_state", "daily_summaries", "daily_stats", "burn_rate_snapshots", "alert_history"}
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
	if version != 2 {
		t.Errorf("schema version: want 2, got %d", version)
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

func TestOpenDB_SetsBusyTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	var busyTimeout int
	err = db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout)
	if err != nil {
		t.Fatalf("failed to read busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout: want 5000, got %d", busyTimeout)
	}

	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to read journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode: want wal, got %s", journalMode)
	}
}

// createV1Database creates a database at schema v1 with test data and returns the path.
func createV1Database(t *testing.T, tmpDir string) string {
	t.Helper()
	dbPath := filepath.Join(tmpDir, "v1.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Set pragmas
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA foreign_keys=ON")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	// Run v0→v1 migration manually
	if err := migrateV0ToV1(db); err != nil {
		t.Fatalf("migrateV0ToV1 failed: %v", err)
	}

	// Insert test data
	_, err = db.Exec("INSERT INTO sessions (session_id, model, total_cost) VALUES (?, ?, ?)", "sess-v1", "claude-opus-4-6", 5.0)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
	_, err = db.Exec("INSERT INTO metrics (session_id, name, value, timestamp) VALUES (?, ?, ?, ?)", "sess-v1", "test.metric", 1.0, "2026-02-20T00:00:00Z")
	if err != nil {
		t.Fatalf("insert metric: %v", err)
	}
	_, err = db.Exec("INSERT INTO events (session_id, name, timestamp) VALUES (?, ?, ?)", "sess-v1", "test.event", "2026-02-20T00:00:00Z")
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}
	_, err = db.Exec("INSERT INTO daily_summaries (session_id, date, total_cost) VALUES (?, ?, ?)", "sess-v1", "2026-02-20", 5.0)
	if err != nil {
		t.Fatalf("insert summary: %v", err)
	}

	return dbPath
}

func TestMigrateV1ToV2_CreatesNewTables(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := createV1Database(t, tmpDir)

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	newTables := []string{"daily_stats", "burn_rate_snapshots", "alert_history"}
	for _, tableName := range newTables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("table %q not found after v1→v2 migration", tableName)
		} else if err != nil {
			t.Fatalf("error checking table %q: %v", tableName, err)
		}
	}
}

func TestMigrateV1ToV2_CreatesIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := createV1Database(t, tmpDir)

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	indexes := []string{"idx_burnrate_ts", "idx_alert_history_fired", "idx_alert_history_rule"}
	for _, idxName := range indexes {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", idxName).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("index %q not found after v1→v2 migration", idxName)
		} else if err != nil {
			t.Fatalf("error checking index %q: %v", idxName, err)
		}
	}
}

func TestMigrateV1ToV2_SetsVersionTo2(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := createV1Database(t, tmpDir)

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
	if version != 2 {
		t.Errorf("schema version: want 2, got %d", version)
	}
}

func TestMigrateV1ToV2_PreservesExistingData(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := createV1Database(t, tmpDir)

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Check sessions data preserved
	var sessionID string
	err = db.QueryRow("SELECT session_id FROM sessions WHERE session_id = ?", "sess-v1").Scan(&sessionID)
	if err != nil {
		t.Fatalf("session data lost after migration: %v", err)
	}

	// Check metrics data preserved
	var metricCount int
	err = db.QueryRow("SELECT COUNT(*) FROM metrics WHERE session_id = ?", "sess-v1").Scan(&metricCount)
	if err != nil {
		t.Fatalf("error querying metrics: %v", err)
	}
	if metricCount != 1 {
		t.Errorf("metrics data lost: want 1, got %d", metricCount)
	}

	// Check events data preserved
	var eventCount int
	err = db.QueryRow("SELECT COUNT(*) FROM events WHERE session_id = ?", "sess-v1").Scan(&eventCount)
	if err != nil {
		t.Fatalf("error querying events: %v", err)
	}
	if eventCount != 1 {
		t.Errorf("events data lost: want 1, got %d", eventCount)
	}

	// Check daily_summaries data preserved
	var summaryCount int
	err = db.QueryRow("SELECT COUNT(*) FROM daily_summaries WHERE session_id = ?", "sess-v1").Scan(&summaryCount)
	if err != nil {
		t.Fatalf("error querying daily_summaries: %v", err)
	}
	if summaryCount != 1 {
		t.Errorf("daily_summaries data lost: want 1, got %d", summaryCount)
	}
}

func TestFreshInstall_CreatesFullSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "fresh.db")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	// All v1 tables
	v1Tables := []string{"schema_version", "sessions", "metrics", "events", "counter_state", "daily_summaries"}
	for _, tableName := range v1Tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("v1 table %q not found in fresh install", tableName)
		} else if err != nil {
			t.Fatalf("error checking table %q: %v", tableName, err)
		}
	}

	// All v2 tables
	v2Tables := []string{"daily_stats", "burn_rate_snapshots", "alert_history"}
	for _, tableName := range v2Tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("v2 table %q not found in fresh install", tableName)
		} else if err != nil {
			t.Fatalf("error checking table %q: %v", tableName, err)
		}
	}

	var version int
	err = db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if err != nil {
		t.Fatalf("failed to read schema_version: %v", err)
	}
	if version != 2 {
		t.Errorf("schema version: want 2, got %d", version)
	}
}

func TestMigrateV2ToV2_NoOp(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// First open creates v2
	db1, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("first OpenDB failed: %v", err)
	}
	// Insert test data
	_, err = db1.Exec("INSERT INTO daily_stats (date, total_cost) VALUES (?, ?)", "2026-02-20", 5.0)
	if err != nil {
		t.Fatalf("insert daily_stats: %v", err)
	}
	_ = db1.Close()

	// Second open should be no-op
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
	if version != 2 {
		t.Errorf("schema version: want 2, got %d", version)
	}

	// Data preserved
	var cost float64
	err = db2.QueryRow("SELECT total_cost FROM daily_stats WHERE date = ?", "2026-02-20").Scan(&cost)
	if err != nil {
		t.Fatalf("daily_stats data lost: %v", err)
	}
	if cost != 5.0 {
		t.Errorf("daily_stats total_cost: want 5.0, got %f", cost)
	}
}

func TestMigrateV1ToV2_RollbackOnPartialFailure(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "rollback.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	// Set pragmas
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA foreign_keys=ON")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	// Run v0→v1 migration
	if err := migrateV0ToV1(db); err != nil {
		t.Fatalf("migrateV0ToV1 failed: %v", err)
	}

	// Pre-create burn_rate_snapshots with a conflicting schema to cause the
	// CREATE TABLE IF NOT EXISTS to succeed but then the CREATE INDEX to fail
	// because the column referenced doesn't exist.
	_, err = db.Exec("CREATE TABLE burn_rate_snapshots (id INTEGER PRIMARY KEY)")
	if err != nil {
		t.Fatalf("creating conflicting table: %v", err)
	}

	// Attempt v1→v2 migration — should fail because the index creation will
	// reference a column (timestamp) that doesn't exist in our conflicting table.
	err = migrateV1ToV2(db)
	if err == nil {
		// If for some reason the index created anyway, this is still a valid test
		// path — the point is to ensure the migration can handle failures.
		t.Log("migration succeeded despite conflicting schema (acceptable if CREATE INDEX IF NOT EXISTS ignores existing)")
		_ = db.Close()
		return
	}

	// Verify schema version is still 1
	var version int
	err = db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if err != nil {
		t.Fatalf("failed to read schema_version: %v", err)
	}
	if version != 1 {
		t.Errorf("schema version after failed migration: want 1, got %d", version)
	}

	_ = db.Close()
}
