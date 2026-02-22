package alerts

import "time"

// Alert rule name constants.
const (
	RuleCostSurge       = "CostSurge"
	RuleRunawayTokens   = "RunawayTokens"
	RuleLoopDetector    = "LoopDetector"
	RuleErrorStorm      = "ErrorStorm"
	RuleStaleSession    = "StaleSession"
	RuleContextPressure = "ContextPressure"
	RuleHighRejection   = "HighRejection"
	RuleSessionCost     = "SessionCost"
)

// Alert severity constants.
const (
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Alert represents a triggered alert from the alert engine.
type Alert struct {
	Rule      string // CostSurge, RunawayTokens, LoopDetector, etc.
	Severity  string // warning, critical
	Message   string
	SessionID string // empty for global alerts
	FiredAt   time.Time
}

// alertKey returns a deduplication key for this alert, combining the rule name
// and session ID. Two alerts with the same key within the dedup window are
// considered duplicates.
func (a Alert) alertKey() string {
	return a.Rule + ":" + a.SessionID
}

// CommandNormalizer normalizes bash commands for loop detection grouping.
// This interface allows for testing with a mock when the real normalizer
// from Agent 3 is not yet available.
type CommandNormalizer interface {
	Normalize(command string) string
}

// defaultNormalizer wraps the NormalizeCommand function from normalizer.go.
type defaultNormalizer struct{}

func (d defaultNormalizer) Normalize(command string) string {
	return NormalizeCommand(command)
}

// AlertPersister persists fired alerts to durable storage.
type AlertPersister interface {
	PersistAlert(alert Alert)
}

// Notifier sends alert notifications via platform-specific mechanisms.
type Notifier interface {
	// Notify sends an alert notification. Implementations must be non-blocking.
	Notify(alert Alert)
}
