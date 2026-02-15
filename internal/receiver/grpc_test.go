package receiver

import (
	"context"
	"fmt"
	"net"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// testPortMapper records source port to session ID mappings for testing.
type testPortMapper struct {
	mappings map[int]string
}

func newTestPortMapper() *testPortMapper {
	return &testPortMapper{mappings: make(map[int]string)}
}

func (m *testPortMapper) RecordSourcePort(sourcePort int, sessionID string) {
	m.mappings[sourcePort] = sessionID
}

// grpcTestClients holds both metric and log gRPC clients for testing.
type grpcTestClients struct {
	metrics colmetricspb.MetricsServiceClient
	logs    collogspb.LogsServiceClient
}

// startTestGRPC creates a gRPC receiver on an ephemeral port and returns
// the receiver, connected clients (metrics + logs), and the client connection for cleanup.
func startTestGRPC(t *testing.T, store state.Store, pm PortMapper) (*GRPCReceiver, grpcTestClients, *grpc.ClientConn) {
	t.Helper()

	cfg := config.ReceiverConfig{
		GRPCPort: 0, // Use ephemeral port for tests.
		Bind:     "127.0.0.1",
	}

	r := NewGRPCReceiver(cfg, store, pm)

	// Manually bind to an ephemeral port.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	r.listener = lis

	r.server = grpc.NewServer()
	colmetricspb.RegisterMetricsServiceServer(r.server, r)
	collogspb.RegisterLogsServiceServer(r.server, &grpcLogsHandler{
		store:      r.store,
		portMapper: r.portMapper,
	})

	go func() {
		_ = r.server.Serve(lis)
	}()

	// Connect a client.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		r.Stop()
		t.Fatalf("failed to connect gRPC client: %v", err)
	}

	clients := grpcTestClients{
		metrics: colmetricspb.NewMetricsServiceClient(conn),
		logs:    collogspb.NewLogsServiceClient(conn),
	}
	return r, clients, conn
}

// makeCostMetricRequest creates an ExportMetricsServiceRequest with a
// claude_code.cost.usage metric for the given session and value.
func makeCostMetricRequest(sessionID string, value float64) *colmetricspb.ExportMetricsServiceRequest {
	ts := uint64(time.Now().UnixNano())
	return &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{
							Key:   "session.id",
							Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: sessionID}},
						},
						{
							Key:   "service.name",
							Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-code"}},
						},
					},
				},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Metrics: []*metricspb.Metric{
							{
								Name: "claude_code.cost.usage",
								Unit: "USD",
								Data: &metricspb.Metric_Sum{
									Sum: &metricspb.Sum{
										DataPoints: []*metricspb.NumberDataPoint{
											{
												TimeUnixNano: ts,
												Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: value},
												Attributes: []*commonpb.KeyValue{
													{
														Key:   "model",
														Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-sonnet-4-5-20250929"}},
													},
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
}

func TestOTLPReceiver_GRPCMetrics(t *testing.T) {
	store := state.NewMemoryStore()
	pm := newTestPortMapper()
	r, clients, conn := startTestGRPC(t, store, pm)
	defer func() {
		conn.Close()
		r.Stop()
	}()

	ctx := context.Background()

	// Send a cost metric for session "sess-001".
	req := makeCostMetricRequest("sess-001", 0.50)
	resp, err := clients.metrics.Export(ctx, req)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// Verify the metric was stored.
	session := store.GetSession("sess-001")
	if session == nil {
		t.Fatal("expected session sess-001 to exist in store")
	}
	if session.TotalCost != 0.50 {
		t.Errorf("expected TotalCost=0.50, got %f", session.TotalCost)
	}
	if len(session.Metrics) != 1 {
		t.Errorf("expected 1 metric, got %d", len(session.Metrics))
	}
	if session.Metrics[0].Name != "claude_code.cost.usage" {
		t.Errorf("expected metric name 'claude_code.cost.usage', got %q", session.Metrics[0].Name)
	}

	// Send a second metric with higher cumulative value.
	req2 := makeCostMetricRequest("sess-001", 1.25)
	_, err = clients.metrics.Export(ctx, req2)
	if err != nil {
		t.Fatalf("second Export failed: %v", err)
	}

	session = store.GetSession("sess-001")
	if session == nil {
		t.Fatal("expected session to exist after second metric")
	}
	// TotalCost should be 0.50 (first delta) + 0.75 (second delta) = 1.25
	if session.TotalCost < 1.24 || session.TotalCost > 1.26 {
		t.Errorf("expected TotalCost ~1.25, got %f", session.TotalCost)
	}

	// Verify source port mapping was recorded.
	if len(pm.mappings) == 0 {
		t.Error("expected at least one source port mapping")
	}
	found := false
	for _, sid := range pm.mappings {
		if sid == "sess-001" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected source port mapped to sess-001")
	}
}

func TestOTLPReceiver_GRPCLogs(t *testing.T) {
	store := state.NewMemoryStore()
	pm := newTestPortMapper()
	r, clients, conn := startTestGRPC(t, store, pm)
	defer func() {
		conn.Close()
		r.Stop()
	}()

	ctx := context.Background()

	// Send an api_request event via gRPC logs.
	ts := uint64(time.Now().UnixNano())
	req := &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{
							Key:   "session.id",
							Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "sess-grpc-logs-001"}},
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
									{Key: "cost_usd", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "0.07"}}},
									{Key: "input_tokens", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 2000}}},
									{Key: "output_tokens", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 500}}},
									{Key: "duration_ms", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 3200}}},
								},
							},
						},
					},
				},
			},
		},
	}

	resp, err := clients.logs.Export(ctx, req)
	if err != nil {
		t.Fatalf("gRPC logs Export failed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// Verify the event was stored.
	session := store.GetSession("sess-grpc-logs-001")
	if session == nil {
		t.Fatal("expected session sess-grpc-logs-001 to exist")
	}
	if len(session.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(session.Events))
	}
	if session.Events[0].Name != "claude_code.api_request" {
		t.Errorf("expected event name 'claude_code.api_request', got %q", session.Events[0].Name)
	}
	if session.Events[0].Attributes["model"] != "claude-sonnet-4-5-20250929" {
		t.Errorf("expected model attribute 'claude-sonnet-4-5-20250929', got %q", session.Events[0].Attributes["model"])
	}

	// Verify source port mapping was recorded.
	found := false
	for _, sid := range pm.mappings {
		if sid == "sess-grpc-logs-001" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected source port mapped to sess-grpc-logs-001")
	}
}

func TestOTLPReceiver_GRPCLogs_MultipleEventTypes(t *testing.T) {
	store := state.NewMemoryStore()
	r, clients, conn := startTestGRPC(t, store, nil)
	defer func() {
		conn.Close()
		r.Stop()
	}()

	ctx := context.Background()
	ts := uint64(time.Now().UnixNano())

	// Send multiple event types in a single request.
	req := &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{
							Key:   "session.id",
							Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "sess-multi"}},
						},
					},
				},
				ScopeLogs: []*logspb.ScopeLogs{
					{
						LogRecords: []*logspb.LogRecord{
							{
								TimeUnixNano: ts,
								EventName:    "claude_code.user_prompt",
								Attributes: []*commonpb.KeyValue{
									{Key: "prompt_length", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "42"}}},
								},
							},
							{
								TimeUnixNano: ts + 1000,
								EventName:    "claude_code.tool_result",
								Attributes: []*commonpb.KeyValue{
									{Key: "tool_name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "Read"}}},
									{Key: "success", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "true"}}},
									{Key: "duration_ms", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "150"}}},
								},
							},
							{
								TimeUnixNano: ts + 2000,
								EventName:    "claude_code.api_error",
								Attributes: []*commonpb.KeyValue{
									{Key: "status_code", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "529"}}},
									{Key: "error", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "overloaded"}}},
									{Key: "attempt", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "1"}}},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := clients.logs.Export(ctx, req)
	if err != nil {
		t.Fatalf("gRPC logs Export failed: %v", err)
	}

	session := store.GetSession("sess-multi")
	if session == nil {
		t.Fatal("expected session sess-multi to exist")
	}
	if len(session.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(session.Events))
	}

	// Verify event ordering and names.
	expectedNames := []string{"claude_code.user_prompt", "claude_code.tool_result", "claude_code.api_error"}
	for i, name := range expectedNames {
		if session.Events[i].Name != name {
			t.Errorf("event[%d]: expected name %q, got %q", i, name, session.Events[i].Name)
		}
	}
}

func TestOTLPReceiver_GRPCLogs_EmptyRequest(t *testing.T) {
	store := state.NewMemoryStore()
	r, clients, conn := startTestGRPC(t, store, nil)
	defer func() {
		conn.Close()
		r.Stop()
	}()

	ctx := context.Background()

	// Empty request should succeed without storing anything.
	resp, err := clients.logs.Export(ctx, &collogspb.ExportLogsServiceRequest{})
	if err != nil {
		t.Fatalf("empty logs request should succeed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response for empty logs request")
	}

	// No sessions should have been created.
	sessions := store.ListSessions()
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after empty request, got %d", len(sessions))
	}
}

func TestOTLPReceiver_MalformedPayload(t *testing.T) {
	store := state.NewMemoryStore()
	r, clients, conn := startTestGRPC(t, store, nil)
	defer func() {
		conn.Close()
		r.Stop()
	}()

	ctx := context.Background()

	// Send a nil request. The gRPC framework handles complete garbage at the
	// protobuf level, so we test with an empty request which our handler
	// should handle gracefully.
	resp, err := clients.metrics.Export(ctx, &colmetricspb.ExportMetricsServiceRequest{})
	if err != nil {
		t.Fatalf("empty request should succeed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response for empty request")
	}

	// Server should still be operational after the empty request.
	req := makeCostMetricRequest("sess-002", 0.10)
	resp, err = clients.metrics.Export(ctx, req)
	if err != nil {
		t.Fatalf("Export after empty request failed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	session := store.GetSession("sess-002")
	if session == nil {
		t.Fatal("expected session sess-002 after recovery from empty request")
	}
}

func TestOTLPReceiver_GRPCMetrics_ResourceMetadata(t *testing.T) {
	store := state.NewMemoryStore()
	r, clients, conn := startTestGRPC(t, store, nil)
	defer func() {
		conn.Close()
		r.Stop()
	}()

	ctx := context.Background()
	ts := uint64(time.Now().UnixNano())

	// Send a metric with resource attributes including metadata fields.
	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "session.id", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "sess-meta-grpc"}}},
						{Key: "service.version", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "1.0.32"}}},
						{Key: "os.type", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "darwin"}}},
						{Key: "os.version", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "24.1.0"}}},
						{Key: "host.arch", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "arm64"}}},
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
												Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 0.10},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := clients.metrics.Export(ctx, req)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	session := store.GetSession("sess-meta-grpc")
	if session == nil {
		t.Fatal("expected session to exist")
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
	if session.SessionID != "sess-meta-grpc" {
		t.Errorf("expected SessionID=sess-meta-grpc, got %q", session.SessionID)
	}
}

func TestOTLPReceiver_GRPCLogs_ResourceMetadata(t *testing.T) {
	store := state.NewMemoryStore()
	r, clients, conn := startTestGRPC(t, store, nil)
	defer func() {
		conn.Close()
		r.Stop()
	}()

	ctx := context.Background()
	ts := uint64(time.Now().UnixNano())

	req := &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "session.id", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "sess-meta-logs"}}},
						{Key: "service.version", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "1.0.32"}}},
						{Key: "os.type", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "linux"}}},
					},
				},
				ScopeLogs: []*logspb.ScopeLogs{
					{
						LogRecords: []*logspb.LogRecord{
							{
								TimeUnixNano: ts,
								EventName:    "claude_code.user_prompt",
							},
						},
					},
				},
			},
		},
	}

	_, err := clients.logs.Export(ctx, req)
	if err != nil {
		t.Fatalf("gRPC logs Export failed: %v", err)
	}

	session := store.GetSession("sess-meta-logs")
	if session == nil {
		t.Fatal("expected session to exist")
	}
	if session.Metadata.ServiceVersion != "1.0.32" {
		t.Errorf("expected ServiceVersion=1.0.32, got %q", session.Metadata.ServiceVersion)
	}
	if session.Metadata.OSType != "linux" {
		t.Errorf("expected OSType=linux, got %q", session.Metadata.OSType)
	}
	// Missing attributes should be empty strings.
	if session.Metadata.OSVersion != "" {
		t.Errorf("expected empty OSVersion, got %q", session.Metadata.OSVersion)
	}
	if session.Metadata.HostArch != "" {
		t.Errorf("expected empty HostArch, got %q", session.Metadata.HostArch)
	}
}

func TestOTLPReceiver_PortConflict(t *testing.T) {
	// Bind to a port first to create a conflict.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer lis.Close()

	port := lis.Addr().(*net.TCPAddr).Port

	store := state.NewMemoryStore()
	cfg := config.ReceiverConfig{
		GRPCPort: port,
		Bind:     "127.0.0.1",
	}

	r := NewGRPCReceiver(cfg, store, nil)
	err = r.Start(context.Background())
	if err == nil {
		r.Stop()
		t.Fatal("expected error for port conflict")
	}

	expected := fmt.Sprintf("port %d already in use", port)
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}
