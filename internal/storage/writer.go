package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

// dailyStatsRow holds the data for a single daily_stats row.
type dailyStatsRow struct {
	Date             string
	TotalCost        float64
	TokenInput       int64
	TokenOutput      int64
	TokenCacheRead   int64
	TokenCacheWrite  int64
	SessionCount     int
	APIRequests      int
	APIErrors        int
	LinesAdded       int
	LinesRemoved     int
	Commits          int
	PRsOpened        int
	CacheEfficiency  float64
	CacheSavingsUSD  float64
	ErrorRate        float64
	RetryRate        float64
	AvgAPILatencyMs  float64
	LatencyP50Ms     float64
	LatencyP95Ms     float64
	LatencyP99Ms     float64
	ModelBreakdown   interface{} // JSON-marshalable
	TopTools         interface{} // JSON-marshalable
	ErrorCategories  interface{} // JSON-marshalable
	LanguageBreakdown interface{} // JSON-marshalable
	DecisionSources  interface{} // JSON-marshalable
	MCPToolUsage     interface{} // JSON-marshalable
}

// burnRateSnapshotRow holds the data for a single burn_rate_snapshots row.
type burnRateSnapshotRow struct {
	Timestamp         string
	TotalCost         float64
	HourlyRate        float64
	Trend             int
	TokenVelocity     float64
	DailyProjection   float64
	MonthlyProjection float64
	PerModel          interface{} // JSON-marshalable
}

// alertHistoryRow holds the data for a single alert_history row.
type alertHistoryRow struct {
	Rule      string
	Severity  string
	Message   string
	SessionID string
	FiredAt   string
}

// sanitizeFloat replaces NaN and Inf with 0.0.
func sanitizeFloat(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0.0
	}
	return v
}

// marshalJSONColumn marshals v to JSON, returning nil on failure and logging the error.
func marshalJSONColumn(name string, v interface{}) interface{} {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("WARNING: failed to marshal %s JSON: %v", name, err)
		return nil
	}
	if len(data) > 1<<20 {
		log.Printf("WARNING: %s JSON column exceeds 1MB (%d bytes)", name, len(data))
	}
	return string(data)
}

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

func (s *SQLiteStore) writeDailyStats(tx *sql.Tx, row *dailyStatsRow) error {
	_, err := tx.Exec(`
		INSERT OR REPLACE INTO daily_stats (
			date, total_cost, token_input, token_output, token_cache_read, token_cache_write,
			session_count, api_requests, api_errors, lines_added, lines_removed,
			commits, prs_opened, cache_efficiency, cache_savings_usd, error_rate, retry_rate,
			avg_api_latency_ms, latency_p50_ms, latency_p95_ms, latency_p99_ms,
			model_breakdown, top_tools, error_categories, language_breakdown,
			decision_sources, mcp_tool_usage
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		row.Date,
		sanitizeFloat(row.TotalCost),
		row.TokenInput,
		row.TokenOutput,
		row.TokenCacheRead,
		row.TokenCacheWrite,
		row.SessionCount,
		row.APIRequests,
		row.APIErrors,
		row.LinesAdded,
		row.LinesRemoved,
		row.Commits,
		row.PRsOpened,
		sanitizeFloat(row.CacheEfficiency),
		sanitizeFloat(row.CacheSavingsUSD),
		sanitizeFloat(row.ErrorRate),
		sanitizeFloat(row.RetryRate),
		sanitizeFloat(row.AvgAPILatencyMs),
		sanitizeFloat(row.LatencyP50Ms),
		sanitizeFloat(row.LatencyP95Ms),
		sanitizeFloat(row.LatencyP99Ms),
		marshalJSONColumn("model_breakdown", row.ModelBreakdown),
		marshalJSONColumn("top_tools", row.TopTools),
		marshalJSONColumn("error_categories", row.ErrorCategories),
		marshalJSONColumn("language_breakdown", row.LanguageBreakdown),
		marshalJSONColumn("decision_sources", row.DecisionSources),
		marshalJSONColumn("mcp_tool_usage", row.MCPToolUsage),
	)
	return err
}

func (s *SQLiteStore) writeBurnRateSnapshot(tx *sql.Tx, row *burnRateSnapshotRow) error {
	_, err := tx.Exec(`
		INSERT INTO burn_rate_snapshots (
			timestamp, total_cost, hourly_rate, trend, token_velocity,
			daily_projection, monthly_projection, per_model
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		row.Timestamp,
		sanitizeFloat(row.TotalCost),
		sanitizeFloat(row.HourlyRate),
		row.Trend,
		sanitizeFloat(row.TokenVelocity),
		sanitizeFloat(row.DailyProjection),
		sanitizeFloat(row.MonthlyProjection),
		marshalJSONColumn("per_model", row.PerModel),
	)
	return err
}

func (s *SQLiteStore) writeAlertHistory(tx *sql.Tx, row *alertHistoryRow) error {
	_, err := tx.Exec(`
		INSERT INTO alert_history (rule, severity, message, session_id, fired_at)
		VALUES (?, ?, ?, ?, ?)
	`, row.Rule, row.Severity, row.Message, row.SessionID, row.FiredAt)
	return err
}
