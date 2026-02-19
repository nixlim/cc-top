package storage

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"
)

func NewStore(cfg config.StorageConfig) (state.Store, bool, error) {
	if cfg.DBPath == "" {
		return state.NewMemoryStore(), false, nil
	}

	dbPath := expandTilde(cfg.DBPath)

	store, err := NewSQLiteStore(dbPath, cfg.RetentionDays, cfg.SummaryRetentionDays)
	if err != nil {
		log.Printf("WARNING: SQLite storage unavailable (%v), falling back to in-memory store", err)
		return state.NewMemoryStore(), false, nil
	}

	return store, true, nil
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
