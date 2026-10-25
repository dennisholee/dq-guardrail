package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// marker is distinctive enough that finding it anywhere in a produced document means a payload
// value escaped into evidence.
const marker = "MARKER7f3a9c"

// markerPayload fills every leaf with the marker, so any field echoed into a verdict, a reject
// file or a header is detectable.
func markerPayload() map[string]any {
	leaf := func(kind string) map[string]any {
		return map[string]any{"type": kind, "value": marker}
	}
	return map[string]any{
		"party": map[string]any{
			"partyId": marker,
			"individual": map[string]any{
				"names":     []any{leaf("LAST"), leaf("LAST")},
				"documents": []any{leaf("PASSPORT")},
			},
		},
		"relationship": map[string]any{"type": "CUSTOMER"},
		"contactPoints": []any{map[string]any{
			"partyId": marker,
			"contacts": []any{map[string]any{
				"type":    "CORRESPONDENCE",
				"address": map[string]any{"street1": marker, "city": marker, "postalCode": marker, "country": "NG"},
			}},
		}},
	}
}

// TestVerdictCarriesNoPayloadValues: the violation object reports paths, not values. If someone
// adds a `value` field to it, or a rule starts interpolating one, this fails.
func TestVerdictCarriesNoPayloadValues(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e, err := NewEngineWithClock(m, func() time.Time {
		return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	plan := m.PlanFor("http", "onboard", "party")
	if plan == nil {
		t.Fatal("no plan")
	}

	decision, err := e.Decide(plan, Request{Payload: markerPayload(), CorrelationID: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8"})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if decision.OK() {
		t.Fatal("the marker payload must produce at least one violation, or this test proves nothing")
	}

	problem := ProblemFromDecision(decision, "/onboard-party", "01J8Z6Q4T7X9K2M3N4P5Q6R7S8")
	body, err := json.Marshal(problem)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), marker) {
		t.Fatalf("a payload value escaped into the verdict: %s", body)
	}
}

// TestSensitiveFieldsExist keeps the invariant honest: if no contract marks anything sensitive,
// the redaction claim is vacuous and nobody would notice.
func TestSensitiveFieldsExist(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	count := 0
	for _, c := range m.Contracts {
		count += countSensitive(c.Raw)
	}
	if count == 0 {
		t.Fatal("no contract marks a field x-dq-sensitive, so redaction is untested in this sample")
	}
	t.Logf("%d sensitive field(s) declared", count)
}

// countSensitive counts fields marked x-dq-sensitive anywhere in a contract.
func countSensitive(node any) int {
	switch v := node.(type) {
	case map[string]any:
		n := 0
		if flag, ok := v["x-dq-sensitive"].(bool); ok && flag {
			n++
		}
		for _, val := range v {
			n += countSensitive(val)
		}
		return n
	case []any:
		n := 0
		for _, val := range v {
			n += countSensitive(val)
		}
		return n
	default:
		return 0
	}
}
