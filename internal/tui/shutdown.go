package tui

import (
	"context"
	"time"
)

// ShutdownManager coordinates graceful shutdown of all cc-top components.
// It handles the drain period, force close, port release, and terminal restoration.
type ShutdownManager struct {
	// DrainTimeout is the maximum time to wait for in-flight requests to complete.
	DrainTimeout time.Duration

	// StopReceiver stops the OTLP receiver from accepting new connections.
	StopReceiver func(ctx context.Context) error

	// StopScanner stops the process scanner.
	StopScanner func()

	// Cleanup performs any additional cleanup (e.g., releasing resources).
	Cleanup func()
}

// NewShutdownManager creates a ShutdownManager with a 5-second drain timeout.
func NewShutdownManager() *ShutdownManager {
	return &ShutdownManager{
		DrainTimeout: 5 * time.Second,
	}
}

// Shutdown performs a graceful shutdown in the correct order:
// 1. Stop accepting new connections
// 2. Drain in-flight requests (up to DrainTimeout)
// 3. Force close remaining connections
// 4. Stop background tasks
// 5. Run cleanup
func (sm *ShutdownManager) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), sm.DrainTimeout)
	defer cancel()

	// Step 1 + 2: Stop receiver with drain.
	if sm.StopReceiver != nil {
		_ = sm.StopReceiver(ctx)
	}

	// Step 3: Stop scanner.
	if sm.StopScanner != nil {
		sm.StopScanner()
	}

	// Step 4: Run cleanup.
	if sm.Cleanup != nil {
		sm.Cleanup()
	}

	return nil
}
