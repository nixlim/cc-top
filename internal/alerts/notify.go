package alerts

// truncateSessionID shortens a session ID for display in notifications.
func truncateSessionID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12] + "..."
}
