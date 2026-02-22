package storage

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/state"
	"github.com/nixlim/cc-top/internal/stats"
)

func TestSQLiteStore_Close_FlushesWrites(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	for range 10 {
		m := state.Metric{
			Name:      "test.metric",
			Value:     1.0,
			Timestamp: time.Now(),
		}
		store.AddMetric("sess-flush", m)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM metrics WHERE session_id = ?", "sess-flush").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 10 {
		t.Errorf("not all writes flushed: want 10, got %d", count)
	}
}

func TestSQLiteStore_Close_RunsAggregation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	m := state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     2.5,
		Timestamp: time.Now(),
	}
	store.AddMetric("sess-agg", m)

	time.Sleep(150 * time.Millisecond)

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM daily_summaries WHERE session_id = ?", "sess-agg").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count == 0 {
		t.Error("daily aggregation not run on Close — no daily_summaries row found")
	}
}

func TestSQLiteStore_Close_EmptyChannel(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	start := time.Now()
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("empty channel close too slow: %v (want <2s)", elapsed)
	}
}

func TestSQLiteStore_AddMetric_AfterClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("AddMetric after Close panicked: %v", r)
		}
	}()

	m := state.Metric{
		Name:      "test.metric",
		Value:     1.0,
		Timestamp: time.Now(),
	}
	store.AddMetric("sess-post-close", m)

	session := store.GetSession("sess-post-close")
	if session == nil {
		t.Error("session not in memory after post-close AddMetric")
	}
}

func TestSQLiteStore_Close_TimesOutDrain(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := newSQLiteStoreWithChannelSize(dbPath, 5, 7, 90)
	if err != nil {
		t.Fatalf("newSQLiteStoreWithChannelSize failed: %v", err)
	}

	for range 3 {
		m := state.Metric{
			Name:      "test.metric",
			Value:     1.0,
			Timestamp: time.Now(),
		}
		store.AddMetric("sess-timeout", m)
	}

	start := time.Now()
	err = store.Close()
	elapsed := time.Since(start)

	if err != nil {
		t.Logf("Close returned error (may be expected): %v", err)
	}

	if elapsed > 15*time.Second {
		t.Errorf("Close took too long: %v (drain timeout should be 10s max)", elapsed)
	}
}

// --- Shutdown Sequence Tests (v63.8) ---

func TestClose_FinalBurnRateSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	var called atomic.Bool
	store.SetBurnRateSnapshotFunc(func() burnrate.BurnRate {
		called.Store(true)
		return burnrate.BurnRate{
			TotalCost:   50.0,
			HourlyRate:  10.0,
			Trend:       burnrate.TrendDown,
			PerModel: []burnrate.ModelBurnRate{
				{Model: "opus", HourlyRate: 10.0, TotalCost: 50.0},
			},
		}
	})

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !called.Load() {
		t.Error("burn rate snapshot callback not called during Close")
	}

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	var totalCost float64
	err = db.QueryRow("SELECT total_cost FROM burn_rate_snapshots ORDER BY id DESC LIMIT 1").Scan(&totalCost)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if totalCost != 50.0 {
		t.Errorf("total_cost: want 50.0, got %f", totalCost)
	}
}

func TestClose_FinalStatsSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	var called atomic.Bool
	store.SetStatsSnapshotFunc(func() stats.DashboardStats {
		called.Store(true)
		return stats.DashboardStats{
			LinesAdded: 999,
			ModelBreakdown: []stats.ModelStats{
				{Model: "opus", TotalCost: 25.0},
			},
		}
	})

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !called.Load() {
		t.Error("stats snapshot callback not called during Close")
	}

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	today := time.Now().Format("2006-01-02")
	var linesAdded int
	err = db.QueryRow("SELECT lines_added FROM daily_stats WHERE date = ?", today).Scan(&linesAdded)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if linesAdded != 999 {
		t.Errorf("lines_added: want 999, got %d", linesAdded)
	}
}

func TestClose_NilCallbacksNoError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	// Don't set any callbacks
	if err := store.Close(); err != nil {
		t.Fatalf("Close with nil callbacks should not error: %v", err)
	}
}

func TestClose_StopsBurnRateTicker(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	store.SetBurnRateSnapshotFunc(func() burnrate.BurnRate {
		return burnrate.BurnRate{TotalCost: 1.0}
	})
	store.StartBurnRateSnapshots()

	if store.burnRateTicker == nil {
		t.Fatal("burn rate ticker should exist after StartBurnRateSnapshots")
	}

	start := time.Now()
	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	elapsed := time.Since(start)

	// Close should complete reasonably quickly
	if elapsed > 10*time.Second {
		t.Errorf("Close took too long with burn rate ticker: %v", elapsed)
	}
}

func TestClose_ShutdownSequenceOrder(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	var order []string

	store.SetBurnRateSnapshotFunc(func() burnrate.BurnRate {
		order = append(order, "burnRate")
		return burnrate.BurnRate{TotalCost: 1.0}
	})

	store.SetStatsSnapshotFunc(func() stats.DashboardStats {
		order = append(order, "stats")
		return stats.DashboardStats{LinesAdded: 1}
	})

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify burn rate is called before stats (steps 2 and 3)
	if len(order) != 2 {
		t.Fatalf("expected 2 callbacks, got %d", len(order))
	}
	if order[0] != "burnRate" {
		t.Errorf("first callback should be burnRate, got %s", order[0])
	}
	if order[1] != "stats" {
		t.Errorf("second callback should be stats, got %s", order[1])
	}
}

func TestSendFinalWrite_DoesNotCheckClosedFlag(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7, 90)
	if err != nil {
		t.Fatalf("NewSQLiteStore failed: %v", err)
	}

	// Set both callbacks that use sendFinalWrite
	store.SetBurnRateSnapshotFunc(func() burnrate.BurnRate {
		return burnrate.BurnRate{TotalCost: 77.0}
	})
	store.SetStatsSnapshotFunc(func() stats.DashboardStats {
		return stats.DashboardStats{
			LinesAdded: 88,
			ModelBreakdown: []stats.ModelStats{
				{Model: "opus", TotalCost: 33.0},
			},
		}
	})

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Reopen and verify both final writes were persisted
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	var burnCost float64
	err = db.QueryRow("SELECT total_cost FROM burn_rate_snapshots ORDER BY id DESC LIMIT 1").Scan(&burnCost)
	if err != nil {
		t.Fatalf("query burn rate: %v", err)
	}
	if burnCost != 77.0 {
		t.Errorf("burn rate total_cost: want 77.0, got %f", burnCost)
	}

	today := time.Now().Format("2006-01-02")
	var linesAdded int
	err = db.QueryRow("SELECT lines_added FROM daily_stats WHERE date = ?", today).Scan(&linesAdded)
	if err != nil {
		t.Fatalf("query daily stats: %v", err)
	}
	if linesAdded != 88 {
		t.Errorf("lines_added: want 88, got %d", linesAdded)
	}
}
