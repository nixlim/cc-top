//go:build linux

package scanner

import (
	"os"
	"path/filepath"
	"time"
)

// NewDefaultScanner creates a Scanner using the Linux /proc filesystem API
// and the given scan interval in seconds. This is the production constructor.
func NewDefaultScanner(intervalSeconds int) *Scanner {
	s := NewScanner(newLinuxProcessAPI(), time.Duration(intervalSeconds)*time.Second)
	home, _ := os.UserHomeDir()
	if home != "" {
		s.globalConfigPaths = append(s.globalConfigPaths,
			filepath.Join(home, ".claude", "settings.json"),
		)
	}
	s.globalConfigPaths = append(s.globalConfigPaths,
		filepath.Join("/etc", "claude-code", "managed-settings.json"),
	)
	return s
}
