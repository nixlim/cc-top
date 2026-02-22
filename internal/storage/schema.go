package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const currentSchemaVersion = 2

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

	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting busy_timeout: %w", err)
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
		fromVersion = 1
	}

	if fromVersion == 1 {
		if err := migrateV1ToV2(db); err != nil {
			return fmt.Errorf("migration v1→v2: %w", err)
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

func migrateV1ToV2(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("starting transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS daily_stats (
			date TEXT PRIMARY KEY,
			total_cost REAL DEFAULT 0,
			token_input INTEGER DEFAULT 0,
			token_output INTEGER DEFAULT 0,
			token_cache_read INTEGER DEFAULT 0,
			token_cache_write INTEGER DEFAULT 0,
			session_count INTEGER DEFAULT 0,
			api_requests INTEGER DEFAULT 0,
			api_errors INTEGER DEFAULT 0,
			lines_added INTEGER DEFAULT 0,
			lines_removed INTEGER DEFAULT 0,
			commits INTEGER DEFAULT 0,
			prs_opened INTEGER DEFAULT 0,
			cache_efficiency REAL DEFAULT 0,
			cache_savings_usd REAL DEFAULT 0,
			error_rate REAL DEFAULT 0,
			retry_rate REAL DEFAULT 0,
			avg_api_latency_ms REAL DEFAULT 0,
			latency_p50_ms REAL DEFAULT 0,
			latency_p95_ms REAL DEFAULT 0,
			latency_p99_ms REAL DEFAULT 0,
			model_breakdown TEXT,
			top_tools TEXT,
			error_categories TEXT,
			language_breakdown TEXT,
			decision_sources TEXT,
			mcp_tool_usage TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("creating daily_stats table: %w", err)
	}

	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS burn_rate_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL,
			total_cost REAL DEFAULT 0,
			hourly_rate REAL DEFAULT 0,
			trend INTEGER DEFAULT 0,
			token_velocity REAL DEFAULT 0,
			daily_projection REAL DEFAULT 0,
			monthly_projection REAL DEFAULT 0,
			per_model TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("creating burn_rate_snapshots table: %w", err)
	}

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_burnrate_ts ON burn_rate_snapshots(timestamp)")
	if err != nil {
		return fmt.Errorf("creating idx_burnrate_ts: %w", err)
	}

	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS alert_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			rule TEXT NOT NULL,
			severity TEXT NOT NULL,
			message TEXT NOT NULL,
			session_id TEXT DEFAULT '',
			fired_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("creating alert_history table: %w", err)
	}

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_alert_history_fired ON alert_history(fired_at)")
	if err != nil {
		return fmt.Errorf("creating idx_alert_history_fired: %w", err)
	}

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_alert_history_rule ON alert_history(rule)")
	if err != nil {
		return fmt.Errorf("creating idx_alert_history_rule: %w", err)
	}

	_, err = tx.Exec("UPDATE schema_version SET version = 2")
	if err != nil {
		return fmt.Errorf("updating schema version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}
