package alerts

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/nixlim/cc-top/internal/burnrate"
	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"
)

// Rule evaluates state store data and returns any triggered alerts.
type Rule interface {
	// Evaluate checks the current state and returns alerts if thresholds are exceeded.
	Evaluate(store state.Store, now time.Time) []Alert
}

// costSurgeRule fires when the hourly cost rate exceeds a threshold.
type costSurgeRule struct {
	threshold  float64
	calculator *burnrate.Calculator
}

func newCostSurgeRule(cfg config.AlertsConfig, calculator *burnrate.Calculator) *costSurgeRule {
	return &costSurgeRule{
		threshold:  cfg.CostSurgeThresholdPerHour,
		calculator: calculator,
	}
}

func (r *costSurgeRule) Evaluate(store state.Store, now time.Time) []Alert {
	br := r.calculator.ComputeWithTime(store, now)
	if br.HourlyRate >= r.threshold {
		return []Alert{{
			Rule:     RuleCostSurge,
			Severity: SeverityCritical,
			Message:  fmt.Sprintf("Cost surge: $%.2f/hr exceeds threshold $%.2f/hr", br.HourlyRate, r.threshold),
			FiredAt:  now,
		}}
	}
	return nil
}

// runawayTokensRule fires when token velocity exceeds a threshold for a sustained period.
type runawayTokensRule struct {
	velocityThreshold float64
	sustainedMinutes  int
	calculator        *burnrate.Calculator
	exceededSince     map[string]time.Time
}

func newRunawayTokensRule(cfg config.AlertsConfig, calculator *burnrate.Calculator) *runawayTokensRule {
	return &runawayTokensRule{
		velocityThreshold: float64(cfg.RunawayTokenVelocity),
		sustainedMinutes:  cfg.RunawayTokenSustainedMinutes,
		calculator:        calculator,
		exceededSince:     make(map[string]time.Time),
	}
}

func (r *runawayTokensRule) Evaluate(store state.Store, now time.Time) []Alert {
	br := r.calculator.ComputeWithTime(store, now)
	key := "" // global key
	if br.TokenVelocity >= r.velocityThreshold {
		if _, ok := r.exceededSince[key]; !ok {
			r.exceededSince[key] = now
		}
		if now.Sub(r.exceededSince[key]) >= time.Duration(r.sustainedMinutes)*time.Minute {
			return []Alert{{
				Rule:     RuleRunawayTokens,
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("Runaway tokens: %.0f tokens/min exceeds threshold %.0f for %d+ min", br.TokenVelocity, r.velocityThreshold, r.sustainedMinutes),
				FiredAt:  now,
			}}
		}
	} else {
		delete(r.exceededSince, key)
	}
	return nil
}

// loopDetectorRule fires when the same command hash fails repeatedly within a time window.
type loopDetectorRule struct {
	threshold  int
	windowMins int
	normalizer CommandNormalizer

	// Per-session tracking: sessionID -> commandHash -> []failureTimestamp
	mu            sync.Mutex
	failures      map[string]map[string][]time.Time
	lastProcessed map[string]int // sessionID -> number of events already processed
}

func newLoopDetectorRule(cfg config.AlertsConfig, normalizer CommandNormalizer) *loopDetectorRule {
	return &loopDetectorRule{
		threshold:     cfg.LoopDetectorThreshold,
		windowMins:    cfg.LoopDetectorWindowMinutes,
		normalizer:    normalizer,
		failures:      make(map[string]map[string][]time.Time),
		lastProcessed: make(map[string]int),
	}
}

func (r *loopDetectorRule) Evaluate(store state.Store, now time.Time) []Alert {
	r.mu.Lock()
	defer r.mu.Unlock()

	window := time.Duration(r.windowMins) * time.Minute
	var alerts []Alert

	for _, session := range store.ListSessions() {
		// Only process new events since last evaluation.
		start := r.lastProcessed[session.SessionID]
		events := session.Events

		for i := start; i < len(events); i++ {
			evt := events[i]
			if evt.Name != "claude_code.tool_result" {
				continue
			}
			if evt.Attributes["tool_name"] != "Bash" {
				continue
			}
			if evt.Attributes["success"] == "true" {
				continue
			}

			// Extract bash_command from tool_parameters.
			command := extractBashCommand(evt.Attributes["tool_parameters"])
			if command == "" {
				continue
			}

			hash := r.normalizer.Normalize(command)
			if hash == "" {
				continue
			}

			// Record the failure.
			if r.failures[session.SessionID] == nil {
				r.failures[session.SessionID] = make(map[string][]time.Time)
			}

			r.failures[session.SessionID][hash] = append(
				r.failures[session.SessionID][hash], evt.Timestamp)
		}

		r.lastProcessed[session.SessionID] = len(events)

		// Check for threshold breaches within the window.
		if sessionFailures, ok := r.failures[session.SessionID]; ok {
			for hash, timestamps := range sessionFailures {
				// Prune old timestamps.
				pruned := pruneTimestamps(timestamps, now.Add(-window))
				sessionFailures[hash] = pruned

				if len(pruned) >= r.threshold {
					alerts = append(alerts, Alert{
						Rule:      RuleLoopDetector,
						Severity:  SeverityWarning,
						SessionID: session.SessionID,
						Message:   fmt.Sprintf("Loop detected: same command failed %d times in %d min", len(pruned), r.windowMins),
						FiredAt:   now,
					})
				}
			}
		}
	}

	return alerts
}

// extractBashCommand extracts the bash_command field from a JSON tool_parameters string.
func extractBashCommand(toolParams string) string {
	if toolParams == "" {
		return ""
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(toolParams), &params); err != nil {
		return ""
	}
	cmd, ok := params["bash_command"]
	if !ok {
		// Try "command" as alternative key.
		cmd, ok = params["command"]
		if !ok {
			return ""
		}
	}
	s, ok := cmd.(string)
	if !ok {
		return ""
	}
	return s
}

// errorStormRule fires when too many api_error events occur in a short window.
type errorStormRule struct {
	threshold int
	window    time.Duration
}

func newErrorStormRule(cfg config.AlertsConfig) *errorStormRule {
	return &errorStormRule{
		threshold: cfg.ErrorStormCount,
		window:    1 * time.Minute,
	}
}

func (r *errorStormRule) Evaluate(store state.Store, now time.Time) []Alert {
	cutoff := now.Add(-r.window)
	var alerts []Alert

	for _, session := range store.ListSessions() {
		count := 0
		for _, evt := range session.Events {
			if evt.Name == "claude_code.api_error" && !evt.Timestamp.Before(cutoff) {
				count++
			}
		}
		if count > r.threshold {
			alerts = append(alerts, Alert{
				Rule:      RuleErrorStorm,
				Severity:  SeverityCritical,
				SessionID: session.SessionID,
				Message:   fmt.Sprintf("Error storm: %d API errors in 1 minute (threshold %d)", count, r.threshold),
				FiredAt:   now,
			})
		}
	}

	return alerts
}

// staleSessionRule fires when a session is active for too long without user prompts.
type staleSessionRule struct {
	maxHours int
}

func newStaleSessionRule(cfg config.AlertsConfig) *staleSessionRule {
	return &staleSessionRule{
		maxHours: cfg.StaleSessionHours,
	}
}

func (r *staleSessionRule) Evaluate(store state.Store, now time.Time) []Alert {
	threshold := time.Duration(r.maxHours) * time.Hour
	var alerts []Alert

	for _, session := range store.ListSessions() {
		if session.Exited {
			continue
		}

		age := now.Sub(session.StartedAt)
		if age < threshold {
			continue
		}

		// Check if any user_prompt events exist.
		hasPrompt := false
		for _, evt := range session.Events {
			if evt.Name == "claude_code.user_prompt" {
				hasPrompt = true
				break
			}
		}

		if !hasPrompt {
			alerts = append(alerts, Alert{
				Rule:      RuleStaleSession,
				Severity:  SeverityWarning,
				SessionID: session.SessionID,
				Message:   fmt.Sprintf("Stale session: active for %.0f hours with no user prompts", age.Hours()),
				FiredAt:   now,
			})
		}
	}

	return alerts
}

// contextPressureRule fires when input_tokens approach the model's context limit.
type contextPressureRule struct {
	pressurePercent int
	modelLimits     map[string]int

	// Track models we have already warned about (one-time warning).
	mu             sync.Mutex
	warnedModels   map[string]bool
}

func newContextPressureRule(cfg config.AlertsConfig, modelLimits map[string]int) *contextPressureRule {
	return &contextPressureRule{
		pressurePercent: cfg.ContextPressurePercent,
		modelLimits:     modelLimits,
		warnedModels:    make(map[string]bool),
	}
}

func (r *contextPressureRule) Evaluate(store state.Store, now time.Time) []Alert {
	r.mu.Lock()
	defer r.mu.Unlock()

	var alerts []Alert

	for _, session := range store.ListSessions() {
		for _, evt := range session.Events {
			if evt.Name != "claude_code.api_request" {
				continue
			}

			model := evt.Attributes["model"]
			if model == "" {
				continue
			}

			limit, ok := r.modelLimits[model]
			if !ok {
				// Model not in limit map: log one-time warning, no alert.
				if !r.warnedModels[model] {
					log.Printf("WARNING: model %q not in context limit map, skipping context pressure check", model)
					r.warnedModels[model] = true
				}
				continue
			}

			inputTokensStr := evt.Attributes["input_tokens"]
			if inputTokensStr == "" {
				continue
			}
			inputTokens, err := strconv.ParseInt(inputTokensStr, 10, 64)
			if err != nil {
				continue
			}

			threshold := float64(limit) * float64(r.pressurePercent) / 100.0
			if float64(inputTokens) > threshold {
				pct := float64(inputTokens) / float64(limit) * 100.0
				alerts = append(alerts, Alert{
					Rule:      RuleContextPressure,
					Severity:  SeverityWarning,
					SessionID: session.SessionID,
					Message:   fmt.Sprintf("Context pressure: %d input tokens (%.0f%% of %d limit for %s)", inputTokens, pct, limit, model),
					FiredAt:   now,
				})
			}
		}
	}

	return alerts
}

// sessionCostRule fires when a session's total cost exceeds a configured threshold.
type sessionCostRule struct {
	threshold float64
}

func newSessionCostRule(cfg config.AlertsConfig) *sessionCostRule {
	return &sessionCostRule{threshold: cfg.SessionCostThreshold}
}

func (r *sessionCostRule) Evaluate(store state.Store, now time.Time) []Alert {
	var alerts []Alert
	for _, session := range store.ListSessions() {
		if session.TotalCost > r.threshold {
			alerts = append(alerts, Alert{
				Rule:      RuleSessionCost,
				Severity:  SeverityWarning,
				SessionID: session.SessionID,
				Message:   fmt.Sprintf("Session cost: $%.2f exceeds threshold $%.2f", session.TotalCost, r.threshold),
				FiredAt:   now,
			})
		}
	}
	return alerts
}

// highRejectionRule fires when tool rejection rate exceeds a configurable threshold in a configurable window.
type highRejectionRule struct {
	window  time.Duration
	percent float64
}

func newHighRejectionRule(cfg config.AlertsConfig) *highRejectionRule {
	return &highRejectionRule{
		window:  time.Duration(cfg.HighRejectionWindowMinutes) * time.Minute,
		percent: float64(cfg.HighRejectionPercent) / 100.0,
	}
}

func (r *highRejectionRule) Evaluate(store state.Store, now time.Time) []Alert {
	cutoff := now.Add(-r.window)
	var alerts []Alert

	windowMins := int(r.window / time.Minute)

	for _, session := range store.ListSessions() {
		var total, rejects int
		for _, evt := range session.Events {
			if evt.Name != "claude_code.tool_decision" {
				continue
			}
			if evt.Timestamp.Before(cutoff) {
				continue
			}
			total++
			if evt.Attributes["decision"] == "reject" {
				rejects++
			}
		}

		if total > 0 {
			rate := float64(rejects) / float64(total)
			if rate > r.percent {
				alerts = append(alerts, Alert{
					Rule:      RuleHighRejection,
					Severity:  SeverityWarning,
					SessionID: session.SessionID,
					Message:   fmt.Sprintf("High rejection rate: %.0f%% of tool decisions rejected (%d/%d in %dmin)", rate*100, rejects, total, windowMins),
					FiredAt:   now,
				})
			}
		}
	}

	return alerts
}

// pruneTimestamps removes timestamps older than cutoff.
func pruneTimestamps(timestamps []time.Time, cutoff time.Time) []time.Time {
	n := 0
	for _, ts := range timestamps {
		if !ts.Before(cutoff) {
			timestamps[n] = ts
			n++
		}
	}
	return timestamps[:n]
}
