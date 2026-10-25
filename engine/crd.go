package engine

import (
	"fmt"
	"sort"
)

// The CRD is an OpenAPI v3.0 structural schema, a strict subset of JSON Schema: every node
// needs a type, and combinators, $ref, if/then/else and $id are not allowed. This file projects
// the binding schema onto that subset and moves the two conditional constraints into CEL,
// because they have no structural equivalent.

// structuralKeywords are the only keywords carried into the CRD. A keyword not listed here is
// dropped rather than copied, so a new JSON Schema keyword cannot silently become invalid.
var structuralKeywords = map[string]bool{
	"type": true, "properties": true, "required": true, "items": true,
	"enum": true, "pattern": true, "format": true, "description": true,
	"minLength": true, "maxLength": true, "minimum": true, "maximum": true,
	"minItems": true, "maxItems": true, "uniqueItems": true, "default": true,
	"additionalProperties": true,
}

// forbiddenInCRD are keywords that must never appear in a structural schema.
var forbiddenInCRD = []string{
	"$ref", "$schema", "$id", "allOf", "anyOf", "oneOf", "not",
	"if", "then", "else", "dependencies", "dependentRequired",
}

// bindingCELRules are the constraints the design states in CEL because a structural schema
// cannot express them. They are hand-mapped from the two if/then blocks in the source schema,
// and TestCRDCarriesTheCELRules fails if this list and the schema ever disagree.
func bindingCELRules() []map[string]any {
	return []map[string]any{
		{
			"rule":    "self.mode != 'ENFORCE' || has(self.fallbackDigest)",
			"message": "mode ENFORCE requires a fallbackDigest: without one, a bundle fetch failure has nothing to fall back to",
		},
		{
			"rule":    "self.mode == 'OFF' || has(self.channels)",
			"message": "SHADOW, WARN and ENFORCE all require at least one channel",
		},
	}
}

// structural projects one JSON Schema node onto the CRD subset, recursively.
func structural(node any) (map[string]any, error) {
	m, ok := node.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("not a schema node")
	}
	out := map[string]any{}
	for key, value := range m {
		if !structuralKeywords[key] {
			continue
		}
		out[key] = value
	}

	if props, ok := m["properties"].(map[string]any); ok {
		converted := map[string]any{}
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child, err := structural(props[k])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			converted[k] = child
		}
		out["properties"] = converted
	}
	if items, ok := m["items"].(map[string]any); ok {
		child, err := structural(items)
		if err != nil {
			return nil, fmt.Errorf("items: %w", err)
		}
		out["items"] = child
	}
	if _, ok := out["type"]; !ok {
		// JSON Schema allows an enum without a type; a structural schema does not. Infer it from
		// the values rather than reject the document, and refuse a mixed enum instead of
		// guessing.
		if inferred, ok := inferType(out["enum"]); ok {
			out["type"] = inferred
		} else {
			return nil, fmt.Errorf("a structural schema node requires a type")
		}
	}
	return out, nil
}

// inferType derives a structural type from an enum's values.
func inferType(enum any) (string, bool) {
	values, ok := enum.([]any)
	if !ok || len(values) == 0 {
		return "", false
	}
	kind := ""
	for _, v := range values {
		current := ""
		switch v.(type) {
		case string:
			current = "string"
		case bool:
			current = "boolean"
		case int, int64, float64:
			current = "number"
		default:
			return "", false
		}
		if kind == "" {
			kind = current
			continue
		}
		if kind != current {
			return "", false
		}
	}
	return kind, true
}

// ValidateStructural reports every way a document violates the structural subset. It runs on
// the generated CRD rather than being assumed, because "we only emitted allowed keywords" is a
// claim, not a proof.
func ValidateStructural(node any, path string) []string {
	problems := []string{}
	m, ok := node.(map[string]any)
	if !ok {
		return append(problems, path+": not a schema node")
	}
	for _, bad := range forbiddenInCRD {
		if _, found := m[bad]; found {
			problems = append(problems, path+": keyword "+bad+" is not allowed in a structural schema")
		}
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for name, child := range props {
			problems = append(problems, ValidateStructural(child, path+"/properties/"+name)...)
		}
	}
	if items, ok := m["items"]; ok {
		problems = append(problems, ValidateStructural(items, path+"/items")...)
	}
	return problems
}
