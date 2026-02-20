//go:build linux

package alerts

import (
	"fmt"
	"log"
	"os/exec"
)

// NotifySendNotifier sends Linux desktop notifications via notify-send.
// Notifications are sent in a non-blocking goroutine to prevent
// the alert engine from stalling on slow notification delivery.
type NotifySendNotifier struct {
	// enabled controls whether notifications are actually sent.
	// When false, Notify is a no-op.
	enabled bool
}

// NewNotifySendNotifier creates a new Linux notification sender.
// If enabled is false, notifications are silently dropped.
func NewNotifySendNotifier(enabled bool) *NotifySendNotifier {
	return &NotifySendNotifier{enabled: enabled}
}

// NewPlatformNotifier creates the platform-appropriate notifier for Linux.
func NewPlatformNotifier(enabled bool) Notifier {
	return NewNotifySendNotifier(enabled)
}

// Notify sends a Linux desktop notification for the given alert.
// The call returns immediately; the notify-send command runs in a background
// goroutine. Errors are logged but do not affect the alert engine.
func (n *NotifySendNotifier) Notify(alert Alert) {
	if !n.enabled {
		return
	}

	title := fmt.Sprintf("cc-top: %s", alert.Rule)
	body := alert.Message
	if alert.SessionID != "" {
		body = fmt.Sprintf("Session: %s\n%s", truncateSessionID(alert.SessionID), alert.Message)
	}

	// Map severity to notify-send urgency level.
	urgency := "normal"
	if alert.Severity == SeverityCritical {
		urgency = "critical"
	}

	go func() {
		if err := sendNotifySend(title, body, urgency); err != nil {
			log.Printf("WARNING: failed to send Linux notification: %v", err)
		}
	}()
}

// sendNotifySend executes notify-send to display a desktop notification.
func sendNotifySend(title, body, urgency string) error {
	cmd := exec.Command("notify-send", "--urgency", urgency, "--app-name", "cc-top", title, body)
	return cmd.Run()
}
