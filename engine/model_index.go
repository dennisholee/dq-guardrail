package engine

import (
	"os"
	"path/filepath"
)

// DigestFile returns the digest a manifest would carry for the file at rel.
func (m *Model) DigestFile(rel string) (string, error) {
	data, err := os.ReadFile(filepath.Join(m.Root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	return digestOf(data), nil
}

// AllPacks returns object packs then intent packs, in load order.
func (m *Model) AllPacks() []*Pack {
	out := make([]*Pack, 0, len(m.ObjectPacks)+len(m.IntentPacks))
	out = append(out, m.ObjectPacks...)
	return append(out, m.IntentPacks...)
}

// PackByName indexes packs by metadata.name.
func (m *Model) PackByName() map[string]*Pack {
	out := map[string]*Pack{}
	for _, p := range m.AllPacks() {
		if p.Metadata.Name != "" {
			out[p.Metadata.Name] = p
		}
	}
	return out
}

// RuleIndex indexes every authored rule by id.
func (m *Model) RuleIndex() map[string]*Rule {
	out := map[string]*Rule{}
	for _, p := range m.AllPacks() {
		for _, r := range p.Rules() {
			rule := r
			out[rule.ID] = &rule
		}
	}
	return out
}

// RulesByPack groups authored rules by pack name.
func (m *Model) RulesByPack() map[string][]Rule {
	out := map[string][]Rule{}
	for _, p := range m.AllPacks() {
		out[p.Metadata.Name] = p.Rules()
	}
	return out
}

// IntentByCode indexes the governed intents by their three-letter code.
func (m *Model) IntentByCode() map[string]*Intent {
	out := map[string]*Intent{}
	if m.Registry == nil {
		return out
	}
	for i := range m.Registry.Intents {
		in := m.Registry.Intents[i]
		out[in.Code] = &in
	}
	return out
}

// IntentByName indexes the governed intents by name.
func (m *Model) IntentByName() map[string]*Intent {
	out := map[string]*Intent{}
	if m.Registry == nil {
		return out
	}
	for i := range m.Registry.Intents {
		in := m.Registry.Intents[i]
		out[in.Name] = &in
	}
	return out
}

// PlanFor returns the compiled plan for a channel, intent and object, or nil.
func (m *Model) PlanFor(channel, intent, object string) *Plan {
	for _, p := range m.Plans {
		if p.Channel == channel && p.Intent == intent && p.Object == object {
			return p
		}
	}
	return nil
}

// CollectRefs walks a decoded tree and returns every "$ref" string it contains.
func CollectRefs(node any) []string {
	out := []string{}
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			for k, val := range v {
				if s, ok := val.(string); ok && k == "$ref" {
					out = append(out, s)
					continue
				}
				walk(val)
			}
		case []any:
			for _, i := range v {
				walk(i)
			}
		}
	}
	walk(node)
	return out
}

// WalkScalars visits every scalar in a decoded tree with its dotted path.
func WalkScalars(node any, fn func(path string, value any)) {
	var walk func(string, any)
	walk = func(p string, n any) {
		switch v := n.(type) {
		case map[string]any:
			for k, val := range v {
				walk(p+"."+k, val)
			}
		case []any:
			for i, val := range v {
				walk(p+"["+itoa(i)+"]", val)
			}
		default:
			fn(p, n)
		}
	}
	walk("", node)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
