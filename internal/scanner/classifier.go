package scanner

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ClassifyTelemetry is a pure function that determines the telemetry status
// of a Claude Code process based on its environment variables and the
// configured OTLP receiver port.
//
// Classification logic:
//   - EnvReadable=false => Unknown
//   - CLAUDE_CODE_ENABLE_TELEMETRY absent or "0" => Off
//   - Telemetry=1, no OTEL_EXPORTER_OTLP_ENDPOINT => ConsoleOnly
//   - Telemetry=1, endpoint port != configuredPort => WrongPort
//   - Telemetry=1, endpoint port == configuredPort, data received => Connected
//   - Telemetry=1, endpoint port == configuredPort, no data yet => Waiting
func ClassifyTelemetry(proc ProcessInfo, configuredPort int, hasReceivedData bool) StatusInfo {
	// If environment is unreadable, we cannot determine anything.
	if !proc.EnvReadable {
		return StatusInfo{
			Status: TelemetryUnknown,
			Icon:   "\u2753", // ?
			Label:  "Unknown",
		}
	}

	// Check if telemetry is enabled.
	telemetryVal, hasTelemetry := proc.EnvVars["CLAUDE_CODE_ENABLE_TELEMETRY"]
	if !hasTelemetry || telemetryVal == "0" || telemetryVal == "" {
		return StatusInfo{
			Status: TelemetryOff,
			Icon:   "\u274c", // red X
			Label:  "No telemetry",
		}
	}

	// Telemetry is enabled. Check exporter configuration.
	endpoint, hasEndpoint := proc.EnvVars["OTEL_EXPORTER_OTLP_ENDPOINT"]
	metricsExporter := proc.EnvVars["OTEL_METRICS_EXPORTER"]
	logsExporter := proc.EnvVars["OTEL_LOGS_EXPORTER"]

	// If no OTLP endpoint set and exporters aren't set to "otlp", it's console only.
	if !hasEndpoint || endpoint == "" {
		if metricsExporter != "otlp" && logsExporter != "otlp" {
			return StatusInfo{
				Status: TelemetryConsoleOnly,
				Icon:   "\u26a0\ufe0f", // warning
				Label:  "Console only",
			}
		}
		// Exporters set to otlp but no endpoint means they'll try default (localhost:4317).
		// Check if our port matches the default.
		if configuredPort == 4317 {
			return connectedOrWaiting(hasReceivedData)
		}
		return StatusInfo{
			Status: TelemetryWrongPort,
			Icon:   "\u26a0\ufe0f", // warning
			Label:  "Wrong port",
		}
	}

	// Parse the endpoint to extract the port.
	endpointPort := extractPort(endpoint, configuredPort)

	if endpointPort == configuredPort {
		return connectedOrWaiting(hasReceivedData)
	}

	return StatusInfo{
		Status: TelemetryWrongPort,
		Icon:   "\u26a0\ufe0f", // warning
		Label:  fmt.Sprintf("Wrong port"),
	}
}

// connectedOrWaiting returns Connected or Waiting status based on whether
// OTLP data has been received from this process.
func connectedOrWaiting(hasReceivedData bool) StatusInfo {
	if hasReceivedData {
		return StatusInfo{
			Status: TelemetryConnected,
			Icon:   "\u2705", // green check
			Label:  "Connected",
		}
	}
	return StatusInfo{
		Status: TelemetryWaiting,
		Icon:   "\u2705", // green check
		Label:  "Waiting...",
	}
}

// extractPort parses a URL endpoint string and returns the port number.
// If no port is explicit, it falls back to the scheme's default port
// (80 for http, 443 for https). If parsing fails, returns -1.
func extractPort(endpoint string, _ int) int {
	// Handle bare host:port without scheme.
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return -1
	}

	portStr := u.Port()
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil {
			return -1
		}
		return p
	}

	// No explicit port: use scheme defaults.
	switch u.Scheme {
	case "http":
		return 80
	case "https":
		return 443
	default:
		return -1
	}
}
