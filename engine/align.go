package engine

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// injectedNames are the identifiers a rule may read that are not contract fields: the prior
// version, and the providers that surface as collections.
var injectedNames = map[string]bool{
	"prior": true, "refdata": true, "delta": true, "valueset": true, "history": true,
}

var thisFieldRe = regexp.MustCompile(`this\.([A-Za-z_][A-Za-z0-9_]*)`)

// AlignTree runs the L1/L3/L4 gates: the API must reference the canonical contracts rather
// than restate them, must not shadow their fields, and every rule must point at a field the
// contract declares.
func AlignTree(m *Model) *Findings {
	f := &Findings{}
	checkL1(m, f)
	checkL3(m, f)
	checkL4(m, f)
	return f
}

// checkL1 requires that an operation addressing an object reaches that object's contract by
// $ref, and that a component named after an object is a reference rather than a copy.
func checkL1(m *Model, f *Findings) {
	for _, api := range m.APIs {
		for opPath, ops := range api.Paths {
			for verb, op := range ops {
				if op.Object == "" {
					continue
				}
				where := fmt.Sprintf("%s#%s %s", api.Path, verb, opPath)
				if !reachableObjects(api, op.RequestBody)[op.Object] {
					f.Error(CodeL1NotReferenced, where,
						fmt.Sprintf("the request never $refs the canonical contract for %q", op.Object))
				}
			}
		}

		for _, name := range sortedAnyKeys(api.Components.Schemas) {
			obj := strings.ToLower(name)
			if _, ok := m.Contracts[obj]; !ok {
				continue
			}
			if len(CollectRefs(api.Components.Schemas[name])) == 0 {
				f.Error(CodeL1Restated, fmt.Sprintf("%s#/components/schemas/%s", api.Path, name),
					"component restates the "+obj+" object instead of $ref-ing its contract")
			}
		}
	}
}

// reachableObjects follows component references and reports the contracts a schema reaches.
func reachableObjects(api *APIDoc, seed any) map[string]bool {
	out := map[string]bool{}
	seen := map[string]bool{}

	var walk func(any)
	walk = func(node any) {
		for _, ref := range CollectRefs(node) {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			if raw, ok := componentRaw(api, ref); ok {
				walk(raw)
				continue
			}
			if strings.Contains(ref, "/catalog/business-objects/") {
				file := strings.SplitN(ref, "#", 2)[0]
				out[strings.TrimSuffix(path.Base(file), ".json")] = true
			}
		}
	}
	walk(seed)
	return out
}

func componentRaw(api *APIDoc, ref string) (any, bool) {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return nil, false
	}
	raw, ok := api.Components.Schemas[strings.TrimPrefix(ref, prefix)]
	return raw, ok
}

func sortedAnyKeys(in map[string]any) []string {
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkL3 rejects a request that redeclares a field the canonical contract already defines.
// The point is not purity: a second definition of partyId is a second thing to change, and
// the one in the API will be the one nobody remembers.
func checkL3(m *Model, f *Findings) {
	for _, api := range m.APIs {
		for opPath, ops := range api.Paths {
			for verb, op := range ops {
				if op.Object == "" {
					continue
				}
				c, ok := m.Contracts[op.Object]
				if !ok {
					continue
				}
				fields := contractFields(c.Raw)
				where := fmt.Sprintf("%s#%s %s", api.Path, verb, opPath)
				for _, name := range wrapperProperties(api, op.RequestBody) {
					if _, clashes := fields[name]; clashes {
						f.Error(CodeL3Shadowed, where,
							fmt.Sprintf("the request redeclares %q, which the %s contract already defines", name, op.Object))
					}
				}
			}
		}
	}
}

// wrapperProperties returns the property names a request wrapper defines literally. A
// property that is itself a $ref to the canonical contract is composition, not shadowing,
// and is skipped.
func wrapperProperties(api *APIDoc, node any) []string {
	out := []string{}
	for _, ref := range CollectRefs(node) {
		raw, ok := componentRaw(api, ref)
		if !ok {
			continue
		}
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		props, ok := m["properties"].(map[string]any)
		if !ok {
			continue
		}
		for _, name := range sortedAnyKeys(props) {
			if len(CollectRefs(props[name])) > 0 {
				continue
			}
			out = append(out, name)
		}
	}
	return out
}

// checkL4 checks the contract-to-rule edge in both directions: a rule must name a field that
// exists (an error), and a field with no rule is a coverage gap (a warning, because not every
// field needs a rule).
func checkL4(m *Model, f *Findings) {
	rulesByObject := map[string][]Rule{}
	for _, p := range m.ObjectPacks {
		rulesByObject[p.Spec.Object.Name] = p.Rules()
	}

	objects := make([]string, 0, len(m.Contracts))
	for name := range m.Contracts {
		objects = append(objects, name)
	}
	sort.Strings(objects)

	for _, obj := range objects {
		c := m.Contracts[obj]
		used := map[string]bool{}

		for _, r := range rulesByObject[obj] {
			for _, field := range referencedFields(r) {
				used[rootField(field)] = true
				if !resolveContractPath(c.Raw, field) {
					f.AddRule(CodeL4RuleField, SeverityError, c.Path,
						fmt.Sprintf("rule %s refers to %q, which the %s contract does not declare", r.ID, field, obj), r.ID)
				}
			}
		}

		for _, name := range sortedAnyKeys(contractFields(c.Raw)) {
			if !used[name] {
				f.Add(CodeL4Uncovered, SeverityWarn, c.Path,
					"field "+name+" is declared but no rule references it")
			}
		}
	}
}

// referencedFields returns the contract fields a rule reads: its declared path, and any
// this.<field> token in its expressions.
func referencedFields(r Rule) []string {
	out := []string{}
	if r.JSONPath != "" {
		out = append(out, r.JSONPath)
	}
	for _, expr := range []string{r.When, r.Assert} {
		for _, mm := range thisFieldRe.FindAllStringSubmatch(expr, -1) {
			name := mm[1]
			if injectedNames[name] {
				continue
			}
			out = append(out, name)
		}
	}
	return out
}

// resolveContractPath reports whether a $ path or a bare field name resolves in the contract.
// Where resolution is impossible (a $ref, a node without properties) it returns true: this
// check must not cry wolf about a field it cannot see.
func resolveContractPath(raw any, ref string) bool {
	root, ok := raw.(map[string]any)
	if !ok {
		return true
	}
	if !strings.HasPrefix(ref, "$") {
		props, ok := root["properties"].(map[string]any)
		if !ok {
			return true
		}
		_, found := props[ref]
		return found
	}

	var node any = root
	for _, seg := range strings.Split(strings.TrimPrefix(ref, "$."), ".") {
		seg = strings.TrimSuffix(seg, "]")
		if i := strings.IndexByte(seg, '['); i >= 0 {
			seg = seg[:i]
		}
		if seg == "" {
			continue
		}
		current, ok := derefNode(root, node).(map[string]any)
		if !ok {
			return true
		}
		props, ok := current["properties"].(map[string]any)
		if !ok {
			return true
		}
		next, ok := props[seg]
		if !ok {
			return false
		}
		node = next
	}
	return true
}

// derefNode follows a single internal #/$defs/<name> reference, because a contract's nested
// shapes live in $defs and a field that points at one has no properties of its own.
func derefNode(root map[string]any, node any) any {
	m, ok := node.(map[string]any)
	if !ok {
		return node
	}
	ref, _ := m["$ref"].(string)
	if !strings.HasPrefix(ref, "#/$defs/") {
		return node
	}
	defs, ok := root["$defs"].(map[string]any)
	if !ok {
		return node
	}
	if target, ok := defs[strings.TrimPrefix(ref, "#/$defs/")]; ok {
		return target
	}
	return node
}

func contractFields(raw any) map[string]any {
	if m, ok := raw.(map[string]any); ok {
		if props, ok := m["properties"].(map[string]any); ok {
			return props
		}
	}
	return map[string]any{}
}

// rootField reduces "$.individual.names" or "individual" to the top-level field it touches,
// because coverage is counted per declared field, not per dotted path.
func rootField(ref string) string {
	s := strings.TrimPrefix(ref, "$.")
	if i := strings.IndexAny(s, ".["); i >= 0 {
		return s[:i]
	}
	return s
}
