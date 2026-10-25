package dqhttp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dennisholee/dq-guardrail/engine"
)

// testConfig builds a Config the tests can adjust — MaxBody in particular.
func testConfig(t *testing.T, mode engine.Mode) (Config, *engine.Engine) {
	t.Helper()
	m, err := engine.Load("../../../examples/party-model")
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
	return Config{
		Engine: e,
		Plans:  map[string]*engine.Plan{"/onboard-party": plan},
		Mode:   mode,
		Correlation: func(*http.Request) string {
			return "01J8Z6Q4T7X9K2M3N4P5Q6R7S8"
		},
	}, e
}

// TestOversizedRecordIsRefusedNotTruncated: a truncated body is the worst outcome — the gate
// neither evaluates nor rejects, and the service receives corrupt JSON and is blamed for it.
func TestOversizedRecordIsRefusedNotTruncated(t *testing.T) {
	cfg, _ := testConfig(t, engine.ModeEnforce)
	cfg.MaxBody = 64
	h := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("an oversized record was passed to the service in ENFORCE mode")
	}))

	big := `{"party":{"partyId":"PTY-00000001","individual":{"names":[` +
		strings.Repeat(`{"type":"LAST","value":"Okafor"},`, 20) + `{"type":"LAST","value":"Okafor"}]}}}`
	if int64(len(big)) <= cfg.MaxBody {
		t.Fatalf("the test body is not oversized: %d bytes", len(big))
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/onboard-party", strings.NewReader(big)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
	var problem map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("the refusal is not a problem document: %v", err)
	}
	if problem["status"] != float64(http.StatusRequestEntityTooLarge) {
		t.Errorf("problem status does not match the response: %v", problem["status"])
	}
}

// TestOversizedRecordPassesThroughIntactInShadow: shadow must be inert, and inert means the
// service sees exactly the bytes the caller sent — not the prefix the gate happened to read.
func TestOversizedRecordPassesThroughIntactInShadow(t *testing.T) {
	cfg, _ := testConfig(t, engine.ModeShadow)
	cfg.MaxBody = 64

	var seen []byte
	h := Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read: %v", err)
		}
		seen = body
		w.WriteHeader(http.StatusOK)
	}))

	big := `{"party":{"partyId":"PTY-00000001","individual":{"names":[` +
		strings.Repeat(`{"type":"LAST","value":"Okafor"},`, 20) + `{"type":"LAST","value":"Okafor"}]}}}`

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/onboard-party", strings.NewReader(big)))

	if rec.Code != http.StatusOK {
		t.Fatalf("shadow must not change the response, got %d", rec.Code)
	}
	if !bytes.Equal(seen, []byte(big)) {
		t.Fatalf("the service received %d of %d bytes; shadow corrupted the body", len(seen), len(big))
	}
}
