package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// defaultSettingsPath returns the default path to Claude Code's settings.json.
func defaultSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// Merge reads ~/.claude/settings.json (or the path specified in opts),
// merges the required OTel environment variables into the "env" block,
// and writes the file back atomically (temp file + rename).
//
// Behaviour:
//   - File not found: creates a new file with the required env vars.
//   - Malformed JSON: creates a .bak backup and returns an error.
//   - Permission denied: returns a clear error.
//   - All keys already correct: returns MergeAlreadyConfigured.
//   - Interactive=false with differing values: warns but does not overwrite.
//   - FixPortOnly=true: only updates OTEL_EXPORTER_OTLP_ENDPOINT.
func Merge(opts MergeOptions) MergeOutput {
	settingsPath := opts.SettingsPath
	if settingsPath == "" {
		settingsPath = defaultSettingsPath()
	}

	grpcPort := opts.GRPCPort
	if grpcPort == 0 {
		grpcPort = 4317
	}

	required := RequiredOTelEnv(grpcPort)

	// When FixPortOnly, only update the endpoint.
	if opts.FixPortOnly {
		limited := make(map[string]string)
		limited["OTEL_EXPORTER_OTLP_ENDPOINT"] = required["OTEL_EXPORTER_OTLP_ENDPOINT"]
		required = limited
	}

	// Read existing file.
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return createNewSettingsFile(settingsPath, required)
		}
		if errors.Is(err, fs.ErrPermission) {
			return MergeOutput{
				Result: MergeError,
				Err:    fmt.Errorf("permission denied reading %s", settingsPath),
			}
		}
		return MergeOutput{
			Result: MergeError,
			Err:    fmt.Errorf("reading settings file: %w", err),
		}
	}

	// Detect indentation before parsing.
	indent := detectIndent(data)

	// Parse JSON.
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		// Malformed JSON: create backup.
		bakPath := settingsPath + ".bak"
		if bakErr := os.WriteFile(bakPath, data, 0644); bakErr != nil {
			return MergeOutput{
				Result:   MergeError,
				Err:      fmt.Errorf("settings.json contains invalid JSON and backup failed: %w", bakErr),
				Messages: []string{fmt.Sprintf("Failed to create backup at %s", bakPath)},
			}
		}
		return MergeOutput{
			Result:   MergeError,
			Err:      fmt.Errorf("settings.json contains invalid JSON (backup saved to %s)", bakPath),
			Messages: []string{fmt.Sprintf("Backup saved to %s", bakPath)},
		}
	}

	// Ensure the "env" block exists.
	envRaw, ok := settings["env"]
	var env map[string]any
	if ok {
		env, ok = envRaw.(map[string]any)
		if !ok {
			// "env" exists but is not an object.
			env = make(map[string]any)
			settings["env"] = env
		}
	} else {
		env = make(map[string]any)
		settings["env"] = env
	}

	// Check current state and merge.
	var (
		messages     []string
		warnings     []string
		anyDifferent bool
		allCorrect   = true
	)

	// Sort keys for deterministic output.
	keys := sortedKeys(required)

	for _, key := range keys {
		wantVal := required[key]
		existing, exists := env[key]

		if !exists {
			// Key absent: add it.
			env[key] = wantVal
			allCorrect = false
			messages = append(messages, fmt.Sprintf("Added %s=%s", key, wantVal))
			continue
		}

		existingStr, _ := existing.(string)
		if existingStr == wantVal {
			// Key present with correct value: leave it.
			continue
		}

		// Key present with different value.
		allCorrect = false
		anyDifferent = true
		if opts.FixPortOnly {
			// FixPortOnly mode forcefully updates the endpoint.
			env[key] = wantVal
			messages = append(messages, fmt.Sprintf("Updated %s from %q to %q", key, existingStr, wantVal))
		} else if opts.Interactive {
			// In interactive mode, signal that confirmation is needed.
			warnings = append(warnings, fmt.Sprintf(
				"%s is set to %q, expected %q",
				key, existingStr, wantVal,
			))
		} else {
			// Non-interactive: skip with warning, do not overwrite.
			warnings = append(warnings, fmt.Sprintf(
				"Warning: %s is set to %q (expected %q), not overwriting",
				key, existingStr, wantVal,
			))
		}
	}

	// If interactive mode has differing values, return NeedsConfirmation without writing.
	if opts.Interactive && anyDifferent {
		return MergeOutput{
			Result:   MergeNeedsConfirmation,
			Messages: messages,
			Warnings: warnings,
		}
	}

	// If all keys are already correct.
	if allCorrect {
		return MergeOutput{
			Result:   MergeAlreadyConfigured,
			Messages: []string{"All OTel environment variables are already configured correctly"},
		}
	}

	// Write the updated file atomically.
	if err := writeSettingsAtomic(settingsPath, settings, indent); err != nil {
		return MergeOutput{
			Result: MergeError,
			Err:    fmt.Errorf("writing settings file: %w", err),
		}
	}

	return MergeOutput{
		Result:   MergeSuccess,
		Messages: messages,
		Warnings: warnings,
	}
}

// createNewSettingsFile creates a new settings.json with the required env vars.
func createNewSettingsFile(path string, required map[string]string) MergeOutput {
	// Ensure the parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return MergeOutput{
				Result: MergeError,
				Err:    fmt.Errorf("permission denied creating directory %s", dir),
			}
		}
		return MergeOutput{
			Result: MergeError,
			Err:    fmt.Errorf("creating directory %s: %w", dir, err),
		}
	}

	env := make(map[string]any, len(required))
	for k, v := range required {
		env[k] = v
	}
	settings := map[string]any{
		"env": env,
	}

	indent := "  " // Default 2 spaces for new files.
	if err := writeSettingsAtomic(path, settings, indent); err != nil {
		return MergeOutput{
			Result: MergeError,
			Err:    fmt.Errorf("creating settings file: %w", err),
		}
	}

	return MergeOutput{
		Result:   MergeSuccess,
		Messages: []string{fmt.Sprintf("Created %s with OTel environment variables", path)},
	}
}

// writeSettingsAtomic writes the settings map to a file atomically using
// a temp file + rename approach to prevent corruption from concurrent writes.
func writeSettingsAtomic(path string, settings map[string]any, indent string) error {
	data, err := json.MarshalIndent(settings, "", indent)
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	// Ensure trailing newline.
	data = append(data, '\n')

	// Write to temp file in the same directory (required for atomic rename).
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".settings-*.json.tmp")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("permission denied writing to %s", dir)
		}
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Clean up temp file on any error.
	defer func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	// Preserve original file permissions if the target file exists.
	if info, err := os.Stat(path); err == nil {
		if chErr := os.Chmod(tmpPath, info.Mode()); chErr != nil {
			// Non-fatal, but try to set permissions.
			_ = chErr
		}
	} else {
		// New file: use 0644.
		if chErr := os.Chmod(tmpPath, 0644); chErr != nil {
			_ = chErr
		}
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming temp file to %s: %w", path, err)
	}
	tmpPath = "" // Prevent deferred removal.

	return nil
}

// sortedKeys returns the keys of a map sorted alphabetically.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
