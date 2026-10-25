package engine

import (
	"fmt"
	"sort"
)

// validateRules checks rule identity, naming, fixture coverage and provider data.
func validateRules(m *Model, f *Findings) {
	intentByCode := m.IntentByCode()

	for _, p := range m.AllPacks() {
		rules := p.Rules()
		seen := map[string]int{}
		codes := map[string]bool{}

		for _, r := range rules {
			seen[r.ID]++
			mm := ruleIDRe.FindStringSubmatch(r.ID)
			if mm == nil {
				f.AddRule(CodeRuleID, SeverityError, p.Path,
					"rule id "+r.ID+" is not (SYN|SEM|STA)-CCC-NNN", r.ID)
				continue
			}
			codes[mm[2]] = true
			if r.JSONPath != "" && !jsonPathRe.MatchString(r.JSONPath) {
				f.AddRule(CodeRulePath, SeverityError, p.Path,
					"path "+r.JSONPath+" does not match the schema's path pattern", r.ID)
			}
		}

		for id, n := range seen {
			if n > 1 {
				f.Error(CodeRuleID, p.Path, fmt.Sprintf("rule id %s appears %d times", id, n))
			}
		}

		if p.IsIntent() {
			want := ""
			if in, ok := m.IntentByName()[p.Spec.Intent]; ok {
				want = in.Code
			}
			for _, r := range rules {
				mm := ruleIDRe.FindStringSubmatch(r.ID)
				if mm == nil || want == "" || mm[2] == want {
					continue
				}
				f.AddRule(CodeRuleCode, SeverityError, p.Path,
					fmt.Sprintf("rule %s uses code %s, but intent %q is %s", r.ID, mm[2], p.Spec.Intent, want), r.ID)
			}
			continue
		}

		if len(codes) > 1 {
			f.Error(CodeRuleCode, p.Path,
				"rule IDs use more than one object code: "+joinStrings(keys(codes)))
		}

		byRule := map[string][]Fixture{}
		for _, t := range p.Spec.Tests {
			if seen[t.Rule] == 0 {
				f.Error(CodeFixtureOrphan, p.Path,
					"fixture references "+t.Rule+", which this pack does not define")
			}
			byRule[t.Rule] = append(byRule[t.Rule], t)
		}

		for _, r := range rules {
			expects := map[string]bool{}
			for _, t := range byRule[r.ID] {
				expects[t.Expect] = true
			}
			if !expects["pass"] {
				f.AddRule(CodeFixtureMissing, SeverityError, p.Path,
					"rule "+r.ID+" has no passing fixture", r.ID)
			}
			if !expects["fail"] {
				f.AddRule(CodeFixtureMissing, SeverityError, p.Path,
					"rule "+r.ID+" has no failing fixture", r.ID)
			}

			for _, need := range r.Needs {
				if need == "valueset" {
					continue // expanded from the contract, never supplied per fixture
				}
				key := need
				if need == "history" {
					key = "prior" // the history provider surfaces as the prior version
				}
				for _, t := range byRule[r.ID] {
					if _, ok := t.Input[key]; !ok {
						f.AddRule(CodeFixtureProvider, SeverityError, p.Path,
							fmt.Sprintf("rule %s needs %s, but a fixture for it carries no %q data", r.ID, need, key), r.ID)
						break
					}
				}
			}
		}
		_ = intentByCode
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func joinStrings(in []string) string {
	out := ""
	for i, s := range in {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	am := map[string]bool{}
	for _, s := range a {
		am[s] = true
	}
	for _, s := range b {
		if !am[s] {
			return false
		}
	}
	return true
}
