package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScorecardIsFresh: the committed scorecard must be what the tree produces. It is a
// generated artefact, and the hand-written version this replaced went stale three times while
// the sample was being built.
func TestScorecardIsFresh(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fresh, err := GenerateScorecard(m)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	committed, err := os.ReadFile(filepath.Join(sampleDir, "evidence", "scorecard.md"))
	if err != nil {
		t.Fatalf("no committed scorecard: %v", err)
	}
	if string(fresh) != string(committed) {
		t.Fatal("the committed scorecard is stale; run `dqctl scorecard`")
	}
}

// TestScorecardDetectsChange proves the comparison is not vacuous.
func TestScorecardDetectsChange(t *testing.T) {
	dir := copySample(t)
	edit(t, dir, "rules/object/party.pack.yaml", "severity: CRITICAL", "severity: MAJOR")

	before, err := os.ReadFile(filepath.Join(sampleDir, "evidence", "scorecard.md"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	after, err := GenerateScorecard(m)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if string(before) == string(after) {
		t.Fatal("changing a rule must change the scorecard")
	}
}

// TestScorecardIsDeterministic: two generations must be byte-identical, or --check fails on
// every run for no reason.
func TestScorecardIsDeterministic(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	a, err := GenerateScorecard(m)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	b, err := GenerateScorecard(m)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if string(a) != string(b) {
		t.Fatal("two generations differ")
	}
}
