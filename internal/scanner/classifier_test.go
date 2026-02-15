package scanner

import (
	"testing"
)

func TestTelemetryClassifier_Connected(t *testing.T) {
	proc := ProcessInfo{
		PID:         4821,
		EnvReadable: true,
		EnvVars: map[string]string{
			"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
			"OTEL_METRICS_EXPORTER":        "otlp",
			"OTEL_LOGS_EXPORTER":           "otlp",
			"OTEL_EXPORTER_OTLP_ENDPOINT":  "http://localhost:4317",
		},
	}

	result := ClassifyTelemetry(proc, 4317, true)

	if result.Status != TelemetryConnected {
		t.Errorf("Status = %v, want TelemetryConnected", result.Status)
	}
	if result.Label != "Connected" {
		t.Errorf("Label = %q, want %q", result.Label, "Connected")
	}
}

func TestTelemetryClassifier_Waiting(t *testing.T) {
	proc := ProcessInfo{
		PID:         4821,
		EnvReadable: true,
		EnvVars: map[string]string{
			"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
			"OTEL_METRICS_EXPORTER":        "otlp",
			"OTEL_LOGS_EXPORTER":           "otlp",
			"OTEL_EXPORTER_OTLP_ENDPOINT":  "http://localhost:4317",
		},
	}

	result := ClassifyTelemetry(proc, 4317, false)

	if result.Status != TelemetryWaiting {
		t.Errorf("Status = %v, want TelemetryWaiting", result.Status)
	}
	if result.Label != "Waiting..." {
		t.Errorf("Label = %q, want %q", result.Label, "Waiting...")
	}
}

func TestTelemetryClassifier_WrongPort(t *testing.T) {
	proc := ProcessInfo{
		PID:         5344,
		EnvReadable: true,
		EnvVars: map[string]string{
			"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
			"OTEL_METRICS_EXPORTER":        "otlp",
			"OTEL_LOGS_EXPORTER":           "otlp",
			"OTEL_EXPORTER_OTLP_ENDPOINT":  "http://localhost:9090",
		},
	}

	result := ClassifyTelemetry(proc, 4317, false)

	if result.Status != TelemetryWrongPort {
		t.Errorf("Status = %v, want TelemetryWrongPort", result.Status)
	}
	if result.Label != "Wrong port" {
		t.Errorf("Label = %q, want %q", result.Label, "Wrong port")
	}
}

func TestTelemetryClassifier_WrongPort_CustomConfiguredPort(t *testing.T) {
	// cc-top is running on port 5317, but the process points to 4317.
	proc := ProcessInfo{
		PID:         5344,
		EnvReadable: true,
		EnvVars: map[string]string{
			"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
			"OTEL_EXPORTER_OTLP_ENDPOINT":  "http://localhost:4317",
		},
	}

	result := ClassifyTelemetry(proc, 5317, false)

	if result.Status != TelemetryWrongPort {
		t.Errorf("Status = %v, want TelemetryWrongPort (process points to 4317 but cc-top is on 5317)", result.Status)
	}
}

func TestTelemetryClassifier_ConsoleOnly(t *testing.T) {
	proc := ProcessInfo{
		PID:         6017,
		EnvReadable: true,
		EnvVars: map[string]string{
			"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
			// No OTEL_EXPORTER_OTLP_ENDPOINT
			// No OTEL_METRICS_EXPORTER or OTEL_LOGS_EXPORTER set to "otlp"
		},
	}

	result := ClassifyTelemetry(proc, 4317, false)

	if result.Status != TelemetryConsoleOnly {
		t.Errorf("Status = %v, want TelemetryConsoleOnly", result.Status)
	}
	if result.Label != "Console only" {
		t.Errorf("Label = %q, want %q", result.Label, "Console only")
	}
}

func TestTelemetryClassifier_ConsoleOnly_ExplicitConsoleExporter(t *testing.T) {
	proc := ProcessInfo{
		PID:         6017,
		EnvReadable: true,
		EnvVars: map[string]string{
			"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
			"OTEL_METRICS_EXPORTER":        "console",
			"OTEL_LOGS_EXPORTER":           "console",
		},
	}

	result := ClassifyTelemetry(proc, 4317, false)

	if result.Status != TelemetryConsoleOnly {
		t.Errorf("Status = %v, want TelemetryConsoleOnly", result.Status)
	}
}

func TestTelemetryClassifier_NoTelemetry(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
	}{
		{
			name: "telemetry explicitly off",
			envVars: map[string]string{
				"CLAUDE_CODE_ENABLE_TELEMETRY": "0",
			},
		},
		{
			name:    "telemetry absent",
			envVars: map[string]string{},
		},
		{
			name: "telemetry empty string",
			envVars: map[string]string{
				"CLAUDE_CODE_ENABLE_TELEMETRY": "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := ProcessInfo{
				PID:         6017,
				EnvReadable: true,
				EnvVars:     tt.envVars,
			}

			result := ClassifyTelemetry(proc, 4317, false)

			if result.Status != TelemetryOff {
				t.Errorf("Status = %v, want TelemetryOff", result.Status)
			}
			if result.Label != "No telemetry" {
				t.Errorf("Label = %q, want %q", result.Label, "No telemetry")
			}
		})
	}
}

func TestTelemetryClassifier_Unknown(t *testing.T) {
	proc := ProcessInfo{
		PID:         7777,
		EnvReadable: false,
		EnvVars:     nil,
	}

	result := ClassifyTelemetry(proc, 4317, false)

	if result.Status != TelemetryUnknown {
		t.Errorf("Status = %v, want TelemetryUnknown", result.Status)
	}
	if result.Label != "Unknown" {
		t.Errorf("Label = %q, want %q", result.Label, "Unknown")
	}
}

func TestTelemetryClassifier_EndpointParsing(t *testing.T) {
	tests := []struct {
		name           string
		endpoint       string
		configuredPort int
		wantStatus     TelemetryStatus
	}{
		{
			name:           "http with matching port",
			endpoint:       "http://localhost:4317",
			configuredPort: 4317,
			wantStatus:     TelemetryWaiting,
		},
		{
			name:           "http with mismatched port",
			endpoint:       "http://localhost:9090",
			configuredPort: 4317,
			wantStatus:     TelemetryWrongPort,
		},
		{
			name:           "custom port matching",
			endpoint:       "http://localhost:5317",
			configuredPort: 5317,
			wantStatus:     TelemetryWaiting,
		},
		{
			name:           "with trailing path",
			endpoint:       "http://localhost:4317/v1/metrics",
			configuredPort: 4317,
			wantStatus:     TelemetryWaiting,
		},
		{
			name:           "127.0.0.1 instead of localhost",
			endpoint:       "http://127.0.0.1:4317",
			configuredPort: 4317,
			wantStatus:     TelemetryWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := ProcessInfo{
				PID:         1234,
				EnvReadable: true,
				EnvVars: map[string]string{
					"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
					"OTEL_EXPORTER_OTLP_ENDPOINT":  tt.endpoint,
				},
			}

			result := ClassifyTelemetry(proc, tt.configuredPort, false)

			if result.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v for endpoint %q with configured port %d",
					result.Status, tt.wantStatus, tt.endpoint, tt.configuredPort)
			}
		})
	}
}

func TestTelemetryClassifier_OtlpExporterWithoutEndpoint(t *testing.T) {
	// When exporters are set to "otlp" but no endpoint is specified,
	// OTLP defaults to localhost:4317. So if cc-top is on 4317, this should
	// be Waiting/Connected; otherwise WrongPort.
	proc := ProcessInfo{
		PID:         1234,
		EnvReadable: true,
		EnvVars: map[string]string{
			"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
			"OTEL_METRICS_EXPORTER":        "otlp",
			"OTEL_LOGS_EXPORTER":           "otlp",
		},
	}

	t.Run("default port matches", func(t *testing.T) {
		result := ClassifyTelemetry(proc, 4317, false)
		if result.Status != TelemetryWaiting {
			t.Errorf("Status = %v, want TelemetryWaiting (otlp defaults to 4317)", result.Status)
		}
	})

	t.Run("custom port does not match default", func(t *testing.T) {
		result := ClassifyTelemetry(proc, 5317, false)
		if result.Status != TelemetryWrongPort {
			t.Errorf("Status = %v, want TelemetryWrongPort (otlp defaults to 4317, not 5317)", result.Status)
		}
	})
}
