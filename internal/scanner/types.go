package scanner

// ProcessInfo holds information about a discovered Claude Code process.
type ProcessInfo struct {
	PID          int
	BinaryName   string
	Args         []string
	CWD          string
	Terminal     string
	EnvVars      map[string]string
	EnvReadable  bool
	IsNew        bool   // first scan cycle where this PID appeared
	Exited       bool
}

// TelemetryStatus classifies a process's telemetry configuration.
type TelemetryStatus int

const (
	TelemetryConnected  TelemetryStatus = iota // ✅ ON, correct endpoint, data received
	TelemetryWaiting                            // ✅ ON, correct endpoint, no data yet
	TelemetryWrongPort                          // ⚠️ ON, wrong endpoint
	TelemetryConsoleOnly                        // ⚠️ ON, no OTLP endpoint
	TelemetryOff                                // ❌ not enabled
	TelemetryUnknown                            // ❓ env unreadable
)

// StatusInfo holds display information for a telemetry status.
type StatusInfo struct {
	Status TelemetryStatus
	Icon   string
	Label  string
}
