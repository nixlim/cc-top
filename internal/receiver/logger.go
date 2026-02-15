package receiver

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

// Logger provides structured debug logging for the OTEL receiver.
// Implementations must be safe for concurrent use.
type Logger interface {
	// LogEvent logs a received OTEL event with its session ID and attributes.
	LogEvent(sessionID string, event state.Event)

	// LogMetric logs a received OTEL metric with its session ID and attributes.
	LogMetric(sessionID string, metric state.Metric)
}

// NopLogger discards all log output. This is the default when debug logging
// is not enabled, and has zero allocation overhead.
type NopLogger struct{}

// LogEvent is a no-op.
func (NopLogger) LogEvent(string, state.Event) {}

// LogMetric is a no-op.
func (NopLogger) LogMetric(string, state.Metric) {}

// logEntry is the JSON structure written by FileLogger.
type logEntry struct {
	Timestamp  string            `json:"ts"`
	Type       string            `json:"type"`
	SessionID  string            `json:"session"`
	Name       string            `json:"name"`
	Value      *float64          `json:"value,omitempty"`
	Attributes map[string]string `json:"attrs,omitempty"`
}

// FileLogger writes structured JSON debug output to an io.Writer.
// Each line is a complete JSON object (JSONL format).
type FileLogger struct {
	w  io.Writer
	mu sync.Mutex
}

// NewFileLogger creates a FileLogger that writes to the given writer.
func NewFileLogger(w io.Writer) *FileLogger {
	return &FileLogger{w: w}
}

// LogEvent writes a JSON line for a received OTEL event.
func (l *FileLogger) LogEvent(sessionID string, e state.Event) {
	ts := e.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	entry := logEntry{
		Timestamp:  ts.UTC().Format(time.RFC3339Nano),
		Type:       "event",
		SessionID:  sessionID,
		Name:       e.Name,
		Attributes: e.Attributes,
	}

	l.write(entry)
}

// LogMetric writes a JSON line for a received OTEL metric.
func (l *FileLogger) LogMetric(sessionID string, m state.Metric) {
	ts := m.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	v := m.Value
	entry := logEntry{
		Timestamp:  ts.UTC().Format(time.RFC3339Nano),
		Type:       "metric",
		SessionID:  sessionID,
		Name:       m.Name,
		Value:      &v,
		Attributes: m.Attributes,
	}

	l.write(entry)
}

// write serialises a logEntry as JSON and writes it as a single line.
// Serialisation errors are silently dropped to avoid disrupting the receiver.
func (l *FileLogger) write(entry logEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "%s\n", data)
}
