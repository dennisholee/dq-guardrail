package engine

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestGovernanceExportValidatesAgainstItsSchema: a generated artefact that does not satisfy the
// schema describing it is a document nobody can ingest.
func TestGovernanceExportValidatesAgainstItsSchema(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	exp, err := BuildGovernanceExport(m)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, err := MarshalGovernanceExport(exp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	reg := loadRegistry(t)
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := reg.Validate("governance-export.schema.yaml", doc); err != nil {
		t.Fatalf("the export does not satisfy its own schema: %v", err)
	}
}

// TestGovernanceExportIsDeterministic: a catalogue integration that churns on every run is noise,
// and noise is how a real change gets ignored.
func TestGovernanceExportIsDeterministic(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	first, err := BuildGovernanceExport(m)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	second, err := BuildGovernanceExport(m)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	a, _ := MarshalGovernanceExport(first)
	b, _ := MarshalGovernanceExport(second)
	if !bytes.Equal(a, b) {
		t.Fatal("two builds of the same tree differ")
	}
}

// TestGovernanceExportJoinsFieldsToRules: the point of the projection is that a catalogue column
// says which assertions govern it.
func TestGovernanceExportJoinsFieldsToRules(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	exp, err := BuildGovernanceExport(m)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	var party *ExportedObject
	for i := range exp.Objects {
		if exp.Objects[i].Name == "party" {
			party = &exp.Objects[i]
		}
	}
	if party == nil {
		t.Fatal("party is not exported")
	}
	if party.Owner == "" || party.ContractDigest == "" {
		t.Errorf("party has no owner or digest: %+v", party)
	}
	if len(party.Rules) == 0 {
		t.Fatal("party has no rules")
	}
	if len(party.ValueSets) == 0 {
		t.Error("party references no value sets, which cannot be right for a contract that does")
	}

	governed := 0
	for _, f := range party.Fields {
		if len(f.GovernedBy) > 0 {
			governed++
		}
	}
	if governed == 0 {
		t.Error("no field is attributed to a rule; the join is the reason this document exists")
	}
	// partyId is named by STA-PTY-001 and by the pattern rule in the contract.
	for _, f := range party.Fields {
		if f.Path != "$.partyId" {
			continue
		}
		joined := strings.Join(f.GovernedBy, ",")
		if !strings.Contains(joined, "STA-PTY-001") {
			t.Errorf("partyId should name STA-PTY-001, got %q", joined)
		}
	}
}

// TestExportChangesWhenTheTreeChanges: the artefact is a projection, so a rule edit must move it.
// Without this, `export --check` would be a guard on nothing.
func TestExportChangesWhenTheTreeChanges(t *testing.T) {
	base, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	before, err := BuildGovernanceExport(base)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	beforeBytes, _ := MarshalGovernanceExport(before)

	dir := copySample(t)
	edit(t, dir, "rules/object/party.pack.yaml",
		"message: A party is exactly one of individual or organization, never both and never neither",
		"message: A party is exactly one of individual or organization")
	changed, err := Load(dir)
	if err != nil {
		t.Fatalf("load changed: %v", err)
	}
	after, err := BuildGovernanceExport(changed)
	if err != nil {
		t.Fatalf("export changed: %v", err)
	}
	afterBytes, _ := MarshalGovernanceExport(after)

	if bytes.Equal(beforeBytes, afterBytes) {
		t.Fatal("the export did not change when a rule message did")
	}
}
