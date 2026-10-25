package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProvenanceDefects proves the digest chain is load-bearing: each mutation breaks a
// link that the manifest claims, and each must be reported.
func TestProvenanceDefects(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		mutate func(t *testing.T, dir string)
	}{
		{
			name: "a pack digest no longer matches the pack",
			want: CodeDigestMismatch,
			mutate: func(t *testing.T, dir string) {
				corruptDigestAfter(t, dir, "evidence/bundle.yaml", "packs:")
			},
		},
		{
			name: "a plan's assert drifted from its pack",
			want: CodePlanDrift,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "evidence/plans/http.change.contactpoint.plan.json", "2160h", "2170h")
			},
		},
		{
			name: "a plan claims a tier its prefix contradicts",
			want: CodePlanDrift,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "evidence/plans/http.change.contactpoint.plan.json", `"tier": 0`, `"tier": 1`)
			},
		},
		{
			name: "a contract changed after the build",
			want: CodeDigestMismatch,
			mutate: func(t *testing.T, dir string) {
				appendBytes(t, dir, "catalog/business-objects/party.json", " ")
			},
		},
		{
			name: "the binding is repinned to a digest that is not the bundle",
			want: CodeDigestMismatch,
			mutate: func(t *testing.T, dir string) {
				corruptDigestAfter(t, dir, "bindings/party-service.yaml", "bundle:")
			},
		},
		{
			name: "a verdict cites a pack that did not produce it",
			want: CodeDigestMismatch,
			mutate: func(t *testing.T, dir string) {
				corruptDigestAfter(t, dir, "evidence/verdicts/onboard-no-correspondence.json", "packDigest")
			},
		},
		{
			name: "a suppression outlives the 30-day window",
			want: CodeSuppressWindow,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "bindings/party-service.yaml",
					"'2026-11-07T00:00:00Z'", "'2027-06-01T00:00:00Z'")
			},
		},
		{
			name: "a suppression names a rule that does not exist",
			want: CodeSuppressUnknown,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "bindings/party-service.yaml", "rule: SYN-CPT-001", "rule: SYN-CPT-009")
			},
		},
		{
			name: "a plan is not listed in the bundle",
			want: CodeChainUnknown,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "evidence/bundle.yaml",
					"plans/http.change.contactpoint.plan.json",
					"plans/http.change.contactpointMISSING.plan.json")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reg := loadRegistry(t)
			dir := copySample(t)
			c.mutate(t, dir)
			m, err := Load(dir)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			f := ValidateTree(m, reg)
			if !hasCode(f, c.want) {
				t.Fatalf("want %s, got %v", c.want, codesOf(f))
			}
		})
	}
}

// TestLintS2PermitsUngovernedConstants records the operational definition of S2: a list of
// constants that no value set owns is not a restated enumeration. The sample's placeholder
// tokens are exactly that case.
func TestLintS2PermitsUngovernedConstants(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	f := &Findings{}
	LintS2(m, f)
	if f.Len() != 0 {
		t.Errorf("expected no S2 findings, got %v", codesOf(f))
	}
}

// TestLintS2CatchesRestatedGovernedCodes proves the gate still fails on the thing it exists
// for: a rule that duplicates a code owned by a value set.
func TestLintS2CatchesRestatedGovernedCodes(t *testing.T) {
	dir := copySample(t)
	// GB is owned by country-code-iso3166-1, so adding it to a literal list restates it.
	edit(t, dir, "rules/intent/onboard.pack.yaml", "'UNKNOWN'", "'UNKNOWN', 'GB'")

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	f := &Findings{}
	LintS2(m, f)
	if !hasRuleFinding(f, CodeS2LiteralEnum, "SEM-ONB-003") {
		t.Fatalf("expected SEM-ONB-003 to be flagged, got %v", codesOf(f))
	}
	if f.Len() != 1 {
		t.Errorf("expected exactly one S2 finding, got %d: %v", f.Len(), codesOf(f))
	}
}

func appendBytes(t *testing.T, dir, rel, text string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open %s: %v", rel, err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatalf("append %s: %v", rel, err)
	}
}
