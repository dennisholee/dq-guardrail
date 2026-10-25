package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// scenarioFile is the stored request a committed verdict was produced from.
type scenarioFile struct {
	Intent         string         `json:"intent"`
	Object         string         `json:"object"`
	CorrelationID  string         `json:"correlationId"`
	ClockDependent bool           `json:"clockDependent"`
	Payload        map[string]any `json:"payload"`
}

// TestVerdictsAreReproducibleFromScenarios is the guarantee that the evidence directory is
// real: every committed verdict must be what the engine produces for its stored request.
// Before this existed, the verdicts were hand-written illustrations.
func TestVerdictsAreReproducibleFromScenarios(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	e, err := NewEngine(m)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	scenarios, err := filepath.Glob(filepath.Join(sampleDir, "evidence", "scenarios", "*.json"))
	if err != nil || len(scenarios) == 0 {
		t.Fatalf("no scenarios found: %v", err)
	}

	checked, skipped := 0, 0
	for _, path := range scenarios {
		name := filepath.Base(path)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var scenario scenarioFile
		if err := json.Unmarshal(raw, &scenario); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if scenario.ClockDependent {
			// A clock-dependent verdict cannot be reproduced by a deterministic run. It is
			// skipped deliberately, and its rule is covered by the evaluator tests instead.
			skipped++
			continue
		}

		plan := m.PlanFor("http", scenario.Intent, scenario.Object)
		if plan == nil {
			t.Fatalf("%s: no plan for %s/%s", name, scenario.Intent, scenario.Object)
		}
		decision, err := e.Decide(plan, Request{Payload: scenario.Payload, CorrelationID: scenario.CorrelationID})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		produced := ProblemFromDecision(decision, "/"+scenario.Intent+"-"+scenario.Object, scenario.CorrelationID)
		if produced == nil {
			t.Fatalf("%s: the engine accepted a record whose verdict says it was rejected", name)
		}

		committedRaw, err := os.ReadFile(filepath.Join(sampleDir, "evidence", "verdicts", name))
		if err != nil {
			t.Fatalf("%s: no committed verdict: %v", name, err)
		}
		var committed Verdict
		if err := json.Unmarshal(committedRaw, &committed); err != nil {
			t.Fatalf("%s: the committed verdict does not parse: %v", name, err)
		}

		compareVerdict(t, name, produced, &committed)
		checked++
	}

	if checked == 0 {
		t.Fatal("no verdicts were checked")
	}
	t.Logf("%d verdicts reproduced, %d skipped as clock-dependent", checked, skipped)
}

func compareVerdict(t *testing.T, name string, want, got *Verdict) {
	t.Helper()
	if want.Status != got.Status || want.Instance != got.Instance || want.Type != got.Type {
		t.Errorf("%s: header differs: want %d %s %s, got %d %s %s",
			name, want.Status, want.Instance, want.Type, got.Status, got.Instance, got.Type)
	}
	if want.CorrelationID != got.CorrelationID {
		t.Errorf("%s: correlationId %s != %s", name, want.CorrelationID, got.CorrelationID)
	}
	if len(want.Violations) != len(got.Violations) {
		t.Fatalf("%s: %d violation(s) produced, the committed verdict has %d",
			name, len(want.Violations), len(got.Violations))
	}
	for i := range want.Violations {
		w, g := want.Violations[i], got.Violations[i]
		if w.RuleID != g.RuleID || w.Severity != g.Severity || w.Dimension != g.Dimension ||
			w.Path != g.Path || w.Message != g.Message || w.PackDigest != g.PackDigest {
			t.Errorf("%s: violation %d differs\n  produced:  %+v\n  committed: %+v", name, i, w, g)
		}
	}
}
