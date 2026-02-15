// Package receiver implements OTLP gRPC and HTTP receivers for ingesting
// OpenTelemetry metrics and log events from Claude Code instances.
//
// The receivers extract session.id from resource and metric/log attributes,
// store data in the state store, and capture inbound connection source ports
// for PID correlation.
package receiver

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

// PortMapper records the mapping between inbound connection source ports and
// session IDs for PID correlation. Implementations must be safe for concurrent use.
type PortMapper interface {
	// RecordSourcePort associates an inbound source port with a session ID.
	RecordSourcePort(sourcePort int, sessionID string)
}

// Receiver manages both gRPC and HTTP OTLP receivers.
type Receiver struct {
	grpc *GRPCReceiver
	http *HTTPReceiver
}

// New creates a new Receiver with gRPC and HTTP endpoints configured from cfg.
// The store is used to persist received metrics and events.
// portMapper may be nil if port correlation is not needed.
func New(cfg config.ReceiverConfig, store state.Store, portMapper PortMapper) *Receiver {
	return &Receiver{
		grpc: NewGRPCReceiver(cfg, store, portMapper),
		http: NewHTTPReceiver(cfg, store, portMapper),
	}
}

// Start begins listening on both gRPC and HTTP endpoints.
// Returns an error if either port is already in use.
func (r *Receiver) Start(ctx context.Context) error {
	if err := r.grpc.Start(ctx); err != nil {
		return err
	}
	if err := r.http.Start(ctx); err != nil {
		// Stop gRPC if HTTP failed to start.
		r.grpc.Stop()
		return err
	}
	return nil
}

// Stop gracefully shuts down both receivers. It drains in-flight requests
// for up to 5 seconds before forcing closure.
func (r *Receiver) Stop() {
	r.grpc.Stop()
	r.http.Stop()
}

// extractSessionID searches for session.id in resource attributes first,
// then falls back to the provided key-value attributes.
func extractSessionID(resource *resourcepb.Resource, attrs []*commonpb.KeyValue) string {
	// Check resource attributes first.
	if resource != nil {
		for _, kv := range resource.GetAttributes() {
			if kv.GetKey() == "session.id" {
				return anyValueToString(kv.GetValue())
			}
		}
	}
	// Fall back to data-point / log-record attributes.
	for _, kv := range attrs {
		if kv.GetKey() == "session.id" {
			return anyValueToString(kv.GetValue())
		}
	}
	return ""
}

// kvToMap converts a slice of OTLP KeyValue pairs to a string map.
// Nested or complex values are converted to their string representation.
func kvToMap(kvs []*commonpb.KeyValue) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		m[kv.GetKey()] = anyValueToString(kv.GetValue())
	}
	return m
}

// anyValueToString extracts a string representation from an OTLP AnyValue.
func anyValueToString(v *commonpb.AnyValue) string {
	if v == nil {
		return ""
	}
	switch val := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return val.StringValue
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(val.IntValue, 10)
	case *commonpb.AnyValue_DoubleValue:
		return strconv.FormatFloat(val.DoubleValue, 'f', -1, 64)
	case *commonpb.AnyValue_BoolValue:
		return strconv.FormatBool(val.BoolValue)
	default:
		return fmt.Sprintf("%v", v.GetValue())
	}
}

// extractMetrics converts OTLP metric data points into state.Metric values
// and stores them in the state store, keyed by session ID.
func extractMetrics(store state.Store, resource *resourcepb.Resource, metrics []*metricspb.Metric, sourcePort int, portMapper PortMapper) {
	for _, m := range metrics {
		var dataPoints []*metricspb.NumberDataPoint

		switch d := m.GetData().(type) {
		case *metricspb.Metric_Sum:
			if d.Sum != nil {
				dataPoints = d.Sum.GetDataPoints()
			}
		case *metricspb.Metric_Gauge:
			if d.Gauge != nil {
				dataPoints = d.Gauge.GetDataPoints()
			}
		default:
			// Skip histogram, summary, exponential histogram for now.
			continue
		}

		for _, dp := range dataPoints {
			sessionID := extractSessionID(resource, dp.GetAttributes())

			// Record source port mapping for PID correlation.
			if portMapper != nil && sessionID != "" && sourcePort > 0 {
				portMapper.RecordSourcePort(sourcePort, sessionID)
			}

			// Extract numeric value from the data point.
			var value float64
			switch v := dp.GetValue().(type) {
			case *metricspb.NumberDataPoint_AsDouble:
				value = v.AsDouble
			case *metricspb.NumberDataPoint_AsInt:
				value = float64(v.AsInt)
			}

			ts := time.Unix(0, int64(dp.GetTimeUnixNano()))
			if dp.GetTimeUnixNano() == 0 {
				ts = time.Now()
			}

			sm := state.Metric{
				Name:       m.GetName(),
				Value:      value,
				Attributes: kvToMap(dp.GetAttributes()),
				Timestamp:  ts,
			}

			store.AddMetric(sessionID, sm)
		}
	}
}

// sourcePortFromAddr extracts the port number from a net.Addr string.
func sourcePortFromAddr(addr net.Addr) int {
	if addr == nil {
		return 0
	}
	_, portStr, err := net.SplitHostPort(addr.String())
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return port
}

// logReceiveError logs a receive error at warning level.
func logReceiveError(protocol, detail string, err error) {
	log.Printf("WARNING: %s receiver: %s: %v", protocol, detail, err)
}
