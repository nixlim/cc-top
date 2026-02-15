package receiver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

// startTestHTTP creates an HTTP receiver on an ephemeral port for testing.
func startTestHTTP(t *testing.T, store state.Store, pm PortMapper) *HTTPReceiver {
	t.Helper()

	cfg := config.ReceiverConfig{
		HTTPPort: 0, // Use ephemeral port.
		Bind:     "127.0.0.1",
	}

	r := NewHTTPReceiver(cfg, store, pm)

	// Manually bind to an ephemeral port.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	r.listener = lis

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/logs", r.handleLogs)
	r.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		_ = r.server.Serve(lis)
	}()

	// Wait briefly for the server to be ready.
	time.Sleep(50 * time.Millisecond)

	return r
}

func TestOTLPReceiver_HTTPEvents(t *testing.T) {
	t.Run("protobuf_content_type", func(t *testing.T) {
		store := state.NewMemoryStore()
		pm := newTestPortMapper()
		r := startTestHTTP(t, store, pm)
		defer r.Stop()

		// Build an OTLP log export request with an api_request event.
		ts := uint64(time.Now().UnixNano())
		req := &collogspb.ExportLogsServiceRequest{
			ResourceLogs: []*logspb.ResourceLogs{
				{
					Resource: &resourcepb.Resource{
						Attributes: []*commonpb.KeyValue{
							{
								Key:   "session.id",
								Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "sess-http-001"}},
							},
						},
					},
					ScopeLogs: []*logspb.ScopeLogs{
						{
							LogRecords: []*logspb.LogRecord{
								{
									TimeUnixNano: ts,
									EventName:    "claude_code.api_request",
									Attributes: []*commonpb.KeyValue{
										{Key: "model", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-sonnet-4-5-20250929"}}},
										{Key: "cost_usd", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "0.05"}}},
										{Key: "input_tokens", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 1500}}},
										{Key: "output_tokens", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 300}}},
										{Key: "duration_ms", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 2100}}},
									},
								},
							},
						},
					},
				},
			},
		}

		body, err := proto.Marshal(req)
		if err != nil {
			t.Fatalf("failed to marshal request: %v", err)
		}

		url := fmt.Sprintf("http://%s/v1/logs", r.Addr().String())
		resp, err := http.Post(url, "application/x-protobuf", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("HTTP POST failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		// Verify the event was stored.
		session := store.GetSession("sess-http-001")
		if session == nil {
			t.Fatal("expected session sess-http-001 to exist")
		}
		if len(session.Events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(session.Events))
		}
		if session.Events[0].Name != "claude_code.api_request" {
			t.Errorf("expected event name 'claude_code.api_request', got %q", session.Events[0].Name)
		}
		if session.Events[0].Attributes["model"] != "claude-sonnet-4-5-20250929" {
			t.Errorf("expected model attribute, got %q", session.Events[0].Attributes["model"])
		}
	})

	t.Run("json_content_type", func(t *testing.T) {
		store := state.NewMemoryStore()
		pm := newTestPortMapper()
		r := startTestHTTP(t, store, pm)
		defer r.Stop()

		// Build a JSON OTLP log export request.
		ts := fmt.Sprintf("%d", time.Now().UnixNano())
		jsonBody := map[string]any{
			"resourceLogs": []map[string]any{
				{
					"resource": map[string]any{
						"attributes": []map[string]any{
							{
								"key":   "session.id",
								"value": map[string]any{"stringValue": "sess-json-001"},
							},
						},
					},
					"scopeLogs": []map[string]any{
						{
							"logRecords": []map[string]any{
								{
									"timeUnixNano": ts,
									"eventName":    "claude_code.user_prompt",
									"attributes": []map[string]any{
										{
											"key":   "prompt_length",
											"value": map[string]any{"intValue": "42"},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		body, err := json.Marshal(jsonBody)
		if err != nil {
			t.Fatalf("failed to marshal JSON: %v", err)
		}

		url := fmt.Sprintf("http://%s/v1/logs", r.Addr().String())
		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("HTTP POST failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		session := store.GetSession("sess-json-001")
		if session == nil {
			t.Fatal("expected session sess-json-001 to exist")
		}
		if len(session.Events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(session.Events))
		}
		if session.Events[0].Name != "claude_code.user_prompt" {
			t.Errorf("expected event name 'claude_code.user_prompt', got %q", session.Events[0].Name)
		}
	})

	t.Run("invalid_payload_returns_400", func(t *testing.T) {
		store := state.NewMemoryStore()
		r := startTestHTTP(t, store, nil)
		defer r.Stop()

		url := fmt.Sprintf("http://%s/v1/logs", r.Addr().String())
		resp, err := http.Post(url, "application/x-protobuf", bytes.NewReader([]byte("not valid protobuf")))
		if err != nil {
			t.Fatalf("HTTP POST failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400 for invalid payload, got %d", resp.StatusCode)
		}

		// Server should still be operational.
		req := &collogspb.ExportLogsServiceRequest{
			ResourceLogs: []*logspb.ResourceLogs{
				{
					Resource: &resourcepb.Resource{
						Attributes: []*commonpb.KeyValue{
							{
								Key:   "session.id",
								Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "sess-recovery"}},
							},
						},
					},
					ScopeLogs: []*logspb.ScopeLogs{
						{
							LogRecords: []*logspb.LogRecord{
								{
									TimeUnixNano: uint64(time.Now().UnixNano()),
									EventName:    "claude_code.user_prompt",
								},
							},
						},
					},
				},
			},
		}

		body, _ := proto.Marshal(req)
		resp2, err := http.Post(url, "application/x-protobuf", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("recovery POST failed: %v", err)
		}
		defer resp2.Body.Close()

		if resp2.StatusCode != http.StatusOK {
			t.Errorf("expected 200 after recovery, got %d", resp2.StatusCode)
		}

		session := store.GetSession("sess-recovery")
		if session == nil {
			t.Fatal("expected session after recovery from invalid payload")
		}
	})

	t.Run("invalid_json_returns_400", func(t *testing.T) {
		store := state.NewMemoryStore()
		r := startTestHTTP(t, store, nil)
		defer r.Stop()

		url := fmt.Sprintf("http://%s/v1/logs", r.Addr().String())
		resp, err := http.Post(url, "application/json", bytes.NewReader([]byte("{invalid json")))
		if err != nil {
			t.Fatalf("HTTP POST failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400 for invalid JSON, got %d", resp.StatusCode)
		}
	})
}
