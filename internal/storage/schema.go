package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const currentSchemaVersion = 1

func OpenDB(dbPath string) (*sql.DB, error) {
	parentDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return nil, fmt.Errorf("creating parent directories: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	if err := migrateSchema(db, dbPath); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func migrateSchema(db *sql.DB, dbPath string) error {
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version'").Scan(&tableName)

	var currentVersion int
	if err == sql.ErrNoRows {
		currentVersion = 0
	} else if err != nil {
		return fmt.Errorf("checking schema_version table: %w", err)
	} else {
		err = db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&currentVersion)
		if err == sql.ErrNoRows {
			currentVersion = 0
		} else if err != nil {
			return fmt.Errorf("reading schema version: %w", err)
		}
	}

	if currentVersion > currentSchemaVersion {
		return fmt.Errorf(
			"database schema version %d is newer than this cc-top version supports (max: %d); upgrade cc-top or delete %s to start fresh",
			currentVersion, currentSchemaVersion, dbPath,
		)
	}

	if currentVersion < currentSchemaVersion {
		if err := applyMigrations(db, currentVersion); err != nil {
			return fmt.Errorf("applying migrations: %w", err)
		}
	}

	return nil
}

func applyMigrations(db *sql.DB, fromVersion int) error {
	if fromVersion == 0 {
		if err := migrateV0ToV1(db); err != nil {
			return fmt.Errorf("migration v0→v1: %w", err)
		}
	}

	return nil
}

func migrateV0ToV1(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("creating schema_version table: %w", err)
	}

	_, err = tx.Exec("INSERT INTO schema_version (version) VALUES (1)")
	if err != nil {
		return fmt.Errorf("inserting schema version: %w", err)
	}

	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			session_id TEXT PRIMARY KEY,
			pid INTEGER,
			terminal TEXT,
			cwd TEXT,
			model TEXT,
			total_cost REAL,
			total_tokens INTEGER,
			cache_read_tokens INTEGER,
			cache_creation_tokens INTEGER,
			active_time_seconds REAL,
			started_at TEXT,
			last_event_at TEXT,
			exited INTEGER,
			fast_mode INTEGER,
			org_id TEXT,
			user_uuid TEXT,
			service_version TEXT,
			os_type TEXT,
			os_version TEXT,
			host_arch TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("creating sessions table: %w", err)
	}

	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			name TEXT NOT NULL,
			value REAL NOT NULL,
			timestamp TEXT NOT NULL,
			attributes TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("creating metrics table: %w", err)
	}

	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			name TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			sequence INTEGER,
			attributes TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("creating events table: %w", err)
	}

	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS counter_state (
			session_id TEXT NOT NULL,
			metric_key TEXT NOT NULL,
			value REAL NOT NULL,
			PRIMARY KEY (session_id, metric_key)
		)
	`)
	if err != nil {
		return fmt.Errorf("creating counter_state table: %w", err)
	}

	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS daily_summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			date TEXT NOT NULL,
			total_cost REAL,
			total_tokens INTEGER,
			api_requests INTEGER,
			api_errors INTEGER,
			active_seconds REAL,
			UNIQUE(session_id, date)
		)
	`)
	if err != nil {
		return fmt.Errorf("creating daily_summaries table: %w", err)
	}

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_metrics_session ON metrics(session_id)")
	if err != nil {
		return fmt.Errorf("creating idx_metrics_session: %w", err)
	}

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_metrics_name ON metrics(name)")
	if err != nil {
		return fmt.Errorf("creating idx_metrics_name: %w", err)
	}

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_metrics_ts ON metrics(timestamp)")
	if err != nil {
		return fmt.Errorf("creating idx_metrics_ts: %w", err)
	}

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id)")
	if err != nil {
		return fmt.Errorf("creating idx_events_session: %w", err)
	}

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_events_name ON events(name)")
	if err != nil {
		return fmt.Errorf("creating idx_events_name: %w", err)
	}

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_events_ts ON events(timestamp)")
	if err != nil {
		return fmt.Errorf("creating idx_events_ts: %w", err)
	}

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_daily_date ON daily_summaries(date)")
	if err != nil {
		return fmt.Errorf("creating idx_daily_date: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}
