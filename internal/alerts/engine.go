// Package alerts implements the cc-top alert engine with configurable rules
// for detecting anomalous patterns in Claude Code telemetry data. It evaluates
// rules periodically and optionally fires macOS system notifications.
package alerts

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"
)

// Engine evaluates alert rules periodically against the state store.
// It deduplicates alerts (same rule+session within 60s) and optionally
// sends macOS notifications via the configured Notifier.
type Engine struct {
	store      state.Store
	rules      []Rule
	notifier   Notifier
	interval   time.Duration
	dedupTTL   time.Duration

	mu         sync.RWMutex
	alerts     []Alert
	lastFired  map[string]time.Time // alertKey -> last fire time for dedup

	cancel     context.CancelFunc
	done       chan struct{}
}

// EngineOption configures the alert engine.
type EngineOption func(*Engine)

// WithNotifier sets the notifier for system notifications.
func WithNotifier(n Notifier) EngineOption {
	return func(e *Engine) {
		e.notifier = n
	}
}

// WithInterval sets the evaluation interval.
func WithInterval(d time.Duration) EngineOption {
	return func(e *Engine) {
		e.interval = d
	}
}

// WithDedupTTL sets the deduplication window. Alerts with the same key
// within this window are suppressed.
func WithDedupTTL(d time.Duration) EngineOption {
	return func(e *Engine) {
		e.dedupTTL = d
	}
}

// NewEngine creates a new alert engine with all built-in rules configured
// from the provided config. The calculator is used for cost/token rate rules.
func NewEngine(store state.Store, cfg config.Config, calculator *burnrate.Calculator, opts ...EngineOption) *Engine {
	e := &Engine{
		store:     store,
		interval:  1 * time.Second,
		dedupTTL:  60 * time.Second,
		lastFired: make(map[string]time.Time),
		done:      make(chan struct{}),
	}

	for _, opt := range opts {
		opt(e)
	}

	normalizer := defaultNormalizer{}

	e.rules = []Rule{
		newCostSurgeRule(cfg.Alerts, calculator),
		newRunawayTokensRule(cfg.Alerts, calculator),
		newLoopDetectorRule(cfg.Alerts, normalizer),
		newErrorStormRule(cfg.Alerts),
		newStaleSessionRule(cfg.Alerts),
		newContextPressureRule(cfg.Alerts, cfg.Models),
		newHighRejectionRule(),
	}

	return e
}

// Start begins periodic evaluation of alert rules. It runs until Stop is called
// or the context is cancelled.
func (e *Engine) Start(ctx context.Context) {
	ctx, e.cancel = context.WithCancel(ctx)

	go func() {
		defer close(e.done)
		ticker := time.NewTicker(e.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.evaluate(time.Now())
			}
		}
	}()
}

// Stop halts the alert engine's periodic evaluation.
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
		<-e.done
	}
}

// evaluate runs all rules and processes any triggered alerts.
func (e *Engine) evaluate(now time.Time) {
	var newAlerts []Alert

	for _, rule := range e.rules {
		triggered := rule.Evaluate(e.store, now)
		for _, alert := range triggered {
			if e.isDuplicate(alert) {
				continue
			}
			e.recordFired(alert)
			newAlerts = append(newAlerts, alert)
		}
	}

	if len(newAlerts) > 0 {
		e.mu.Lock()
		e.alerts = append(e.alerts, newAlerts...)
		e.mu.Unlock()

		if e.notifier != nil {
			for _, alert := range newAlerts {
				e.notifier.Notify(alert)
			}
		}
	}
}

// EvaluateNow runs a single evaluation cycle immediately. This is primarily
// useful for testing without waiting for the ticker.
func (e *Engine) EvaluateNow() {
	e.evaluate(time.Now())
}

// EvaluateAt runs a single evaluation cycle at the specified time.
// Useful for deterministic testing.
func (e *Engine) EvaluateAt(now time.Time) {
	e.evaluate(now)
}

// Alerts returns a snapshot of all alerts that have been fired.
func (e *Engine) Alerts() []Alert {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]Alert, len(e.alerts))
	copy(result, e.alerts)
	return result
}

// isDuplicate checks whether the same alert (rule+session) was fired within
// the dedup window.
func (e *Engine) isDuplicate(alert Alert) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	key := alert.alertKey()
	lastFired, ok := e.lastFired[key]
	if !ok {
		return false
	}
	return alert.FiredAt.Sub(lastFired) < e.dedupTTL
}

// recordFired marks an alert as fired for deduplication purposes.
func (e *Engine) recordFired(alert Alert) {
	e.mu.Lock()
	defer e.mu.Unlock()

	key := alert.alertKey()
	e.lastFired[key] = alert.FiredAt

	// Prune old dedup entries to prevent unbounded growth.
	for k, t := range e.lastFired {
		if alert.FiredAt.Sub(t) > 2*e.dedupTTL {
			delete(e.lastFired, k)
		}
	}

	log.Printf("ALERT [%s] %s: %s", alert.Severity, alert.Rule, alert.Message)
}
