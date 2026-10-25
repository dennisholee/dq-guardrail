// Package dqextproc is C7b: the Envoy ext_proc service. Envoy calls it per request and it
// answers with a decision, so the enforcing happens inside the proxy and the application is
// never touched.
//
// The behaviour is specified in Delivery Design Pack §2.7 and this file follows it row by row:
// request headers are read for the routing key and the correlation id, the request body is
// buffered and decided once on end_of_stream, and the response is either a header mutation
// (clean, warn) or an ImmediateResponse (enforced rejection, or a platform failure). Response
// headers and bodies are never touched.
package dqextproc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/grpc"

	"github.com/dennisholee/dq-guardrail/engine"
)

// defaultMaxBody matches the library adapter. Exceeding it is a platform failure, not a
// violation: the record was never examined.
const defaultMaxBody = 1 << 20

// Config is everything the service needs. Mode comes from the binding, rendered into Envoy
// configuration per route, so the filter never reads a CRD on the request path.
type Config struct {
	Engine  *engine.Engine
	Plans   map[string]*engine.Plan
	Mode    engine.Mode
	MaxBody int64
}

// Server implements envoy.service.ext_proc.v3.ExternalProcessor.
type Server struct {
	extprocv3.UnimplementedExternalProcessorServer
	cfg Config
}

// NewServer validates the configuration at start-up rather than per request. A filter that
// starts without a plan cannot decide anything, and finding that out from live traffic is the
// expensive way to learn it.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Engine == nil {
		return nil, errors.New("ext_proc needs an engine")
	}
	if len(cfg.Plans) == 0 {
		return nil, errors.New("ext_proc needs at least one plan; without one every request passes")
	}
	if cfg.MaxBody == 0 {
		cfg.MaxBody = defaultMaxBody
	}
	return &Server{cfg: cfg}, nil
}

// Register attaches the service to a gRPC server.
func (s *Server) Register(g grpc.ServiceRegistrar) {
	extprocv3.RegisterExternalProcessorServer(g, s)
}

// streamState is one request's state. Envoy opens a stream per request, so nothing here is
// shared and nothing needs locking.
type streamState struct {
	path          string
	correlationID string
	body          []byte
	oversized     bool
	passthrough   bool

	// decided holds what the response phase needs. A header mutation sent in the request phase
	// travels upstream, so the caller-visible digest can only be set when Envoy asks for the
	// response headers (§2.7).
	decided    bool
	digest     string
	violations int
	warn       bool
}

// Process runs one request's stream.
func (s *Server) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	st := &streamState{}
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		resp, err := s.handle(req, st)
		if err != nil {
			// Returning an error ends the stream. Whether that rejects the request or lets it
			// through is Envoy's decision (failure_mode_allow), which §2.7 sets per mode.
			return err
		}
		if resp == nil {
			continue
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

// handle answers one message. It returns nil when no response is owed.
func (s *Server) handle(req *extprocv3.ProcessingRequest, st *streamState) (*extprocv3.ProcessingResponse, error) {
	switch v := req.Request.(type) {
	case *extprocv3.ProcessingRequest_RequestHeaders:
		st.path = headerValue(v.RequestHeaders.GetHeaders(), ":path")
		if st.path == "" {
			// Defensive: Envoy always sends :path. An empty path is not adopted, so the
			// request passes through rather than being decided against the wrong plan.
			st.passthrough = true
		}
		st.correlationID = headerValue(v.RequestHeaders.GetHeaders(), "x-correlation-id")
		_, adopted := s.cfg.Plans[st.path]
		st.passthrough = !adopted || s.cfg.Mode == engine.ModeOff
		return headersContinue(false), nil

	case *extprocv3.ProcessingRequest_RequestBody:
		chunk := v.RequestBody
		if !st.oversized {
			if int64(len(st.body))+int64(len(chunk.GetBody())) > s.cfg.MaxBody {
				// Stop buffering: the record is already over the limit and holding the rest
				// would only make the failure more expensive.
				st.oversized = true
				st.body = nil
			} else {
				st.body = append(st.body, chunk.GetBody()...)
			}
		}
		if !chunk.GetEndOfStream() {
			// Half a party is not a party: a chunk is never decided on its own.
			return bodyContinue(nil), nil
		}
		return s.decide(st), nil

	case *extprocv3.ProcessingRequest_ResponseHeaders:
		return responseHeadersContinue(s.traceability(st)), nil
	case *extprocv3.ProcessingRequest_ResponseBody:
		return bodyContinue(nil), nil
	case *extprocv3.ProcessingRequest_RequestTrailers:
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestTrailers{
			RequestTrailers: &extprocv3.TrailersResponse{}}}, nil
	case *extprocv3.ProcessingRequest_ResponseTrailers:
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseTrailers{
			ResponseTrailers: &extprocv3.TrailersResponse{}}}, nil
	default:
		return nil, nil
	}
}

// decide applies §2.7 steps 3 to 6 to one complete record.
func (s *Server) decide(st *streamState) *extprocv3.ProcessingResponse {
	plan := s.cfg.Plans[st.path]
	if st.passthrough || plan == nil {
		return bodyContinue(nil)
	}
	enforce := s.cfg.Mode == engine.ModeEnforce

	if st.oversized {
		if !enforce {
			return bodyContinue(nil)
		}
		return s.failure(typev3.StatusCode_PayloadTooLarge, 413, "DQ_PAYLOAD_TOO_LARGE",
			"Record too large",
			fmt.Sprintf("the record exceeds the gate's %d byte limit", s.cfg.MaxBody), st)
	}

	var payload map[string]any
	if len(st.body) == 0 || json.Unmarshal(st.body, &payload) != nil {
		if !enforce {
			return bodyContinue(nil)
		}
		return s.failure(typev3.StatusCode_UnsupportedMediaType, 415, "DQ_UNSUPPORTED_MEDIA_TYPE",
			"Unsupported media type",
			"the body is not a JSON object, so it cannot be validated", st)
	}

	decision, err := s.cfg.Engine.Decide(plan, engine.Request{
		Payload:       payload,
		CorrelationID: st.correlationID,
	})
	if err != nil {
		// The error text can embed operand values, so it is not sent: the code names the fault.
		if !enforce {
			return bodyContinue(nil)
		}
		return s.failure(typev3.StatusCode_InternalServerError, 500, "DQ_RULE_ERROR",
			"Rule error",
			"the rule pack could not be evaluated for this request", st)
	}

	digest := s.cfg.Engine.PackDigestFor(plan.Object)
	st.decided, st.digest, st.violations = true, digest, len(decision.Violations)

	if decision.OK() {
		return bodyContinue(nil)
	}

	switch s.cfg.Mode {
	case engine.ModeShadow:
		// Shadow observes: no header, no rejection, no change to the request.
		return bodyContinue(nil)
	case engine.ModeWarn:
		st.warn = true
		return bodyContinue(nil)
	default:
		problem := engine.ProblemFromDecision(decision, st.path, st.correlationID)
		body, err := json.Marshal(problem)
		if err != nil {
			return s.failure(typev3.StatusCode_InternalServerError, 500, "DQ_RULE_ERROR",
				"Rule error", "the refusal could not be rendered", st)
		}
		return immediate(typev3.StatusCode_UnprocessableEntity, body, [][2]string{
			{"content-type", "application/problem+json"},
			{"DQ-Pack-Digest", digest},
			{"x-dq-correlation-id", st.correlationID},
		})
	}
}

// traceability is the header set the caller receives: the digest that decided, plus the warn
// annotations. In an ImmediateResponse these travel inline; on a forwarded request they are
// applied in the response phase.
func (s *Server) traceability(st *streamState) *extprocv3.HeaderMutation {
	if !st.decided || st.digest == "" {
		return nil
	}
	h := setHeaders("DQ-Pack-Digest", st.digest)
	if st.warn {
		h = appendHeader(h, "x-dq-warn", "1")
		h = appendHeader(h, "x-dq-violations", strconv.Itoa(st.violations))
		h = appendHeader(h, "x-dq-correlation-id", st.correlationID)
	}
	return h
}

// failure renders a §2.6 platform failure as an ImmediateResponse, so the upstream is never
// called and the caller learns the gate could not tell rather than that its data is wrong.
func (s *Server) failure(code typev3.StatusCode, status int, dqCode, title, detail string, st *streamState) *extprocv3.ProcessingResponse {
	body, err := json.Marshal(engine.FailureProblem(status, dqCode, title, detail, st.path, st.correlationID))
	if err != nil {
		body = []byte(`{"status":` + strconv.Itoa(status) + `,"code":"` + dqCode + `"}`)
	}
	return immediate(code, body, [][2]string{
		{"content-type", "application/problem+json"},
		{"x-dq-correlation-id", st.correlationID},
	})
}

// headerValue reads one header, preferring the raw bytes Envoy sends.
func headerValue(hm *corev3.HeaderMap, key string) string {
	if hm == nil {
		return ""
	}
	for _, h := range hm.GetHeaders() {
		if h.GetKey() != key {
			continue
		}
		if len(h.GetRawValue()) > 0 {
			return string(h.GetRawValue())
		}
		return h.GetValue()
	}
	return ""
}

func headersContinue(response bool) *extprocv3.ProcessingResponse {
	common := &extprocv3.CommonResponse{Status: extprocv3.CommonResponse_CONTINUE}
	if response {
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &extprocv3.HeadersResponse{Response: common}}}
	}
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{
		RequestHeaders: &extprocv3.HeadersResponse{Response: common}}}
}

func responseHeadersContinue(h *extprocv3.HeaderMutation) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseHeaders{
		ResponseHeaders: &extprocv3.HeadersResponse{Response: &extprocv3.CommonResponse{
			Status:         extprocv3.CommonResponse_CONTINUE,
			HeaderMutation: h,
		}}}}
}

func bodyContinue(h *extprocv3.HeaderMutation) *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestBody{
		RequestBody: &extprocv3.BodyResponse{Response: &extprocv3.CommonResponse{
			Status:         extprocv3.CommonResponse_CONTINUE,
			HeaderMutation: h,
		}}}}
}

// immediate terminates the request inside the proxy: the upstream is never called.
func immediate(code typev3.StatusCode, body []byte, headers [][2]string) *extprocv3.ProcessingResponse {
	set := make([]*corev3.HeaderValueOption, 0, len(headers))
	for _, h := range headers {
		if h[1] == "" {
			continue
		}
		set = append(set, headerOption(h[0], h[1]))
	}
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ImmediateResponse{
		ImmediateResponse: &extprocv3.ImmediateResponse{
			Status:  &typev3.HttpStatus{Code: code},
			Headers: &extprocv3.HeaderMutation{SetHeaders: set},
			Body:    body,
		}}}
}

func setHeaders(key, value string) *extprocv3.HeaderMutation {
	if value == "" {
		return nil
	}
	return &extprocv3.HeaderMutation{SetHeaders: []*corev3.HeaderValueOption{headerOption(key, value)}}
}

func appendHeader(h *extprocv3.HeaderMutation, key, value string) *extprocv3.HeaderMutation {
	if h == nil {
		return setHeaders(key, value)
	}
	h.SetHeaders = append(h.SetHeaders, headerOption(key, value))
	return h
}

func headerOption(key, value string) *corev3.HeaderValueOption {
	return &corev3.HeaderValueOption{Header: &corev3.HeaderValue{
		Key:      key,
		RawValue: []byte(value),
	}}
}
