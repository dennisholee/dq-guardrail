package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// SchemaRegistry holds the compiled formats from catalog/schemas. Documents are validated
// against these rather than against hand-written checks: a second implementation of a
// format is a second definition of it.
type SchemaRegistry struct {
	compiler *jsonschema.Compiler
	byName   map[string]*jsonschema.Schema
	idByName map[string]string
	cache    map[string]*jsonschema.Schema
}

// LoadSchemaRegistry compiles every *.schema.yaml in dir. Each is registered under its
// absolute $id, which is also how sibling $refs between those files resolve — because the
// files share a directory and therefore a URI prefix.
func LoadSchemaRegistry(dir string) (*SchemaRegistry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("schema registry %s: %w", dir, err)
	}

	type pending struct{ name, id string }
	var list []pending
	compiler := jsonschema.NewCompiler()

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema.yaml") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, e.Name()))
		if readErr != nil {
			return nil, readErr
		}
		var doc any
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		doc = normalise(doc)
		m, _ := doc.(map[string]any)
		id, _ := m["$id"].(string)
		if id == "" {
			return nil, fmt.Errorf("%s declares no $id", e.Name())
		}
		if err := compiler.AddResource(id, doc); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		list = append(list, pending{name: e.Name(), id: id})
	}

	reg := &SchemaRegistry{
		compiler: compiler,
		byName:   map[string]*jsonschema.Schema{},
		idByName: map[string]string{},
		cache:    map[string]*jsonschema.Schema{},
	}
	for _, p := range list {
		s, err := compiler.Compile(p.id)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.name, err)
		}
		reg.byName[p.name] = s
		reg.idByName[p.name] = p.id
	}
	if len(reg.byName) == 0 {
		return nil, fmt.Errorf("no schemas found in %s", dir)
	}
	return reg, nil
}

// Names lists the compiled formats.
func (r *SchemaRegistry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	return out
}

// Validate checks doc against the named format. The name may carry a JSON pointer
// fragment — "violation.schema.yaml#/$defs/problem" — because some formats are a single
// definition inside a larger document, not the document's root.
func (r *SchemaRegistry) Validate(name string, doc any) error {
	key := name
	base := name
	if i := strings.IndexByte(name, '#'); i >= 0 {
		base = name[:i]
	}

	id, ok := r.idByName[base]
	if !ok {
		return fmt.Errorf("no schema named %q", base)
	}

	s, cached := r.cache[key]
	if !cached {
		if strings.Contains(name, "#") {
			loc := id + name[len(base):]
			compiled, err := r.compiler.Compile(loc)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			s = compiled
		} else {
			s = r.byName[base]
		}
		r.cache[key] = s
	}

	v, err := jsonValue(doc)
	if err != nil {
		return err
	}
	return s.Validate(v)
}

// SchemaForPath maps a document's location in the tree to the format that governs it.
// A path with no governing format returns "" — the caller decides whether that is a gap.
func SchemaForPath(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	base := filepath.Base(rel)

	switch {
	case rel == "catalog/intents.yaml":
		return "intent-registry.schema.yaml"
	case dir == "catalog/value-sets":
		return "value-set.schema.yaml"
	case dir == "catalog/business-objects" && strings.HasSuffix(base, ".meta.yaml"):
		return "object-metadata.schema.yaml"
	case dir == "catalog/business-objects" && strings.HasSuffix(base, ".json"):
		return "object-contract.profile.schema.yaml"
	case dir == "rules/object":
		return "quality-pack.schema.yaml"
	case dir == "rules/intent":
		return "intent-pack.schema.yaml"
	case dir == "bindings":
		return "quality-binding.schema.yaml"
	case rel == "evidence/bundle.yaml":
		return "bundle-manifest.schema.yaml"
	case dir == "evidence/plans":
		return "plan.schema.yaml"
	case dir == "evidence/verdicts":
		return "violation.schema.yaml#/$defs/problem"
	case strings.HasSuffix(base, ".manifest.json"):
		return "file-manifest.schema.yaml"
	}
	return ""
}

// jsonValue converts a decoded YAML tree into the JSON value model the validator expects,
// so that numbers, nulls and maps match what a JSON consumer would see.
func jsonValue(doc any) (any, error) {
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// normalise converts any map[any]any produced by YAML decoding into map[string]any.
func normalise(node any) any {
	switch v := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = normalise(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[fmt.Sprint(k)] = normalise(val)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = normalise(val)
		}
		return out
	default:
		return node
	}
}
