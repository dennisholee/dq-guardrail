package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseError records a document that could not be decoded. Loading never aborts: a tree
// with one broken file must still report everything else that is wrong with it.
type ParseError struct {
	Path    string
	Message string
}

// APIOperation is the part of an OpenAPI operation this tool needs.
type APIOperation struct {
	Intent        string         `yaml:"x-dq-intent"`
	Object        string         `yaml:"x-dq-object"`
	ObjectVersion string         `yaml:"x-dq-object-version"`
	Binding       string         `yaml:"x-dq-binding"`
	OperationID   string         `yaml:"operationId"`
	RequestBody   map[string]any `yaml:"requestBody"`
	Responses     map[string]any `yaml:"responses"`
}

// APIDoc is the OpenAPI surface, reduced to paths and their dq declarations.
type APIDoc struct {
	OpenAPI    string                             `yaml:"openapi"`
	Paths      map[string]map[string]APIOperation `yaml:"paths"`
	Components struct {
		Schemas map[string]any `yaml:"schemas"`
	} `yaml:"components"`
	Path string `yaml:"-"`
	Raw  any    `yaml:"-"`
}

// Model is one loaded artefact tree.
type Model struct {
	Root        string
	Docs        []Doc
	ParseErrors []ParseError

	Registry  *IntentRegistry
	ValueSets map[string]*ValueSet
	Contracts map[string]*Contract
	Metas     map[string]*Meta

	ObjectPacks []*Pack
	IntentPacks []*Pack

	Plans    map[string]*Plan
	Bundle   *Bundle
	Bindings []*Binding
	Verdicts []*Verdict

	APIs []*APIDoc
}

// Load walks root and decodes every document it recognises. A directory may legitimately
// be absent; a document that fails to decode never aborts the walk.
func Load(root string) (*Model, error) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	m := &Model{
		Root:      root,
		ValueSets: map[string]*ValueSet{},
		Contracts: map[string]*Contract{},
		Metas:     map[string]*Meta{},
		Plans:     map[string]*Plan{},
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			// Hidden directories are skipped, but the root itself is walked even when its own
			// name starts with a dot — otherwise a checkout into .tmp would load nothing.
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		m.load(path, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}

// load decodes one document twice: once into a generic tree for schema validation and
// temporal inspection, once into the typed view this tool reasons about.
func (m *Model) load(path, rel string) {
	data, err := os.ReadFile(path)
	if err != nil {
		m.ParseErrors = append(m.ParseErrors, ParseError{Path: rel, Message: err.Error()})
		return
	}

	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		m.ParseErrors = append(m.ParseErrors, ParseError{Path: rel, Message: err.Error()})
		return
	}
	m.Docs = append(m.Docs, Doc{Path: rel, Raw: raw})

	digest := digestOf(data)
	base := filepath.Base(rel)
	dir := filepath.ToSlash(filepath.Dir(rel))

	decode := func(target any) bool {
		if err := yaml.Unmarshal(data, target); err != nil {
			m.ParseErrors = append(m.ParseErrors, ParseError{Path: rel, Message: err.Error()})
			return false
		}
		return true
	}

	switch {
	case rel == "catalog/intents.yaml":
		r := &IntentRegistry{}
		if decode(r) {
			r.Path = rel
			m.Registry = r
		}

	case dir == "catalog/value-sets":
		v := &ValueSet{}
		if decode(v) {
			v.Path = rel
			m.ValueSets[v.ValueSet] = v
		}

	case dir == "catalog/business-objects" && strings.HasSuffix(base, ".meta.yaml"):
		meta := &Meta{}
		if decode(meta) {
			meta.Path = rel
			m.Metas[meta.Object] = meta
		}

	case dir == "catalog/business-objects" && strings.HasSuffix(base, ".json"):
		c := &Contract{}
		if decode(c) {
			c.Path, c.Digest, c.Raw = rel, digest, raw
			if mm := contractIDRe.FindStringSubmatch(c.ID); mm != nil {
				c.Object, c.Version = mm[1], mm[2]
			}
			m.Contracts[strings.TrimSuffix(base, ".json")] = c
		}

	case dir == "rules/object":
		p := &Pack{}
		if decode(p) {
			p.Path, p.Digest = rel, digest
			m.ObjectPacks = append(m.ObjectPacks, p)
		}

	case dir == "rules/intent":
		p := &Pack{}
		if decode(p) {
			p.Path, p.Digest = rel, digest
			m.IntentPacks = append(m.IntentPacks, p)
		}

	case dir == "evidence/plans":
		p := &Plan{}
		if decode(p) {
			p.Path, p.Digest = rel, digest
			m.Plans[rel] = p
		}

	case rel == "evidence/bundle.yaml":
		b := &Bundle{}
		if decode(b) {
			b.Path, b.Digest = rel, digest
			m.Bundle = b
		}

	case dir == "evidence/verdicts":
		v := &Verdict{}
		if decode(v) {
			v.Path = rel
			m.Verdicts = append(m.Verdicts, v)
		}

	case dir == "bindings":
		b := &Binding{}
		if decode(b) {
			b.Path = rel
			m.Bindings = append(m.Bindings, b)
		}

	case dir == "spec/openapi":
		a := &APIDoc{}
		if decode(a) {
			a.Path, a.Raw = rel, raw
			m.APIs = append(m.APIs, a)
		}
	}
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// contractIDRe is the canonical contract URI shape: .../schemas/<object>/<major.minor.patch>.
var contractIDRe = regexp.MustCompile(`^https://registry\.internal/dq/schemas/([a-z][a-z0-9]*)/(\d+\.\d+\.\d+)$`)
