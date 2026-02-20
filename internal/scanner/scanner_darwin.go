//go:build darwin

package scanner

import (
	"os"
	"path/filepath"
	"time"
)

// NewDefaultScanner creates a Scanner using the real macOS libproc API
// and the given scan interval in seconds. This is the production constructor.
func NewDefaultScanner(intervalSeconds int) *Scanner {
	s := NewScanner(newDarwinProcessAPI(), time.Duration(intervalSeconds)*time.Second)
	home, _ := os.UserHomeDir()
	if home != "" {
		s.globalConfigPaths = append(s.globalConfigPaths,
			filepath.Join(home, ".claude", "settings.json"),
		)
	}
	s.globalConfigPaths = append(s.globalConfigPaths,
		filepath.Join("/Library", "Application Support", "ClaudeCode", "managed-settings.json"),
	)
	return s
}
