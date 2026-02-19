package storage

import (
	"log"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

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
