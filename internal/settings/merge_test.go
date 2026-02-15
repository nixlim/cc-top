package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// helper to read and parse settings.json from a path.
func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading settings file: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parsing settings JSON: %v", err)
	}
	return result
}

// helper to get the env block from parsed settings.
func getEnv(t *testing.T, settings map[string]any) map[string]any {
	t.Helper()
	envRaw, ok := settings["env"]
	if !ok {
		t.Fatal("settings missing 'env' key")
	}
	env, ok := envRaw.(map[string]any)
	if !ok {
		t.Fatal("'env' is not an object")
	}
	return env
}

func TestSettingsMerge_AddKeys(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Create a file with existing env vars but no OTel keys.
	initial := map[string]any{
		"env": map[string]any{
			"MY_VAR": "keep",
		},
		"other_key": true,
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	result := Merge(MergeOptions{
		SettingsPath: settingsPath,
		Interactive:  false,
	})

	if result.Result != MergeSuccess {
		t.Fatalf("expected MergeSuccess, got %v (err: %v)", result.Result, result.Err)
	}

	settings := readSettings(t, settingsPath)
	env := getEnv(t, settings)

	// Existing var should be preserved.
	if env["MY_VAR"] != "keep" {
		t.Errorf("MY_VAR: want 'keep', got %v", env["MY_VAR"])
	}

	// Other top-level key should be preserved.
	if settings["other_key"] != true {
		t.Errorf("other_key: want true, got %v", settings["other_key"])
	}

	// OTel keys should be added.
	expectedKeys := RequiredOTelEnv(4317)
	for key, wantVal := range expectedKeys {
		got, ok := env[key]
		if !ok {
			t.Errorf("missing OTel key: %s", key)
			continue
		}
		if got != wantVal {
			t.Errorf("%s: want %q, got %v", key, wantVal, got)
		}
	}
}

func TestSettingsMerge_CreateFile(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// The .claude directory does not exist yet.
	result := Merge(MergeOptions{
		SettingsPath: settingsPath,
		Interactive:  false,
	})

	if result.Result != MergeSuccess {
		t.Fatalf("expected MergeSuccess, got %v (err: %v)", result.Result, result.Err)
	}

	// File should now exist.
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("settings.json was not created: %v", err)
	}

	settings := readSettings(t, settingsPath)
	env := getEnv(t, settings)

	expectedKeys := RequiredOTelEnv(4317)
	for key, wantVal := range expectedKeys {
		got, ok := env[key]
		if !ok {
			t.Errorf("missing OTel key: %s", key)
			continue
		}
		if got != wantVal {
			t.Errorf("%s: want %q, got %v", key, wantVal, got)
		}
	}
}

func TestSettingsMerge_PreserveIndent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Create a file with 4-space indentation.
	content := `{
    "env": {
        "MY_VAR": "keep"
    },
    "other": "value"
}
`
	if err := os.WriteFile(settingsPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := Merge(MergeOptions{
		SettingsPath: settingsPath,
		Interactive:  false,
	})

	if result.Result != MergeSuccess {
		t.Fatalf("expected MergeSuccess, got %v (err: %v)", result.Result, result.Err)
	}

	// Read the raw file and check indentation.
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	// The file should use 4-space indentation.
	detected := detectIndent(data)
	if detected != "    " {
		t.Errorf("indentation: want 4 spaces, got %q", detected)
	}

	// Verify content is valid JSON and preserves existing keys.
	settings := readSettings(t, settingsPath)
	if settings["other"] != "value" {
		t.Errorf("other: want 'value', got %v", settings["other"])
	}
	env := getEnv(t, settings)
	if env["MY_VAR"] != "keep" {
		t.Errorf("MY_VAR: want 'keep', got %v", env["MY_VAR"])
	}
}

func TestSettingsMerge_AlreadyConfigured(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Create a file with all correct OTel keys.
	envMap := make(map[string]any)
	for k, v := range RequiredOTelEnv(4317) {
		envMap[k] = v
	}
	initial := map[string]any{
		"env": envMap,
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	result := Merge(MergeOptions{
		SettingsPath: settingsPath,
		Interactive:  false,
	})

	if result.Result != MergeAlreadyConfigured {
		t.Errorf("expected MergeAlreadyConfigured, got %v", result.Result)
	}
}

func TestSettingsMerge_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Create a malformed JSON file.
	if err := os.WriteFile(settingsPath, []byte(`{invalid json`), 0644); err != nil {
		t.Fatal(err)
	}

	result := Merge(MergeOptions{
		SettingsPath: settingsPath,
		Interactive:  false,
	})

	if result.Result != MergeError {
		t.Fatalf("expected MergeError for malformed JSON, got %v", result.Result)
	}
	if result.Err == nil {
		t.Error("expected non-nil error for malformed JSON")
	}

	// A backup should have been created.
	bakPath := settingsPath + ".bak"
	bakData, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("backup file not created: %v", err)
	}
	if string(bakData) != `{invalid json` {
		t.Errorf("backup content: want '{invalid json', got %q", string(bakData))
	}
}

func TestSettingsMerge_PermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission tests not reliable on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("cannot test permission denied as root")
	}

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Create a file and make it read-only, then make the directory read-only
	// so we can't write temp files either.
	if err := os.WriteFile(settingsPath, []byte(`{"env": {}}`), 0444); err != nil {
		t.Fatal(err)
	}
	// Make directory unwritable so temp file creation fails.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() {
		// Restore permissions for cleanup.
		os.Chmod(dir, 0755)
	}()

	result := Merge(MergeOptions{
		SettingsPath: settingsPath,
		Interactive:  false,
	})

	// It should either be MergeError due to write failure,
	// or MergeAlreadyConfigured if all env is empty (it's {} so keys are missing,
	// meaning it will try to write and fail).
	if result.Result != MergeError {
		t.Errorf("expected MergeError for permission denied, got %v", result.Result)
	}
	if result.Err == nil {
		t.Error("expected non-nil error for permission denied")
	}
}

func TestSettingsMerge_DifferentValue_NonInteractive(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Create a file with a different endpoint.
	initial := map[string]any{
		"env": map[string]any{
			"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
			"OTEL_METRICS_EXPORTER":       "otlp",
			"OTEL_LOGS_EXPORTER":          "otlp",
			"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc",
			"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:9090", // Different!
			"OTEL_METRIC_EXPORT_INTERVAL": "5000",
			"OTEL_LOGS_EXPORT_INTERVAL":   "2000",
		},
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	result := Merge(MergeOptions{
		SettingsPath: settingsPath,
		Interactive:  false,
	})

	// Non-interactive mode should succeed but warn about the different value.
	if result.Result != MergeSuccess {
		t.Fatalf("expected MergeSuccess (with warnings), got %v (err: %v)", result.Result, result.Err)
	}

	if len(result.Warnings) == 0 {
		t.Error("expected warnings about differing value, got none")
	}

	// The differing value should NOT have been overwritten.
	settings := readSettings(t, settingsPath)
	env := getEnv(t, settings)
	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://localhost:9090" {
		t.Errorf("endpoint should NOT have been overwritten, got %v", env["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
}

func TestSettingsMerge_FixWrongPort(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Create a file with wrong port but other keys correct.
	initial := map[string]any{
		"env": map[string]any{
			"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
			"OTEL_METRICS_EXPORTER":       "otlp",
			"OTEL_LOGS_EXPORTER":          "otlp",
			"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc",
			"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:9090",
			"OTEL_METRIC_EXPORT_INTERVAL": "5000",
			"OTEL_LOGS_EXPORT_INTERVAL":   "2000",
			"CUSTOM_VAR":                  "preserve_me",
		},
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	result := Merge(MergeOptions{
		SettingsPath: settingsPath,
		Interactive:  false,
		FixPortOnly:  true,
	})

	if result.Result != MergeSuccess {
		t.Fatalf("expected MergeSuccess, got %v (err: %v)", result.Result, result.Err)
	}

	settings := readSettings(t, settingsPath)
	env := getEnv(t, settings)

	// Endpoint should be fixed.
	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://localhost:4317" {
		t.Errorf("endpoint: want http://localhost:4317, got %v", env["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}

	// Other keys should be untouched.
	if env["CUSTOM_VAR"] != "preserve_me" {
		t.Errorf("CUSTOM_VAR should be preserved, got %v", env["CUSTOM_VAR"])
	}
	if env["OTEL_METRICS_EXPORTER"] != "otlp" {
		t.Errorf("OTEL_METRICS_EXPORTER should be preserved, got %v", env["OTEL_METRICS_EXPORTER"])
	}
}

func TestSettingsMerge_Interactive_DifferentValue(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// Create a file with a different endpoint.
	initial := map[string]any{
		"env": map[string]any{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:9090",
		},
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	result := Merge(MergeOptions{
		SettingsPath: settingsPath,
		Interactive:  true,
	})

	// Interactive mode should return NeedsConfirmation.
	if result.Result != MergeNeedsConfirmation {
		t.Errorf("expected MergeNeedsConfirmation, got %v", result.Result)
	}

	if len(result.Warnings) == 0 {
		t.Error("expected warnings about differing value, got none")
	}
}

func TestSettingsMerge_CustomPort(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	result := Merge(MergeOptions{
		SettingsPath: settingsPath,
		Interactive:  false,
		GRPCPort:     5317,
	})

	if result.Result != MergeSuccess {
		t.Fatalf("expected MergeSuccess, got %v (err: %v)", result.Result, result.Err)
	}

	settings := readSettings(t, settingsPath)
	env := getEnv(t, settings)

	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://localhost:5317" {
		t.Errorf("endpoint: want http://localhost:5317, got %v", env["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
}

func TestSettingsMerge_TabIndent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	// File with tab indentation.
	content := "{\n\t\"env\": {},\n\t\"other\": true\n}\n"
	if err := os.WriteFile(settingsPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := Merge(MergeOptions{
		SettingsPath: settingsPath,
		Interactive:  false,
	})

	if result.Result != MergeSuccess {
		t.Fatalf("expected MergeSuccess, got %v (err: %v)", result.Result, result.Err)
	}

	data, _ := os.ReadFile(settingsPath)
	detected := detectIndent(data)
	if detected != "\t" {
		t.Errorf("indentation: want tab, got %q", detected)
	}
}
