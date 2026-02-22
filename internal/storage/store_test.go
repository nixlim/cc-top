package storage

import (
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/alerts"
	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/state"
	"github.com/nixlim/cc-top/internal/stats"
)

func TestSQLiteStore_AddMetric_PersistsToSQLite(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	m := state.Metric{
		Name:      "test.metric",
		Value:     42.5,
		Timestamp: time.Now(),
		Attributes: map[string]string{
			"model": "claude-opus-4-6",
		},
	}
	store.AddMetric("sess-001", m)

	time.Sleep(150 * time.Millisecond)

	db := store.db
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM metrics WHERE session_id = ? AND name = ?", "sess-001", "test.metric").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query metrics: %v", err)
	}
	if count != 1 {
		t.Errorf("metric not persisted: want 1 row, got %d", count)
	}

	var attributes string
	err = db.QueryRow("SELECT attributes FROM metrics WHERE session_id = ? AND name = ?", "sess-001", "test.metric").Scan(&attributes)
	if err != nil {
		t.Fatalf("failed to read attributes: %v", err)
	}
	if attributes == "" {
		t.Error("attributes not persisted")
	}
}

func TestSQLiteStore_AddEvent_PersistsToSQLite(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	e := state.Event{
		Name:      "test.event",
		Timestamp: time.Now(),
		Attributes: map[string]string{
			"status": "success",
		},
	}
	store.AddEvent("sess-002", e)

	time.Sleep(150 * time.Millisecond)

	db := store.db
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM events WHERE session_id = ? AND name = ?", "sess-002", "test.event").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query events: %v", err)
	}
	if count != 1 {
		t.Errorf("event not persisted: want 1 row, got %d", count)
	}
}

func TestSQLiteStore_UpdatePID_PersistsToSQLite(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	m := state.Metric{
		Name:      "claude_code.session.count",
		Value:     1,
		Timestamp: time.Now(),
	}
	store.AddMetric("sess-003", m)

	store.UpdatePID("sess-003", 12345)

	time.Sleep(150 * time.Millisecond)

	db := store.db
	var pid int
	err = db.QueryRow("SELECT pid FROM sessions WHERE session_id = ?", "sess-003").Scan(&pid)
	if err != nil {
		t.Fatalf("failed to query session PID: %v", err)
	}
	if pid != 12345 {
		t.Errorf("PID not persisted: want 12345, got %d", pid)
	}
}

func TestSQLiteStore_BatchFlush50Ops(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	start := time.Now()
	for i := 0; i < 50; i++ {
		m := state.Metric{
			Name:      "test.metric",
			Value:     float64(i),
			Timestamp: time.Now(),
		}
		store.AddMetric("sess-batch", m)
	}

	time.Sleep(50 * time.Millisecond)
	elapsed := time.Since(start)

	db := store.db
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM metrics WHERE session_id = ?", "sess-batch").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query metrics: %v", err)
	}
	if count != 50 {
		t.Errorf("batch flush failed: want 50 rows, got %d", count)
	}

	if elapsed > 200*time.Millisecond {
		t.Errorf("batch flush too slow: elapsed %v, want <200ms", elapsed)
	}
}

func TestSQLiteStore_TimeFlush100ms(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	m := state.Metric{
		Name:      "test.metric",
		Value:     1.0,
		Timestamp: time.Now(),
	}
	store.AddMetric("sess-time", m)

	time.Sleep(150 * time.Millisecond)

	db := store.db
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM metrics WHERE session_id = ?", "sess-time").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query metrics: %v", err)
	}
	if count != 1 {
		t.Errorf("time-based flush failed: want 1 row, got %d", count)
	}
}

func TestSQLiteStore_ReadsFromMemory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	m := state.Metric{
		Name:      "test.metric",
		Value:     99.9,
		Timestamp: time.Now(),
	}
	store.AddMetric("sess-read", m)

	session := store.GetSession("sess-read")
	if session == nil {
		t.Fatal("session not found in memory")
	}

	if len(session.Metrics) != 1 {
		t.Errorf("metrics not in memory: want 1, got %d", len(session.Metrics))
	}
	if session.Metrics[0].Value != 99.9 {
		t.Errorf("metric value in memory: want 99.9, got %f", session.Metrics[0].Value)
	}
}

func TestSQLiteStore_AddMetric_PersistsSessionFields(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	m := state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     3.50,
		Timestamp: time.Now(),
		Attributes: map[string]string{
			"model":         "claude-opus-4-6",
			"terminal.type": "vscode",
		},
	}
	store.AddMetric("sess-snap", m)

	time.Sleep(200 * time.Millisecond)

	db := store.db
	var model, terminal string
	var totalCost float64
	err = db.QueryRow(`
		SELECT COALESCE(model, ''), COALESCE(terminal, ''), COALESCE(total_cost, 0)
		FROM sessions WHERE session_id = ?
	`, "sess-snap").Scan(&model, &terminal, &totalCost)
	if err != nil {
		t.Fatalf("failed to query session: %v", err)
	}

	if model != "claude-opus-4-6" {
		t.Errorf("model not persisted: want 'claude-opus-4-6', got %q", model)
	}
	if terminal != "vscode" {
		t.Errorf("terminal not persisted: want 'vscode', got %q", terminal)
	}
	if totalCost != 3.50 {
		t.Errorf("total_cost not persisted: want 3.50, got %f", totalCost)
	}
}

func TestSQLiteStore_AddEvent_PersistsSessionFields(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	e := state.Event{
		Name:      "claude_code.api_request",
		Timestamp: time.Now(),
		Attributes: map[string]string{
			"model":                 "claude-opus-4-6",
			"organization.id":      "org-123",
			"user.account_uuid":    "uuid-456",
			"cache_read_tokens":    "5000",
			"cache_creation_tokens": "2000",
		},
	}
	store.AddEvent("sess-evt-snap", e)

	time.Sleep(200 * time.Millisecond)

	db := store.db
	var model, orgID, userUUID string
	var cacheRead, cacheCreation int64
	err = db.QueryRow(`
		SELECT COALESCE(model, ''), COALESCE(org_id, ''), COALESCE(user_uuid, ''),
		       COALESCE(cache_read_tokens, 0), COALESCE(cache_creation_tokens, 0)
		FROM sessions WHERE session_id = ?
	`, "sess-evt-snap").Scan(&model, &orgID, &userUUID, &cacheRead, &cacheCreation)
	if err != nil {
		t.Fatalf("failed to query session: %v", err)
	}

	if model != "claude-opus-4-6" {
		t.Errorf("model not persisted: want 'claude-opus-4-6', got %q", model)
	}
	if orgID != "org-123" {
		t.Errorf("org_id not persisted: want 'org-123', got %q", orgID)
	}
	if userUUID != "uuid-456" {
		t.Errorf("user_uuid not persisted: want 'uuid-456', got %q", userUUID)
	}
	if cacheRead != 5000 {
		t.Errorf("cache_read_tokens not persisted: want 5000, got %d", cacheRead)
	}
	if cacheCreation != 2000 {
		t.Errorf("cache_creation_tokens not persisted: want 2000, got %d", cacheCreation)
	}
}

func TestSQLiteStore_UpdateMetadata_PersistsToSQLite(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	store.AddMetric("sess-meta", state.Metric{
		Name:      "claude_code.session.count",
		Value:     1,
		Timestamp: time.Now(),
	})

	store.UpdateMetadata("sess-meta", state.SessionMetadata{
		ServiceVersion: "1.2.3",
		OSType:         "darwin",
		OSVersion:      "24.6.0",
		HostArch:       "arm64",
	})

	time.Sleep(200 * time.Millisecond)

	db := store.db
	var serviceVersion, osType, osVersion, hostArch string
	err = db.QueryRow(`
		SELECT COALESCE(service_version, ''), COALESCE(os_type, ''), COALESCE(os_version, ''), COALESCE(host_arch, '')
		FROM sessions WHERE session_id = ?
	`, "sess-meta").Scan(&serviceVersion, &osType, &osVersion, &hostArch)
	if err != nil {
		t.Fatalf("failed to query session metadata: %v", err)
	}

	if serviceVersion != "1.2.3" {
		t.Errorf("service_version: want '1.2.3', got %q", serviceVersion)
	}
	if osType != "darwin" {
		t.Errorf("os_type: want 'darwin', got %q", osType)
	}
	if osVersion != "24.6.0" {
		t.Errorf("os_version: want '24.6.0', got %q", osVersion)
	}
	if hostArch != "arm64" {
		t.Errorf("host_arch: want 'arm64', got %q", hostArch)
	}
}

func TestSQLiteStore_WriteErrorDoesNotCrash(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	_ = store.db.Close()

	m := state.Metric{
		Name:      "test.metric",
		Value:     1.0,
		Timestamp: time.Now(),
	}
	store.AddMetric("sess-error", m)

	time.Sleep(150 * time.Millisecond)

	session := store.GetSession("sess-error")
	if session == nil {
		t.Fatal("session not found in memory after write error")
	}

	err = store.Close()
	if err != nil {
		t.Logf("Close returned error (expected due to closed db): %v", err)
	}
}

func TestSQLiteStore_ChannelOverflow_IncrementsCounter(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := newSQLiteStoreWithChannelSize(dbPath, 5, 7, 90)
	if err != nil {
		t.Fatalf("newSQLiteStoreWithChannelSize failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	_ = store.db.Close()

	time.Sleep(50 * time.Millisecond)

	for range 20 {
		m := state.Metric{
			Name:      "test.metric",
			Value:     1.0,
			Timestamp: time.Now(),
		}
		store.AddMetric("sess-overflow", m)
	}

	time.Sleep(50 * time.Millisecond)

	dropped := store.DroppedWrites()
	if dropped == 0 {
		t.Error("expected DroppedWrites > 0 when channel overflows, got 0")
	}
	t.Logf("Dropped %d writes out of 20 attempted", dropped)
}

// --- Callback Integration Tests (v63.6) ---

func TestSetStatsSnapshotFunc_CalledDuringMaintenance(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	called := false
	store.SetStatsSnapshotFunc(func() stats.DashboardStats {
		called = true
		return stats.DashboardStats{
			LinesAdded: 42,
			ModelBreakdown: []stats.ModelStats{
				{Model: "opus", TotalCost: 5.0, TotalTokens: 10000},
			},
		}
	})

	err := store.runMaintenanceCycle(7, 90)
	if err != nil {
		t.Fatalf("runMaintenanceCycle failed: %v", err)
	}

	if !called {
		t.Error("stats snapshot function not called during maintenance")
	}

	// Wait for async write to flush
	time.Sleep(200 * time.Millisecond)

	today := time.Now().Format("2006-01-02")
	var linesAdded int
	err = store.db.QueryRow("SELECT lines_added FROM daily_stats WHERE date = ?", today).Scan(&linesAdded)
	if err != nil {
		t.Fatalf("query daily_stats: %v", err)
	}
	if linesAdded != 42 {
		t.Errorf("lines_added: want 42, got %d", linesAdded)
	}
}

func TestSetStatsSnapshotFunc_NilSkipsSilently(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	// Don't set any callback — nil by default
	err := store.runMaintenanceCycle(7, 90)
	if err != nil {
		t.Fatalf("runMaintenanceCycle with nil callback should not error: %v", err)
	}
}

func TestSetBurnRateSnapshotFunc_NilSkipsStartBurnRate(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	// Don't set any callback — nil by default
	store.StartBurnRateSnapshots() // should return immediately, no goroutine

	if store.burnRateTicker != nil {
		t.Error("burn rate ticker should be nil when callback is nil")
	}
	if store.burnRateDone != nil {
		t.Error("burn rate done channel should be nil when callback is nil")
	}
}

func TestStartBurnRateSnapshots_CreatesTickerAndChannel(t *testing.T) {
	store := newTestStore(t)

	store.SetBurnRateSnapshotFunc(func() burnrate.BurnRate {
		return burnrate.BurnRate{TotalCost: 10.0}
	})

	store.StartBurnRateSnapshots()

	if store.burnRateTicker == nil {
		t.Error("burn rate ticker should be created")
	}
	if store.burnRateDone == nil {
		t.Error("burn rate done channel should be created")
	}

	_ = store.Close()
}

// --- Daily Stats Write Tests ---

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}
	return store
}

func TestWriteDailyStats_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	ds := stats.DashboardStats{
		LinesAdded:   100,
		LinesRemoved: 50,
		Commits:      5,
		PRs:          2,
		CacheEfficiency: 0.85,
		AvgAPILatency:   1.5, // seconds
		ErrorRate:        0.05,
		RetryRate:        0.02,
		CacheSavingsUSD:  3.50,
		LatencyPercentiles: stats.LatencyPercentiles{P50: 0.5, P95: 1.2, P99: 2.0},
		ModelBreakdown: []stats.ModelStats{
			{Model: "claude-opus-4-6", TotalCost: 10.0, TotalTokens: 50000},
		},
		TopTools: []stats.ToolUsage{
			{ToolName: "Bash", Count: 20},
		},
		ToolPerformance: []stats.ToolPerf{
			{ToolName: "Bash", AvgDurationMS: 150.0, P95DurationMS: 300.0},
		},
		ErrorCategories: map[string]int{"rate_limit": 3},
		LanguageBreakdown: map[string]int{"Go": 50},
		DecisionSources: map[string]int{"user": 10},
		MCPToolUsage: map[string]int{"server:tool": 5},
		TokenBreakdown: map[string]int64{"input": 30000, "output": 15000, "cacheRead": 4000, "cacheCreation": 1000},
	}

	store.WriteDailyStats("2026-02-20", ds)
	time.Sleep(200 * time.Millisecond)

	var totalCost float64
	var linesAdded, linesRemoved, commits, prsOpened int
	var avgLatMs, p50Ms, p95Ms, p99Ms, cacheEff, errRate, retryRate, cacheSavings float64
	var tokenInput, tokenOutput, tokenCacheRead, tokenCacheWrite int64
	err := store.db.QueryRow(`
		SELECT total_cost, lines_added, lines_removed, commits, prs_opened,
			avg_api_latency_ms, latency_p50_ms, latency_p95_ms, latency_p99_ms,
			cache_efficiency, error_rate, retry_rate, cache_savings_usd,
			token_input, token_output, token_cache_read, token_cache_write
		FROM daily_stats WHERE date = ?
	`, "2026-02-20").Scan(&totalCost, &linesAdded, &linesRemoved, &commits, &prsOpened,
		&avgLatMs, &p50Ms, &p95Ms, &p99Ms,
		&cacheEff, &errRate, &retryRate, &cacheSavings,
		&tokenInput, &tokenOutput, &tokenCacheRead, &tokenCacheWrite)
	if err != nil {
		t.Fatalf("query daily_stats: %v", err)
	}

	if totalCost != 10.0 {
		t.Errorf("total_cost: want 10.0, got %f", totalCost)
	}
	if linesAdded != 100 {
		t.Errorf("lines_added: want 100, got %d", linesAdded)
	}
	if linesRemoved != 50 {
		t.Errorf("lines_removed: want 50, got %d", linesRemoved)
	}
	if commits != 5 {
		t.Errorf("commits: want 5, got %d", commits)
	}
	if prsOpened != 2 {
		t.Errorf("prs_opened: want 2, got %d", prsOpened)
	}
	if tokenInput != 30000 {
		t.Errorf("token_input: want 30000, got %d", tokenInput)
	}
	if tokenOutput != 15000 {
		t.Errorf("token_output: want 15000, got %d", tokenOutput)
	}
	if tokenCacheRead != 4000 {
		t.Errorf("token_cache_read: want 4000, got %d", tokenCacheRead)
	}
	if tokenCacheWrite != 1000 {
		t.Errorf("token_cache_write: want 1000, got %d", tokenCacheWrite)
	}
}

func TestWriteDailyStats_LatencyConversion(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	ds := stats.DashboardStats{
		AvgAPILatency: 1.5,
		LatencyPercentiles: stats.LatencyPercentiles{P50: 0.5, P95: 1.2, P99: 2.0},
	}

	store.WriteDailyStats("2026-02-20", ds)
	time.Sleep(200 * time.Millisecond)

	var avgLatMs, p50Ms, p95Ms, p99Ms float64
	err := store.db.QueryRow(`
		SELECT avg_api_latency_ms, latency_p50_ms, latency_p95_ms, latency_p99_ms
		FROM daily_stats WHERE date = ?
	`, "2026-02-20").Scan(&avgLatMs, &p50Ms, &p95Ms, &p99Ms)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if avgLatMs != 1500.0 {
		t.Errorf("avg_api_latency_ms: want 1500.0, got %f", avgLatMs)
	}
	if p50Ms != 500.0 {
		t.Errorf("latency_p50_ms: want 500.0, got %f", p50Ms)
	}
	if p95Ms != 1200.0 {
		t.Errorf("latency_p95_ms: want 1200.0, got %f", p95Ms)
	}
	if p99Ms != 2000.0 {
		t.Errorf("latency_p99_ms: want 2000.0, got %f", p99Ms)
	}
}

func TestWriteDailyStats_Upsert(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	ds1 := stats.DashboardStats{LinesAdded: 10}
	store.WriteDailyStats("2026-02-20", ds1)
	time.Sleep(200 * time.Millisecond)

	ds2 := stats.DashboardStats{LinesAdded: 25}
	store.WriteDailyStats("2026-02-20", ds2)
	time.Sleep(200 * time.Millisecond)

	var count int
	err := store.db.QueryRow("SELECT COUNT(*) FROM daily_stats WHERE date = ?", "2026-02-20").Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("upsert should produce 1 row, got %d", count)
	}

	var linesAdded int
	err = store.db.QueryRow("SELECT lines_added FROM daily_stats WHERE date = ?", "2026-02-20").Scan(&linesAdded)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if linesAdded != 25 {
		t.Errorf("lines_added after upsert: want 25, got %d", linesAdded)
	}
}

func TestWriteDailyStats_NaNInfSanitizedOnWrite(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	ds := stats.DashboardStats{
		CacheEfficiency: math.NaN(),
		ErrorRate:       math.Inf(1),
		RetryRate:       math.Inf(-1),
		AvgAPILatency:   math.NaN(),
	}

	store.WriteDailyStats("2026-02-20", ds)
	time.Sleep(200 * time.Millisecond)

	var cacheEff, errRate, retryRate, avgLat float64
	err := store.db.QueryRow(`
		SELECT cache_efficiency, error_rate, retry_rate, avg_api_latency_ms
		FROM daily_stats WHERE date = ?
	`, "2026-02-20").Scan(&cacheEff, &errRate, &retryRate, &avgLat)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if cacheEff != 0.0 {
		t.Errorf("cache_efficiency NaN not sanitized: got %f", cacheEff)
	}
	if errRate != 0.0 {
		t.Errorf("error_rate +Inf not sanitized: got %f", errRate)
	}
	if retryRate != 0.0 {
		t.Errorf("retry_rate -Inf not sanitized: got %f", retryRate)
	}
	if avgLat != 0.0 {
		t.Errorf("avg_api_latency_ms NaN not sanitized: got %f", avgLat)
	}
}

func TestWriteDailyStats_JSONColumns(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	ds := stats.DashboardStats{
		ModelBreakdown: []stats.ModelStats{
			{Model: "opus", TotalCost: 5.0, TotalTokens: 10000},
			{Model: "sonnet", TotalCost: 3.0, TotalTokens: 8000},
		},
		TopTools:    []stats.ToolUsage{{ToolName: "Read", Count: 15}},
		ErrorCategories: map[string]int{"rate_limit": 2, "server_error": 1},
		LanguageBreakdown: map[string]int{"Go": 30, "Python": 20},
		DecisionSources: map[string]int{"user": 5, "config": 3},
		MCPToolUsage: map[string]int{"mcp:tool1": 10},
	}

	store.WriteDailyStats("2026-02-20", ds)
	time.Sleep(200 * time.Millisecond)

	var modelJSON, toolsJSON, errCatJSON, langJSON, decJSON, mcpJSON sql.NullString
	err := store.db.QueryRow(`
		SELECT model_breakdown, top_tools, error_categories, language_breakdown,
			decision_sources, mcp_tool_usage
		FROM daily_stats WHERE date = ?
	`, "2026-02-20").Scan(&modelJSON, &toolsJSON, &errCatJSON, &langJSON, &decJSON, &mcpJSON)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if !modelJSON.Valid || modelJSON.String == "" {
		t.Error("model_breakdown should be non-null JSON")
	}
	if !toolsJSON.Valid || toolsJSON.String == "" {
		t.Error("top_tools should be non-null JSON")
	}
	if !errCatJSON.Valid || errCatJSON.String == "" {
		t.Error("error_categories should be non-null JSON")
	}
	if !langJSON.Valid || langJSON.String == "" {
		t.Error("language_breakdown should be non-null JSON")
	}
	if !decJSON.Valid || decJSON.String == "" {
		t.Error("decision_sources should be non-null JSON")
	}
	if !mcpJSON.Valid || mcpJSON.String == "" {
		t.Error("mcp_tool_usage should be non-null JSON")
	}
}

func TestWriteDailyStats_JSONMarshalFailure(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	// Use a channel value that cannot be marshaled to JSON
	ds := stats.DashboardStats{
		LinesAdded: 42,
	}

	// Directly create a dailyStatsRow with an unmarshalable value
	row := &dailyStatsRow{
		Date:           "2026-02-20",
		LinesAdded:     42,
		ModelBreakdown: make(chan int), // channels cannot be JSON-marshaled
	}

	store.sendWrite(writeOp{opType: "dailyStats", dailyStats: row})
	time.Sleep(200 * time.Millisecond)

	var linesAdded int
	var modelJSON sql.NullString
	err := store.db.QueryRow("SELECT lines_added, model_breakdown FROM daily_stats WHERE date = ?", "2026-02-20").
		Scan(&linesAdded, &modelJSON)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if linesAdded != 42 {
		t.Errorf("lines_added: want 42, got %d", linesAdded)
	}
	if modelJSON.Valid {
		t.Error("model_breakdown should be NULL on marshal failure")
	}

	_ = ds // use the var
}

// --- Burn Rate Snapshot Write Tests ---

func TestWriteBurnRateSnapshot_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	br := burnrate.BurnRate{
		TotalCost:         25.50,
		HourlyRate:        5.10,
		Trend:             burnrate.TrendUp,
		TokenVelocity:     1500.0,
		DailyProjection:   122.40,
		MonthlyProjection: 3672.0,
		PerModel: []burnrate.ModelBurnRate{
			{Model: "opus", HourlyRate: 3.0, TotalCost: 15.0},
		},
	}

	store.WriteBurnRateSnapshot(br)
	time.Sleep(200 * time.Millisecond)

	var totalCost, hourlyRate, tokenVelocity, dailyProj, monthlyProj float64
	var trend int
	err := store.db.QueryRow(`
		SELECT total_cost, hourly_rate, trend, token_velocity, daily_projection, monthly_projection
		FROM burn_rate_snapshots ORDER BY id DESC LIMIT 1
	`).Scan(&totalCost, &hourlyRate, &trend, &tokenVelocity, &dailyProj, &monthlyProj)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if totalCost != 25.50 {
		t.Errorf("total_cost: want 25.50, got %f", totalCost)
	}
	if hourlyRate != 5.10 {
		t.Errorf("hourly_rate: want 5.10, got %f", hourlyRate)
	}
	if trend != 1 {
		t.Errorf("trend: want 1 (TrendUp), got %d", trend)
	}
	if tokenVelocity != 1500.0 {
		t.Errorf("token_velocity: want 1500.0, got %f", tokenVelocity)
	}
	if dailyProj != 122.40 {
		t.Errorf("daily_projection: want 122.40, got %f", dailyProj)
	}
	if monthlyProj != 3672.0 {
		t.Errorf("monthly_projection: want 3672.0, got %f", monthlyProj)
	}
}

func TestWriteBurnRateSnapshot_PerModelJSON(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	br := burnrate.BurnRate{
		PerModel: []burnrate.ModelBurnRate{
			{Model: "opus", HourlyRate: 3.0, TotalCost: 15.0},
			{Model: "sonnet", HourlyRate: 2.0, TotalCost: 10.0},
		},
	}

	store.WriteBurnRateSnapshot(br)
	time.Sleep(200 * time.Millisecond)

	var perModelJSON sql.NullString
	err := store.db.QueryRow("SELECT per_model FROM burn_rate_snapshots ORDER BY id DESC LIMIT 1").Scan(&perModelJSON)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !perModelJSON.Valid || perModelJSON.String == "" {
		t.Error("per_model should be non-null JSON")
	}
}

func TestWriteBurnRateSnapshot_NaNInfSanitized(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	br := burnrate.BurnRate{
		HourlyRate:      math.NaN(),
		DailyProjection: math.Inf(1),
		TokenVelocity:   math.Inf(-1),
	}

	store.WriteBurnRateSnapshot(br)
	time.Sleep(200 * time.Millisecond)

	var hourlyRate, dailyProj, tokenVel float64
	err := store.db.QueryRow(`
		SELECT hourly_rate, daily_projection, token_velocity
		FROM burn_rate_snapshots ORDER BY id DESC LIMIT 1
	`).Scan(&hourlyRate, &dailyProj, &tokenVel)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if hourlyRate != 0.0 {
		t.Errorf("hourly_rate NaN not sanitized: got %f", hourlyRate)
	}
	if dailyProj != 0.0 {
		t.Errorf("daily_projection Inf not sanitized: got %f", dailyProj)
	}
	if tokenVel != 0.0 {
		t.Errorf("token_velocity -Inf not sanitized: got %f", tokenVel)
	}
}

func TestWriteBurnRateSnapshot_JSONMarshalFailure(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	row := &burnRateSnapshotRow{
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		TotalCost:    5.0,
		HourlyRate:   1.0,
		PerModel:     make(chan int), // channels cannot be JSON-marshaled
	}

	store.sendWrite(writeOp{opType: "burnRateSnapshot", burnRate: row})
	time.Sleep(200 * time.Millisecond)

	var totalCost float64
	var perModelJSON sql.NullString
	err := store.db.QueryRow("SELECT total_cost, per_model FROM burn_rate_snapshots ORDER BY id DESC LIMIT 1").
		Scan(&totalCost, &perModelJSON)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if totalCost != 5.0 {
		t.Errorf("total_cost: want 5.0, got %f", totalCost)
	}
	if perModelJSON.Valid {
		t.Error("per_model should be NULL on marshal failure")
	}
}

// --- Alert History Write Tests ---

func TestWriteAlertHistory_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	alert := alerts.Alert{
		Rule:      "CostSurge",
		Severity:  "critical",
		Message:   "Cost surge detected: $5.00/hr",
		SessionID: "sess-123",
		FiredAt:   time.Now(),
	}

	store.PersistAlert(alert)
	time.Sleep(200 * time.Millisecond)

	var rule, severity, message, sessionID, firedAt string
	err := store.db.QueryRow(`
		SELECT rule, severity, message, session_id, fired_at
		FROM alert_history ORDER BY id DESC LIMIT 1
	`).Scan(&rule, &severity, &message, &sessionID, &firedAt)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if rule != "CostSurge" {
		t.Errorf("rule: want CostSurge, got %s", rule)
	}
	if severity != "critical" {
		t.Errorf("severity: want critical, got %s", severity)
	}
	if message != "Cost surge detected: $5.00/hr" {
		t.Errorf("message: want 'Cost surge detected: $5.00/hr', got %s", message)
	}
	if sessionID != "sess-123" {
		t.Errorf("session_id: want sess-123, got %s", sessionID)
	}
}

func TestWriteAlertHistory_EmptySessionID(t *testing.T) {
	store := newTestStore(t)
	defer func() { _ = store.Close() }()

	alert := alerts.Alert{
		Rule:      "ErrorStorm",
		Severity:  "critical",
		Message:   "Error storm detected",
		SessionID: "",
		FiredAt:   time.Now(),
	}

	store.PersistAlert(alert)
	time.Sleep(200 * time.Millisecond)

	var sessionID string
	err := store.db.QueryRow("SELECT session_id FROM alert_history ORDER BY id DESC LIMIT 1").Scan(&sessionID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if sessionID != "" {
		t.Errorf("session_id: want empty string, got %q", sessionID)
	}
}
