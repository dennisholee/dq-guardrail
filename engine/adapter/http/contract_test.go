package dqhttp

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dennisholee/dq-guardrail/engine"
)

// These tests pin the §2.7 contract for the rows where a record cannot be decided: a body that
// is not JSON, and a pack that does not compile. Both were silent passes before the contract
// existed — the failure mode DDP §8.1 names explicitly, that coverage must fall visibly.
//
// They encode the distinction §2.6 draws between a violation and a failure code. A 422 says the
// record is wrong; a 415 or a 500 says the gate could not answer. Conflating them teaches
// clients either to retry bad data or to quarantine good data.

// TestNonJSONBodyIsRefusedInEnforce: passing it on would validate nothing and report nothing.
func TestNonJSONBodyIsRefusedInEnforce(t *testing.T) {
	h, reached := setup(t, engine.ModeEnforce)
	rec := post(t, h, "not json at all")

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("want 415, got %d", rec.Code)
	}
	if *reached != 0 {
		t.Fatalf("the service was called for a body that was refused (%d)", *reached)
	}
	if code := problemCode(t, rec); code != "DQ_UNSUPPORTED_MEDIA_TYPE" {
		t.Errorf("want DQ_UNSUPPORTED_MEDIA_TYPE, got %q", code)
	}
	if ct := rec.Header().Get("content-type"); ct != "application/problem+json" {
		t.Errorf("want a problem document, got %q", ct)
	}
}

// TestNonJSONBodyPassesThroughInShadow: shadow observes. If it refused anything it would not be
// shadow, and the mode would be unsafe to adopt before an enforcement decision exists.
func TestNonJSONBodyPassesThroughInShadow(t *testing.T) {
	h, reached := setup(t, engine.ModeShadow)
	rec := post(t, h, "not json at all")

	if rec.Code != http.StatusCreated || *reached != 1 {
		t.Fatalf("shadow must pass through: code=%d reached=%d", rec.Code, *reached)
	}
}

// TestDeclaredNonJSONContentTypeIsRefused: the header is a claim about the body, and it is
// checked before the body is read.
func TestDeclaredNonJSONContentTypeIsRefused(t *testing.T) {
	h, reached := setup(t, engine.ModeEnforce)
	req := httptest.NewRequest(http.MethodPost, "/onboard-party", strings.NewReader(badOnboard))
	req.Header.Set("content-type", "application/xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType || *reached != 0 {
		t.Fatalf("want 415 and no upstream call, got code=%d reached=%d", rec.Code, *reached)
	}
}

// TestJSONWithCharsetIsAccepted: a charset parameter is not a different media type, and refusing
// it would break clients that are behaving correctly.
func TestJSONWithCharsetIsAccepted(t *testing.T) {
	h, reached := setup(t, engine.ModeEnforce)
	req := httptest.NewRequest(http.MethodPost, "/onboard-party", strings.NewReader(badOnboard))
	req.Header.Set("content-type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want the record evaluated and rejected with 422, got %d", rec.Code)
	}
	if *reached != 0 {
		t.Fatal("an invalid record reached the service")
	}
}

// TestEngineErrorIsRefusedInEnforce: a pack that does not compile cannot prove a record good.
func TestEngineErrorIsRefusedInEnforce(t *testing.T) {
	h, reached := brokenPackHandler(t, engine.ModeEnforce)
	rec := post(t, h, validOnboard)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", rec.Code)
	}
	if *reached != 0 {
		t.Fatalf("the service was called for a record the gate could not decide (%d)", *reached)
	}
	if code := problemCode(t, rec); code != "DQ_RULE_ERROR" {
		t.Errorf("want DQ_RULE_ERROR, got %q", code)
	}
}

// TestEngineErrorPassesThroughInWarn: WARN annotates and forwards; a broken pack does not turn it
// into ENFORCE.
func TestEngineErrorPassesThroughInWarn(t *testing.T) {
	h, reached := brokenPackHandler(t, engine.ModeWarn)
	rec := post(t, h, validOnboard)

	if rec.Code != http.StatusCreated || *reached != 1 {
		t.Fatalf("warn must pass through: code=%d reached=%d", rec.Code, *reached)
	}
}

const validOnboard = `{
  "party": {"partyId": "PTY-00000001", "individual": {"names": [
    {"type": "FIRST", "value": "Amara"}, {"type": "LAST", "value": "Okafor"}]}},
  "relationship": {"type": "CUSTOMER", "lineOfBusiness": "RETAIL"},
  "contactPoints": [{"partyId": "PTY-00000001", "contacts": [
    {"type": "CORRESPONDENCE", "address": {"street1": "14 Marina Road", "city": "Lagos", "postalCode": "101241", "country": "NG"}}]}]
}`

// problemCode reads the extension member that names the failure code.
func problemCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("the refusal is not a problem document: %v", err)
	}
	code, _ := doc["code"].(string)
	return code
}

// brokenPackHandler builds a middleware over a copy of the sample whose party pack cannot
// compile, so Decide returns an error rather than a decision.
func brokenPackHandler(t *testing.T, mode engine.Mode) (http.Handler, *int) {
	t.Helper()
	dir := t.TempDir()
	copyTree(t, "../../../examples/party-model", dir)

	packPath := filepath.Join(dir, "rules", "object", "party.pack.yaml")
	raw, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatalf("read pack: %v", err)
	}
	const intact = `assert: "('individual' in this) != ('organization' in this)"`
	if !strings.Contains(string(raw), intact) {
		t.Fatal("the assertion this test breaks is not in the pack")
	}
	broken := strings.Replace(string(raw), intact, `assert: "this."`, 1)
	if err := os.WriteFile(packPath, []byte(broken), 0o644); err != nil {
		t.Fatalf("write pack: %v", err)
	}

	m, err := engine.Load(dir)
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
	h := Middleware(Config{
		Engine: e,
		Plans:  map[string]*engine.Plan{"/onboard-party": plan},
		Mode:   mode,
		Correlation: func(*http.Request) string {
			return "01J8Z6Q4T7X9K2M3N4P5Q6R7S8"
		},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		w.WriteHeader(http.StatusCreated)
	}))
	return h, &reached
}

// copyTree copies an artefact tree, because these tests need to damage a pack.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy tree: %v", err)
	}
}
