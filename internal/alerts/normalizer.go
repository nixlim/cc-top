package alerts

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// commandFamily defines a group of semantically equivalent commands
// that should be treated as the same command for loop detection.
type commandFamily struct {
	name     string   // canonical family name used for hashing
	prefixes []string // command prefixes that belong to this family
}

// knownFamilies lists test-runner families and other command groups
// whose variants should be normalized to a single canonical form.
var knownFamilies = []commandFamily{
	{
		name: "npm-test",
		prefixes: []string{
			"npm test",
			"npm run test",
			"npm exec jest",
			"npx jest",
			"npx vitest",
			"yarn test",
			"yarn run test",
			"yarn jest",
			"yarn vitest",
			"pnpm test",
			"pnpm run test",
			"pnpm exec jest",
		},
	},
	{
		name: "pytest",
		prefixes: []string{
			"pytest",
			"python -m pytest",
			"python3 -m pytest",
			"py.test",
		},
	},
	{
		name: "go-test",
		prefixes: []string{
			"go test",
		},
	},
}

// NormalizeCommand groups semantically similar bash commands by prefix
// matching against known command families, returning a stable SHA-256 hash.
//
// Known test-runner families (npm/npx/yarn test variants, pytest variants,
// go test variants) are all mapped to the same hash within their family.
// Unknown commands are hashed individually using their full command string.
//
// An empty command returns an empty string. Large commands (e.g., 10KB+) are
// hashed without error since SHA-256 handles arbitrary-length input.
func NormalizeCommand(command string) string {
	if command == "" {
		return ""
	}

	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return ""
	}

	// Check each known family for a prefix match.
	for _, family := range knownFamilies {
		for _, prefix := range family.prefixes {
			if matchesPrefix(trimmed, prefix) {
				return hashString(family.name)
			}
		}
	}

	// Unknown command: hash the full command string.
	return hashString(trimmed)
}

// matchesPrefix returns true if cmd matches the given prefix. The match is
// satisfied if cmd equals the prefix exactly, or if cmd starts with the prefix
// followed by a space (indicating additional arguments).
func matchesPrefix(cmd, prefix string) bool {
	if cmd == prefix {
		return true
	}
	if len(cmd) > len(prefix) && strings.HasPrefix(cmd, prefix) && cmd[len(prefix)] == ' ' {
		return true
	}
	return false
}

// hashString returns the hex-encoded SHA-256 hash of s.
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
