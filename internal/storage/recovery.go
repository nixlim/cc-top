package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

func (s *SQLiteStore) recoverSessions() error {
	rows, err := s.db.Query(`
		SELECT session_id, pid, terminal, cwd, model, total_cost, total_tokens,
		       cache_read_tokens, cache_creation_tokens, active_time_seconds,
		       started_at, last_event_at, exited, fast_mode, org_id, user_uuid,
		       service_version, os_type, os_version, host_arch
		FROM sessions
		WHERE datetime(last_event_at) > datetime('now', '-24 hours')
	`)
	if err != nil {
		return fmt.Errorf("querying recent sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var failCount int
	for rows.Next() {
		var sessionID string
		var pid sql.NullInt64
		var terminal, cwd, model sql.NullString
		var totalCost, activeTimeSeconds sql.NullFloat64
		var totalTokens, cacheReadTokens, cacheCreationTokens sql.NullInt64
		var startedAt, lastEventAt sql.NullString
		var exited, fastMode sql.NullInt64
		var orgID, userUUID, serviceVersion, osType, osVersion, hostArch sql.NullString

		err := rows.Scan(
			&sessionID, &pid, &terminal, &cwd, &model,
			&totalCost, &totalTokens, &cacheReadTokens, &cacheCreationTokens,
			&activeTimeSeconds, &startedAt, &lastEventAt, &exited, &fastMode,
			&orgID, &userUUID, &serviceVersion, &osType, &osVersion, &hostArch,
		)
		if err != nil {
			failCount++
			log.Printf("ERROR: failed to scan session row: %v", err)
			continue
		}

		session := &state.SessionData{
			SessionID:           sessionID,
			PID:                 int(pid.Int64),
			Terminal:            terminal.String,
			CWD:                 cwd.String,
			Model:               model.String,
			TotalCost:           totalCost.Float64,
			TotalTokens:         totalTokens.Int64,
			CacheReadTokens:     cacheReadTokens.Int64,
			CacheCreationTokens: cacheCreationTokens.Int64,
			ActiveTime:          time.Duration(activeTimeSeconds.Float64 * float64(time.Second)),
			Exited:              exited.Int64 == 1,
			FastMode:            fastMode.Int64 == 1,
			PreviousValues:      make(map[string]float64),
			Metrics:             []state.Metric{},
			Events:              []state.Event{},
		}

		if startedAt.Valid {
			if t, err := time.Parse(time.RFC3339Nano, startedAt.String); err == nil {
				session.StartedAt = t
			}
		}
		if lastEventAt.Valid {
			if t, err := time.Parse(time.RFC3339Nano, lastEventAt.String); err == nil {
				session.LastEventAt = t
			}
		}

		session.Metadata = state.SessionMetadata{
			ServiceVersion: serviceVersion.String,
			OSType:         osType.String,
			OSVersion:      osVersion.String,
			HostArch:       hostArch.String,
		}

		if err := s.recoverCounterState(sessionID, session); err != nil {
			log.Printf("ERROR: failed to recover counter state for %s: %v", sessionID, err)
		}

		if err := s.recoverMetrics(sessionID, session); err != nil {
			log.Printf("ERROR: failed to recover metrics for %s: %v", sessionID, err)
		}

		if err := s.recoverEvents(sessionID, session); err != nil {
			log.Printf("ERROR: failed to recover events for %s: %v", sessionID, err)
		}

		s.RestoreSession(session)
	}

	if failCount > 0 {
		log.Printf("WARNING: %d sessions failed to recover from database", failCount)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating sessions: %w", err)
	}

	return nil
}

func (s *SQLiteStore) recoverCounterState(sessionID string, session *state.SessionData) error {
	rows, err := s.db.Query(`
		SELECT metric_key, value
		FROM counter_state
		WHERE session_id = ?
	`, sessionID)
	if err != nil {
		return fmt.Errorf("querying counter state: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var key string
		var value float64
		if err := rows.Scan(&key, &value); err != nil {
			log.Printf("ERROR: failed to scan counter state row: %v", err)
			continue
		}
		session.PreviousValues[key] = value
	}

	return rows.Err()
}

func (s *SQLiteStore) recoverMetrics(sessionID string, session *state.SessionData) error {
	rows, err := s.db.Query(`
		SELECT name, value, timestamp, attributes
		FROM metrics
		WHERE session_id = ?
		ORDER BY timestamp ASC
	`, sessionID)
	if err != nil {
		return fmt.Errorf("querying metrics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name string
		var value float64
		var timestamp string
		var attributesJSON sql.NullString

		if err := rows.Scan(&name, &value, &timestamp, &attributesJSON); err != nil {
			log.Printf("ERROR: failed to scan metric row: %v", err)
			continue
		}

		metric := state.Metric{
			Name:  name,
			Value: value,
		}

		if t, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
			metric.Timestamp = t
		}

		if attributesJSON.Valid && attributesJSON.String != "" {
			var attrs map[string]string
			if err := json.Unmarshal([]byte(attributesJSON.String), &attrs); err == nil {
				metric.Attributes = attrs
			}
		}

		session.Metrics = append(session.Metrics, metric)
	}

	return rows.Err()
}

func (s *SQLiteStore) recoverEvents(sessionID string, session *state.SessionData) error {
	rows, err := s.db.Query(`
		SELECT name, timestamp, sequence, attributes
		FROM events
		WHERE session_id = ?
		ORDER BY sequence ASC
	`, sessionID)
	if err != nil {
		return fmt.Errorf("querying events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name string
		var timestamp string
		var sequence int64
		var attributesJSON sql.NullString

		if err := rows.Scan(&name, &timestamp, &sequence, &attributesJSON); err != nil {
			log.Printf("ERROR: failed to scan event row: %v", err)
			continue
		}

		event := state.Event{
			Name:     name,
			Sequence: sequence,
		}

		if t, err := time.Parse(time.RFC3339Nano, timestamp); err == nil {
			event.Timestamp = t
		}

		if attributesJSON.Valid && attributesJSON.String != "" {
			var attrs map[string]string
			if err := json.Unmarshal([]byte(attributesJSON.String), &attrs); err == nil {
				event.Attributes = attrs
			}
		}

		session.Events = append(session.Events, event)
	}

	return rows.Err()
}
