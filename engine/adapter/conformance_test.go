// Package conformance_test holds the cross-channel tests: T4 in the Delivery Design Pack's test
// strategy, which claims that the same record produces the same verdict on every channel.
//
// It lives outside any one adapter because that is the only place it can: the claim is *about*
// the adapters agreeing, and a test inside an adapter would be measuring one side of it.
package conformance_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/dennisholee/dq-guardrail/engine"
	dqextproc "github.com/dennisholee/dq-guardrail/engine/adapter/extproc"
	dqhttp "github.com/dennisholee/dq-guardrail/engine/adapter/http"
	"github.com/dennisholee/dq-guardrail/engine/adapter/kafka"
)

const sampleDir = "../../examples/party-model"

// scenario is one record from the sample's evidence, with the address it is sent to.
type scenario struct {
	name    string
	intent  string
	object  string
	path    string
	payload []byte
}

func loadScenarios(t *testing.T) []scenario {
	t.Helper()
	dir := filepath.Join(sampleDir, "evidence", "scenarios")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}
	var out []scenario
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var sc struct {
			Intent  string          `json:"intent"`
			Object  string          `json:"object"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(raw, &sc); err != nil || len(sc.Payload) == 0 {
			continue
		}
		out = append(out, scenario{
			name:    strings.TrimSuffix(entry.Name(), ".json"),
			intent:  sc.Intent,
			object:  sc.Object,
			path:    "/" + sc.Intent + "-" + sc.Object,
			payload: sc.Payload,
		})
	}
	if len(out) == 0 {
		t.Fatal("no scenarios")
	}
	return out
}

// verdict is the part of a decision worth comparing: whether it passed, which rules fired and
// where, and the digest that decided it.
type verdict struct {
	OK         bool
	Digest     string
	Violations []string
}

func fromDecision(d engine.Decision) verdict {
	v := verdict{OK: d.OK()}
	for _, x := range d.Violations {
		if v.Digest == "" {
			v.Digest = x.PackDigest
		}
		v.Violations = append(v.Violations,
			strings.Join([]string{x.RuleID, x.Severity, x.Path, x.Dimension, x.Message, x.PackDigest}, "|"))
	}
	sort.Strings(v.Violations)
	return v
}

func equal(a, b verdict) bool {
	if a.OK != b.OK || a.Digest != b.Digest || len(a.Violations) != len(b.Violations) {
		return false
	}
	for i := range a.Violations {
		if a.Violations[i] != b.Violations[i] {
			return false
		}
	}
	return true
}

// fixture is one engine plus the plans the channels need, built once.
type fixture struct {
	engine *engine.Engine
	plans  map[string]*engine.Plan
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	m, err := engine.Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// A fixed clock: SEM-CHG-002 reads now(), and two channels that disagreed only because they
	// were built a millisecond apart would be a false positive.
	e, err := engine.NewEngineWithClock(m, func() time.Time {
		return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	plans := map[string]*engine.Plan{}
	for _, p := range m.Plans {
		if p.Channel == "http" {
			plans["/"+p.Intent+"-"+p.Object] = p
		}
	}
	return &fixture{engine: e, plans: plans}
}

// httpDecision sends the record through the library adapter and returns the decision it made.
func (f *fixture) httpDecision(t *testing.T, sc scenario) (verdict, []byte) {
	t.Helper()
	var captured *engine.Decision
	h := dqhttp.Middleware(dqhttp.Config{
		Engine: f.engine,
		Plans:  f.plans,
		Mode:   engine.ModeEnforce,
		OnDecision: func(d engine.Decision) {
			captured = &d
		},
		Correlation: func(*http.Request) string { return "01J8Z6Q4T7X9K2M3N4P5Q6R7S8" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, sc.path, strings.NewReader(string(sc.payload)))
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if captured == nil {
		t.Fatalf("%s: the middleware made no decision (status %d)", sc.name, rec.Code)
	}
	return fromDecision(*captured), rec.Body.Bytes()
}

// kafkaDecision sends the record through the stream gate.
func (f *fixture) kafkaDecision(t *testing.T, sc scenario) verdict {
	t.Helper()
	g := &dqkafka.Gate{Engine: f.engine, Mode: engine.ModeEnforce}
	out, err := g.Intercept(dqkafka.Record{
		Topic: "party.inbound",
		Key:   []byte(sc.object),
		Value: sc.payload,
		Headers: map[string]string{
			"dq.intent": sc.intent,
			"dq.object": sc.object,
		},
	})
	if err != nil {
		t.Fatalf("%s: kafka gate: %v", sc.name, err)
	}
	return fromDecision(out.Decision)
}

// TestEveryChannelAgreesOnTheVerdict is T4: one record, one verdict, whichever door it comes
// through. The three are compared against the engine's own answer, so a channel that quietly
// dropped a rule, bound `this` differently or missed a provider shows up as a difference.
func TestEveryChannelAgreesOnTheVerdict(t *testing.T) {
	f := newFixture(t)
	for _, sc := range loadScenarios(t) {
		t.Run(sc.name, func(t *testing.T) {
			var payload map[string]any
			if err := json.Unmarshal(sc.payload, &payload); err != nil {
				t.Fatalf("payload: %v", err)
			}
			plan := f.plans[sc.path]
			if plan == nil {
				t.Fatalf("no plan for %s", sc.path)
			}
			reference, err := f.engine.Decide(plan, engine.Request{
				Payload:       payload,
				CorrelationID: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8",
			})
			if err != nil {
				t.Fatalf("decide: %v", err)
			}
			want := fromDecision(reference)

			httpVerdict, _ := f.httpDecision(t, sc)
			if !equal(want, httpVerdict) {
				t.Errorf("http disagrees with the engine\n engine: %+v\n   http: %+v", want, httpVerdict)
			}

			kafkaVerdict := f.kafkaDecision(t, sc)
			if !equal(want, kafkaVerdict) {
				t.Errorf("kafka disagrees with the engine\n engine: %+v\n  kafka: %+v", want, kafkaVerdict)
			}

			// ext_proc answers with a document, not a Decision, so it is compared on the document
			// below; here it only has to agree on pass or fail.
			status, body := f.extProcBody(t, sc)
			if want.OK && status != http.StatusCreated {
				t.Errorf("ext_proc rejected a record the engine passed: %d", status)
			}
			if !want.OK && status != http.StatusUnprocessableEntity {
				t.Errorf("ext_proc passed a record the engine rejected: %d %s", status, body)
			}
		})
	}
}

// TestHTTPAndExtProcRenderTheSameProblemDocument: the two HTTP implementations must not describe
// the same failure differently. They share engine.ProblemFromDecision; this proves it.
func TestHTTPAndExtProcRenderTheSameProblemDocument(t *testing.T) {
	f := newFixture(t)
	for _, sc := range loadScenarios(t) {
		t.Run(sc.name, func(t *testing.T) {
			want, httpBody := f.httpDecision(t, sc)
			if want.OK {
				t.Skip("a clean record has no problem document")
			}
			status, procBody := f.extProcBody(t, sc)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("ext_proc status %d", status)
			}
			if got, wantDoc := normalise(t, procBody), normalise(t, httpBody); got != wantDoc {
				t.Errorf("the two adapters render different documents\n  http: %s\n   ext: %s", wantDoc, got)
			}
		})
	}
}

// normalise re-encodes a JSON document with sorted keys, so a trailing newline or key order does
// not count as a difference. The *content* has to match; the framing is Envoy's business.
func normalise(t *testing.T, raw []byte) string {
	t.Helper()
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("not JSON: %v (%s)", err, raw)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(out)
}

// extProcBody sends the record through a real gRPC ext_proc stream and returns the response body
// the caller would receive. bufconn means real protobuf over a real gRPC stack, not a fake.
func (f *fixture) extProcBody(t *testing.T, sc scenario) (int, []byte) {
	t.Helper()
	srv, err := dqextproc.NewServer(dqextproc.Config{
		Engine: f.engine,
		Plans:  f.plans,
		Mode:   engine.ModeEnforce,
	})
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	lis := bufconn.Listen(1 << 20)
	g := grpc.NewServer()
	srv.Register(g)
	go func() { _ = g.Serve(lis) }()
	defer g.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := extprocv3.NewExternalProcessorClient(conn).Process(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}

	send := func(r *extprocv3.ProcessingRequest) *extprocv3.ProcessingResponse {
		if err := stream.Send(r); err != nil {
			t.Fatalf("send: %v", err)
		}
		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		return resp
	}

	send(&extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestHeaders{
		RequestHeaders: &extprocv3.HttpHeaders{Headers: &corev3.HeaderMap{Headers: []*corev3.HeaderValue{
			{Key: ":path", RawValue: []byte(sc.path)},
			{Key: "x-correlation-id", RawValue: []byte("01J8Z6Q4T7X9K2M3N4P5Q6R7S8")},
		}}},
	}})
	resp := send(&extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestBody{
		RequestBody: &extprocv3.HttpBody{Body: sc.payload, EndOfStream: true},
	}})
	if imm := resp.GetImmediateResponse(); imm != nil {
		return int(imm.GetStatus().GetCode()), imm.GetBody()
	}
	return http.StatusCreated, nil
}
