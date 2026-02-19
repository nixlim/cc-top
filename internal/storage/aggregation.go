package storage

import (
	"fmt"
	"time"
)

func (s *SQLiteStore) runDailyAggregation() error {
	today := time.Now().Format("2006-01-02")

	_, err := s.db.Exec(`
		INSERT INTO daily_summaries (session_id, date, total_cost, total_tokens, api_requests, api_errors, active_seconds)
		SELECT
			src.session_id,
			? AS date,
			src.total_cost,
			src.total_tokens,
			COALESCE(ev.api_requests, 0),
			COALESCE(ev.api_errors, 0),
			src.active_seconds
		FROM (
			SELECT
				m.session_id,
				MAX(CASE WHEN m.name = 'claude_code.cost.usage' THEN m.value ELSE 0 END) AS total_cost,
				MAX(CASE WHEN m.name = 'claude_code.token.usage' THEN CAST(m.value AS INTEGER) ELSE 0 END) AS total_tokens,
				MAX(CASE WHEN m.name = 'claude_code.active_time.total' THEN m.value ELSE 0 END) AS active_seconds
			FROM metrics m
			WHERE date(m.timestamp) = ?
			GROUP BY m.session_id
		) src
		LEFT JOIN (
			SELECT
				e.session_id,
				COUNT(*) AS api_requests,
				COUNT(CASE WHEN e.attributes LIKE '%"error"%' OR e.attributes LIKE '%"status":"error"%' THEN 1 END) AS api_errors
			FROM events e
			WHERE e.name = 'claude_code.api_request' AND date(e.timestamp) = ?
			GROUP BY e.session_id
		) ev ON src.session_id = ev.session_id
		ON CONFLICT(session_id, date) DO UPDATE SET
			total_cost = excluded.total_cost,
			total_tokens = excluded.total_tokens,
			api_requests = excluded.api_requests,
			api_errors = excluded.api_errors,
			active_seconds = excluded.active_seconds
	`, today, today, today)
	if err != nil {
		return fmt.Errorf("daily aggregation: %w", err)
	}

	return nil
}
