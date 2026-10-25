package engine

import (
	"fmt"
	"sort"
)

// ExpandContract returns a self-contained JSON Schema for an object: every value-set reference
// becomes an inline enum, so tier 0 can be evaluated with no access to the catalogue.
//
// This is the step the design calls expansion. Doing it at load time rather than at request
// time is what keeps the runtime small enough to run inside a filter: by the time a payload
// arrives, the schema is already a plain JSON Schema.
func ExpandContract(c *Contract, valueSets map[string]*ValueSet) (map[string]any, error) {
	src, ok := c.Raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: contract is not a mapping", c.Path)
	}
	expanded, err := expandNode(src, valueSets)
	if err != nil {
		return nil, err
	}
	doc, ok := expanded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expansion did not produce a mapping", c.Path)
	}
	return doc, nil
}

func expandNode(node any, valueSets map[string]*ValueSet) (any, error) {
	switch v := node.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok && len(ref) > len(valuesetURIPrefix) &&
			ref[:len(valuesetURIPrefix)] == valuesetURIPrefix {
			name := ref[len(valuesetURIPrefix):]
			vs, found := valueSets[name]
			if !found {
				return nil, fmt.Errorf("value set %q is not in the catalogue", name)
			}
			codes := vs.Codes()
			sort.Strings(codes)
			out := map[string]any{"enum": toAnySlice(codes)}
			if vs.CaseSensitive == false && len(codes) > 0 {
				out["description"] = "values from " + name
			}
			return out, nil
		}
		out := make(map[string]any, len(v))
		for key, val := range v {
			expanded, err := expandNode(val, valueSets)
			if err != nil {
				return nil, err
			}
			out[key] = expanded
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			expanded, err := expandNode(val, valueSets)
			if err != nil {
				return nil, err
			}
			out[i] = expanded
		}
		return out, nil
	default:
		return node, nil
	}
}

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

// ObjectCode returns the three-letter code a plan's rules use for an object. It is derived
// from the rules rather than declared, because no artefact declares it and inventing a field
// would be a schema change; the derived value is used only to name structural violations.
func ObjectCode(m *Model, object string) string {
	for _, p := range m.ObjectPacks {
		if p.Spec.Object.Name != object {
			continue
		}
		for _, r := range p.Rules() {
			if mm := ruleIDRe.FindStringSubmatch(r.ID); mm != nil {
				return mm[2]
			}
		}
	}
	return "OBJ"
}
