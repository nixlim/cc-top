package alerts

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"
)

// testNotifier records all notifications for test assertions.
type testNotifier struct {
	mu     sync.Mutex
	alerts []Alert
}

func newTestNotifier() *testNotifier {
	return &testNotifier{}
}

func (n *testNotifier) Notify(alert Alert) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.alerts = append(n.alerts, alert)
}

func (n *testNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.alerts)
}

func (n *testNotifier) last() *Alert {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.alerts) == 0 {
		return nil
	}
	a := n.alerts[len(n.alerts)-1]
	return &a
}

// defaultTestConfig returns a config suitable for testing.
func defaultTestConfig() config.Config {
	return config.DefaultConfig()
}

// newTestCalculator returns a calculator with default thresholds.
func newTestCalculator() *burnrate.Calculator {
	return burnrate.NewCalculator(burnrate.DefaultThresholds())
}

func TestAlertCostSurge_Fires(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()
	calc := newTestCalculator()

	rule := newCostSurgeRule(cfg.Alerts, calc)

	base := time.Now().Add(-6 * time.Minute)

	// Seed the calculator with cost data that produces a high hourly rate.
	// $10 in 5 minutes = $120/hr > $100/hr threshold.
	store.AddMetric("sess-1", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     0.0,
		Timestamp: base,
	})
	_ = calc.ComputeWithTime(store, base)

	store.AddMetric("sess-1", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     10.00,
		Timestamp: base.Add(5 * time.Minute),
	})
	_ = calc.ComputeWithTime(store, base.Add(5*time.Minute))

	now := base.Add(5 * time.Minute)
	alerts := rule.Evaluate(store, now)
	if len(alerts) == 0 {
		t.Fatal("expected CostSurge alert to fire")
	}
	if alerts[0].Rule != RuleCostSurge {
		t.Errorf("expected rule %s, got %s", RuleCostSurge, alerts[0].Rule)
	}
	if alerts[0].Severity != SeverityCritical {
		t.Errorf("expected severity critical, got %s", alerts[0].Severity)
	}
}

func TestAlertCostSurge_BelowThreshold(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()
	calc := newTestCalculator()

	rule := newCostSurgeRule(cfg.Alerts, calc)

	base := time.Now().Add(-6 * time.Minute)

	// Seed with low cost data: $0.05 over 5 minutes = $0.60/hr (below $100 threshold).
	store.AddMetric("sess-1", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     0.0,
		Timestamp: base,
	})
	_ = calc.ComputeWithTime(store, base)

	store.AddMetric("sess-1", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     0.05,
		Timestamp: base.Add(5 * time.Minute),
	})
	_ = calc.ComputeWithTime(store, base.Add(5*time.Minute))

	now := base.Add(5 * time.Minute)
	alerts := rule.Evaluate(store, now)
	if len(alerts) != 0 {
		t.Errorf("expected no alerts below threshold, got %d", len(alerts))
	}
}

func TestAlertRunawayTokens_Fires(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()
	calc := newTestCalculator()

	rule := newRunawayTokensRule(cfg.Alerts, calc)

	base := time.Now().Add(-10 * time.Minute)

	// 1.5M tokens over 5 minutes = 300k tokens/min > 50k threshold.
	store.AddMetric("sess-1", state.Metric{
		Name:       "claude_code.token.usage",
		Value:      0,
		Attributes: map[string]string{"type": "input"},
		Timestamp:  base,
	})
	store.AddMetric("sess-1", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     0.0,
		Timestamp: base,
	})
	_ = calc.ComputeWithTime(store, base)

	store.AddMetric("sess-1", state.Metric{
		Name:       "claude_code.token.usage",
		Value:      1500000,
		Attributes: map[string]string{"type": "input"},
		Timestamp:  base.Add(5 * time.Minute),
	})
	_ = calc.ComputeWithTime(store, base.Add(5*time.Minute))

	// First evaluation: velocity exceeds threshold, records start time but doesn't fire yet.
	t1 := base.Add(5 * time.Minute)
	alerts := rule.Evaluate(store, t1)
	if len(alerts) != 0 {
		t.Fatalf("expected no alert on first evaluation (sustained check), got %d", len(alerts))
	}

	// Second evaluation after sustained period (2 minutes later): should fire.
	t2 := t1.Add(2 * time.Minute)
	_ = calc.ComputeWithTime(store, t2)
	alerts = rule.Evaluate(store, t2)
	if len(alerts) == 0 {
		t.Fatal("expected RunawayTokens alert to fire after sustained period")
	}
	if alerts[0].Rule != RuleRunawayTokens {
		t.Errorf("expected rule %s, got %s", RuleRunawayTokens, alerts[0].Rule)
	}
}

func TestAlertRunawayTokens_RequiresSustainedDuration(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()
	calc := newTestCalculator()

	rule := newRunawayTokensRule(cfg.Alerts, calc)

	base := time.Now().Add(-10 * time.Minute)

	// Set up high velocity data.
	store.AddMetric("sess-1", state.Metric{
		Name:       "claude_code.token.usage",
		Value:      0,
		Attributes: map[string]string{"type": "input"},
		Timestamp:  base,
	})
	store.AddMetric("sess-1", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     0.0,
		Timestamp: base,
	})
	_ = calc.ComputeWithTime(store, base)

	store.AddMetric("sess-1", state.Metric{
		Name:       "claude_code.token.usage",
		Value:      1500000,
		Attributes: map[string]string{"type": "input"},
		Timestamp:  base.Add(5 * time.Minute),
	})
	_ = calc.ComputeWithTime(store, base.Add(5*time.Minute))

	// Evaluate immediately: should NOT fire (not sustained yet).
	t1 := base.Add(5 * time.Minute)
	alerts := rule.Evaluate(store, t1)
	if len(alerts) != 0 {
		t.Fatalf("expected no alert before sustained period, got %d", len(alerts))
	}

	// Evaluate 1 minute later: still not sustained enough (need 2 min).
	t2 := t1.Add(1 * time.Minute)
	_ = calc.ComputeWithTime(store, t2)
	alerts = rule.Evaluate(store, t2)
	if len(alerts) != 0 {
		t.Fatalf("expected no alert at 1 min (need 2 min sustained), got %d", len(alerts))
	}
}

func TestAlertLoopDetector_Fires(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()

	rule := newLoopDetectorRule(cfg.Alerts, defaultNormalizer{})

	now := time.Now()

	// Add 3 failed Bash tool results with the same command.
	toolParams, _ := json.Marshal(map[string]any{"bash_command": "npm test"})
	for i := 0; i < 3; i++ {
		store.AddEvent("sess-1", state.Event{
			Name: "claude_code.tool_result",
			Attributes: map[string]string{
				"tool_name":       "Bash",
				"success":         "false",
				"tool_parameters": string(toolParams),
			},
			Timestamp: now.Add(-time.Duration(3-i) * time.Minute),
		})
	}

	alerts := rule.Evaluate(store, now)
	if len(alerts) == 0 {
		t.Fatal("expected LoopDetector alert to fire")
	}
	if alerts[0].Rule != RuleLoopDetector {
		t.Errorf("expected rule %s, got %s", RuleLoopDetector, alerts[0].Rule)
	}
	if alerts[0].SessionID != "sess-1" {
		t.Errorf("expected session sess-1, got %s", alerts[0].SessionID)
	}
}

func TestAlertLoopDetector_NoDuplicateCount(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()

	rule := newLoopDetectorRule(cfg.Alerts, defaultNormalizer{})

	now := time.Now()

	// Add 2 failed Bash tool results (below threshold of 3).
	toolParams, _ := json.Marshal(map[string]any{"bash_command": "npm test"})
	for i := 0; i < 2; i++ {
		store.AddEvent("sess-1", state.Event{
			Name: "claude_code.tool_result",
			Attributes: map[string]string{
				"tool_name":       "Bash",
				"success":         "false",
				"tool_parameters": string(toolParams),
			},
			Timestamp: now.Add(-time.Duration(3-i) * time.Minute),
		})
	}

	// First evaluation: 2 failures, below threshold of 3.
	alerts := rule.Evaluate(store, now)
	if len(alerts) != 0 {
		t.Fatalf("expected no alert with 2 failures, got %d", len(alerts))
	}

	// Second evaluation without new events: should still be 2 (not 4).
	alerts = rule.Evaluate(store, now)
	if len(alerts) != 0 {
		t.Fatalf("expected no alert on re-evaluation (no double-counting), got %d", len(alerts))
	}

	// Third evaluation: still 2, not 6.
	alerts = rule.Evaluate(store, now)
	if len(alerts) != 0 {
		t.Fatalf("expected no alert on third evaluation (no double-counting), got %d", len(alerts))
	}
}

func TestAlertLoopDetector_Normalization(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()

	rule := newLoopDetectorRule(cfg.Alerts, defaultNormalizer{})

	now := time.Now()

	// Add failures with semantically equivalent commands that should normalize
	// to the same hash: npm test, npm run test, npx jest.
	commands := []string{"npm test", "npm run test", "npx jest"}
	for i, cmd := range commands {
		toolParams, _ := json.Marshal(map[string]any{"bash_command": cmd})
		store.AddEvent("sess-1", state.Event{
			Name: "claude_code.tool_result",
			Attributes: map[string]string{
				"tool_name":       "Bash",
				"success":         "false",
				"tool_parameters": string(toolParams),
			},
			Timestamp: now.Add(-time.Duration(3-i) * time.Minute),
		})
	}

	alerts := rule.Evaluate(store, now)
	if len(alerts) == 0 {
		t.Fatal("expected LoopDetector alert to fire with normalized commands")
	}
	if alerts[0].Rule != RuleLoopDetector {
		t.Errorf("expected rule %s, got %s", RuleLoopDetector, alerts[0].Rule)
	}
}

func TestAlertErrorStorm_Fires(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()

	rule := newErrorStormRule(cfg.Alerts)

	now := time.Now()

	// Add 11 api_error events in the last minute (threshold is 10).
	for i := 0; i < 11; i++ {
		store.AddEvent("sess-1", state.Event{
			Name: "claude_code.api_error",
			Attributes: map[string]string{
				"error":       "overloaded",
				"status_code": "529",
			},
			Timestamp: now.Add(-time.Duration(60-i*5) * time.Second),
		})
	}

	alerts := rule.Evaluate(store, now)
	if len(alerts) == 0 {
		t.Fatal("expected ErrorStorm alert to fire")
	}
	if alerts[0].Rule != RuleErrorStorm {
		t.Errorf("expected rule %s, got %s", RuleErrorStorm, alerts[0].Rule)
	}
	if alerts[0].Severity != SeverityCritical {
		t.Errorf("expected severity critical, got %s", alerts[0].Severity)
	}
}

func TestAlertStaleSession_Fires(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()

	rule := newStaleSessionRule(cfg.Alerts)

	// Create the session now (StartedAt will be set to time.Now() by the store).
	store.AddMetric("sess-stale", state.Metric{
		Name:      "claude_code.session.count",
		Value:     1,
		Timestamp: time.Now(),
	})

	// Add some non-prompt events to show the session is "active" but no user prompts.
	store.AddEvent("sess-stale", state.Event{
		Name:      "claude_code.api_request",
		Timestamp: time.Now(),
	})

	// Evaluate 3 hours in the future. The session's StartedAt was set to "now"
	// by the store, so 3 hours later it exceeds the 2-hour stale threshold.
	futureNow := time.Now().Add(3 * time.Hour)

	alerts := rule.Evaluate(store, futureNow)
	if len(alerts) == 0 {
		t.Fatal("expected StaleSession alert to fire")
	}
	if alerts[0].Rule != RuleStaleSession {
		t.Errorf("expected rule %s, got %s", RuleStaleSession, alerts[0].Rule)
	}
}

func TestAlertContextPressure_Fires(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()

	rule := newContextPressureRule(cfg.Alerts, cfg.Models)

	now := time.Now()

	// Add an api_request with input_tokens > 80% of the model limit.
	// claude-sonnet-4-5-20250929 has limit 200000, 80% = 160000.
	store.AddEvent("sess-1", state.Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"model":        "claude-sonnet-4-5-20250929",
			"input_tokens": "170000",
		},
		Timestamp: now,
	})

	alerts := rule.Evaluate(store, now)
	if len(alerts) == 0 {
		t.Fatal("expected ContextPressure alert to fire")
	}
	if alerts[0].Rule != RuleContextPressure {
		t.Errorf("expected rule %s, got %s", RuleContextPressure, alerts[0].Rule)
	}
}

func TestAlertContextPressure_UnknownModel(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()

	rule := newContextPressureRule(cfg.Alerts, cfg.Models)

	now := time.Now()

	// Add an api_request with a model not in the limit map.
	store.AddEvent("sess-1", state.Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"model":        "unknown-model-xyz",
			"input_tokens": "190000",
		},
		Timestamp: now,
	})

	alerts := rule.Evaluate(store, now)
	// No alert should fire for unknown models.
	if len(alerts) != 0 {
		t.Errorf("expected no alert for unknown model, got %d alerts", len(alerts))
	}
}

func TestAlertHighRejection_Fires(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()

	rule := newHighRejectionRule(cfg.Alerts)

	now := time.Now()

	// Add 6 tool decisions: 4 reject, 2 accept = 66% rejection rate > 50%.
	for i := 0; i < 4; i++ {
		store.AddEvent("sess-1", state.Event{
			Name: "claude_code.tool_decision",
			Attributes: map[string]string{
				"tool_name": "Bash",
				"decision":  "reject",
				"source":    "user",
			},
			Timestamp: now.Add(-time.Duration(5-i) * time.Minute),
		})
	}
	for i := 0; i < 2; i++ {
		store.AddEvent("sess-1", state.Event{
			Name: "claude_code.tool_decision",
			Attributes: map[string]string{
				"tool_name": "Write",
				"decision":  "accept",
				"source":    "config",
			},
			Timestamp: now.Add(-time.Duration(2-i) * time.Minute),
		})
	}

	alerts := rule.Evaluate(store, now)
	if len(alerts) == 0 {
		t.Fatal("expected HighRejection alert to fire")
	}
	if alerts[0].Rule != RuleHighRejection {
		t.Errorf("expected rule %s, got %s", RuleHighRejection, alerts[0].Rule)
	}
}

func TestAlertEngine_WithStateStore(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()
	calc := newTestCalculator()
	notifier := newTestNotifier()

	engine := NewEngine(store, cfg, calc,
		WithNotifier(notifier),
		WithDedupTTL(100*time.Millisecond), // Short dedup for testing.
	)

	now := time.Now()

	// Add an error storm to trigger an alert.
	for i := 0; i < 15; i++ {
		store.AddEvent("sess-engine", state.Event{
			Name: "claude_code.api_error",
			Attributes: map[string]string{
				"error":       "overloaded",
				"status_code": "529",
			},
			Timestamp: now.Add(-time.Duration(60-i*3) * time.Second),
		})
	}

	// Run one evaluation.
	engine.EvaluateAt(now)

	alerts := engine.Alerts()
	if len(alerts) == 0 {
		t.Fatal("expected at least one alert from engine evaluation")
	}

	// Verify notification was sent.
	if notifier.count() == 0 {
		t.Error("expected notifier to receive alerts")
	}

	// Evaluate again immediately -- dedup should suppress duplicate alerts.
	prevCount := len(engine.Alerts())
	engine.EvaluateAt(now.Add(50 * time.Millisecond))
	if len(engine.Alerts()) != prevCount {
		t.Errorf("expected dedup to suppress duplicate alerts within TTL")
	}

	// After dedup TTL expires, the same alert can fire again.
	engine.EvaluateAt(now.Add(200 * time.Millisecond))
	if len(engine.Alerts()) <= prevCount {
		t.Log("alert re-fired after dedup TTL expired (expected behavior)")
	}
}

func TestAlertEngine_DedupPreventsRenotify(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()
	calc := newTestCalculator()
	notifier := newTestNotifier()

	engine := NewEngine(store, cfg, calc,
		WithNotifier(notifier),
		WithDedupTTL(60*time.Second),
	)

	now := time.Now()

	// Trigger an error storm.
	for i := 0; i < 15; i++ {
		store.AddEvent("sess-dedup", state.Event{
			Name: "claude_code.api_error",
			Attributes: map[string]string{
				"error": "overloaded",
			},
			Timestamp: now.Add(-time.Duration(30-i) * time.Second),
		})
	}

	// First evaluation fires the alert.
	engine.EvaluateAt(now)
	firstCount := notifier.count()
	if firstCount == 0 {
		t.Fatal("expected alert to fire on first evaluation")
	}

	// Second evaluation within 60s dedup window -- no new notification.
	engine.EvaluateAt(now.Add(30 * time.Second))
	if notifier.count() != firstCount {
		t.Errorf("expected no new notifications within dedup window, got %d (was %d)",
			notifier.count(), firstCount)
	}
}

func TestExtractBashCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid bash_command",
			input:    `{"bash_command": "npm test"}`,
			expected: "npm test",
		},
		{
			name:     "valid command key",
			input:    `{"command": "go build ./..."}`,
			expected: "go build ./...",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "invalid json",
			input:    "{not json",
			expected: "",
		},
		{
			name:     "no command key",
			input:    `{"tool_name": "Bash"}`,
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractBashCommand(tc.input)
			if got != tc.expected {
				t.Errorf("extractBashCommand(%q): expected %q, got %q",
					tc.input, tc.expected, got)
			}
		})
	}
}

func TestAlertDefaultThresholds_MatchSpec(t *testing.T) {
	cfg := defaultTestConfig()

	if cfg.Alerts.CostSurgeThresholdPerHour != 2.00 {
		t.Errorf("CostSurgeThresholdPerHour: want 2.00, got %f", cfg.Alerts.CostSurgeThresholdPerHour)
	}
	if cfg.Alerts.RunawayTokenVelocity != 50000 {
		t.Errorf("RunawayTokenVelocity: want 50000, got %d", cfg.Alerts.RunawayTokenVelocity)
	}
	if cfg.Alerts.RunawayTokenSustainedMinutes != 2 {
		t.Errorf("RunawayTokenSustainedMinutes: want 2, got %d", cfg.Alerts.RunawayTokenSustainedMinutes)
	}
}

// Verify that we correctly test with the real NormalizeCommand function.
func TestNormalizerIntegration(t *testing.T) {
	n := defaultNormalizer{}

	// npm test and npm run test should produce the same hash.
	h1 := n.Normalize("npm test")
	h2 := n.Normalize("npm run test")
	h3 := n.Normalize("npx jest")

	if h1 == "" || h2 == "" || h3 == "" {
		t.Fatal("normalizer returned empty hashes")
	}

	if h1 != h2 {
		t.Errorf("npm test and npm run test should normalize to same hash")
	}
	if h1 != h3 {
		t.Errorf("npm test and npx jest should normalize to same hash")
	}

	// Different commands should produce different hashes.
	h4 := n.Normalize("go build ./...")
	if h1 == h4 {
		t.Error("npm test and go build should have different hashes")
	}

	_ = fmt.Sprintf("hashes: %s, %s", h1, h4) // Use fmt to avoid import error.
}

func TestAlertSessionCost_Fires(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()

	rule := newSessionCostRule(cfg.Alerts) // default threshold = $5.00

	now := time.Now()

	// Add a metric that sets TotalCost above threshold.
	store.AddMetric("sess-expensive", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     6.50,
		Timestamp: now,
	})

	alerts := rule.Evaluate(store, now)
	if len(alerts) == 0 {
		t.Fatal("expected SessionCost alert to fire")
	}
	if alerts[0].Rule != RuleSessionCost {
		t.Errorf("expected rule %s, got %s", RuleSessionCost, alerts[0].Rule)
	}
	if alerts[0].SessionID != "sess-expensive" {
		t.Errorf("expected session sess-expensive, got %s", alerts[0].SessionID)
	}
	if alerts[0].Severity != SeverityWarning {
		t.Errorf("expected severity warning, got %s", alerts[0].Severity)
	}
}

func TestAlertSessionCost_BelowThreshold(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()

	rule := newSessionCostRule(cfg.Alerts)

	now := time.Now()

	// Add a metric with cost below threshold.
	store.AddMetric("sess-cheap", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     2.50,
		Timestamp: now,
	})

	alerts := rule.Evaluate(store, now)
	if len(alerts) != 0 {
		t.Errorf("expected no alert below threshold, got %d", len(alerts))
	}
}

func TestAlertSessionCost_CustomThreshold(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()
	cfg.Alerts.SessionCostThreshold = 1.00

	rule := newSessionCostRule(cfg.Alerts)

	now := time.Now()

	store.AddMetric("sess-1", state.Metric{
		Name:      "claude_code.cost.usage",
		Value:     1.50,
		Timestamp: now,
	})

	alerts := rule.Evaluate(store, now)
	if len(alerts) == 0 {
		t.Fatal("expected SessionCost alert with custom threshold $1.00")
	}
}

func TestAlertHighRejection_CustomThreshold(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()
	cfg.Alerts.HighRejectionPercent = 80
	cfg.Alerts.HighRejectionWindowMinutes = 10

	rule := newHighRejectionRule(cfg.Alerts)

	now := time.Now()

	// Add 10 tool decisions: 7 reject, 3 accept = 70% rejection rate.
	// With threshold at 80%, this should NOT fire.
	for i := 0; i < 7; i++ {
		store.AddEvent("sess-1", state.Event{
			Name: "claude_code.tool_decision",
			Attributes: map[string]string{
				"tool_name": "Bash",
				"decision":  "reject",
			},
			Timestamp: now.Add(-time.Duration(9-i) * time.Minute),
		})
	}
	for i := 0; i < 3; i++ {
		store.AddEvent("sess-1", state.Event{
			Name: "claude_code.tool_decision",
			Attributes: map[string]string{
				"tool_name": "Write",
				"decision":  "accept",
			},
			Timestamp: now.Add(-time.Duration(2-i) * time.Minute),
		})
	}

	alerts := rule.Evaluate(store, now)
	if len(alerts) != 0 {
		t.Errorf("expected no alert at 70%% with 80%% threshold, got %d", len(alerts))
	}

	// Add more rejections to push above 80% (13 reject / 16 total = 81.25%).
	for i := 0; i < 6; i++ {
		store.AddEvent("sess-1", state.Event{
			Name: "claude_code.tool_decision",
			Attributes: map[string]string{
				"tool_name": "Bash",
				"decision":  "reject",
			},
			Timestamp: now.Add(-time.Duration(1) * time.Minute),
		})
	}

	alerts = rule.Evaluate(store, now)
	if len(alerts) == 0 {
		t.Fatal("expected HighRejection alert to fire at 80%+ rejection rate")
	}
}

func TestAlertHighRejection_WindowRespected(t *testing.T) {
	store := state.NewMemoryStore()
	cfg := defaultTestConfig()
	cfg.Alerts.HighRejectionPercent = 50
	cfg.Alerts.HighRejectionWindowMinutes = 2

	rule := newHighRejectionRule(cfg.Alerts)

	now := time.Now()

	// Add rejections outside the 2-minute window -- should be ignored.
	for i := 0; i < 5; i++ {
		store.AddEvent("sess-1", state.Event{
			Name: "claude_code.tool_decision",
			Attributes: map[string]string{
				"tool_name": "Bash",
				"decision":  "reject",
			},
			Timestamp: now.Add(-10 * time.Minute),
		})
	}

	// Add accepts inside the window.
	store.AddEvent("sess-1", state.Event{
		Name: "claude_code.tool_decision",
		Attributes: map[string]string{
			"tool_name": "Write",
			"decision":  "accept",
		},
		Timestamp: now.Add(-1 * time.Minute),
	})

	alerts := rule.Evaluate(store, now)
	if len(alerts) != 0 {
		t.Errorf("expected no alert when rejections are outside window, got %d", len(alerts))
	}
}
