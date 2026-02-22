package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/state"
	"github.com/nixlim/cc-top/internal/stats"
)

func TestQueryDailySummaries_FromDailySummariesTable(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	today := time.Now().Format("2006-01-02")
	_, err = store.db.Exec(
		"INSERT INTO daily_summaries (session_id, date, total_cost, total_tokens, api_requests, api_errors, active_seconds) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"sess-A", today, 5.50, 10000, 20, 2, 120.0)
	if err != nil {
		t.Fatalf("insert summary: %v", err)
	}

	summaries := store.QueryDailySummaries(7)
	if len(summaries) != 1 {
		t.Fatalf("want 1 summary, got %d", len(summaries))
	}

	ds := summaries[0]
	if ds.Date != today {
		t.Errorf("date: want %s, got %s", today, ds.Date)
	}
	if ds.TotalCost != 5.50 {
		t.Errorf("total_cost: want 5.50, got %f", ds.TotalCost)
	}
	if ds.TotalTokens != 10000 {
		t.Errorf("total_tokens: want 10000, got %d", ds.TotalTokens)
	}
	if ds.APIRequests != 20 {
		t.Errorf("api_requests: want 20, got %d", ds.APIRequests)
	}
	if ds.APIErrors != 2 {
		t.Errorf("api_errors: want 2, got %d", ds.APIErrors)
	}
	if ds.SessionCount != 1 {
		t.Errorf("session_count: want 1, got %d", ds.SessionCount)
	}
}

func TestQueryDailySummaries_FromLiveMetrics(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Now()
	nowStr := now.Format(time.RFC3339Nano)

	_, err = store.db.Exec(
		"INSERT INTO metrics (session_id, name, value, timestamp) VALUES (?, ?, ?, ?)",
		"sess-live", "claude_code.cost.usage", 3.25, nowStr)
	if err != nil {
		t.Fatalf("insert cost metric: %v", err)
	}

	_, err = store.db.Exec(
		"INSERT INTO metrics (session_id, name, value, timestamp) VALUES (?, ?, ?, ?)",
		"sess-live", "claude_code.token.usage", 8000.0, nowStr)
	if err != nil {
		t.Fatalf("insert token metric: %v", err)
	}

	_, err = store.db.Exec(
		"INSERT INTO events (session_id, name, timestamp, attributes) VALUES (?, ?, ?, ?)",
		"sess-live", "claude_code.api_request", nowStr, `{"status":"success"}`)
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	_, err = store.db.Exec(
		"INSERT INTO events (session_id, name, timestamp, attributes) VALUES (?, ?, ?, ?)",
		"sess-live", "claude_code.api_request", nowStr, `{"status":"error"}`)
	if err != nil {
		t.Fatalf("insert error event: %v", err)
	}

	summaries := store.QueryDailySummaries(7)
	if len(summaries) != 1 {
		t.Fatalf("want 1 summary from live metrics, got %d", len(summaries))
	}

	ds := summaries[0]
	if ds.TotalCost != 3.25 {
		t.Errorf("total_cost: want 3.25, got %f", ds.TotalCost)
	}
	if ds.TotalTokens != 8000 {
		t.Errorf("total_tokens: want 8000, got %d", ds.TotalTokens)
	}
	if ds.APIRequests != 2 {
		t.Errorf("api_requests: want 2, got %d", ds.APIRequests)
	}
	if ds.APIErrors != 1 {
		t.Errorf("api_errors: want 1 (status:error event), got %d", ds.APIErrors)
	}
}

func TestQueryDailySummaries_NoDoubleCount(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	today := time.Now().Format("2006-01-02")
	nowStr := time.Now().Format(time.RFC3339Nano)

	_, err = store.db.Exec(
		"INSERT INTO daily_summaries (session_id, date, total_cost, total_tokens, api_requests, api_errors, active_seconds) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"sess-dup", today, 2.00, 5000, 10, 1, 60.0)
	if err != nil {
		t.Fatalf("insert summary: %v", err)
	}

	_, err = store.db.Exec(
		"INSERT INTO metrics (session_id, name, value, timestamp) VALUES (?, ?, ?, ?)",
		"sess-dup", "claude_code.cost.usage", 2.00, nowStr)
	if err != nil {
		t.Fatalf("insert metric: %v", err)
	}

	summaries := store.QueryDailySummaries(7)
	if len(summaries) != 1 {
		t.Fatalf("want 1 summary, got %d", len(summaries))
	}

	if summaries[0].TotalCost != 2.00 {
		t.Errorf("total_cost should not double-count: want 2.00, got %f", summaries[0].TotalCost)
	}
}

func TestQueryDailySummaries_EmptyDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	summaries := store.QueryDailySummaries(7)
	if len(summaries) != 0 {
		t.Errorf("empty DB should return 0 summaries, got %d", len(summaries))
	}
}

func TestQueryDailySummaries_MultipleSessions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	today := time.Now().Format("2006-01-02")

	for _, sess := range []struct {
		id   string
		cost float64
	}{
		{"sess-1", 1.50},
		{"sess-2", 2.50},
		{"sess-3", 3.00},
	} {
		_, err = store.db.Exec(
			"INSERT INTO daily_summaries (session_id, date, total_cost, total_tokens, api_requests, api_errors, active_seconds) VALUES (?, ?, ?, ?, ?, ?, ?)",
			sess.id, today, sess.cost, 1000, 5, 0, 30.0)
		if err != nil {
			t.Fatalf("insert summary for %s: %v", sess.id, err)
		}
	}

	summaries := store.QueryDailySummaries(7)
	if len(summaries) != 1 {
		t.Fatalf("want 1 date row (aggregated), got %d", len(summaries))
	}

	ds := summaries[0]
	if ds.TotalCost != 7.00 {
		t.Errorf("total_cost: want 7.00 (sum of 3 sessions), got %f", ds.TotalCost)
	}
	if ds.SessionCount != 3 {
		t.Errorf("session_count: want 3, got %d", ds.SessionCount)
	}
}

func TestQueryDailySummaries_ExcludesOldData(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	oldDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	recentDate := time.Now().AddDate(0, 0, -3).Format("2006-01-02")

	_, err = store.db.Exec(
		"INSERT INTO daily_summaries (session_id, date, total_cost, total_tokens, api_requests, api_errors, active_seconds) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"sess-old", oldDate, 10.0, 20000, 50, 5, 300.0)
	if err != nil {
		t.Fatalf("insert old summary: %v", err)
	}

	_, err = store.db.Exec(
		"INSERT INTO daily_summaries (session_id, date, total_cost, total_tokens, api_requests, api_errors, active_seconds) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"sess-recent", recentDate, 5.0, 10000, 25, 2, 150.0)
	if err != nil {
		t.Fatalf("insert recent summary: %v", err)
	}

	summaries := store.QueryDailySummaries(7)
	if len(summaries) != 1 {
		t.Fatalf("want 1 summary (recent only), got %d", len(summaries))
	}
	if summaries[0].Date != recentDate {
		t.Errorf("date: want %s, got %s", recentDate, summaries[0].Date)
	}
}

func TestQueryDailySummaries_ViaAddMetricPath(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	store.AddMetric("sess-full", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     4.75,
		Timestamp: time.Now(),
	})
	store.AddMetric("sess-full", state.Metric{
		Name:      "claude_code.token.usage",
		Value:     12000,
		Timestamp: time.Now(),
	})
	store.AddEvent("sess-full", state.Event{
		Name:      "claude_code.api_request",
		Timestamp: time.Now(),
		Attributes: map[string]string{
			"status": "success",
		},
	})

	time.Sleep(200 * time.Millisecond)

	summaries := store.QueryDailySummaries(7)
	if len(summaries) != 1 {
		t.Fatalf("want 1 summary from live data, got %d", len(summaries))
	}

	ds := summaries[0]
	if ds.TotalCost != 4.75 {
		t.Errorf("total_cost: want 4.75, got %f", ds.TotalCost)
	}
	if ds.TotalTokens != 12000 {
		t.Errorf("total_tokens: want 12000, got %d", ds.TotalTokens)
	}
	if ds.APIRequests != 1 {
		t.Errorf("api_requests: want 1, got %d", ds.APIRequests)
	}
}

// --- QueryDailyStats Tests (v63.7) ---

func TestQueryDailyStats_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	ds := stats.DashboardStats{
		LinesAdded:    100,
		LinesRemoved:  50,
		Commits:       5,
		PRs:           2,
		CacheEfficiency: 0.85,
		AvgAPILatency:   1.5,
		ErrorRate:        0.05,
		RetryRate:        0.02,
		CacheSavingsUSD:  3.50,
		LatencyPercentiles: stats.LatencyPercentiles{P50: 0.5, P95: 1.2, P99: 2.0},
		ModelBreakdown: []stats.ModelStats{
			{Model: "opus", TotalCost: 10.0, TotalTokens: 50000},
		},
		TopTools: []stats.ToolUsage{{ToolName: "Bash", Count: 20}},
		TokenBreakdown: map[string]int64{"input": 30000, "output": 15000, "cacheRead": 4000, "cacheCreation": 1000},
	}

	store.WriteDailyStats("2026-02-20", ds)
	time.Sleep(200 * time.Millisecond)

	rows := store.QueryDailyStats(7)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}

	r := rows[0]
	if r.Date != "2026-02-20" {
		t.Errorf("date: want 2026-02-20, got %s", r.Date)
	}
	if r.TotalCost != 10.0 {
		t.Errorf("total_cost: want 10.0, got %f", r.TotalCost)
	}
	if r.LinesAdded != 100 {
		t.Errorf("lines_added: want 100, got %d", r.LinesAdded)
	}
	if r.Commits != 5 {
		t.Errorf("commits: want 5, got %d", r.Commits)
	}
}

func TestQueryDailyStats_LatencyConversionMsToSeconds(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	ds := stats.DashboardStats{
		AvgAPILatency:      1.5,
		LatencyPercentiles: stats.LatencyPercentiles{P50: 0.5, P95: 1.2, P99: 2.0},
	}

	store.WriteDailyStats("2026-02-20", ds)
	time.Sleep(200 * time.Millisecond)

	rows := store.QueryDailyStats(7)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}

	r := rows[0]
	// Stored as ms (1500, 500, 1200, 2000), read back as seconds
	if r.AvgAPILatency != 1.5 {
		t.Errorf("avg_api_latency: want 1.5, got %f", r.AvgAPILatency)
	}
	if r.LatencyP50 != 0.5 {
		t.Errorf("latency_p50: want 0.5, got %f", r.LatencyP50)
	}
	if r.LatencyP95 != 1.2 {
		t.Errorf("latency_p95: want 1.2, got %f", r.LatencyP95)
	}
	if r.LatencyP99 != 2.0 {
		t.Errorf("latency_p99: want 2.0, got %f", r.LatencyP99)
	}
}

func TestQueryDailyStats_MergesDailySummaries(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	// Insert daily_stats for today
	store.WriteDailyStats(today, stats.DashboardStats{
		LinesAdded: 42,
		ModelBreakdown: []stats.ModelStats{
			{Model: "opus", TotalCost: 5.0},
		},
	})
	time.Sleep(200 * time.Millisecond)

	// Insert daily_summaries for yesterday (no daily_stats)
	_, err := store.db.Exec(
		"INSERT INTO daily_summaries (session_id, date, total_cost, total_tokens, api_requests, api_errors, active_seconds) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"sess-A", yesterday, 3.0, 8000, 15, 1, 60.0)
	if err != nil {
		t.Fatalf("insert summary: %v", err)
	}

	rows := store.QueryDailyStats(7)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (merged), got %d", len(rows))
	}

	// Should be newest first
	if rows[0].Date != today {
		t.Errorf("first row should be today (%s), got %s", today, rows[0].Date)
	}
	if rows[1].Date != yesterday {
		t.Errorf("second row should be yesterday (%s), got %s", yesterday, rows[1].Date)
	}
	if rows[1].TotalCost != 3.0 {
		t.Errorf("yesterday total_cost from summaries: want 3.0, got %f", rows[1].TotalCost)
	}
}

func TestQueryDailyStats_EmptyDB(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	rows := store.QueryDailyStats(7)
	if len(rows) != 0 {
		t.Errorf("empty DB should return 0 rows, got %d", len(rows))
	}
}

func TestQueryDailyStats_JSONColumns(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	ds := stats.DashboardStats{
		ModelBreakdown: []stats.ModelStats{
			{Model: "opus", TotalCost: 5.0, TotalTokens: 10000},
		},
		TopTools: []stats.ToolUsage{{ToolName: "Bash", Count: 20}},
	}

	store.WriteDailyStats("2026-02-20", ds)
	time.Sleep(200 * time.Millisecond)

	rows := store.QueryDailyStats(7)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}

	if rows[0].ModelBreakdown == "" {
		t.Error("model_breakdown should be non-empty JSON")
	}
	if rows[0].TopTools == "" {
		t.Error("top_tools should be non-empty JSON")
	}
}

// --- QueryBurnRateDailySummary Tests ---

func TestQueryBurnRateDailySummary_Aggregation(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	today := time.Now().UTC().Format("2006-01-02")
	ts1 := today + "T10:00:00Z"
	ts2 := today + "T15:00:00Z"

	_, err := store.db.Exec(
		"INSERT INTO burn_rate_snapshots (timestamp, total_cost, hourly_rate, trend, token_velocity, daily_projection, monthly_projection) VALUES (?, ?, ?, ?, ?, ?, ?)",
		ts1, 10.0, 2.0, 1, 500.0, 48.0, 1440.0)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, err = store.db.Exec(
		"INSERT INTO burn_rate_snapshots (timestamp, total_cost, hourly_rate, trend, token_velocity, daily_projection, monthly_projection) VALUES (?, ?, ?, ?, ?, ?, ?)",
		ts2, 15.0, 4.0, 1, 700.0, 96.0, 2880.0)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	rows := store.QueryBurnRateDailySummary(7)
	if len(rows) != 1 {
		t.Fatalf("want 1 day, got %d", len(rows))
	}

	r := rows[0]
	if r.Date != today {
		t.Errorf("date: want %s, got %s", today, r.Date)
	}
	if r.AvgHourlyRate != 3.0 {
		t.Errorf("avg_hourly_rate: want 3.0, got %f", r.AvgHourlyRate)
	}
	if r.MaxHourlyRate != 4.0 {
		t.Errorf("max_hourly_rate: want 4.0, got %f", r.MaxHourlyRate)
	}
	if r.SnapshotCount != 2 {
		t.Errorf("snapshot_count: want 2, got %d", r.SnapshotCount)
	}
}

func TestQueryBurnRateDailySummary_EmptyDB(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	rows := store.QueryBurnRateDailySummary(7)
	if len(rows) != 0 {
		t.Errorf("empty DB should return 0 rows, got %d", len(rows))
	}
}

// --- QueryBurnRateSnapshots Tests ---

func TestQueryBurnRateSnapshots_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	br := burnrate.BurnRate{
		TotalCost:         25.0,
		HourlyRate:        5.0,
		Trend:             burnrate.TrendUp,
		TokenVelocity:     1500.0,
		DailyProjection:   120.0,
		MonthlyProjection: 3600.0,
		PerModel: []burnrate.ModelBurnRate{
			{Model: "opus", HourlyRate: 3.0, TotalCost: 15.0},
		},
	}

	store.WriteBurnRateSnapshot(br)
	time.Sleep(200 * time.Millisecond)

	rows := store.QueryBurnRateSnapshots(7)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}

	r := rows[0]
	if r.TotalCost != 25.0 {
		t.Errorf("total_cost: want 25.0, got %f", r.TotalCost)
	}
	if r.HourlyRate != 5.0 {
		t.Errorf("hourly_rate: want 5.0, got %f", r.HourlyRate)
	}
	if r.Trend != 1 {
		t.Errorf("trend: want 1, got %d", r.Trend)
	}
	if r.PerModel == "" {
		t.Error("per_model should be non-empty JSON")
	}
}

func TestQueryBurnRateSnapshots_Max500(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	// Insert 510 snapshots directly
	for i := range 510 {
		ts := time.Now().Add(time.Duration(-i) * time.Minute).UTC().Format(time.RFC3339)
		_, err := store.db.Exec(
			"INSERT INTO burn_rate_snapshots (timestamp, total_cost, hourly_rate, trend, token_velocity, daily_projection, monthly_projection) VALUES (?, ?, ?, ?, ?, ?, ?)",
			ts, float64(i), 1.0, 0, 100.0, 24.0, 720.0)
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	rows := store.QueryBurnRateSnapshots(7)
	if len(rows) != 500 {
		t.Errorf("max 500 snapshots: want 500, got %d", len(rows))
	}
}

func TestQueryBurnRateSnapshots_EmptyDB(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	rows := store.QueryBurnRateSnapshots(7)
	if len(rows) != 0 {
		t.Errorf("empty DB should return 0 rows, got %d", len(rows))
	}
}

// --- QueryBurnRateSnapshotsForDate Tests ---

func TestQueryBurnRateSnapshotsForDate_Filters(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	today := time.Now().UTC().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).UTC().Format("2006-01-02")

	_, _ = store.db.Exec(
		"INSERT INTO burn_rate_snapshots (timestamp, total_cost, hourly_rate, trend, token_velocity, daily_projection, monthly_projection) VALUES (?, ?, ?, ?, ?, ?, ?)",
		today+"T10:00:00Z", 10.0, 2.0, 0, 500.0, 48.0, 1440.0)
	_, _ = store.db.Exec(
		"INSERT INTO burn_rate_snapshots (timestamp, total_cost, hourly_rate, trend, token_velocity, daily_projection, monthly_projection) VALUES (?, ?, ?, ?, ?, ?, ?)",
		yesterday+"T10:00:00Z", 5.0, 1.0, 0, 200.0, 24.0, 720.0)

	rows := store.QueryBurnRateSnapshotsForDate(today)
	if len(rows) != 1 {
		t.Fatalf("want 1 snapshot for today, got %d", len(rows))
	}
	if rows[0].TotalCost != 10.0 {
		t.Errorf("total_cost: want 10.0, got %f", rows[0].TotalCost)
	}
}

// --- QueryAlertHistory Tests ---

func TestQueryAlertHistory_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	firedAt := time.Now().UTC().Format(time.RFC3339)
	_, err := store.db.Exec(
		"INSERT INTO alert_history (rule, severity, message, session_id, fired_at) VALUES (?, ?, ?, ?, ?)",
		"CostSurge", "critical", "Cost surge detected", "sess-1", firedAt)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	rows := store.QueryAlertHistory(7, "")
	if len(rows) != 1 {
		t.Fatalf("want 1 alert, got %d", len(rows))
	}

	r := rows[0]
	if r.Rule != "CostSurge" {
		t.Errorf("rule: want CostSurge, got %s", r.Rule)
	}
	if r.Severity != "critical" {
		t.Errorf("severity: want critical, got %s", r.Severity)
	}
	if r.SessionID != "sess-1" {
		t.Errorf("session_id: want sess-1, got %s", r.SessionID)
	}
}

func TestQueryAlertHistory_RuleFilter(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = store.db.Exec(
		"INSERT INTO alert_history (rule, severity, message, fired_at) VALUES (?, ?, ?, ?)",
		"CostSurge", "critical", "alert 1", now)
	_, _ = store.db.Exec(
		"INSERT INTO alert_history (rule, severity, message, fired_at) VALUES (?, ?, ?, ?)",
		"ErrorStorm", "warning", "alert 2", now)
	_, _ = store.db.Exec(
		"INSERT INTO alert_history (rule, severity, message, fired_at) VALUES (?, ?, ?, ?)",
		"CostSurge", "critical", "alert 3", now)

	rows := store.QueryAlertHistory(7, "CostSurge")
	if len(rows) != 2 {
		t.Fatalf("want 2 CostSurge alerts, got %d", len(rows))
	}
	for _, r := range rows {
		if r.Rule != "CostSurge" {
			t.Errorf("expected CostSurge, got %s", r.Rule)
		}
	}
}

func TestQueryAlertHistory_Max200(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	for i := range 210 {
		ts := time.Now().Add(time.Duration(-i) * time.Minute).UTC().Format(time.RFC3339)
		_, err := store.db.Exec(
			"INSERT INTO alert_history (rule, severity, message, fired_at) VALUES (?, ?, ?, ?)",
			"CostSurge", "warning", "alert", ts)
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	rows := store.QueryAlertHistory(7, "")
	if len(rows) != 200 {
		t.Errorf("max 200 alerts: want 200, got %d", len(rows))
	}
}

func TestQueryAlertHistory_EmptyDB(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	rows := store.QueryAlertHistory(7, "")
	if len(rows) != 0 {
		t.Errorf("empty DB should return 0 alerts, got %d", len(rows))
	}
}

// --- QueryDistinctAlertRules Tests ---

func TestQueryDistinctAlertRules(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = store.db.Exec(
		"INSERT INTO alert_history (rule, severity, message, fired_at) VALUES (?, ?, ?, ?)",
		"CostSurge", "critical", "a1", now)
	_, _ = store.db.Exec(
		"INSERT INTO alert_history (rule, severity, message, fired_at) VALUES (?, ?, ?, ?)",
		"ErrorStorm", "warning", "a2", now)
	_, _ = store.db.Exec(
		"INSERT INTO alert_history (rule, severity, message, fired_at) VALUES (?, ?, ?, ?)",
		"CostSurge", "critical", "a3", now)

	rules := store.QueryDistinctAlertRules()
	if len(rules) != 2 {
		t.Fatalf("want 2 distinct rules, got %d", len(rules))
	}
	// ORDER BY rule, so CostSurge first
	if rules[0] != "CostSurge" {
		t.Errorf("first rule: want CostSurge, got %s", rules[0])
	}
	if rules[1] != "ErrorStorm" {
		t.Errorf("second rule: want ErrorStorm, got %s", rules[1])
	}
}

func TestQueryDistinctAlertRules_EmptyDB(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	rules := store.QueryDistinctAlertRules()
	if len(rules) != 0 {
		t.Errorf("empty DB should return 0 rules, got %d", len(rules))
	}
}
