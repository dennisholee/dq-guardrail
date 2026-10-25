package engine

import "testing"

func testEngine(t *testing.T) (*Engine, *Model) {
	t.Helper()
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e, err := NewEngine(m)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	return e, m
}

func onboardPlan(t *testing.T, m *Model) *Plan {
	t.Helper()
	p, ok := m.Plans["evidence/plans/http.onboard.party.plan.json"]
	if !ok {
		t.Fatal("the onboard plan is missing")
	}
	return p
}

// onboardRequest builds the composite body /onboard-party actually receives.
func onboardRequest(party map[string]any) map[string]any {
	return map[string]any{
		"party": party,
		"relationship": map[string]any{
			"partyReferenceId": "CIF-000123456",
			"partyId":          "PTY-00000001",
			"type":             "CUSTOMER",
			"lineOfBusiness":   "RETAIL",
		},
		"contactPoints": []any{
			map[string]any{
				"partyId": "PTY-00000001",
				"contacts": []any{
					map[string]any{
						"type":    "CORRESPONDENCE",
						"address": map[string]any{"street1": "14 Marina Road", "city": "Lagos", "postalCode": "101241", "country": "NG"},
					},
				},
			},
		},
	}
}

// TestDecideStructuralFailure: a record that does not match the contract is rejected by tier 0
// with the reserved 000 rule id, before any semantic rule runs.
func TestDecideStructuralFailure(t *testing.T) {
	e, m := testEngine(t)
	d, err := e.Decide(onboardPlan(t, m), Request{
		Payload: onboardRequest(map[string]any{
			"individual": map[string]any{
				"names": []any{map[string]any{"type": "FIRST", "value": "Amara"}},
			},
		}),
		CorrelationID: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8",
	})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if d.OK() {
		t.Fatal("a record without partyId must not pass")
	}
	if d.Violations[0].RuleID != "SYN-PTY-000" {
		t.Fatalf("structural failures use the reserved 000 id, got %s", d.Violations[0].RuleID)
	}
	if d.RulesRun != 0 {
		t.Fatalf("semantic rules must not run after a structural failure, ran %d", d.RulesRun)
	}
	if d.Violations[0].PackDigest == "" {
		t.Fatal("a violation must carry the digest of the pack responsible")
	}
}

// TestDecideSemanticFailure: a structurally valid record that breaks an authored rule. The
// object rule is bound to the party member, not to the composite root.
func TestDecideSemanticFailure(t *testing.T) {
	e, m := testEngine(t)
	d, err := e.Decide(onboardPlan(t, m), Request{
		Payload: onboardRequest(map[string]any{
			"partyId": "PTY-00000002",
			"individual": map[string]any{
				"names": []any{
					map[string]any{"type": "LAST", "value": "Okafor"},
					map[string]any{"type": "LAST", "value": "Adeyemi"},
				},
			},
		}),
		CorrelationID: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8",
	})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !hasViolation(d, "SYN-PTY-001") {
		t.Fatalf("expected SYN-PTY-001, got %v", violationIDs(d))
	}
}

// TestDecidePassesAValidRecord: nothing fires, and rules did run — a plan that reports OK
// because it evaluated nothing would be indistinguishable from a good record.
func TestDecidePassesAValidRecord(t *testing.T) {
	e, m := testEngine(t)
	d, err := e.Decide(onboardPlan(t, m), Request{
		Payload: onboardRequest(map[string]any{
			"partyId": "PTY-00000001",
			"individual": map[string]any{
				"names": []any{
					map[string]any{"type": "FIRST", "value": "Amara"},
					map[string]any{"type": "LAST", "value": "Okafor"},
				},
			},
		}),
		CorrelationID: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8",
	})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !d.OK() {
		t.Fatalf("expected a clean decision, got %v", violationIDs(d))
	}
	if d.RulesRun == 0 {
		t.Fatal("rules must have run")
	}
}

// TestDecideHonoursRelax: the onboard plan waives SEM-PTY-003, so an organisation without a
// registration number is recorded as relaxed rather than rejected.
func TestDecideHonoursRelax(t *testing.T) {
	e, m := testEngine(t)
	d, err := e.Decide(onboardPlan(t, m), Request{
		Payload: onboardRequest(map[string]any{
			"partyId": "PTY-00000006",
			"organization": map[string]any{
				"name":                   "Northwind Trading LLP",
				"countryOfIncorporation": "GB",
			},
		}),
		CorrelationID: "01J8Z6Q4T7X9K2M3N4P5Q6R7S8",
	})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if hasViolation(d, "SEM-PTY-003") {
		t.Fatal("SEM-PTY-003 is waived by this plan and must not be enforced")
	}
	if len(d.Relaxed) == 0 || d.Relaxed[0] != "SEM-PTY-003" {
		t.Fatalf("the waived rule must still be recorded, got %v", d.Relaxed)
	}
}

// TestExpandedContractIsIndependent: expansion inlines the enums, so tier 0 needs no access to
// the catalogue once compiled. Internal #/$defs references stay resolved inside the document.
func TestExpandedContractIsIndependent(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	doc, err := ExpandContract(m.Contracts["party"], m.ValueSets)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	for _, ref := range CollectRefs(doc) {
		if len(ref) >= len(valuesetURIPrefix) && ref[:len(valuesetURIPrefix)] == valuesetURIPrefix {
			t.Fatalf("expansion left a catalogue reference %s; the compiled schema must be self-contained", ref)
		}
	}
}

func hasViolation(d Decision, ruleID string) bool {
	for _, v := range d.Violations {
		if v.RuleID == ruleID {
			return true
		}
	}
	return false
}

func violationIDs(d Decision) []string {
	out := []string{}
	for _, v := range d.Violations {
		out = append(out, v.RuleID+"@"+v.Path)
	}
	return out
}
