//go:build darwin

package alerts

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// OSAScriptNotifier sends macOS system notifications via osascript.
// Notifications are sent in a non-blocking goroutine to prevent
// the alert engine from stalling on slow notification delivery.
type OSAScriptNotifier struct {
	// enabled controls whether notifications are actually sent.
	// When false, Notify is a no-op.
	enabled bool
}

// NewOSAScriptNotifier creates a new macOS notification sender.
// If enabled is false, notifications are silently dropped.
func NewOSAScriptNotifier(enabled bool) *OSAScriptNotifier {
	return &OSAScriptNotifier{enabled: enabled}
}

// Notify sends a macOS notification for the given alert.
// The call returns immediately; the osascript command runs in a background
// goroutine. Errors are logged but do not affect the alert engine.
func (n *OSAScriptNotifier) Notify(alert Alert) {
	if !n.enabled {
		return
	}

	// Build notification text.
	title := fmt.Sprintf("cc-top: %s", alert.Rule)
	subtitle := ""
	if alert.SessionID != "" {
		subtitle = fmt.Sprintf("Session: %s", truncateSessionID(alert.SessionID))
	}
	message := alert.Message

	go func() {
		if err := sendOSANotification(title, subtitle, message); err != nil {
			log.Printf("WARNING: failed to send macOS notification: %v", err)
		}
	}()
}

// sendOSANotification executes osascript to display a macOS notification.
func sendOSANotification(title, subtitle, message string) error {
	// Escape double quotes in the message to prevent AppleScript injection.
	title = escapeAppleScript(title)
	subtitle = escapeAppleScript(subtitle)
	message = escapeAppleScript(message)

	script := fmt.Sprintf(
		`display notification "%s" with title "%s"`,
		message, title,
	)
	if subtitle != "" {
		script = fmt.Sprintf(
			`display notification "%s" with title "%s" subtitle "%s"`,
			message, title, subtitle,
		)
	}

	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

// escapeAppleScript escapes characters that could break AppleScript strings.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// truncateSessionID shortens a session ID for display in notifications.
func truncateSessionID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "..."
}
