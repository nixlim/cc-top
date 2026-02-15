package alerts

import (
	"strings"
	"testing"
)

func TestCommandNormalizer_PrefixMatch(t *testing.T) {
	t.Run("empty command returns empty string", func(t *testing.T) {
		got := NormalizeCommand("")
		if got != "" {
			t.Errorf("NormalizeCommand(%q) = %q, want %q", "", got, "")
		}
	})

	t.Run("whitespace-only command returns empty string", func(t *testing.T) {
		got := NormalizeCommand("   ")
		if got != "" {
			t.Errorf("NormalizeCommand(%q) = %q, want %q", "   ", got, "")
		}
	})

	t.Run("npm test family maps to same hash", func(t *testing.T) {
		commands := []string{
			"npm test",
			"npm run test",
			"npx jest",
			"npx jest --verbose",
			"yarn test",
			"yarn run test",
			"npm test -- --coverage",
			"npx vitest",
			"pnpm test",
		}
		first := NormalizeCommand(commands[0])
		if first == "" {
			t.Fatal("NormalizeCommand returned empty for npm test family")
		}
		for _, cmd := range commands[1:] {
			got := NormalizeCommand(cmd)
			if got != first {
				t.Errorf("NormalizeCommand(%q) = %q, want %q (same as %q)", cmd, got, first, commands[0])
			}
		}
	})

	t.Run("pytest family maps to same hash", func(t *testing.T) {
		commands := []string{
			"pytest",
			"pytest -v",
			"python -m pytest",
			"python -m pytest tests/",
			"python3 -m pytest",
			"py.test",
		}
		first := NormalizeCommand(commands[0])
		if first == "" {
			t.Fatal("NormalizeCommand returned empty for pytest family")
		}
		for _, cmd := range commands[1:] {
			got := NormalizeCommand(cmd)
			if got != first {
				t.Errorf("NormalizeCommand(%q) = %q, want %q (same as %q)", cmd, got, first, commands[0])
			}
		}
	})

	t.Run("go test family maps to same hash", func(t *testing.T) {
		commands := []string{
			"go test",
			"go test ./...",
			"go test -race -v ./pkg/...",
		}
		first := NormalizeCommand(commands[0])
		if first == "" {
			t.Fatal("NormalizeCommand returned empty for go test family")
		}
		for _, cmd := range commands[1:] {
			got := NormalizeCommand(cmd)
			if got != first {
				t.Errorf("NormalizeCommand(%q) = %q, want %q (same as %q)", cmd, got, first, commands[0])
			}
		}
	})

	t.Run("different families produce different hashes", func(t *testing.T) {
		npmHash := NormalizeCommand("npm test")
		pytestHash := NormalizeCommand("pytest")
		goHash := NormalizeCommand("go test")

		if npmHash == pytestHash {
			t.Error("npm test and pytest should produce different hashes")
		}
		if npmHash == goHash {
			t.Error("npm test and go test should produce different hashes")
		}
		if pytestHash == goHash {
			t.Error("pytest and go test should produce different hashes")
		}
	})

	t.Run("unknown commands get their own hash", func(t *testing.T) {
		h1 := NormalizeCommand("curl http://example.com")
		h2 := NormalizeCommand("ls -la")
		h3 := NormalizeCommand("curl http://example.com")

		if h1 == "" || h2 == "" {
			t.Fatal("unknown commands should produce non-empty hash")
		}
		if h1 == h2 {
			t.Error("different unknown commands should have different hashes")
		}
		if h1 != h3 {
			t.Error("identical unknown commands should have the same hash")
		}
	})

	t.Run("unknown command is distinct from known families", func(t *testing.T) {
		unknown := NormalizeCommand("make test")
		known := NormalizeCommand("npm test")
		if unknown == known {
			t.Error("unknown command should not match known family hash")
		}
	})

	t.Run("prefix must match at word boundary", func(t *testing.T) {
		// "go testing" should NOT match "go test" family
		goTestHash := NormalizeCommand("go test")
		goTestingHash := NormalizeCommand("go testing")
		if goTestHash == goTestingHash {
			t.Error("'go testing' should not match 'go test' family (not a word boundary)")
		}
	})

	t.Run("large command hashes without error", func(t *testing.T) {
		large := "echo " + strings.Repeat("x", 10*1024) // 10KB+
		got := NormalizeCommand(large)
		if got == "" {
			t.Error("large command should produce a non-empty hash")
		}
		if len(got) != 64 { // SHA-256 hex is 64 chars
			t.Errorf("hash length = %d, want 64", len(got))
		}
	})

	t.Run("hash is valid hex SHA-256", func(t *testing.T) {
		got := NormalizeCommand("npm test")
		if len(got) != 64 {
			t.Fatalf("hash length = %d, want 64", len(got))
		}
		for _, c := range got {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("hash contains non-hex character: %c", c)
			}
		}
	})

	t.Run("leading and trailing whitespace is trimmed", func(t *testing.T) {
		h1 := NormalizeCommand("npm test")
		h2 := NormalizeCommand("  npm test  ")
		if h1 != h2 {
			t.Error("trimmed commands should produce the same hash")
		}
	})
}
