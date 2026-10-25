package engine

import (
	"sort"
	"strings"
)

// validateVerdicts checks that a captured verdict could have been produced by the packs in
// this tree: the rule exists, the wording matches, and the digest names the owning pack.
func validateVerdicts(m *Model, f *Findings) {
	rules := m.RuleIndex()
	packs := m.PackByName()

	for _, v := range m.Verdicts {
		if v.Status != 422 || !strings.HasPrefix(v.Type, "https://") {
			f.Error(CodeVerdictInvalid, v.Path, "not an RFC 9457 problem document with status 422")
		}
		if len(v.Violations) == 0 {
			f.Error(CodeVerdictInvalid, v.Path, "a problem document must carry at least one violation")
		}

		// The envelope summarises the decision, so its digest must be one of the packs that
		// actually produced a violation below it. A summary nobody checks is a summary that
		// drifts.
		if v.PackDigest != "" && len(v.Violations) > 0 {
			matches := false
			for _, x := range v.Violations {
				if x.PackDigest == v.PackDigest {
					matches = true
					break
				}
			}
			if !matches {
				f.Error(CodeDigestMismatch, v.Path,
					"the envelope packDigest is not the digest of any pack named in its violations")
			}
		}

		for _, x := range v.Violations {
			src, ok := rules[x.RuleID]
			if !ok {
				f.AddRule(CodeVerdictInvalid, SeverityError, v.Path,
					"rule "+x.RuleID+" exists in no pack", x.RuleID)
				continue
			}
			if x.Severity != src.Severity {
				f.AddRule(CodeVerdictInvalid, SeverityError, v.Path,
					x.RuleID+" severity "+x.Severity+" != pack "+src.Severity, x.RuleID)
			}
			if x.Dimension != src.Dimension {
				f.AddRule(CodeVerdictInvalid, SeverityError, v.Path,
					x.RuleID+" dimension differs from "+src.PackName, x.RuleID)
			}
			if x.Message != src.Message {
				f.AddRule(CodeVerdictInvalid, SeverityError, v.Path,
					x.RuleID+" message differs from "+src.PackName, x.RuleID)
			}
			if p, ok := packs[src.PackName]; ok && x.PackDigest != p.Digest {
				f.AddRule(CodeDigestMismatch, SeverityError, v.Path,
					x.RuleID+" packDigest is not the digest of "+src.PackName, x.RuleID)
			}
			if !jsonPathRe.MatchString(x.Path) {
				f.AddRule(CodeVerdictInvalid, SeverityError, v.Path,
					"path "+x.Path+" is not a JSON pointer path", x.RuleID)
			}
			if !ulidRe.MatchString(x.CorrelationID) {
				f.AddRule(CodeVerdictInvalid, SeverityError, v.Path,
					"correlationId is not a 26-character ULID", x.RuleID)
			}
			if v.CorrelationID != "" && x.CorrelationID != "" && v.CorrelationID != x.CorrelationID {
				f.Error(CodeVerdictInvalid, v.Path,
					"the envelope correlationId differs from the violation's")
			}
			if c, ok := m.Contracts[x.Object]; ok && x.ObjectVersion != c.Version {
				f.AddRule(CodeVersionDrift, SeverityError, v.Path,
					x.RuleID+" objectVersion "+x.ObjectVersion+" != contract "+c.Version, x.RuleID)
			}
		}
	}
}

// Coverage is the declared-versus-enforced view a steward needs: a threshold with no rule
// covering its dimension is a promise nobody keeps.
type Coverage struct {
	Object   string   `json:"object"`
	Declared []string `json:"declared"`
	Covered  []string `json:"covered"`
	Gaps     []string `json:"gaps"`
}

// CoverageReport computes the view above for every object that declares thresholds.
func CoverageReport(m *Model) []Coverage {
	byPack := m.RulesByPack()
	out := []Coverage{}

	objects := make([]string, 0, len(m.Metas))
	for obj := range m.Metas {
		objects = append(objects, obj)
	}
	sort.Strings(objects)

	for _, obj := range objects {
		meta := m.Metas[obj]
		declared := make([]string, 0, len(meta.Thresholds))
		for dim := range meta.Thresholds {
			declared = append(declared, dim)
		}
		sort.Strings(declared)

		coveredSet := map[string]bool{}
		for _, r := range byPack["object."+obj] {
			coveredSet[r.Dimension] = true
		}
		covered := make([]string, 0, len(coveredSet))
		for dim := range coveredSet {
			covered = append(covered, dim)
		}
		sort.Strings(covered)

		gaps := []string{}
		for _, dim := range declared {
			if !coveredSet[dim] {
				gaps = append(gaps, dim)
			}
		}
		out = append(out, Coverage{Object: obj, Declared: declared, Covered: covered, Gaps: gaps})
	}
	return out
}

// validateCoverage reports each gap as an informational finding.
func validateCoverage(m *Model, f *Findings) {
	for _, c := range CoverageReport(m) {
		for _, gap := range c.Gaps {
			f.Add(CodeCoverageGap, SeverityInfo, m.Metas[c.Object].Path,
				c.Object+" declares a "+gap+" threshold, but no rule covers that dimension")
		}
	}
}
