package receiver

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// GRPCReceiver listens for OTLP metrics and log events via gRPC on the configured port.
// It implements MetricsServiceServer for metrics. Log events are handled by an internal
// grpcLogsHandler that implements LogsServiceServer separately, since both interfaces
// define an Export method with different signatures.
type GRPCReceiver struct {
	colmetricspb.UnimplementedMetricsServiceServer

	cfg        config.ReceiverConfig
	store      state.Store
	portMapper PortMapper
	logger     Logger
	server     *grpc.Server
	listener   net.Listener
}

// grpcLogsHandler implements LogsServiceServer for the gRPC receiver.
// It is a separate type because LogsServiceServer.Export and MetricsServiceServer.Export
// have conflicting signatures and cannot coexist on the same struct.
type grpcLogsHandler struct {
	collogspb.UnimplementedLogsServiceServer

	store      state.Store
	portMapper PortMapper
	logger     Logger
}

// NewGRPCReceiver creates a new gRPC-based OTLP metrics receiver.
func NewGRPCReceiver(cfg config.ReceiverConfig, store state.Store, portMapper PortMapper, logger Logger) *GRPCReceiver {
	return &GRPCReceiver{
		cfg:        cfg,
		store:      store,
		portMapper: portMapper,
		logger:     logger,
	}
}

// Start binds the gRPC server to the configured address and begins accepting
// connections. Returns an error if the port is already in use.
func (r *GRPCReceiver) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", r.cfg.Bind, r.cfg.GRPCPort)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d already in use", r.cfg.GRPCPort)
	}
	r.listener = lis

	r.server = grpc.NewServer()
	colmetricspb.RegisterMetricsServiceServer(r.server, r)
	collogspb.RegisterLogsServiceServer(r.server, &grpcLogsHandler{
		store:      r.store,
		portMapper: r.portMapper,
		logger:     r.logger,
	})

	log.Printf("OTLP gRPC receiver listening on %s", addr)

	go func() {
		if err := r.server.Serve(lis); err != nil {
			log.Printf("gRPC server stopped: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the gRPC server. Pending RPCs are given a brief
// window to complete before the server is forcefully stopped.
func (r *GRPCReceiver) Stop() {
	if r.server != nil {
		r.server.GracefulStop()
	}
}

// Export handles incoming ExportMetricsServiceRequest RPCs. It extracts
// session.id from resource and metric attributes, stores the metrics in the
// state store, and records the inbound source port for PID correlation.
//
// Malformed payloads return an OTLP error response without crashing the server.
func (r *GRPCReceiver) Export(ctx context.Context, req *colmetricspb.ExportMetricsServiceRequest) (*colmetricspb.ExportMetricsServiceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	// Extract source port from the peer address for PID correlation.
	sourcePort := 0
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		sourcePort = sourcePortFromAddr(p.Addr)
	}

	for _, rm := range req.GetResourceMetrics() {
		resource := rm.GetResource()

		for _, sm := range rm.GetScopeMetrics() {
			extractMetrics(r.store, resource, sm.GetMetrics(), sourcePort, r.portMapper, r.logger)
		}
	}

	return &colmetricspb.ExportMetricsServiceResponse{}, nil
}

// Export handles incoming ExportLogsServiceRequest RPCs. It extracts
// session.id from resource and log record attributes, stores the events in the
// state store, and records the inbound source port for PID correlation.
func (h *grpcLogsHandler) Export(ctx context.Context, req *collogspb.ExportLogsServiceRequest) (*collogspb.ExportLogsServiceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "empty request")
	}

	// Extract source port from the peer address for PID correlation.
	sourcePort := 0
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		sourcePort = sourcePortFromAddr(p.Addr)
	}

	processLogExport(h.store, h.portMapper, req, sourcePort, h.logger)

	return &collogspb.ExportLogsServiceResponse{}, nil
}

// Addr returns the listener's network address, or nil if not started.
// This is primarily useful for testing with ephemeral ports.
func (r *GRPCReceiver) Addr() net.Addr {
	if r.listener != nil {
		return r.listener.Addr()
	}
	return nil
}
