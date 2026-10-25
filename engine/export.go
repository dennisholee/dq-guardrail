package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// GovernanceExport is the read-only projection of the artefact graph into a data catalogue.
//
// It exists because the catalogue is where an owner actually looks, and a rule that lives only in
// a pull request is a rule nobody outside this repository knows about. What it is not: a second
// source of truth. It is generated, `--check`-guarded, and one-way (S5).
type GovernanceExport struct {
	ExportFormat  int              `json:"exportFormat"`
	GeneratedFrom ExportedFrom     `json:"generatedFrom"`
	Objects       []ExportedObject `json:"objects"`
}

// ExportedFrom is the provenance a reader needs to know which tree this describes.
type ExportedFrom struct {
	BundleDigest  string `json:"bundleDigest"`
	ObjectCount   int    `json:"objectCount"`
	RuleCount     int    `json:"ruleCount"`
	ValueSetCount int    `json:"valueSetCount"`
}

// ExportedObject is one governed object as a catalogue sees it: an entity with fields, an owner,
// a criticality, the rules that govern it and the enumerations it depends on.
type ExportedObject struct {
	Name           string             `json:"name"`
	Version        string             `json:"version"`
	Description    string             `json:"description"`
	ContractDigest string             `json:"contractDigest"`
	Owner          string             `json:"owner"`
	Steward        *ExportedSteward   `json:"steward,omitempty"`
	Criticality    string             `json:"criticality,omitempty"`
	Sensitivity    string             `json:"sensitivity,omitempty"`
	Retention      string             `json:"retention,omitempty"`
	AllowedIntents []string           `json:"allowedIntents,omitempty"`
	Thresholds     map[string]float64 `json:"thresholds,omitempty"`
	Fields         []ExportedField    `json:"fields"`
	Rules          []ExportedRule     `json:"rules"`
	ValueSets      []ExportedValueSet `json:"valueSets,omitempty"`
}

type ExportedSteward struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

// ExportedField is one field, the rules that name it, and its own description. The join from
// catalogue column to assertion is the point of the document.
type ExportedField struct {
	Path        string   `json:"path"`
	Description string   `json:"description"`
	Required    bool     `json:"required,omitempty"`
	GovernedBy  []string `json:"governedBy"`
}

type ExportedRule struct {
	RuleID     string `json:"ruleId"`
	Severity   string `json:"severity"`
	Dimension  string `json:"dimension"`
	Message    string `json:"message"`
	PackDigest string `json:"packDigest"`
}

type ExportedValueSet struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

// BuildGovernanceExport derives the projection from the tree. Order is fixed everywhere so the
// document is byte-reproducible: a catalogue integration that churns on every run is noise.
func BuildGovernanceExport(m *Model) (*GovernanceExport, error) {
	packByName := m.PackByName()
	ruleIndex := m.RuleIndex()

	// packName -> object name, so a rule can be attributed to the object it governs.
	packObject := map[string]string{}
	for _, p := range m.ObjectPacks {
		packObject[p.Metadata.Name] = p.Spec.Object.Name
	}

	rulesByObject := map[string][]*Rule{}
	for _, id := range sortedRuleIDs(ruleIndex) {
		r := ruleIndex[id]
		if object := packObject[r.PackName]; object != "" {
			rulesByObject[object] = append(rulesByObject[object], r)
		}
		// An intent rule governs a request rather than an object, so it is not attributed.
	}

	out := &GovernanceExport{ExportFormat: 1}
	names := make([]string, 0, len(m.Contracts))
	for name := range m.Contracts {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		c := m.Contracts[name]
		rules := rulesByObject[name]

		obj := ExportedObject{
			Name:           name,
			Version:        c.Version,
			Description:    contractDescription(c.Raw),
			ContractDigest: c.Digest,
			Fields:         buildFields(c.Raw, rules),
			Rules:          exportRules(rules, packByName),
		}
		if meta := m.Metas[name]; meta != nil {
			obj.Owner = meta.Owner
			if meta.Steward.Name != "" || meta.Steward.Email != "" {
				obj.Steward = &ExportedSteward{Name: meta.Steward.Name, Email: meta.Steward.Email}
			}
			obj.Criticality = meta.Criticality
			obj.Sensitivity = meta.Sensitivity
			obj.Retention = meta.Retention
			obj.AllowedIntents = meta.AllowedIntents
			obj.Thresholds = meta.Thresholds
		}
		if obj.Owner == "" {
			return nil, fmt.Errorf("object %s has no owner; a catalogue entry without one is decoration", name)
		}

		obj.ValueSets = referencedValueSets(c.Raw, m)
		out.Objects = append(out.Objects, obj)
	}

	out.GeneratedFrom = ExportedFrom{
		ObjectCount:   len(out.Objects),
		RuleCount:     len(ruleIndex),
		ValueSetCount: len(m.ValueSets),
	}
	if m.Bundle != nil {
		out.GeneratedFrom.BundleDigest = m.Bundle.Digest
	}
	if out.GeneratedFrom.BundleDigest == "" {
		return nil, fmt.Errorf("no bundle manifest, so the export cannot say which tree it describes")
	}
	return out, nil
}

// MarshalGovernanceExport renders the document deterministically: indented, no timestamps, no
// map iteration order.
func MarshalGovernanceExport(e *GovernanceExport) ([]byte, error) {
	body, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func sortedRuleIDs(index map[string]*Rule) []string {
	ids := make([]string, 0, len(index))
	for id := range index {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// exportRules carries each rule's own message and the digest of the pack that owns it, so a
// catalogue reader can go from a failed assertion to the artefact that defines it.
func exportRules(rules []*Rule, packs map[string]*Pack) []ExportedRule {
	out := make([]ExportedRule, 0, len(rules))
	for _, r := range rules {
		digest := ""
		if p := packs[r.PackName]; p != nil {
			digest = p.Digest
		}
		out = append(out, ExportedRule{
			RuleID:     r.ID,
			Severity:   r.Severity,
			Dimension:  r.Dimension,
			Message:    r.Message,
			PackDigest: digest,
		})
	}
	return out
}

// contractDescription reads the contract's own title and description. A catalogue entry that
// carries neither is a name and nothing else.
func contractDescription(raw any) string {
	doc, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	title, _ := doc["title"].(string)
	desc, _ := doc["description"].(string)
	switch {
	case desc != "" && title != "":
		return title + ": " + desc
	case desc != "":
		return desc
	case title != "":
		return title
	}
	return ""
}

// buildFields emits one entry per top-level property, each naming the rules whose path starts
// there. Nested paths are attributed to their top-level field, which is how a catalogue models
// columns; a reader who needs the leaf has the rule's own path in the violation.
func buildFields(raw any, rules []*Rule) []ExportedField {
	doc, ok := raw.(map[string]any)
	if !ok {
		return []ExportedField{}
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		return []ExportedField{}
	}
	required := map[string]bool{}
	if list, ok := doc["required"].([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok {
				required[s] = true
			}
		}
	}

	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)

	fields := make([]ExportedField, 0, len(names))
	for _, name := range names {
		f := ExportedField{
			Path:       "$." + name,
			Required:   required[name],
			GovernedBy: []string{},
		}
		if spec, ok := props[name].(map[string]any); ok {
			f.Description, _ = spec["description"].(string)
		}
		prefix := "$." + name
		for _, r := range rules {
			if r.JSONPath == prefix || strings.HasPrefix(r.JSONPath, prefix+".") || strings.HasPrefix(r.JSONPath, prefix+"[") {
				f.GovernedBy = append(f.GovernedBy, r.ID)
			}
		}
		sort.Strings(f.GovernedBy)
		fields = append(fields, f)
	}
	return fields
}

// referencedValueSets finds the governed enumerations a contract depends on, by scanning its
// $refs. A contract never restates a value set (S2), so the reference is the only link.
func referencedValueSets(raw any, m *Model) []ExportedValueSet {
	const prefix = "https://registry.internal/dq/valuesets/"
	found := map[string]bool{}
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			for key, val := range v {
				if key == "$ref" {
					if s, ok := val.(string); ok && strings.HasPrefix(s, prefix) {
						found[strings.TrimPrefix(s, prefix)] = true
					}
				}
				walk(val)
			}
		case []any:
			for _, val := range v {
				walk(val)
			}
		}
	}
	walk(raw)

	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ExportedValueSet, 0, len(names))
	for _, name := range names {
		vs := m.ValueSets[name]
		if vs == nil {
			continue // a dangling reference is validate's problem, not the export's
		}
		digest, err := m.DigestFile(vs.Path)
		if err != nil {
			return out
		}
		out = append(out, ExportedValueSet{Name: name, Version: vs.Version, Digest: digest})
	}
	return out
}
