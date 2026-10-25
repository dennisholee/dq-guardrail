package engine

import "fmt"

// validatePlans checks that each compiled plan agrees with the packs it was compiled from.
// A hand-authored plan drifts; comparing every field is what makes the drift visible.
func validatePlans(m *Model, f *Findings) {
	rules := m.RuleIndex()
	tierOfPrefix := map[string]int{"SYN": 0, "SEM": 1, "STA": 2}

	bundleRelax := map[string]bool{}
	if m.Bundle != nil {
		for _, rx := range m.Bundle.Relax {
			bundleRelax[rx.Rule] = true
		}
	}

	for path, plan := range m.Plans {
		if c, ok := m.Contracts[plan.Object]; !ok {
			f.Error(CodeChainUnknown, path, "object "+plan.Object+" has no contract")
		} else {
			if plan.ObjectVersion != c.Version {
				f.Error(CodeVersionDrift, path,
					fmt.Sprintf("objectVersion %s != contract %s", plan.ObjectVersion, c.Version))
			}
			if plan.SchemaMatcher.Ref != c.ID {
				f.Error(CodeVersionDrift, path, "schemaMatcher.ref does not name the contract $id")
			}
			if plan.SchemaMatcher.Digest != c.Digest {
				f.Error(CodeDigestMismatch, path, "schemaMatcher.digest does not match the contract file")
			}
		}

		needs := map[string]bool{}
		for _, r := range plan.Rules {
			mm := ruleIDRe.FindStringSubmatch(r.ID)
			if mm == nil {
				f.AddRule(CodeRuleID, SeverityError, path,
					"rule id "+r.ID+" is not (SYN|SEM|STA)-CCC-NNN", r.ID)
				continue
			}
			if want, ok := tierOfPrefix[mm[1]]; ok && r.Tier != want {
				f.AddRule(CodePlanDrift, SeverityError, path,
					fmt.Sprintf("%s is tier %d by its prefix, the plan says %d", r.ID, want, r.Tier), r.ID)
			}

			src, ok := rules[r.ID]
			if !ok {
				f.AddRule(CodePlanDrift, SeverityError, path,
					"rule "+r.ID+" exists in no pack", r.ID)
				continue
			}
			if src.Assert != r.Assert {
				f.AddRule(CodePlanDrift, SeverityError, path,
					r.ID+" assert differs from "+src.PackName, r.ID)
			}
			if src.Severity != r.Severity {
				f.AddRule(CodePlanDrift, SeverityError, path,
					r.ID+" severity differs from "+src.PackName, r.ID)
			}
			if src.Dimension != r.Dimension {
				f.AddRule(CodePlanDrift, SeverityError, path,
					r.ID+" dimension differs from "+src.PackName, r.ID)
			}
			if src.When != r.When {
				f.AddRule(CodePlanDrift, SeverityError, path,
					r.ID+" when differs from "+src.PackName, r.ID)
			}
			if !sameSet(src.Needs, r.Needs) {
				f.AddRule(CodePlanDrift, SeverityError, path,
					r.ID+" needs differ from "+src.PackName, r.ID)
			}
			for _, n := range r.Needs {
				needs[n] = true
			}
		}

		declared := map[string]bool{}
		for _, p := range plan.Providers {
			declared[p] = true
		}
		for n := range needs {
			if !declared[n] {
				f.Error(CodePlanDrift, path, "a rule needs "+n+" but the plan does not declare it")
			}
		}
		for p := range declared {
			if !needs[p] && p != "valueset" {
				f.Error(CodePlanDrift, path, "declares provider "+p+" that no rule needs")
			}
		}

		for _, rx := range plan.Relax {
			if !bundleRelax[rx.Rule] {
				f.Error(CodeRelaxUnknown, path,
					"relax for "+rx.Rule+" is not carried into the bundle manifest")
			}
		}
	}
}
