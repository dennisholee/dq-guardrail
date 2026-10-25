package engine

import "testing"

// TestGeneratedRuleIndexIsFresh: the committed artefact must match the plans it claims to
// describe. This is the S5 gate — a derived artefact that drifts is a second source of truth.
func TestGeneratedRuleIndexIsFresh(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	committed, err := ReadRuleIndex(sampleDir)
	if err != nil {
		t.Fatalf("no committed rule index: %v", err)
	}
	problems, err := CheckRuleIndex(m, committed)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) > 0 {
		t.Fatalf("the committed rule index is stale: %v", problems)
	}
}

// TestGeneratedRuleIndexDetectsStalePlan proves the check is not vacuous. The rule IDs are
// unchanged by this edit, so a check that compared only IDs would pass.
func TestGeneratedRuleIndexDetectsStalePlan(t *testing.T) {
	dir := copySample(t)
	edit(t, dir, "evidence/plans/http.change.contactpoint.plan.json", "2160h", "2161h")

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	committed, err := ReadRuleIndex(dir)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	problems, err := CheckRuleIndex(m, committed)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) == 0 {
		t.Fatal("a changed plan must make the rule index stale")
	}
}

// TestGeneratedRuleIndexIsDeterministic: regeneration must be byte-stable, or the freshness
// check would fail on every run for no reason.
func TestGeneratedRuleIndexIsDeterministic(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	a, err := GenerateRuleIndex(m)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	b, err := GenerateRuleIndex(m)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if string(a) != string(b) {
		t.Fatal("two generations differ")
	}
}
