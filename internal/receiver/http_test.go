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
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
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
	mux.HandleFunc("/v1/metrics", r.handleMetrics)
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

func TestOTLPReceiver_HTTPEvents_ResourceMetadata(t *testing.T) {
	store := state.NewMemoryStore()
	r := startTestHTTP(t, store, nil)
	defer r.Stop()

	// Build an OTLP log export request with resource metadata attributes.
	ts := uint64(time.Now().UnixNano())
	req := &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "session.id", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "sess-http-meta"}}},
						{Key: "service.version", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "1.0.32"}}},
						{Key: "os.type", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "darwin"}}},
						{Key: "os.version", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "24.1.0"}}},
						{Key: "host.arch", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "arm64"}}},
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

	session := store.GetSession("sess-http-meta")
	if session == nil {
		t.Fatal("expected session sess-http-meta to exist")
	}
	if session.Metadata.ServiceVersion != "1.0.32" {
		t.Errorf("expected ServiceVersion=1.0.32, got %q", session.Metadata.ServiceVersion)
	}
	if session.Metadata.OSType != "darwin" {
		t.Errorf("expected OSType=darwin, got %q", session.Metadata.OSType)
	}
	if session.Metadata.OSVersion != "24.1.0" {
		t.Errorf("expected OSVersion=24.1.0, got %q", session.Metadata.OSVersion)
	}
	if session.Metadata.HostArch != "arm64" {
		t.Errorf("expected HostArch=arm64, got %q", session.Metadata.HostArch)
	}
	// session.id extraction should still work.
	if session.SessionID != "sess-http-meta" {
		t.Errorf("expected SessionID=sess-http-meta, got %q", session.SessionID)
	}
}

func TestOTLPReceiver_HTTPMetrics(t *testing.T) {
	t.Run("valid_protobuf_returns_200", func(t *testing.T) {
		store := state.NewMemoryStore()
		pm := newTestPortMapper()
		r := startTestHTTP(t, store, pm)
		defer r.Stop()

		ts := uint64(time.Now().UnixNano())
		req := &colmetricspb.ExportMetricsServiceRequest{
			ResourceMetrics: []*metricspb.ResourceMetrics{
				{
					Resource: &resourcepb.Resource{
						Attributes: []*commonpb.KeyValue{
							{Key: "session.id", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "sess-http-metrics-001"}}},
						},
					},
					ScopeMetrics: []*metricspb.ScopeMetrics{
						{
							Metrics: []*metricspb.Metric{
								{
									Name: "claude_code.cost.usage",
									Data: &metricspb.Metric_Sum{
										Sum: &metricspb.Sum{
											DataPoints: []*metricspb.NumberDataPoint{
												{
													TimeUnixNano: ts,
													Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 0.25},
													Attributes: []*commonpb.KeyValue{
														{Key: "model", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-sonnet-4-5-20250929"}}},
													},
												},
											},
											IsMonotonic: true,
										},
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

		url := fmt.Sprintf("http://%s/v1/metrics", r.Addr().String())
		resp, err := http.Post(url, "application/x-protobuf", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("HTTP POST failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		// Verify the metric was stored.
		session := store.GetSession("sess-http-metrics-001")
		if session == nil {
			t.Fatal("expected session sess-http-metrics-001 to exist")
		}
		if session.TotalCost != 0.25 {
			t.Errorf("expected TotalCost=0.25, got %f", session.TotalCost)
		}
		if len(session.Metrics) != 1 {
			t.Errorf("expected 1 metric, got %d", len(session.Metrics))
		}
		if session.Metrics[0].Name != "claude_code.cost.usage" {
			t.Errorf("expected metric name 'claude_code.cost.usage', got %q", session.Metrics[0].Name)
		}
	})

	t.Run("GET_returns_405", func(t *testing.T) {
		store := state.NewMemoryStore()
		r := startTestHTTP(t, store, nil)
		defer r.Stop()

		url := fmt.Sprintf("http://%s/v1/metrics", r.Addr().String())
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("HTTP GET failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", resp.StatusCode)
		}
	})

	t.Run("malformed_payload_returns_400", func(t *testing.T) {
		store := state.NewMemoryStore()
		r := startTestHTTP(t, store, nil)
		defer r.Stop()

		url := fmt.Sprintf("http://%s/v1/metrics", r.Addr().String())
		resp, err := http.Post(url, "application/x-protobuf", bytes.NewReader([]byte("not valid protobuf")))
		if err != nil {
			t.Fatalf("HTTP POST failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("metrics_appear_in_store", func(t *testing.T) {
		store := state.NewMemoryStore()
		r := startTestHTTP(t, store, nil)
		defer r.Stop()

		ts := uint64(time.Now().UnixNano())
		req := &colmetricspb.ExportMetricsServiceRequest{
			ResourceMetrics: []*metricspb.ResourceMetrics{
				{
					Resource: &resourcepb.Resource{
						Attributes: []*commonpb.KeyValue{
							{Key: "session.id", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "sess-http-metrics-store"}}},
						},
					},
					ScopeMetrics: []*metricspb.ScopeMetrics{
						{
							Metrics: []*metricspb.Metric{
								{
									Name: "claude_code.token.usage",
									Data: &metricspb.Metric_Sum{
										Sum: &metricspb.Sum{
											DataPoints: []*metricspb.NumberDataPoint{
												{
													TimeUnixNano: ts,
													Value:        &metricspb.NumberDataPoint_AsInt{AsInt: 5000},
												},
											},
											IsMonotonic: true,
										},
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

		url := fmt.Sprintf("http://%s/v1/metrics", r.Addr().String())
		resp, err := http.Post(url, "application/x-protobuf", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("HTTP POST failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		session := store.GetSession("sess-http-metrics-store")
		if session == nil {
			t.Fatal("expected session to exist after metric ingestion")
		}
		if session.TotalTokens != 5000 {
			t.Errorf("expected TotalTokens=5000, got %d", session.TotalTokens)
		}
	})
}
