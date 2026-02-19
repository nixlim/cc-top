package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

func (s *SQLiteStore) writeMetric(tx *sql.Tx, sessionID string, m state.Metric) error {
	var attributesJSON string
	if len(m.Attributes) > 0 {
		bytes, err := json.Marshal(m.Attributes)
		if err != nil {
			return fmt.Errorf("marshaling attributes: %w", err)
		}
		attributesJSON = string(bytes)
	}

	_, err := tx.Exec(
		"INSERT INTO metrics (session_id, name, value, timestamp, attributes) VALUES (?, ?, ?, ?, ?)",
		sessionID, m.Name, m.Value, m.Timestamp.Format(time.RFC3339Nano), attributesJSON,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO sessions (session_id, last_event_at) VALUES (?, ?)
		ON CONFLICT(session_id) DO UPDATE SET last_event_at=excluded.last_event_at
	`, sessionID, m.Timestamp.Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) writeEvent(tx *sql.Tx, sessionID string, e state.Event) error {
	var attributesJSON string
	if len(e.Attributes) > 0 {
		bytes, err := json.Marshal(e.Attributes)
		if err != nil {
			return fmt.Errorf("marshaling attributes: %w", err)
		}
		attributesJSON = string(bytes)
	}

	_, err := tx.Exec(
		"INSERT INTO events (session_id, name, timestamp, sequence, attributes) VALUES (?, ?, ?, ?, ?)",
		sessionID, e.Name, e.Timestamp.Format(time.RFC3339Nano), e.Sequence, attributesJSON,
	)
	return err
}

func (s *SQLiteStore) writePID(tx *sql.Tx, sessionID string, pid int) error {
	_, err := tx.Exec(`
		INSERT INTO sessions (session_id, pid) VALUES (?, ?)
		ON CONFLICT(session_id) DO UPDATE SET pid=excluded.pid
	`, sessionID, pid)
	return err
}

func (s *SQLiteStore) writeMetadata(tx *sql.Tx, sessionID string, meta state.SessionMetadata) error {
	_, err := tx.Exec(`
		INSERT INTO sessions (session_id, service_version, os_type, os_version, host_arch)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			service_version=excluded.service_version,
			os_type=excluded.os_type,
			os_version=excluded.os_version,
			host_arch=excluded.host_arch
	`, sessionID, meta.ServiceVersion, meta.OSType, meta.OSVersion, meta.HostArch)
	return err
}

func (s *SQLiteStore) writeExited(tx *sql.Tx, pid int) error {
	_, err := tx.Exec("UPDATE sessions SET exited = 1 WHERE pid = ?", pid)
	return err
}

func (s *SQLiteStore) writeCounterState(tx *sql.Tx, sessionID string, key string, value float64) error {
	_, err := tx.Exec(`
		INSERT INTO counter_state (session_id, metric_key, value) VALUES (?, ?, ?)
		ON CONFLICT(session_id, metric_key) DO UPDATE SET value=excluded.value
	`, sessionID, key, value)
	return err
}

func (s *SQLiteStore) writeSessionSnapshot(tx *sql.Tx, sessionID string, snap *sessionSnapshot) error {
	fastMode := 0
	if snap.FastMode {
		fastMode = 1
	}

	_, err := tx.Exec(`
		INSERT INTO sessions (session_id, model, terminal, cwd, total_cost, total_tokens,
			cache_read_tokens, cache_creation_tokens, active_time_seconds,
			started_at, fast_mode, org_id, user_uuid)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			model=COALESCE(NULLIF(excluded.model, ''), sessions.model),
			terminal=COALESCE(NULLIF(excluded.terminal, ''), sessions.terminal),
			cwd=COALESCE(NULLIF(excluded.cwd, ''), sessions.cwd),
			total_cost=excluded.total_cost,
			total_tokens=excluded.total_tokens,
			cache_read_tokens=excluded.cache_read_tokens,
			cache_creation_tokens=excluded.cache_creation_tokens,
			active_time_seconds=excluded.active_time_seconds,
			started_at=COALESCE(NULLIF(excluded.started_at, ''), sessions.started_at),
			fast_mode=excluded.fast_mode,
			org_id=COALESCE(NULLIF(excluded.org_id, ''), sessions.org_id),
			user_uuid=COALESCE(NULLIF(excluded.user_uuid, ''), sessions.user_uuid)
	`, sessionID, snap.Model, snap.Terminal, snap.CWD,
		snap.TotalCost, snap.TotalTokens,
		snap.CacheReadTokens, snap.CacheCreationTokens, snap.ActiveTimeSeconds,
		snap.StartedAt, fastMode, snap.OrgID, snap.UserUUID)
	return err
}
