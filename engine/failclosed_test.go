package engine

import (
	"strings"
	"testing"
	"time"
)

// TestUnevaluableRecordFailsClosed: a payload can pass tier 0 and still make a rule unable to
// answer — a member the contract allows to be absent, read by a rule that assumed it. The gate
// must not turn that into a 500: the record has not been proven good, so it fails, and it says
// which rule could not answer.
func TestUnevaluableRecordFailsClosed(t *testing.T) {
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

	// Structurally valid — party with a surname — but without the contact points a rule reads.
	payload := map[string]any{
		"party": map[string]any{
			"partyId": "PTY-00000009",
			"individual": map[string]any{
				"names": []any{map[string]any{"type": "LAST", "value": "Okafor"}},
			},
		},
	}

	decision, err := e.Decide(plan, Request{Payload: payload, CorrelationID: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8"})
	if err != nil {
		t.Fatalf("a data-dependent evaluation failure must not surface as an error: %v", err)
	}
	if decision.OK() {
		t.Fatal("a record whose rules could not be evaluated passed the gate")
	}
	if len(decision.Violations) == 0 {
		t.Fatal("no violation reported")
	}

	found := false
	for _, v := range decision.Violations {
		if !strings.Contains(v.Message, "could not be evaluated") {
			continue
		}
		found = true
		if v.Severity != SeverityViolationCritical {
			t.Errorf("an unevaluable rule should be CRITICAL, got %s", v.Severity)
		}
		if v.RuleID == "" {
			t.Error("the violation does not say which rule could not answer")
		}
	}
	if !found {
		t.Errorf("no violation explained that a rule could not be evaluated: %+v", decision.Violations)
	}
}
