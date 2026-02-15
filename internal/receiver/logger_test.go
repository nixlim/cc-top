package receiver

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/state"
)

func TestNopLogger_DoesNotPanic(t *testing.T) {
	var l NopLogger
	l.LogEvent("sess-1", state.Event{
		Name:       "claude_code.api_request",
		Attributes: map[string]string{"model": "opus-4-6"},
		Timestamp:  time.Now(),
	})
	l.LogMetric("sess-1", state.Metric{
		Name:       "claude_code.cost.usage",
		Value:      0.05,
		Attributes: map[string]string{"model": "opus-4-6"},
		Timestamp:  time.Now(),
	})
}

func TestFileLogger_LogEvent(t *testing.T) {
	var buf bytes.Buffer
	l := NewFileLogger(&buf)

	ts := time.Date(2026, 2, 15, 10, 30, 0, 0, time.UTC)
	l.LogEvent("sess-abc", state.Event{
		Name: "claude_code.api_request",
		Attributes: map[string]string{
			"model":        "claude-opus-4-6",
			"input_tokens": "100",
		},
		Timestamp: ts,
	})

	output := buf.String()
	if output == "" {
		t.Fatal("expected output, got empty string")
	}

	// Should be valid JSON.
	var entry logEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &entry); err != nil {
		t.Fatalf("invalid JSON output: %v\nOutput: %s", err, output)
	}

	if entry.Type != "event" {
		t.Errorf("expected type=event, got %q", entry.Type)
	}
	if entry.SessionID != "sess-abc" {
		t.Errorf("expected session=sess-abc, got %q", entry.SessionID)
	}
	if entry.Name != "claude_code.api_request" {
		t.Errorf("expected name=claude_code.api_request, got %q", entry.Name)
	}
	if entry.Attributes["model"] != "claude-opus-4-6" {
		t.Errorf("expected attrs.model=claude-opus-4-6, got %q", entry.Attributes["model"])
	}
	if entry.Value != nil {
		t.Errorf("expected no value for event, got %v", *entry.Value)
	}
	if entry.Timestamp != "2026-02-15T10:30:00Z" {
		t.Errorf("expected ts=2026-02-15T10:30:00Z, got %q", entry.Timestamp)
	}
}

func TestFileLogger_LogMetric(t *testing.T) {
	var buf bytes.Buffer
	l := NewFileLogger(&buf)

	ts := time.Date(2026, 2, 15, 10, 31, 0, 0, time.UTC)
	l.LogMetric("sess-xyz", state.Metric{
		Name:  "claude_code.cost.usage",
		Value: 0.042,
		Attributes: map[string]string{
			"model": "claude-haiku-4-5-20251001",
		},
		Timestamp: ts,
	})

	output := buf.String()
	var entry logEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &entry); err != nil {
		t.Fatalf("invalid JSON output: %v\nOutput: %s", err, output)
	}

	if entry.Type != "metric" {
		t.Errorf("expected type=metric, got %q", entry.Type)
	}
	if entry.SessionID != "sess-xyz" {
		t.Errorf("expected session=sess-xyz, got %q", entry.SessionID)
	}
	if entry.Name != "claude_code.cost.usage" {
		t.Errorf("expected name=claude_code.cost.usage, got %q", entry.Name)
	}
	if entry.Value == nil || *entry.Value != 0.042 {
		t.Errorf("expected value=0.042, got %v", entry.Value)
	}
	if entry.Attributes["model"] != "claude-haiku-4-5-20251001" {
		t.Errorf("expected attrs.model=claude-haiku-4-5-20251001, got %q", entry.Attributes["model"])
	}
}

func TestFileLogger_JSONL_Format(t *testing.T) {
	var buf bytes.Buffer
	l := NewFileLogger(&buf)

	ts := time.Now()
	l.LogEvent("s1", state.Event{Name: "e1", Timestamp: ts})
	l.LogEvent("s2", state.Event{Name: "e2", Timestamp: ts})
	l.LogMetric("s3", state.Metric{Name: "m1", Value: 1.0, Timestamp: ts})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSONL lines, got %d", len(lines))
	}

	// Each line should be independently valid JSON.
	for i, line := range lines {
		var entry logEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestFileLogger_ConcurrentSafety(t *testing.T) {
	var buf bytes.Buffer
	l := NewFileLogger(&buf)

	var wg sync.WaitGroup
	ts := time.Now()

	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			l.LogEvent("sess", state.Event{
				Name:       "claude_code.api_request",
				Attributes: map[string]string{"model": "opus"},
				Timestamp:  ts,
			})
		}()
		go func() {
			defer wg.Done()
			l.LogMetric("sess", state.Metric{
				Name:       "claude_code.cost.usage",
				Value:      0.01,
				Attributes: map[string]string{"model": "opus"},
				Timestamp:  ts,
			})
		}()
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 200 {
		t.Errorf("expected 200 lines from concurrent writes, got %d", len(lines))
	}

	// Every line should be valid JSON (no interleaving).
	for i, line := range lines {
		var entry logEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d is not valid JSON (possible interleaving): %v", i, err)
		}
	}
}

func TestFileLogger_NilAttributes(t *testing.T) {
	var buf bytes.Buffer
	l := NewFileLogger(&buf)

	// Events/metrics with nil attributes should not panic.
	l.LogEvent("s1", state.Event{Name: "e1", Timestamp: time.Now()})
	l.LogMetric("s1", state.Metric{Name: "m1", Value: 1.0, Timestamp: time.Now()})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
}

func TestFileLogger_ZeroTimestamp(t *testing.T) {
	var buf bytes.Buffer
	l := NewFileLogger(&buf)

	// Zero timestamp should be replaced with current time (non-zero).
	l.LogEvent("s1", state.Event{Name: "e1"})

	var entry logEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &entry); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if entry.Timestamp == "" {
		t.Error("expected non-empty timestamp for zero-time event")
	}
}

// Verify Logger interface compliance at compile time.
var _ Logger = NopLogger{}
var _ Logger = (*FileLogger)(nil)
