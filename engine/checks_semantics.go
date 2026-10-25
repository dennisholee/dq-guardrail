package engine

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// validateVersions enforces that a meta file and a pack name the same contract version as
// the contract itself. Drift here is silent otherwise: a pack would validate yesterday's
// shape against today's payload.
func validateVersions(m *Model, f *Findings) {
	for obj, meta := range m.Metas {
		c, ok := m.Contracts[obj]
		if !ok {
			f.Error(CodeChainUnknown, meta.Path, "no canonical contract for object "+obj)
			continue
		}
		if meta.Version != c.Version {
			f.Error(CodeVersionDrift, meta.Path,
				fmt.Sprintf("version %s != contract %s", meta.Version, c.Version))
		}
	}

	for _, p := range m.ObjectPacks {
		obj := p.Spec.Object.Name
		c, ok := m.Contracts[obj]
		if !ok {
			f.Error(CodeChainUnknown, p.Path, "governs unknown object "+obj)
			continue
		}
		if p.Spec.Object.SchemaVersion != c.Version {
			f.Error(CodeVersionDrift, p.Path,
				fmt.Sprintf("schemaVersion %s != contract %s", p.Spec.Object.SchemaVersion, c.Version))
		}
		if p.Metadata.Name != "object."+obj {
			f.Error(CodeVocabulary, p.Path, "metadata.name must be object."+obj)
		}
		if p.Spec.Syntax == nil {
			f.Error(CodeVocabulary, p.Path, "an object pack requires a syntax block naming fromSchema")
			continue
		}
		ref := p.Spec.Syntax.FromSchema.Ref
		if mm := contractIDRe.FindStringSubmatch(ref); mm == nil {
			f.Error(CodeRefUnresolved, p.Path, "fromSchema.ref "+ref+" is not a contract URI")
		} else if target, ok := m.Contracts[mm[1]]; !ok || target.Version != mm[2] {
			f.Error(CodeRefUnresolved, p.Path, "fromSchema.ref "+ref+" does not resolve to a contract")
		}
	}
}

// validateIntents checks the governed vocabulary and everything that must agree with it.
func validateIntents(m *Model, f *Findings) {
	if m.Registry == nil {
		f.Error(CodeRegistryMissing, "catalog/intents.yaml",
			"the intent registry is required: intent rule IDs are named after it")
		return
	}

	codeOwner := map[string]string{}
	for _, in := range m.Registry.Intents {
		if !objectNameRe.MatchString(in.Name) {
			f.Error(CodeIntentUnknown, m.Registry.Path,
				fmt.Sprintf("intent name %q must be lowercase and hyphen-free", in.Name))
		}
		if len(in.Code) != 3 || in.Code != strings.ToUpper(in.Code) {
			f.Error(CodeIntentUnknown, m.Registry.Path,
				fmt.Sprintf("intent %q code %q must be three uppercase letters", in.Name, in.Code))
		}
		if prev, ok := codeOwner[in.Code]; ok {
			f.Error(CodeIntentUnknown, m.Registry.Path,
				fmt.Sprintf("code %q is used by both %q and %q", in.Code, prev, in.Name))
		} else {
			codeOwner[in.Code] = in.Name
		}
	}

	byName := m.IntentByName()
	for obj, meta := range m.Metas {
		for _, want := range meta.AllowedIntents {
			if _, ok := byName[want]; !ok {
				f.Error(CodeIntentUnknown, meta.Path,
					fmt.Sprintf("%s allows intent %q, which is not in the registry", obj, want))
			}
		}
	}

	rules := m.RuleIndex()
	for _, p := range m.IntentPacks {
		intent := p.Spec.Intent
		reg, known := byName[intent]
		if !known {
			f.Error(CodeIntentUnknown, p.Path, "intent "+intent+" is not in the registry")
		}
		if p.Metadata.Name != "intent."+intent {
			f.Error(CodeVocabulary, p.Path, "metadata.name must be intent."+intent)
		}
		if len(p.Spec.AppliesTo) == 0 {
			f.Error(CodeVocabulary, p.Path, "an intent pack must declare appliesTo")
		}
		for _, obj := range p.Spec.AppliesTo {
			if _, ok := m.Contracts[obj]; !ok {
				f.Error(CodeChainUnknown, p.Path, "appliesTo unknown object "+obj)
				continue
			}
			meta := m.Metas[obj]
			if meta == nil || !slices.Contains(meta.AllowedIntents, intent) {
				f.Error(CodeIntentUnknown, p.Path,
					fmt.Sprintf("appliesTo %s, but %s.meta.yaml does not allow %q", obj, obj, intent))
			}
		}

		for _, rx := range p.Spec.Relax {
			if _, ok := rules[rx.Rule]; !ok {
				f.Error(CodeRelaxUnknown, p.Path, "relax references unknown rule "+rx.Rule)
			}
			if len(rx.ApprovedBy) < 2 {
				f.Error(CodeRelaxUnknown, p.Path, "relax for "+rx.Rule+" needs at least two approvers")
			}
			exp, err := time.Parse(time.RFC3339, rx.ExpiresAt)
			if err != nil {
				f.Error(CodeRelaxExpired, p.Path,
					"relax for "+rx.Rule+" has an unparseable expiresAt: "+rx.ExpiresAt)
			} else if exp.Before(time.Now()) {
				f.Error(CodeRelaxExpired, p.Path,
					"relax for "+rx.Rule+" expired on "+rx.ExpiresAt)
			}
		}
		_ = reg
	}
}
