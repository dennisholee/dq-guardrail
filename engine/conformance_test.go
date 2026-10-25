package engine

import (
	"path/filepath"
	"testing"
)

// TestSampleIsClean is the acceptance test for M1.1: the worked sample, which claims to be
// conformant, must produce no error-severity findings.
func TestSampleIsClean(t *testing.T) {
	reg := loadRegistry(t)
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	f := ValidateTree(m, reg)
	for _, x := range f.Items() {
		if x.Severity == SeverityError {
			t.Errorf("unexpected error finding: %s %s — %s", x.Code, x.Path, x.Message)
		}
	}
	if len(m.Docs) == 0 || len(m.Contracts) == 0 {
		t.Fatalf("the sample did not load: %d documents, %d contracts", len(m.Docs), len(m.Contracts))
	}
	t.Logf("%d documents, %d contracts, %d object packs, %d intent packs, %d plans, %d verdicts",
		len(m.Docs), len(m.Contracts), len(m.ObjectPacks), len(m.IntentPacks), len(m.Plans), len(m.Verdicts))
}

// TestStructureDefects turns each defect this project has already met into a permanent
// case. A check without a case that proves it can fail is a check nobody has exercised.
func TestStructureDefects(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		mutate func(t *testing.T, dir string)
	}{
		{
			name: "contract reference does not resolve",
			want: CodeRefUnresolved,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "catalog/business-objects/party.json", "#/$defs/organization", "#/$defs/org")
			},
		},
		{
			name: "pack targets a version the contract does not have",
			want: CodeVersionDrift,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "rules/object/party.pack.yaml", "schemaVersion: 1.0.0", "schemaVersion: 2.0.0")
			},
		},
		{
			name: "a rule loses its failing fixture",
			want: CodeFixtureMissing,
			mutate: func(t *testing.T, dir string) {
				deleteLines(t, dir, "rules/object/party.pack.yaml", "expect: fail")
			},
		},
		{
			name: "a contract uses a forbidden combinator",
			want: CodeCombinator,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "catalog/business-objects/party.json",
					`"required": ["partyId"],`,
					`"required": ["partyId"], "oneOf": [{"required": ["individual"]}],`)
			},
		},
		{
			name: "a contract invents a keyword",
			want: CodeSchema,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "catalog/business-objects/party.json",
					`"minLength": 2,`, `"minLength": 2, "default": "x",`)
			},
		},
		{
			name: "an intent rule uses a code the registry does not grant",
			want: CodeRuleCode,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "rules/intent/onboard.pack.yaml", "SEM-ONB-001", "SEM-XXX-001")
			},
		},
		{
			name: "the endpoint disagrees with its address key",
			want: CodeAPIKey,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "spec/openapi/party-api.yaml", "x-dq-intent: onboard", "x-dq-intent: change")
			},
		},
		{
			name: "a date is left unquoted",
			want: CodeTemporal,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "catalog/intents.yaml", "approvedOn: '2026-09-15'", "approvedOn: 2026-09-15")
			},
		},
		{
			name: "a rule path uses a wildcard the schema forbids",
			want: CodeRulePath,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "rules/object/contactpoint.pack.yaml",
					"path: $.contacts\n", "path: $.contacts[*].address\n")
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

// TestRegistryIsRequired documents that the tool cannot validate without the formats.
func TestRegistryIsRequired(t *testing.T) {
	if _, err := LoadSchemaRegistry(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("expected an error for a missing registry")
	}
}
