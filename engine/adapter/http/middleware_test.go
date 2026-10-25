package dqhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dennisholee/dq-guardrail/engine"
)

func setup(t *testing.T, mode engine.Mode) (http.Handler, *int) {
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

	reached := 0
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	handler := Middleware(Config{
		Engine: e,
		Plans:  map[string]*engine.Plan{"/onboard-party": plan},
		Mode:   mode,
		Correlation: func(*http.Request) string {
			return "01J8Z6Q4T7X9K2M3N4P5Q6R7S8"
		},
	})(app)
	return handler, &reached
}

const badOnboard = `{
  "party": {"individual": {"names": [{"type": "FIRST", "value": "Amara"}]}},
  "relationship": {"type": "CUSTOMER", "lineOfBusiness": "RETAIL"},
  "contactPoints": []
}`

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/onboard-party", strings.NewReader(body))
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestEnforceRejectsWithTheProblemDocument: the adapter produces the RFC 9457 body the
// violation schema describes, and the service never sees the request.
func TestEnforceRejectsWithTheProblemDocument(t *testing.T) {
	h, reached := setup(t, engine.ModeEnforce)
	rec := post(t, h, badOnboard)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
	if *reached != 0 {
		t.Fatal("the service must not be reached when the record is rejected")
	}
	if ct := rec.Header().Get("content-type"); ct != "application/problem+json" {
		t.Fatalf("content-type %q", ct)
	}
	// The design names this header: a caller can see which rules decided the write.
	if rec.Header().Get("DQ-Pack-Digest") == "" {
		t.Fatal("a rejection must carry DQ-Pack-Digest")
	}

	var problem engine.Verdict
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("the body is not the problem document: %v", err)
	}
	if problem.Status != 422 || len(problem.Violations) == 0 {
		t.Fatalf("unexpected problem document: %+v", problem)
	}
	if problem.Violations[0].RuleID != "SYN-PTY-000" {
		t.Fatalf("expected the structural rule id, got %s", problem.Violations[0].RuleID)
	}
}

// TestShadowPassesThrough: shadow mode answers 201 and the decision is still reported, which is
// the whole point of shadow — measure before enforcing.
func TestShadowPassesThrough(t *testing.T) {
	h, reached := setup(t, engine.ModeShadow)
	rec := post(t, h, badOnboard)

	if rec.Code != http.StatusCreated {
		t.Fatalf("shadow must not reject: got %d", rec.Code)
	}
	if *reached != 1 {
		t.Fatal("the service must be reached in shadow mode")
	}
}

// TestWarnAnnotates: warn mode lets the write through but marks it, so a caller can see it.
func TestWarnAnnotates(t *testing.T) {
	h, reached := setup(t, engine.ModeWarn)
	rec := post(t, h, badOnboard)

	if rec.Code != http.StatusCreated || *reached != 1 {
		t.Fatalf("warn must pass through: code=%d reached=%d", rec.Code, *reached)
	}
	if rec.Header().Get("x-dq-violations") == "" {
		t.Fatal("warn must annotate the response")
	}
}

// TestAcceptedRequestCarriesTheDigest: a write that passes also says which rules let it pass,
// which is what makes a later "why was this accepted?" answerable.
func TestAcceptedRequestCarriesTheDigest(t *testing.T) {
	h, reached := setup(t, engine.ModeEnforce)
	good := `{
      "party": {"partyId": "PTY-00000001", "individual": {"names": [
        {"type": "FIRST", "value": "Amara"}, {"type": "LAST", "value": "Okafor"}]}},
      "relationship": {"type": "CUSTOMER", "lineOfBusiness": "RETAIL"},
      "contactPoints": [{"partyId": "PTY-00000001", "contacts": [
        {"type": "CORRESPONDENCE", "address": {"street1": "14 Marina Road", "city": "Lagos", "postalCode": "101241", "country": "NG"}}]}]
    }`
	rec := post(t, h, good)

	if rec.Code != http.StatusCreated || *reached != 1 {
		t.Fatalf("a valid record must reach the service: code=%d reached=%d", rec.Code, *reached)
	}
	if rec.Header().Get("DQ-Pack-Digest") == "" {
		t.Fatal("an accepted write must carry DQ-Pack-Digest")
	}
}

// TestUnroutedPathPassesThrough: only declared endpoints are decided.
func TestUnroutedPathPassesThrough(t *testing.T) {
	h, reached := setup(t, engine.ModeEnforce)
	req := httptest.NewRequest(http.MethodPost, "/something-else", strings.NewReader(badOnboard))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated || *reached != 1 {
		t.Fatalf("expected pass-through: code=%d reached=%d", rec.Code, *reached)
	}
}
