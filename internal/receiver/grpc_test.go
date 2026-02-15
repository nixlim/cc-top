package receiver

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
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

// startTestGRPC creates a gRPC receiver on an ephemeral port and returns
// the receiver, a connected client, and the client connection for cleanup.
func startTestGRPC(t *testing.T, store state.Store, pm PortMapper) (*GRPCReceiver, colmetricspb.MetricsServiceClient, *grpc.ClientConn) {
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

	client := colmetricspb.NewMetricsServiceClient(conn)
	return r, client, conn
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
	r, client, conn := startTestGRPC(t, store, pm)
	defer func() {
		conn.Close()
		r.Stop()
	}()

	ctx := context.Background()

	// Send a cost metric for session "sess-001".
	req := makeCostMetricRequest("sess-001", 0.50)
	resp, err := client.Export(ctx, req)
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
	_, err = client.Export(ctx, req2)
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

func TestOTLPReceiver_MalformedPayload(t *testing.T) {
	store := state.NewMemoryStore()
	r, client, conn := startTestGRPC(t, store, nil)
	defer func() {
		conn.Close()
		r.Stop()
	}()

	ctx := context.Background()

	// Send a nil request. The gRPC framework handles complete garbage at the
	// protobuf level, so we test with an empty request which our handler
	// should handle gracefully.
	resp, err := client.Export(ctx, &colmetricspb.ExportMetricsServiceRequest{})
	if err != nil {
		t.Fatalf("empty request should succeed: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response for empty request")
	}

	// Server should still be operational after the empty request.
	req := makeCostMetricRequest("sess-002", 0.10)
	resp, err = client.Export(ctx, req)
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
