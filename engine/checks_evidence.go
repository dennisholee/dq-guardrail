package engine

import (
	"fmt"
	"strings"
	"time"
)

// validateBundle recomputes every digest the manifest claims. A provenance file that
// cannot fail is decoration, so this check is the point of the evidence directory.
func validateBundle(m *Model, f *Findings) {
	b := m.Bundle
	if b == nil {
		return // a tree without evidence is legitimate; align still runs
	}

	for _, o := range b.Objects {
		c, ok := m.Contracts[o.Name]
		if !ok {
			f.Error(CodeChainUnknown, b.Path, "object "+o.Name+" has no contract")
			continue
		}
		if o.Version != c.Version {
			f.Error(CodeVersionDrift, b.Path,
				fmt.Sprintf("%s version %s != contract %s", o.Name, o.Version, c.Version))
		}
		if want, err := m.DigestFile(c.Path); err == nil && o.ContractDigest != want {
			f.Error(CodeDigestMismatch, b.Path,
				fmt.Sprintf("%s contractDigest does not match %s", o.Name, c.Path))
		}
		if meta := m.Metas[o.Name]; meta != nil {
			if o.Criticality != "" && meta.Criticality != "" && o.Criticality != meta.Criticality {
				f.Error(CodeVersionDrift, b.Path,
					fmt.Sprintf("%s criticality %s != meta %s", o.Name, o.Criticality, meta.Criticality))
			}
			if o.Sensitivity != "" && meta.Sensitivity != "" && o.Sensitivity != meta.Sensitivity {
				f.Error(CodeVersionDrift, b.Path,
					fmt.Sprintf("%s sensitivity %s != meta %s", o.Name, o.Sensitivity, meta.Sensitivity))
			}
		}
	}

	// Value sets and the intent registry are claims in the manifest like any other. Before
	// bundleFormat 2 they were not claims at all; now that they are, validate recomputes them.
	for _, vs := range b.ValueSets {
		want, err := m.DigestFile(vs.Path)
		if err != nil {
			f.Error(CodeChainUnknown, b.Path, "value set "+vs.Name+" names "+vs.Path+", which does not exist")
			continue
		}
		if vs.Digest != want {
			f.Error(CodeDigestMismatch, b.Path,
				fmt.Sprintf("value set %s digest does not match %s", vs.Name, vs.Path))
		}
	}
	if b.Intents != nil {
		if want, err := m.DigestFile(b.Intents.Path); err != nil {
			f.Error(CodeChainUnknown, b.Path, "intents names "+b.Intents.Path+", which does not exist")
		} else if b.Intents.Digest != want {
			f.Error(CodeDigestMismatch, b.Path,
				"intents digest does not match "+b.Intents.Path)
		}
	}

	packByName := m.PackByName()
	for _, bp := range b.Packs {
		p, ok := packByName[bp.Name]
		if !ok {
			f.Error(CodeChainUnknown, b.Path, "pack "+bp.Name+" does not exist")
			continue
		}
		if bp.Version != p.Metadata.Version {
			f.Error(CodeVersionDrift, b.Path,
				fmt.Sprintf("pack %s version %s != %s", bp.Name, bp.Version, p.Metadata.Version))
		}
		if bp.Digest != p.Digest {
			f.Error(CodeDigestMismatch, b.Path, "pack "+bp.Name+" digest does not match "+p.Path)
		}
		if bp.RuleCount != nil && *bp.RuleCount != len(p.Rules()) {
			f.Error(CodeVersionDrift, b.Path,
				fmt.Sprintf("pack %s ruleCount %d != %d", bp.Name, *bp.RuleCount, len(p.Rules())))
		}
	}

	listed := map[string]bool{}
	for _, bp := range b.Plans {
		// The manifest's plan path is relative to the bundle root, which is evidence/ here.
		key := "evidence/" + bp.Plan
		listed[key] = true
		plan, ok := m.Plans[key]
		if !ok {
			f.Error(CodeChainUnknown, b.Path, "plan "+bp.Plan+" does not exist")
			continue
		}
		if bp.Digest != plan.Digest {
			f.Error(CodeDigestMismatch, b.Path, "plan "+bp.Plan+" digest does not match the file")
		}
		if bp.Channel != plan.Channel || bp.Intent != plan.Intent ||
			bp.Object != plan.Object || bp.ObjectVersion != plan.ObjectVersion {
			f.Error(CodePlanDrift, b.Path,
				"the bundle entry for "+bp.Plan+" disagrees with the plan's own header")
		}
	}
	for path := range m.Plans {
		if !listed[path] {
			f.Error(CodeChainUnknown, path, "this plan is not listed in evidence/bundle.yaml")
		}
	}

	rules := m.RuleIndex()
	for _, rx := range b.Relax {
		if _, ok := rules[rx.Rule]; !ok {
			f.Error(CodeRelaxUnknown, b.Path, "bundle relax names unknown rule "+rx.Rule)
		}
	}
}

// validateBindings checks the deployment pin against the evidence it points at.
func validateBindings(m *Model, f *Findings) {
	apiPaths := map[string]bool{}
	for _, a := range m.APIs {
		for p := range a.Paths {
			apiPaths[p] = true
		}
	}
	rules := m.RuleIndex()

	for _, b := range m.Bindings {
		if mm := bundlePinRe.FindStringSubmatch(b.Spec.Bundle); mm == nil {
			f.Error(CodeBindingPath, b.Path, "the bundle reference is not pinned by digest")
		} else if m.Bundle != nil && mm[1] != m.Bundle.Digest {
			f.Error(CodeDigestMismatch, b.Path,
				"the pinned bundle digest does not match evidence/bundle.yaml")
		}

		if b.Spec.Mode == "ENFORCE" && b.Spec.FallbackDigest == "" {
			f.Error(CodeBindingPath, b.Path, "mode ENFORCE requires a fallbackDigest")
		}
		if b.Spec.FallbackDigest != "" && !digestRe.MatchString(b.Spec.FallbackDigest) {
			f.Error(CodeBindingPath, b.Path, "fallbackDigest is not a sha256 digest")
		}

		http := b.Spec.Channels.HTTP
		if http == nil {
			continue
		}
		for _, p := range http.Paths {
			if !apiPaths[p] {
				f.Error(CodeBindingPath, b.Path, "path "+p+" is not in the API contract")
			}
			intent, object := addressKey(p)
			if !m.hasPlan("http", intent, object) {
				f.Error(CodeBindingPlan, b.Path,
					fmt.Sprintf("path %s has no compiled plan at evidence/plans/http.%s.%s.plan.json", p, intent, object))
			}
		}

		for _, s := range b.Spec.Suppress {
			if _, ok := rules[s.Rule]; !ok {
				f.Error(CodeSuppressUnknown, b.Path,
					"suppress names "+s.Rule+", which exists in no pack")
			}
			until, err := time.Parse(time.RFC3339, s.Until)
			if err != nil {
				f.Error(CodeSuppressWindow, b.Path,
					"the suppression of "+s.Rule+" has an unparseable until: "+s.Until)
				continue
			}
			if until.After(time.Now().Add(30 * 24 * time.Hour)) {
				f.Error(CodeSuppressWindow, b.Path,
					"the suppression of "+s.Rule+" runs beyond the 30-day window")
			}
		}
	}
}

// addressKey splits /<intent>-<object> on the first hyphen and rejoins the remainder, which
// is why neither part may itself contain a hyphen.
func addressKey(path string) (intent, object string) {
	key := strings.TrimPrefix(path, "/")
	parts := strings.Split(key, "-")
	if len(parts) < 2 {
		return key, ""
	}
	return parts[0], strings.Join(parts[1:], "")
}

func (m *Model) hasPlan(channel, intent, object string) bool {
	want := "evidence/plans/" + channel + "." + intent + "." + object + ".plan.json"
	_, ok := m.Plans[want]
	return ok
}
