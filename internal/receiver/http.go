package receiver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/nixlim/cc-top/internal/config"
	"github.com/nixlim/cc-top/internal/state"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

// HTTPReceiver listens for OTLP log exports via HTTP POST on the configured port.
// It supports both protobuf and JSON content types as specified by the OTLP/HTTP
// protocol, and extracts session.id and source port information from each request.
type HTTPReceiver struct {
	cfg        config.ReceiverConfig
	store      state.Store
	portMapper PortMapper
	logger     Logger
	server     *http.Server
	listener   net.Listener
}

// NewHTTPReceiver creates a new HTTP-based OTLP log/event receiver.
func NewHTTPReceiver(cfg config.ReceiverConfig, store state.Store, portMapper PortMapper, logger Logger) *HTTPReceiver {
	return &HTTPReceiver{
		cfg:        cfg,
		store:      store,
		portMapper: portMapper,
		logger:     logger,
	}
}

// Start binds the HTTP server to the configured address and begins accepting
// connections. Returns an error if the port is already in use.
func (r *HTTPReceiver) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", r.cfg.Bind, r.cfg.HTTPPort)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d already in use", r.cfg.HTTPPort)
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

	log.Printf("OTLP HTTP receiver listening on %s", addr)

	go func() {
		if err := r.server.Serve(lis); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server stopped: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server with a 5-second deadline
// for in-flight requests to complete.
func (r *HTTPReceiver) Stop() {
	if r.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.server.Shutdown(ctx); err != nil {
			log.Printf("HTTP server forced shutdown: %v", err)
		}
	}
}

// handleLogs processes incoming OTLP HTTP log export requests. It accepts
// both application/x-protobuf and application/json content types.
// Invalid payloads receive an HTTP 400 response; the server continues operating.
func (r *HTTPReceiver) handleLogs(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		logReceiveError("HTTP", "reading request body", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer req.Body.Close()

	// Extract source port from the remote address.
	sourcePort := 0
	if req.RemoteAddr != "" {
		addr := &netAddr{network: "tcp", addr: req.RemoteAddr}
		sourcePort = sourcePortFromAddr(addr)
	}

	exportReq, err := r.decodeLogsRequest(req.Header.Get("Content-Type"), body)
	if err != nil {
		logReceiveError("HTTP", "decoding payload", err)
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	processLogExport(r.store, r.portMapper, exportReq, sourcePort, r.logger)

	// Return success response.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

// decodeLogsRequest parses the request body based on the content type.
// Supports application/x-protobuf (default) and application/json.
func (r *HTTPReceiver) decodeLogsRequest(contentType string, body []byte) (*collogspb.ExportLogsServiceRequest, error) {
	exportReq := &collogspb.ExportLogsServiceRequest{}

	switch contentType {
	case "application/json":
		if err := decodeLogsJSON(body, exportReq); err != nil {
			return nil, fmt.Errorf("JSON decode: %w", err)
		}
	default:
		// Default to protobuf (application/x-protobuf or empty content type).
		if err := proto.Unmarshal(body, exportReq); err != nil {
			return nil, fmt.Errorf("protobuf decode: %w", err)
		}
	}

	return exportReq, nil
}

// handleMetrics processes incoming OTLP HTTP metric export requests.
// It accepts application/x-protobuf content type (default).
// Invalid payloads receive an HTTP 400 response; the server continues operating.
func (r *HTTPReceiver) handleMetrics(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		logReceiveError("HTTP", "reading metrics request body", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer req.Body.Close()

	// Extract source port from the remote address.
	sourcePort := 0
	if req.RemoteAddr != "" {
		addr := &netAddr{network: "tcp", addr: req.RemoteAddr}
		sourcePort = sourcePortFromAddr(addr)
	}

	exportReq, err := r.decodeMetricsRequest(req.Header.Get("Content-Type"), body)
	if err != nil {
		logReceiveError("HTTP", "decoding metrics payload", err)
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	for _, rm := range exportReq.GetResourceMetrics() {
		resource := rm.GetResource()
		for _, sm := range rm.GetScopeMetrics() {
			extractMetrics(r.store, resource, sm.GetMetrics(), sourcePort, r.portMapper, r.logger)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

// decodeMetricsRequest parses the metrics request body based on content type.
// Supports application/x-protobuf (default).
func (r *HTTPReceiver) decodeMetricsRequest(contentType string, body []byte) (*colmetricspb.ExportMetricsServiceRequest, error) {
	exportReq := &colmetricspb.ExportMetricsServiceRequest{}

	// Default to protobuf decoding.
	if err := proto.Unmarshal(body, exportReq); err != nil {
		return nil, fmt.Errorf("protobuf decode: %w", err)
	}

	return exportReq, nil
}

// decodeLogsJSON decodes a JSON-encoded OTLP logs export request.
// This handles the simplified JSON representation used by OTLP/HTTP.
func decodeLogsJSON(body []byte, out *collogspb.ExportLogsServiceRequest) error {
	// Parse the JSON into a generic structure, then convert to proto.
	var raw jsonExportLogsRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}

	for _, rl := range raw.ResourceLogs {
		resourceLogs := &logspb.ResourceLogs{}

		if rl.Resource != nil {
			resourceLogs.Resource = &resourcepb.Resource{}
			for _, attr := range rl.Resource.Attributes {
				resourceLogs.Resource.Attributes = append(resourceLogs.Resource.Attributes,
					jsonAttrToKV(attr))
			}
		}

		for _, sl := range rl.ScopeLogs {
			scopeLog := &logspb.ScopeLogs{}
			for _, lr := range sl.LogRecords {
				logRecord := &logspb.LogRecord{
					TimeUnixNano: lr.TimeUnixNano,
					EventName:    lr.EventName,
				}
				if lr.Body != nil {
					logRecord.Body = jsonValueToAnyValue(lr.Body)
				}
				for _, attr := range lr.Attributes {
					logRecord.Attributes = append(logRecord.Attributes,
						jsonAttrToKV(attr))
				}
				scopeLog.LogRecords = append(scopeLog.LogRecords, logRecord)
			}
			resourceLogs.ScopeLogs = append(resourceLogs.ScopeLogs, scopeLog)
		}

		out.ResourceLogs = append(out.ResourceLogs, resourceLogs)
	}

	return nil
}

// Addr returns the listener's network address, or nil if not started.
// This is primarily useful for testing with ephemeral ports.
func (r *HTTPReceiver) Addr() net.Addr {
	if r.listener != nil {
		return r.listener.Addr()
	}
	return nil
}

// netAddr implements net.Addr for extracting source ports from HTTP RemoteAddr.
type netAddr struct {
	network string
	addr    string
}

func (a *netAddr) Network() string { return a.network }
func (a *netAddr) String() string  { return a.addr }

// JSON types for OTLP/HTTP log export decoding.

type jsonExportLogsRequest struct {
	ResourceLogs []jsonResourceLogs `json:"resourceLogs"`
}

type jsonResourceLogs struct {
	Resource  *jsonResource   `json:"resource"`
	ScopeLogs []jsonScopeLogs `json:"scopeLogs"`
}

type jsonResource struct {
	Attributes []jsonKeyValue `json:"attributes"`
}

type jsonScopeLogs struct {
	LogRecords []jsonLogRecord `json:"logRecords"`
}

type jsonLogRecord struct {
	TimeUnixNano uint64         `json:"timeUnixNano,string"`
	EventName    string         `json:"eventName"`
	Body         *jsonAnyValue  `json:"body"`
	Attributes   []jsonKeyValue `json:"attributes"`
}

type jsonKeyValue struct {
	Key   string       `json:"key"`
	Value jsonAnyValue `json:"value"`
}

type jsonAnyValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    *string  `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
}

func jsonAttrToKV(attr jsonKeyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   attr.Key,
		Value: jsonValueToAnyValue(&attr.Value),
	}
}

func jsonValueToAnyValue(v *jsonAnyValue) *commonpb.AnyValue {
	if v == nil {
		return nil
	}
	if v.StringValue != nil {
		return &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: *v.StringValue},
		}
	}
	if v.IntValue != nil {
		// OTLP JSON encodes int64 as string.
		i, _ := fmt.Sscanf(*v.IntValue, "%d", new(int64))
		_ = i
		var val int64
		fmt.Sscanf(*v.IntValue, "%d", &val)
		return &commonpb.AnyValue{
			Value: &commonpb.AnyValue_IntValue{IntValue: val},
		}
	}
	if v.DoubleValue != nil {
		return &commonpb.AnyValue{
			Value: &commonpb.AnyValue_DoubleValue{DoubleValue: *v.DoubleValue},
		}
	}
	if v.BoolValue != nil {
		return &commonpb.AnyValue{
			Value: &commonpb.AnyValue_BoolValue{BoolValue: *v.BoolValue},
		}
	}
	return &commonpb.AnyValue{
		Value: &commonpb.AnyValue_StringValue{StringValue: ""},
	}
}
