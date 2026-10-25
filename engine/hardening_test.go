package engine

import (
	"strings"
	"testing"
)

// TestPatternAuditAcceptsTheSample: every pattern in the sample is anchored and free of nested
// quantifiers, so the gate must be quiet.
func TestPatternAuditAcceptsTheSample(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	f := AuditPatterns(m)
	if f.Len() != 0 {
		for _, x := range f.Items() {
			t.Errorf("unexpected finding: %s", x.Message)
		}
	}
}

// TestPatternAuditCatchesTheShapesThatMatter: an unanchored pattern is a validation hole; a
// nested quantifier is a denial of service that arrives with traffic.
func TestPatternAuditCatchesTheShapesThatMatter(t *testing.T) {
	cases := []struct {
		pattern string
		want    string
	}{
		{"[0-9]+", "not anchored"},
		{"^(a+)+$", "backtrack"},
		{"^(a*)*b$", "backtrack"},
		{"^(x+x+)+y$", "backtrack"},
	}
	for _, c := range cases {
		problems := strings.Join(patternProblems(c.pattern), "; ")
		if !strings.Contains(problems, c.want) {
			t.Errorf("%q: expected %q, got %q", c.pattern, c.want, problems)
		}
	}
}

// TestPatternAuditAllowsSafeShapes: the audit must not cry wolf on the patterns a contract
// legitimately needs, or it will be switched off.
func TestPatternAuditAllowsSafeShapes(t *testing.T) {
	safe := []string{
		"^PTY-[0-9]{8}$",
		"^[A-Z0-9][A-Z0-9-]{5,19}$",
		"^[A-Z]{1,2}[0-9][0-9A-Z]? ?[0-9][A-Z]{2}$",
		"^[0-9]{5}(-[0-9]{4})?$",
		"^(a|b)+$",
	}
	for _, p := range safe {
		if problems := patternProblems(p); len(problems) > 0 {
			t.Errorf("%q should be accepted, got %v", p, problems)
		}
	}
}

// TestValidateReportsAnUnanchoredContractPattern: the audit is wired into validate, so an
// unsafe pattern in a contract fails the build rather than the gate.
func TestValidateReportsAnUnanchoredContractPattern(t *testing.T) {
	dir := copySample(t)
	edit(t, dir, "catalog/business-objects/party.json",
		`"pattern": "^PTY-[0-9]{8}$"`, `"pattern": "PTY-[0-9]{8}"`)

	reg := loadRegistry(t)
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	f := ValidateTree(m, reg)
	if !hasCode(f, CodePatternUnsafe) {
		t.Fatalf("want %s, got %v", CodePatternUnsafe, codesOf(f))
	}
}
