package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/state"
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
