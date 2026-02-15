package settings

import "strings"

// detectIndent examines JSON text and returns the indentation string used.
// It looks for the first indented line (after removing leading/trailing whitespace lines)
// and uses that as the indent. Defaults to two spaces if no indentation is detected.
func detectIndent(data []byte) string {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		// Find the first line that starts with whitespace (indented line).
		trimmed := strings.TrimLeft(line, " \t")
		if len(trimmed) < len(line) && len(trimmed) > 0 {
			indent := line[:len(line)-len(trimmed)]
			return indent
		}
	}
	// Default: 2 spaces.
	return "  "
}
