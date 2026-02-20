//go:build darwin

package alerts

import (
	"testing"
	"time"
)

func TestAlertNotification_OSAScript(t *testing.T) {
	// Test the notifier interface and AppleScript string escaping.
	// We don't actually run osascript in tests to avoid UI popups.
	notifier := NewOSAScriptNotifier(false) // disabled = no-op

	alert := Alert{
		Rule:      RuleCostSurge,
		Severity:  SeverityCritical,
		Message:   `Cost surge: $5.00/hr exceeds threshold $2.00/hr with "special" chars`,
		SessionID: "sess-notification-test-1234567890",
		FiredAt:   time.Now(),
	}

	// Should not panic even with special characters.
	notifier.Notify(alert)

	// Test escaping function.
	escaped := escapeAppleScript(`He said "hello" and \n stuff`)
	expected := `He said \"hello\" and \\n stuff`
	if escaped != expected {
		t.Errorf("escapeAppleScript: expected %q, got %q", expected, escaped)
	}

	// Test with enabled notifier (will attempt osascript but that's fine in CI).
	enabledNotifier := NewOSAScriptNotifier(true)
	if enabledNotifier.enabled != true {
		t.Error("expected notifier to be enabled")
	}

	// Verify the constructor works correctly.
	disabledNotifier := NewOSAScriptNotifier(false)
	if disabledNotifier.enabled != false {
		t.Error("expected notifier to be disabled")
	}
}
