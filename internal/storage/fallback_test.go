package storage

import (
	"path/filepath"
	"testing"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"
)

func TestFallback_SQLiteSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := config.StorageConfig{
		DBPath:               dbPath,
		RetentionDays:        7,
		SummaryRetentionDays: 90,
	}

	store, isPersistent, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	if !isPersistent {
		t.Error("expected isPersistent=true for valid DB path")
	}

	if _, ok := store.(*SQLiteStore); !ok {
		t.Errorf("expected *SQLiteStore, got %T", store)
	}
}

func TestFallback_UnwritablePath(t *testing.T) {
	cfg := config.StorageConfig{
		DBPath:               "/nonexistent/deeply/nested/unwritable/path/test.db",
		RetentionDays:        7,
		SummaryRetentionDays: 90,
	}

	store, isPersistent, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore should not return error on fallback: %v", err)
	}
	defer func() { _ = store.Close() }()

	if isPersistent {
		t.Error("expected isPersistent=false for unwritable path")
	}

	if _, ok := store.(*state.MemoryStore); !ok {
		t.Errorf("expected *state.MemoryStore fallback, got %T", store)
	}
}

func TestFallback_ExplicitInMemory(t *testing.T) {
	cfg := config.StorageConfig{
		DBPath:               "",
		RetentionDays:        7,
		SummaryRetentionDays: 90,
	}

	store, isPersistent, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	if isPersistent {
		t.Error("expected isPersistent=false for empty db_path")
	}

	if _, ok := store.(*state.MemoryStore); !ok {
		t.Errorf("expected *state.MemoryStore, got %T", store)
	}
}
