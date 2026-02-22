package storage

import (
	"context"
	"fmt"
	"log"
	"time"
)

const (
	maintenanceInterval = 1 * time.Hour
	vacuumInterval      = 7 * 24 * time.Hour
)

func (s *SQLiteStore) startMaintenance(ctx context.Context, retentionDays, summaryRetentionDays int) {
	go s.maintenanceLoop(ctx, retentionDays, summaryRetentionDays)
}

func (s *SQLiteStore) maintenanceLoop(ctx context.Context, retentionDays, summaryRetentionDays int) {
	defer close(s.maintenanceDone)

	lastVacuum := time.Now()
	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.runMaintenanceCycle(retentionDays, summaryRetentionDays); err != nil {
				log.Printf("ERROR: maintenance cycle failed: %v", err)
			}

			if time.Since(lastVacuum) >= vacuumInterval {
				if _, err := s.db.Exec("VACUUM"); err != nil {
					log.Printf("ERROR: VACUUM failed: %v", err)
				} else {
					lastVacuum = time.Now()
				}
			}
		}
	}
}

func (s *SQLiteStore) runMaintenanceCycle(retentionDays, summaryRetentionDays int) error {
	// Capture stats snapshot if callback is set (FR-005).
	if s.statsSnapshotFn != nil {
		ds := s.statsSnapshotFn()
		today := time.Now().Format("2006-01-02")
		s.WriteDailyStats(today, ds)
	}

	retentionModifier := fmt.Sprintf("-%d days", retentionDays)
	summaryModifier := fmt.Sprintf("-%d days", summaryRetentionDays)

	_, err := s.db.Exec(`
		INSERT INTO daily_summaries (session_id, date, total_cost, total_tokens, api_requests, api_errors, active_seconds)
		SELECT
			src.session_id,
			src.date,
			src.total_cost,
			src.total_tokens,
			COALESCE(ev.api_requests, 0),
			COALESCE(ev.api_errors, 0),
			src.active_seconds
		FROM (
			SELECT
				m.session_id,
				date(m.timestamp) AS date,
				MAX(CASE WHEN m.name = 'claude_code.cost.usage' THEN m.value ELSE 0 END) AS total_cost,
				MAX(CASE WHEN m.name = 'claude_code.token.usage' THEN CAST(m.value AS INTEGER) ELSE 0 END) AS total_tokens,
				MAX(CASE WHEN m.name = 'claude_code.active_time.total' THEN m.value ELSE 0 END) AS active_seconds
			FROM metrics m
			WHERE datetime(m.timestamp) < datetime('now', ?)
			GROUP BY m.session_id, date(m.timestamp)
		) src
		LEFT JOIN (
			SELECT
				e.session_id,
				date(e.timestamp) AS date,
				COUNT(*) AS api_requests,
				COUNT(CASE WHEN e.attributes LIKE '%"error"%' OR e.attributes LIKE '%"status":"error"%' THEN 1 END) AS api_errors
			FROM events e
			WHERE e.name = 'claude_code.api_request' AND datetime(e.timestamp) < datetime('now', ?)
			GROUP BY e.session_id, date(e.timestamp)
		) ev ON src.session_id = ev.session_id AND src.date = ev.date
		ON CONFLICT(session_id, date) DO UPDATE SET
			total_cost = excluded.total_cost,
			total_tokens = excluded.total_tokens,
			api_requests = excluded.api_requests,
			api_errors = excluded.api_errors,
			active_seconds = excluded.active_seconds
	`, retentionModifier, retentionModifier)
	if err != nil {
		return fmt.Errorf("aggregating old data: %w", err)
	}

	_, err = s.db.Exec("DELETE FROM metrics WHERE datetime(timestamp) < datetime('now', ?)", retentionModifier)
	if err != nil {
		return fmt.Errorf("pruning old metrics: %w", err)
	}

	_, err = s.db.Exec("DELETE FROM events WHERE datetime(timestamp) < datetime('now', ?)", retentionModifier)
	if err != nil {
		return fmt.Errorf("pruning old events: %w", err)
	}

	_, err = s.db.Exec("DELETE FROM daily_summaries WHERE date < date('now', ?)", summaryModifier)
	if err != nil {
		return fmt.Errorf("pruning old summaries: %w", err)
	}

	// Prune new v2 tables
	_, err = s.db.Exec("DELETE FROM burn_rate_snapshots WHERE timestamp < datetime('now', ?)", retentionModifier)
	if err != nil {
		return fmt.Errorf("pruning old burn rate snapshots: %w", err)
	}

	_, err = s.db.Exec("DELETE FROM daily_stats WHERE date < date('now', ?)", summaryModifier)
	if err != nil {
		return fmt.Errorf("pruning old daily stats: %w", err)
	}

	_, err = s.db.Exec("DELETE FROM alert_history WHERE fired_at < datetime('now', ?)", summaryModifier)
	if err != nil {
		return fmt.Errorf("pruning old alert history: %w", err)
	}

	return nil
}
