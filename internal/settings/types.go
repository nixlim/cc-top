// Package settings handles reading and writing Claude Code's settings.json file,
// specifically merging OTel environment variables for telemetry configuration.
package settings

import "fmt"

// MergeResult indicates the outcome of a settings merge operation.
type MergeResult int

const (
	// MergeSuccess indicates OTel keys were successfully added or updated.
	MergeSuccess MergeResult = iota
	// MergeAlreadyConfigured indicates all OTel keys are already set correctly.
	MergeAlreadyConfigured
	// MergeNeedsConfirmation indicates some keys have different values and need user confirmation.
	MergeNeedsConfirmation
	// MergeError indicates the merge failed.
	MergeError
)

// String returns a human-readable description of the MergeResult.
func (r MergeResult) String() string {
	switch r {
	case MergeSuccess:
		return "success"
	case MergeAlreadyConfigured:
		return "already configured"
	case MergeNeedsConfirmation:
		return "needs confirmation"
	case MergeError:
		return "error"
	default:
		return fmt.Sprintf("MergeResult(%d)", int(r))
	}
}

// MergeOutput contains the result of a merge operation along with any
// messages, warnings, or errors produced during the process.
type MergeOutput struct {
	Result   MergeResult
	Messages []string // Informational messages about what was done.
	Warnings []string // Warnings about skipped or differing values.
	Err      error    // Non-nil only when Result == MergeError.
}

// MergeOptions controls the behaviour of the settings merge operation.
type MergeOptions struct {
	// SettingsPath is the path to settings.json. Defaults to ~/.claude/settings.json.
	SettingsPath string

	// Interactive controls whether the merge should prompt for confirmation
	// when existing values differ. When false (e.g. --setup), differing values
	// are skipped with a warning.
	Interactive bool

	// FixPortOnly when true limits the merge to only updating
	// OTEL_EXPORTER_OTLP_ENDPOINT. Used by the [F] "Fix misconfigured" action.
	FixPortOnly bool

	// GRPCPort is the port cc-top listens on. Used to construct the endpoint URL.
	// Defaults to 4317 if zero.
	GRPCPort int
}

// RequiredOTelEnv returns the required OTel environment variables and their expected values.
// The grpcPort parameter specifies the port cc-top is listening on.
func RequiredOTelEnv(grpcPort int) map[string]string {
	if grpcPort == 0 {
		grpcPort = 4317
	}
	return map[string]string{
		"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
		"OTEL_METRICS_EXPORTER":       "otlp",
		"OTEL_LOGS_EXPORTER":          "otlp",
		"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc",
		"OTEL_EXPORTER_OTLP_ENDPOINT": fmt.Sprintf("http://localhost:%d", grpcPort),
		"OTEL_METRIC_EXPORT_INTERVAL": "5000",
		"OTEL_LOGS_EXPORT_INTERVAL":   "2000",
	}
}
