package engine

import "testing"

// TestAlignIsClean: the sample's API composes from the canonical contracts, so L1/L3 pass.
// A warning about a field with no rule is expected and allowed.
func TestAlignIsClean(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	f := AlignTree(m)
	for _, x := range f.Items() {
		if x.Severity == SeverityError {
			t.Errorf("unexpected error finding: %s %s — %s", x.Code, x.Path, x.Message)
		}
	}
}

// TestAlignDefects proves each gate can fail. L1 and L3 are the two halves of "the API and
// the contract are the same definition"; L4 is the contract-to-rule edge.
func TestAlignDefects(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		mutate func(t *testing.T, dir string)
	}{
		{
			name: "a component copies the object instead of referencing it",
			want: CodeL1Restated,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "spec/openapi/party-api.yaml",
					"    Party:\n      $ref: '../../catalog/business-objects/party.json'",
					"    Party:\n      type: object\n      properties:\n        partyId: { type: string }")
			},
		},
		{
			name: "the request body stops referencing the contract",
			want: CodeL1NotReferenced,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "spec/openapi/party-api.yaml",
					"            schema: { $ref: '#/components/schemas/OnboardPartyRequest' }",
					"            schema: { type: object }")
			},
		},
		{
			name: "a request wrapper redeclares a contract field",
			want: CodeL3Shadowed,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "spec/openapi/party-api.yaml",
					"      properties:\n        party: { $ref: '#/components/schemas/Party' }",
					"      properties:\n        partyId: { type: string }\n        party: { $ref: '#/components/schemas/Party' }")
			},
		},
		{
			name: "a rule points at a field the contract does not declare",
			want: CodeL4RuleField,
			mutate: func(t *testing.T, dir string) {
				edit(t, dir, "rules/object/party.pack.yaml",
					"path: $.individual.names", "path: $.individual.namez")
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copySample(t)
			c.mutate(t, dir)
			m, err := Load(dir)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			f := AlignTree(m)
			if !hasCode(f, c.want) {
				t.Fatalf("want %s, got %v", c.want, codesOf(f))
			}
		})
	}
}
