package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func bindingSchema(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "catalog", "schemas", "quality-binding.schema.yaml"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	return normalise(doc).(map[string]any)
}

// TestCRDIsStructurallyValid is the check that matters: the CRD schema must contain no keyword
// the API server rejects. Emitting only allowed keywords is a claim; this is the proof.
func TestCRDIsStructurallyValid(t *testing.T) {
	out, err := GenerateCRD(bindingSchema(t))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	var crd map[string]any
	if err := yaml.Unmarshal(out, &crd); err != nil {
		t.Fatalf("the CRD does not parse: %v", err)
	}
	if crd["kind"] != "CustomResourceDefinition" || crd["apiVersion"] != "apiextensions.k8s.io/v1" {
		t.Fatalf("wrong type: %v %v", crd["apiVersion"], crd["kind"])
	}

	spec := crd["spec"].(map[string]any)
	if spec["group"] != "dq.example.com" || spec["scope"] != "Namespaced" {
		t.Fatalf("group=%v scope=%v", spec["group"], spec["scope"])
	}
	names := spec["names"].(map[string]any)
	if names["kind"] != "QualityBinding" || names["plural"] != "qualitybindings" {
		t.Fatalf("names: %v", names)
	}

	versions := spec["versions"].([]any)
	schema := versions[0].(map[string]any)["schema"].(map[string]any)["openAPIV3Schema"]
	if problems := ValidateStructural(schema, ""); len(problems) > 0 {
		t.Fatalf("the generated CRD is not a structural schema:\n  %s", strings.Join(problems, "\n  "))
	}
}

// TestCRDCarriesTheCELRules ties the generated CEL to the source schema: every if/then block in
// the schema must have a counterpart, so adding a third conditional cannot silently be dropped.
func TestCRDCarriesTheCELRules(t *testing.T) {
	schema := bindingSchema(t)
	spec := schema["properties"].(map[string]any)["spec"].(map[string]any)

	conditionals := 0
	if allOf, ok := spec["allOf"].([]any); ok {
		for _, entry := range allOf {
			if m, ok := entry.(map[string]any); ok {
				if _, hasIf := m["if"]; hasIf {
					conditionals++
				}
			}
		}
	}
	rules := bindingCELRules()
	if conditionals != len(rules) {
		t.Fatalf("the schema declares %d conditional constraints but %d CEL rules are emitted; "+
			"a structural schema cannot carry if/then, so each one needs a rule", conditionals, len(rules))
	}

	out, err := GenerateCRD(schema)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, rule := range rules {
		if !strings.Contains(string(out), rule["rule"].(string)) {
			t.Errorf("the CRD does not carry the CEL rule %q", rule["rule"])
		}
	}
}

// TestCRDForbidsCombinators documents that the guard itself works: a schema that leaks a
// combinator is reported rather than silently accepted.
func TestCRDForbidsCombinators(t *testing.T) {
	leaky := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"bad": map[string]any{"oneOf": []any{map[string]any{"type": "string"}}},
		},
	}
	problems := ValidateStructural(leaky, "")
	if len(problems) == 0 {
		t.Fatal("a combinator must be reported")
	}
}
