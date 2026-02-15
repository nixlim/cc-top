package receiver

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// GRPCReceiver listens for OTLP metrics via gRPC on the configured port.
// It implements the MetricsServiceServer interface from the OTLP proto
// definitions and captures inbound source ports for PID correlation.
type GRPCReceiver struct {
	colmetricspb.UnimplementedMetricsServiceServer

	cfg        config.ReceiverConfig
	store      state.Store
	portMapper PortMapper
	server     *grpc.Server
	listener   net.Listener
}

// NewGRPCReceiver creates a new gRPC-based OTLP metrics receiver.
func NewGRPCReceiver(cfg config.ReceiverConfig, store state.Store, portMapper PortMapper) *GRPCReceiver {
	return &GRPCReceiver{
		cfg:        cfg,
		store:      store,
		portMapper: portMapper,
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
			extractMetrics(r.store, resource, sm.GetMetrics(), sourcePort, r.portMapper)
		}
	}

	return &colmetricspb.ExportMetricsServiceResponse{}, nil
}

// Addr returns the listener's network address, or nil if not started.
// This is primarily useful for testing with ephemeral ports.
func (r *GRPCReceiver) Addr() net.Addr {
	if r.listener != nil {
		return r.listener.Addr()
	}
	return nil
}
