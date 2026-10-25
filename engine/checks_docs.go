package engine

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ruleIDRe     = regexp.MustCompile(`^(SYN|SEM|STA)-([A-Z]{3})-([0-9]{3})$`)
	objectNameRe = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	jsonPathRe   = regexp.MustCompile(`^\$(\.[A-Za-z_][A-Za-z0-9_]*|\[[0-9]+\])*$`)
	ulidRe       = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)
	bundlePinRe  = regexp.MustCompile(`@(sha256:[a-f0-9]{64})$`)
	digestRe     = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	semverRe     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

const (
	valuesetURIPrefix = "https://registry.internal/dq/valuesets/"
	schemaURIPrefix   = "https://registry.internal/dq/schemas/"
)

// combinators are excluded from canonical contracts by design: "exactly one of" is a
// tier-1 rule, which produces a violation a caller can act on.
var combinators = []string{"allOf", "anyOf", "oneOf", "not"}

// ValidateTree runs every check and returns the accumulated findings. The order matches
// the reader's mental model: documents, then contracts, then semantics, then evidence.
func ValidateTree(m *Model, reg *SchemaRegistry) *Findings {
	f := &Findings{}
	validateDocs(m, reg, f)
	validateAPI(m, f)
	validateContracts(m, f)
	mergeFindings(f, AuditPatterns(m))
	validateVersions(m, f)
	validateIntents(m, f)
	validateRules(m, f)
	validatePlans(m, f)
	validateBundle(m, f)
	validateBindings(m, f)
	validateVerdicts(m, f)
	validateCoverage(m, f)
	return f
}

// validateDocs parses, schema-validates and inspects every document.
func validateDocs(m *Model, reg *SchemaRegistry, f *Findings) {
	for _, pe := range m.ParseErrors {
		f.Error(CodeParse, pe.Path, "does not parse: "+pe.Message)
	}

	for _, d := range m.Docs {
		name := SchemaForPath(d.Path)
		if name == "" {
			continue
		}
		if err := reg.Validate(name, d.Raw); err != nil {
			f.Error(CodeSchema, d.Path, "violates "+name+": "+oneLine(err.Error()))
		}
	}

	// YAML turns an unquoted 2026-09-15 into a timestamp. That is not a string, so it
	// fails `type: string` for any consumer that is not our own emitter.
	for _, d := range m.Docs {
		rel := d.Path
		WalkScalars(d.Raw, func(p string, v any) {
			if _, ok := v.(time.Time); ok {
				f.Error(CodeTemporal, rel+at(p),
					"unquoted date/timestamp decodes as a timestamp, not a string — quote it")
			}
		})
	}
}

// validateContracts checks the $ref graph and the design's own exclusions.
func validateContracts(m *Model, f *Findings) {
	for name, c := range m.Contracts {
		if c.ID == "" {
			f.Error(CodeVocabulary, c.Path, "missing $id")
		}
		if c.Object != "" && c.Object != name {
			f.Error(CodeVocabulary, c.Path,
				fmt.Sprintf("$id names object %q but the file is %q", c.Object, name))
		}
		for _, bad := range combinators {
			if containsKey(c.Raw, bad) {
				f.Error(CodeCombinator, c.Path,
					"combinator "+bad+" is forbidden in a canonical contract")
			}
		}

		defs := map[string]any{}
		if dm, ok := c.Raw.(map[string]any); ok {
			if d, ok := dm["$defs"].(map[string]any); ok {
				defs = d
			}
		}

		for _, ref := range CollectRefs(c.Raw) {
			switch {
			case strings.HasPrefix(ref, "#/$defs/"):
				if _, ok := defs[strings.TrimPrefix(ref, "#/$defs/")]; !ok {
					f.Error(CodeRefUnresolved, c.Path, "$ref "+ref+" has no matching $defs entry")
				}
			case strings.HasPrefix(ref, valuesetURIPrefix):
				vs := strings.TrimPrefix(ref, valuesetURIPrefix)
				if _, ok := m.ValueSets[vs]; !ok {
					f.Error(CodeRefUnresolved, c.Path, "$ref value set "+vs+" has no file in catalog/value-sets")
				}
			case strings.HasPrefix(ref, schemaURIPrefix):
				if mm := contractIDRe.FindStringSubmatch(ref); mm != nil {
					target, ok := m.Contracts[mm[1]]
					if !ok {
						f.Error(CodeRefUnresolved, c.Path, "$ref object "+mm[1]+" has no contract")
					} else if target.Version != mm[2] {
						f.Error(CodeRefUnresolved, c.Path,
							fmt.Sprintf("$ref %s names version %s, contract is %s", ref, mm[2], target.Version))
					}
				} else {
					f.Error(CodeRefUnresolved, c.Path, "$ref "+ref+" is not a contract or value-set URI")
				}
			default:
				f.Error(CodeRefUnresolved, c.Path,
					"$ref "+ref+" is neither a catalogue URI nor #/$defs/<name>")
			}
		}
	}
}

func containsKey(node any, key string) bool {
	switch v := node.(type) {
	case map[string]any:
		if _, ok := v[key]; ok {
			return true
		}
		for _, val := range v {
			if containsKey(val, key) {
				return true
			}
		}
	case []any:
		for _, val := range v {
			if containsKey(val, key) {
				return true
			}
		}
	}
	return false
}

func at(scalarPath string) string {
	if scalarPath == "" {
		return ""
	}
	return "#" + scalarPath
}

func oneLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	runes := []rune(s)
	if len(runes) > 240 {
		s = string(runes[:240]) + "…"
	}
	return s
}
