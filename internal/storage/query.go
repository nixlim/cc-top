package storage

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

// DailyStatsRow represents a row from the daily_stats table for query results.
type DailyStatsRow struct {
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
	AvgAPILatency    float64 // seconds (converted from ms on read)
	LatencyP50       float64 // seconds
	LatencyP95       float64 // seconds
	LatencyP99       float64 // seconds
	ModelBreakdown   string  // raw JSON
	TopTools         string  // raw JSON
	ErrorCategories  string  // raw JSON
	LanguageBreakdown string // raw JSON
	DecisionSources  string  // raw JSON
	MCPToolUsage     string  // raw JSON
}

// BurnRateDailySummary aggregates burn rate snapshots by day.
type BurnRateDailySummary struct {
	Date                 string
	AvgHourlyRate        float64
	MaxHourlyRate        float64
	AvgTokenVelocity     float64
	AvgDailyProjection   float64
	AvgMonthlyProjection float64
	SnapshotCount        int
}

// BurnRateSnapshotRow represents a single burn rate snapshot for query results.
type BurnRateSnapshotRow struct {
	Timestamp         string
	TotalCost         float64
	HourlyRate        float64
	Trend             int
	TokenVelocity     float64
	DailyProjection   float64
	MonthlyProjection float64
	PerModel          string // raw JSON
}

// AlertHistoryRow represents a single alert history row for query results.
type AlertHistoryRow struct {
	ID        int
	Rule      string
	Severity  string
	Message   string
	SessionID string
	FiredAt   string
}

func (s *SQLiteStore) QueryDailySummaries(days int) []state.DailySummary {
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

	rows, err := s.db.Query(`
		SELECT date, SUM(total_cost), SUM(total_tokens), SUM(api_requests), SUM(api_errors),
			COUNT(DISTINCT session_id) AS session_count
		FROM (
			SELECT session_id, date, total_cost, total_tokens, api_requests, api_errors
			FROM daily_summaries
			WHERE date >= ?

			UNION ALL

			SELECT
				src.session_id,
				src.date,
				src.total_cost,
				src.total_tokens,
				COALESCE(ev.api_requests, 0),
				COALESCE(ev.api_errors, 0)
			FROM (
				SELECT
					m.session_id,
					date(m.timestamp) AS date,
					MAX(CASE WHEN m.name = 'claude_code.cost.usage' THEN m.value ELSE 0 END) AS total_cost,
					MAX(CASE WHEN m.name = 'claude_code.token.usage' THEN CAST(m.value AS INTEGER) ELSE 0 END) AS total_tokens
				FROM metrics m
				WHERE date(m.timestamp) >= ?
				AND NOT EXISTS (
					SELECT 1 FROM daily_summaries ds
					WHERE ds.session_id = m.session_id AND ds.date = date(m.timestamp)
				)
				GROUP BY m.session_id, date(m.timestamp)
			) src
			LEFT JOIN (
				SELECT
					e.session_id,
					date(e.timestamp) AS date,
					COUNT(*) AS api_requests,
					COUNT(CASE WHEN e.attributes LIKE '%"error"%' OR e.attributes LIKE '%"status":"error"%' THEN 1 END) AS api_errors
				FROM events e
				WHERE e.name = 'claude_code.api_request' AND date(e.timestamp) >= ?
				GROUP BY e.session_id, date(e.timestamp)
			) ev ON src.session_id = ev.session_id AND src.date = ev.date
		)
		GROUP BY date
		ORDER BY date DESC
	`, cutoff, cutoff, cutoff)
	if err != nil {
		log.Printf("ERROR: querying daily summaries: %v", err)
		return nil
	}
	defer func() { _ = rows.Close() }()

	var summaries []state.DailySummary
	for rows.Next() {
		var ds state.DailySummary
		if err := rows.Scan(&ds.Date, &ds.TotalCost, &ds.TotalTokens,
			&ds.APIRequests, &ds.APIErrors, &ds.SessionCount); err != nil {
			log.Printf("ERROR: scanning daily summary row: %v", err)
			continue
		}
		summaries = append(summaries, ds)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ERROR: iterating daily summary rows: %v", err)
	}
	return summaries
}

// QueryDailyStats returns daily stats rows for the given number of days,
// newest first. Merges data from daily_stats and daily_summaries tables (FR-035).
// Latency values are converted from milliseconds back to seconds on read (FR-034).
func (s *SQLiteStore) QueryDailyStats(days int) []DailyStatsRow {
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

	rows, err := s.db.Query(`
		SELECT date, total_cost, token_input, token_output, token_cache_read, token_cache_write,
			session_count, api_requests, api_errors, lines_added, lines_removed,
			commits, prs_opened, cache_efficiency, cache_savings_usd, error_rate, retry_rate,
			avg_api_latency_ms, latency_p50_ms, latency_p95_ms, latency_p99_ms,
			model_breakdown, top_tools, error_categories, language_breakdown,
			decision_sources, mcp_tool_usage
		FROM daily_stats
		WHERE date >= ?
		ORDER BY date DESC
	`, cutoff)
	if err != nil {
		log.Printf("ERROR: querying daily stats: %v", err)
		return nil
	}
	defer func() { _ = rows.Close() }()

	seenDates := make(map[string]bool)
	var result []DailyStatsRow

	for rows.Next() {
		var r DailyStatsRow
		var avgLatMs, p50Ms, p95Ms, p99Ms float64
		var modelJSON, toolsJSON, errCatJSON, langJSON, decJSON, mcpJSON sql.NullString

		if err := rows.Scan(
			&r.Date, &r.TotalCost, &r.TokenInput, &r.TokenOutput, &r.TokenCacheRead, &r.TokenCacheWrite,
			&r.SessionCount, &r.APIRequests, &r.APIErrors, &r.LinesAdded, &r.LinesRemoved,
			&r.Commits, &r.PRsOpened, &r.CacheEfficiency, &r.CacheSavingsUSD, &r.ErrorRate, &r.RetryRate,
			&avgLatMs, &p50Ms, &p95Ms, &p99Ms,
			&modelJSON, &toolsJSON, &errCatJSON, &langJSON, &decJSON, &mcpJSON,
		); err != nil {
			log.Printf("ERROR: scanning daily stats row: %v", err)
			continue
		}

		// Convert ms → seconds (FR-034)
		r.AvgAPILatency = avgLatMs / 1000
		r.LatencyP50 = p50Ms / 1000
		r.LatencyP95 = p95Ms / 1000
		r.LatencyP99 = p99Ms / 1000

		// Unmarshal JSON columns; return empty string on failure (FR-012)
		r.ModelBreakdown = nullStringValue(modelJSON)
		r.TopTools = nullStringValue(toolsJSON)
		r.ErrorCategories = nullStringValue(errCatJSON)
		r.LanguageBreakdown = nullStringValue(langJSON)
		r.DecisionSources = nullStringValue(decJSON)
		r.MCPToolUsage = nullStringValue(mcpJSON)

		seenDates[r.Date] = true
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ERROR: iterating daily stats rows: %v", err)
	}

	// Merge in daily_summaries for dates not covered by daily_stats (FR-035)
	summaryRows, err := s.db.Query(`
		SELECT date, SUM(total_cost), SUM(total_tokens), SUM(api_requests), SUM(api_errors),
			COUNT(DISTINCT session_id)
		FROM daily_summaries
		WHERE date >= ?
		GROUP BY date
		ORDER BY date DESC
	`, cutoff)
	if err != nil {
		log.Printf("ERROR: querying daily summaries for merge: %v", err)
		return result
	}
	defer func() { _ = summaryRows.Close() }()

	for summaryRows.Next() {
		var date string
		var totalCost float64
		var totalTokens int64
		var apiReqs, apiErrs, sessionCount int

		if err := summaryRows.Scan(&date, &totalCost, &totalTokens, &apiReqs, &apiErrs, &sessionCount); err != nil {
			log.Printf("ERROR: scanning daily summary merge row: %v", err)
			continue
		}
		if seenDates[date] {
			continue
		}
		result = append(result, DailyStatsRow{
			Date:         date,
			TotalCost:    totalCost,
			TokenInput:   totalTokens,
			SessionCount: sessionCount,
			APIRequests:  apiReqs,
			APIErrors:    apiErrs,
		})
	}
	if err := summaryRows.Err(); err != nil {
		log.Printf("ERROR: iterating daily summary merge rows: %v", err)
	}

	// Re-sort by date descending after merge
	sortDailyStatsDesc(result)

	return result
}

// QueryBurnRateDailySummary aggregates burn rate snapshots by day.
func (s *SQLiteStore) QueryBurnRateDailySummary(days int) []BurnRateDailySummary {
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

	rows, err := s.db.Query(`
		SELECT date(timestamp) AS day,
			AVG(hourly_rate), MAX(hourly_rate),
			AVG(token_velocity), AVG(daily_projection), AVG(monthly_projection),
			COUNT(*)
		FROM burn_rate_snapshots
		WHERE date(timestamp) >= ?
		GROUP BY day
		ORDER BY day DESC
	`, cutoff)
	if err != nil {
		log.Printf("ERROR: querying burn rate daily summary: %v", err)
		return nil
	}
	defer func() { _ = rows.Close() }()

	var result []BurnRateDailySummary
	for rows.Next() {
		var r BurnRateDailySummary
		if err := rows.Scan(&r.Date, &r.AvgHourlyRate, &r.MaxHourlyRate,
			&r.AvgTokenVelocity, &r.AvgDailyProjection, &r.AvgMonthlyProjection, &r.SnapshotCount); err != nil {
			log.Printf("ERROR: scanning burn rate daily summary row: %v", err)
			continue
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ERROR: iterating burn rate daily summary rows: %v", err)
	}
	return result
}

// QueryBurnRateSnapshots returns individual burn rate snapshots, max 500 (FR-023).
func (s *SQLiteStore) QueryBurnRateSnapshots(days int) []BurnRateSnapshotRow {
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)

	rows, err := s.db.Query(`
		SELECT timestamp, total_cost, hourly_rate, trend, token_velocity,
			daily_projection, monthly_projection, per_model
		FROM burn_rate_snapshots
		WHERE timestamp >= ?
		ORDER BY timestamp DESC
		LIMIT 500
	`, cutoff)
	if err != nil {
		log.Printf("ERROR: querying burn rate snapshots: %v", err)
		return nil
	}
	defer func() { _ = rows.Close() }()

	var result []BurnRateSnapshotRow
	for rows.Next() {
		var r BurnRateSnapshotRow
		var perModelJSON sql.NullString
		if err := rows.Scan(&r.Timestamp, &r.TotalCost, &r.HourlyRate, &r.Trend,
			&r.TokenVelocity, &r.DailyProjection, &r.MonthlyProjection, &perModelJSON); err != nil {
			log.Printf("ERROR: scanning burn rate snapshot row: %v", err)
			continue
		}
		r.PerModel = nullStringValue(perModelJSON)
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ERROR: iterating burn rate snapshot rows: %v", err)
	}
	return result
}

// QueryBurnRateSnapshotsForDate returns all burn rate snapshots for a specific date.
func (s *SQLiteStore) QueryBurnRateSnapshotsForDate(date string) []BurnRateSnapshotRow {
	rows, err := s.db.Query(`
		SELECT timestamp, total_cost, hourly_rate, trend, token_velocity,
			daily_projection, monthly_projection, per_model
		FROM burn_rate_snapshots
		WHERE date(timestamp) = ?
		ORDER BY timestamp ASC
	`, date)
	if err != nil {
		log.Printf("ERROR: querying burn rate snapshots for date %s: %v", date, err)
		return nil
	}
	defer func() { _ = rows.Close() }()

	var result []BurnRateSnapshotRow
	for rows.Next() {
		var r BurnRateSnapshotRow
		var perModelJSON sql.NullString
		if err := rows.Scan(&r.Timestamp, &r.TotalCost, &r.HourlyRate, &r.Trend,
			&r.TokenVelocity, &r.DailyProjection, &r.MonthlyProjection, &perModelJSON); err != nil {
			log.Printf("ERROR: scanning burn rate snapshot row: %v", err)
			continue
		}
		r.PerModel = nullStringValue(perModelJSON)
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ERROR: iterating burn rate snapshot rows: %v", err)
	}
	return result
}

// QueryAlertHistory returns alert history rows, max 200 (FR-024).
// If ruleFilter is non-empty, only alerts matching that rule are returned.
func (s *SQLiteStore) QueryAlertHistory(days int, ruleFilter string) []AlertHistoryRow {
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.RFC3339)

	var dbRows *sql.Rows
	var err error

	if ruleFilter != "" {
		dbRows, err = s.db.Query(`
			SELECT id, rule, severity, message, session_id, fired_at
			FROM alert_history
			WHERE fired_at >= ? AND rule = ?
			ORDER BY fired_at DESC
			LIMIT 200
		`, cutoff, ruleFilter)
	} else {
		dbRows, err = s.db.Query(`
			SELECT id, rule, severity, message, session_id, fired_at
			FROM alert_history
			WHERE fired_at >= ?
			ORDER BY fired_at DESC
			LIMIT 200
		`, cutoff)
	}
	if err != nil {
		log.Printf("ERROR: querying alert history: %v", err)
		return nil
	}
	defer func() { _ = dbRows.Close() }()

	var result []AlertHistoryRow
	for dbRows.Next() {
		var r AlertHistoryRow
		if err := dbRows.Scan(&r.ID, &r.Rule, &r.Severity, &r.Message, &r.SessionID, &r.FiredAt); err != nil {
			log.Printf("ERROR: scanning alert history row: %v", err)
			continue
		}
		result = append(result, r)
	}
	if err := dbRows.Err(); err != nil {
		log.Printf("ERROR: iterating alert history rows: %v", err)
	}
	return result
}

// QueryDistinctAlertRules returns the distinct alert rule names from history.
func (s *SQLiteStore) QueryDistinctAlertRules() []string {
	rows, err := s.db.Query("SELECT DISTINCT rule FROM alert_history ORDER BY rule")
	if err != nil {
		log.Printf("ERROR: querying distinct alert rules: %v", err)
		return nil
	}
	defer func() { _ = rows.Close() }()

	var result []string
	for rows.Next() {
		var rule string
		if err := rows.Scan(&rule); err != nil {
			log.Printf("ERROR: scanning alert rule: %v", err)
			continue
		}
		result = append(result, rule)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ERROR: iterating alert rules: %v", err)
	}
	return result
}

// nullStringValue returns the string value from a sql.NullString, or empty string if NULL.
func nullStringValue(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// sortDailyStatsDesc sorts DailyStatsRow by Date descending.
func sortDailyStatsDesc(rows []DailyStatsRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].Date > rows[j-1].Date; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// unmarshalJSONField is a helper for query result JSON deserialization (FR-012).
func unmarshalJSONField(data string, target interface{}) {
	if data == "" {
		return
	}
	if err := json.Unmarshal([]byte(data), target); err != nil {
		log.Printf("WARNING: failed to unmarshal JSON field: %v", err)
	}
}
