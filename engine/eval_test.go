package engine

import "testing"

// TestFixturesMatchExpectations is the acceptance test for the rule language: every fixture
// in the sample must produce the outcome it claims. Until this existed, the fixtures were
// reviewed by reading them.
func TestFixturesMatchExpectations(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	results, err := RunFixtures(m)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no fixtures ran")
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("%s/%s (expect %s): %v", r.Pack, r.Rule, r.Expect, r.Err)
			continue
		}
		if r.Mismatch {
			t.Errorf("%s/%s: expected %s, rule ran=%v passed=%v",
				r.Pack, r.Rule, r.Expect, r.Ran, r.Passed)
		}
	}
	t.Logf("%d fixtures evaluated", len(results))
}

// TestEvalGuardSuppressesRule: a false `when` means the rule did not apply, which is not the
// same as passing. Conflating the two would let a guard quietly absorb every record.
func TestEvalGuardSuppressesRule(t *testing.T) {
	ev, err := NewEvaluator()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	rule := Rule{
		ID:     "SEM-PTY-004",
		When:   "'organization' in this",
		Assert: "false", // would fail if it ran
	}
	out, err := ev.EvalRule(rule, EvalInput{This: map[string]any{"partyId": "PTY-00000001"}})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if out.Ran {
		t.Fatal("the rule should not have run")
	}

	ran, err := ev.EvalRule(rule, EvalInput{This: map[string]any{"organization": map[string]any{"name": "X"}}})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !ran.Ran || ran.Passed {
		t.Fatalf("with the guard true the rule must run and fail: ran=%v passed=%v", ran.Ran, ran.Passed)
	}
}

// TestEvalRejectsTimeDependentRules records the deliberate limit: a deterministic fixture run
// cannot supply a clock, so a rule that reads now() must fail loudly rather than pass by
// accident.
func TestEvalRejectsTimeDependentRules(t *testing.T) {
	ev, err := NewEvaluator()
	if err != nil {
		t.Fatalf("env: %v", err)
	}
	rule := Rule{
		ID:     "SEM-CHG-003",
		When:   "'lastChange' in this",
		Assert: "now() - timestamp(this.lastChange.changedAt) >= duration('2160h')",
	}
	_, evalErr := ev.EvalRule(rule, EvalInput{This: map[string]any{"lastChange": map[string]any{"changedAt": "2026-01-01T00:00:00Z"}}})
	if evalErr == nil {
		t.Fatal("now() must not resolve in a deterministic run")
	}
}
