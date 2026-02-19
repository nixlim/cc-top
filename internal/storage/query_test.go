package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/state"
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
