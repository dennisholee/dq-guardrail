package dqextproc

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"

	"github.com/dennisholee/dq-guardrail/engine"
)

const sampleDir = "../../../examples/party-model"

// fakeStream feeds a fixed message sequence and records what the server answers.
type fakeStream struct {
	grpc.ServerStream
	requests []*extprocv3.ProcessingRequest
	next     int
	sent     []*extprocv3.ProcessingResponse
}

func (f *fakeStream) Context() context.Context { return context.Background() }

func (f *fakeStream) Recv() (*extprocv3.ProcessingRequest, error) {
	if f.next >= len(f.requests) {
		return nil, io.EOF
	}
	r := f.requests[f.next]
	f.next++
	return r, nil
}

func (f *fakeStream) Send(r *extprocv3.ProcessingResponse) error {
	f.sent = append(f.sent, r)
	return nil
}

// testServer builds the service over the sample, breaking nothing.
func testServer(t *testing.T, mode engine.Mode) *Server {
	t.Helper()
	m, err := engine.Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e, err := engine.NewEngine(m)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	plan := m.Plans["evidence/plans/http.onboard.party.plan.json"]
	if plan == nil {
		t.Fatal("the onboard plan is missing")
	}
	s, err := NewServer(Config{
		Engine: e,
		Plans:  map[string]*engine.Plan{"/onboard-party": plan},
		Mode:   mode,
	})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	return s
}

func headerRequest(path, correlationID string) *extprocv3.ProcessingRequest {
	headers := []*corev3.HeaderValue{
		{Key: ":path", RawValue: []byte(path)},
		{Key: ":method", RawValue: []byte("POST")},
	}
	if correlationID != "" {
		headers = append(headers, &corev3.HeaderValue{Key: "x-correlation-id", RawValue: []byte(correlationID)})
	}
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestHeaders{
		RequestHeaders: &extprocv3.HttpHeaders{Headers: &corev3.HeaderMap{Headers: headers}},
	}}
}

func bodyRequest(body string, endOfStream bool) *extprocv3.ProcessingRequest {
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestBody{
		RequestBody: &extprocv3.HttpBody{Body: []byte(body), EndOfStream: endOfStream},
	}}
}

// run drives the server through a message sequence and returns its responses.
func run(t *testing.T, s *Server, reqs ...*extprocv3.ProcessingRequest) []*extprocv3.ProcessingResponse {
	t.Helper()
	stream := &fakeStream{requests: reqs}
	if err := s.Process(stream); err != nil {
		t.Fatalf("process: %v", err)
	}
	return stream.sent
}

const validOnboard = `{
  "party": {"partyId": "PTY-00000001", "individual": {"names": [
    {"type": "FIRST", "value": "Amara"}, {"type": "LAST", "value": "Okafor"}]}},
  "relationship": {"type": "CUSTOMER", "lineOfBusiness": "RETAIL"},
  "contactPoints": [{"partyId": "PTY-00000001", "contacts": [
    {"type": "CORRESPONDENCE", "address": {"street1": "14 Marina Road", "city": "Lagos", "postalCode": "101241", "country": "NG"}}]}]
}`

// missingCorrespondence is structurally valid and semantically incomplete.
const missingCorrespondence = `{
  "party": {"partyId": "PTY-00000004", "individual": {"names": [
    {"type": "LAST", "value": "Okafor"}]}},
  "relationship": {"partyId": "PTY-00000004", "type": "CUSTOMER", "lineOfBusiness": "RETAIL"},
  "contactPoints": [{"partyId": "PTY-00000004", "contacts": [
    {"type": "MOBILE", "address": {"street1": "1 X", "city": "Lagos", "postalCode": "101241", "country": "NG"}}]}]
}`

// responseHeaderRequest is the phase Envoy sends after the upstream answers.
func responseHeaderRequest() *extprocv3.ProcessingRequest {
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_ResponseHeaders{
		ResponseHeaders: &extprocv3.HttpHeaders{Headers: &corev3.HeaderMap{
			Headers: []*corev3.HeaderValue{{Key: ":status", RawValue: []byte("201")}},
		}},
	}}
}

// TestCleanRecordContinuesAndSetsTheDigest: the digest is set in the response phase, because a
// header mutation in the request phase travels upstream and the caller never sees it.
func TestCleanRecordContinuesAndSetsTheDigest(t *testing.T) {
	s := testServer(t, engine.ModeEnforce)
	sent := run(t, s,
		headerRequest("/onboard-party", "01J8Z6Q4T7X9K2M3N4P5Q6R7S8"),
		bodyRequest(validOnboard, true),
		responseHeaderRequest(),
	)
	if len(sent) != 3 {
		t.Fatalf("want three responses, got %d", len(sent))
	}
	body := sent[1].GetRequestBody()
	if body == nil || body.GetResponse().GetStatus() != extprocv3.CommonResponse_CONTINUE {
		t.Fatalf("want CONTINUE on the body, got %v", sent[1])
	}
	rh := sent[2].GetResponseHeaders()
	if rh == nil {
		t.Fatalf("want a response-headers reply, got %v", sent[2])
	}
	if !hasHeader(rh.GetResponse().GetHeaderMutation(), "DQ-Pack-Digest") {
		t.Errorf("a clean record must carry the digest it was decided with")
	}
}

// TestInvalidRecordIsRejectedInsideTheProxy: an ImmediateResponse means the upstream never runs.
func TestInvalidRecordIsRejectedInsideTheProxy(t *testing.T) {
	s := testServer(t, engine.ModeEnforce)
	sent := run(t, s,
		headerRequest("/onboard-party", "01J8Z6Q4T7X9K2M3N4P5Q6R7S8"),
		bodyRequest(missingCorrespondence, true),
	)
	imm := sent[1].GetImmediateResponse()
	if imm == nil {
		t.Fatalf("want an ImmediateResponse, got %v", sent[1])
	}
	if got := imm.GetStatus().GetCode(); got != 422 {
		t.Fatalf("want 422, got %v", got)
	}
	var doc map[string]any
	if err := json.Unmarshal(imm.GetBody(), &doc); err != nil {
		t.Fatalf("the refusal is not a problem document: %v", err)
	}
	if doc["status"] != float64(422) {
		t.Errorf("problem status does not match: %v", doc["status"])
	}
	if !hasHeader(imm.GetHeaders(), "DQ-Pack-Digest") {
		t.Errorf("a refusal must say which digest refused it")
	}
}

// TestShadowPassesWithNoHeaders: shadow observes. A header here would change the request, and a
// rejection would make the mode unsafe to adopt.
func TestShadowPassesWithNoHeaders(t *testing.T) {
	s := testServer(t, engine.ModeShadow)
	sent := run(t, s,
		headerRequest("/onboard-party", "01J8Z6Q4T7X9K2M3N4P5Q6R7S8"),
		bodyRequest(missingCorrespondence, true),
	)
	if sent[1].GetImmediateResponse() != nil {
		t.Fatal("shadow rejected a request")
	}
	hm := sent[1].GetRequestBody().GetResponse().GetHeaderMutation()
	if hm != nil && len(hm.GetSetHeaders()) > 0 {
		t.Errorf("shadow added headers: %v", hm.GetSetHeaders())
	}
}

// TestWarnPassesWithAnnotationHeaders.
func TestWarnPassesWithAnnotationHeaders(t *testing.T) {
	s := testServer(t, engine.ModeWarn)
	sent := run(t, s,
		headerRequest("/onboard-party", "01J8Z6Q4T7X9K2M3N4P5Q6R7S8"),
		bodyRequest(missingCorrespondence, true),
		responseHeaderRequest(),
	)
	if sent[1].GetImmediateResponse() != nil {
		t.Fatal("warn rejected a request")
	}
	hm := sent[2].GetResponseHeaders().GetResponse().GetHeaderMutation()
	for _, key := range []string{"DQ-Pack-Digest", "x-dq-warn", "x-dq-violations", "x-dq-correlation-id"} {
		if !hasHeader(hm, key) {
			t.Errorf("warn did not set %s", key)
		}
	}
}

// TestUnroutedPathPassesThrough: only declared endpoints are decided.
func TestUnroutedPathPassesThrough(t *testing.T) {
	s := testServer(t, engine.ModeEnforce)
	sent := run(t, s,
		headerRequest("/something-else", ""),
		bodyRequest("not even json", true),
	)
	if sent[1].GetImmediateResponse() != nil {
		t.Fatalf("an unrouted path was decided: %v", sent[1].GetImmediateResponse())
	}
}

// TestChunkedBodyIsDecidedOnceOnEndOfStream: a chunk is never decided alone.
func TestChunkedBodyIsDecidedOnceOnEndOfStream(t *testing.T) {
	s := testServer(t, engine.ModeEnforce)
	half := len(missingCorrespondence) / 2
	sent := run(t, s,
		headerRequest("/onboard-party", ""),
		bodyRequest(missingCorrespondence[:half], false),
		bodyRequest(missingCorrespondence[half:], true),
	)
	if len(sent) != 3 {
		t.Fatalf("want three responses, got %d", len(sent))
	}
	if sent[1].GetImmediateResponse() != nil {
		t.Fatal("a half-record was decided: the first chunk must only continue")
	}
	if sent[2].GetImmediateResponse() == nil {
		t.Fatal("the completed record was not decided")
	}
}

// TestNonJSONBodyIsRefusedInEnforceAndPassedInShadow.
func TestNonJSONBodyIsRefusedInEnforceAndPassedInShadow(t *testing.T) {
	enforce := run(t, testServer(t, engine.ModeEnforce),
		headerRequest("/onboard-party", ""),
		bodyRequest("not json", true),
	)
	imm := enforce[1].GetImmediateResponse()
	if imm == nil || imm.GetStatus().GetCode() != 415 {
		t.Fatalf("want 415 in enforce, got %v", enforce[1])
	}
	if code := failureCode(t, imm.GetBody()); code != "DQ_UNSUPPORTED_MEDIA_TYPE" {
		t.Errorf("want DQ_UNSUPPORTED_MEDIA_TYPE, got %q", code)
	}

	shadow := run(t, testServer(t, engine.ModeShadow),
		headerRequest("/onboard-party", ""),
		bodyRequest("not json", true),
	)
	if shadow[1].GetImmediateResponse() != nil {
		t.Fatal("shadow refused a non-JSON body")
	}
}

// TestOversizedBodyIsRefusedWithoutBufferingIt.
func TestOversizedBodyIsRefusedWithoutBufferingIt(t *testing.T) {
	s := testServer(t, engine.ModeEnforce)
	s.cfg.MaxBody = 100
	sent := run(t, s,
		headerRequest("/onboard-party", ""),
		bodyRequest(validOnboard, true),
	)
	imm := sent[1].GetImmediateResponse()
	if imm == nil || imm.GetStatus().GetCode() != 413 {
		t.Fatalf("want 413, got %v", sent[1])
	}
	if code := failureCode(t, imm.GetBody()); code != "DQ_PAYLOAD_TOO_LARGE" {
		t.Errorf("want DQ_PAYLOAD_TOO_LARGE, got %q", code)
	}
}

func failureCode(t *testing.T, body []byte) string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("the failure is not a problem document: %v", err)
	}
	code, _ := doc["code"].(string)
	return code
}

// hasHeader reports whether a header mutation sets key.
func hasHeader(hm *extprocv3.HeaderMutation, key string) bool {
	if hm == nil {
		return false
	}
	for _, h := range hm.GetSetHeaders() {
		if h.GetHeader().GetKey() == key {
			return true
		}
	}
	return false
}
