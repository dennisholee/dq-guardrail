package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	sampleDir = "../examples/party-model"
	schemaDir = "../catalog/schemas"
)

func loadRegistry(t *testing.T) *SchemaRegistry {
	t.Helper()
	reg, err := LoadSchemaRegistry(schemaDir)
	if err != nil {
		t.Fatalf("schema registry: %v", err)
	}
	return reg
}

// copySample gives each case a private copy of the worked sample, so a mutation cannot
// leak into the next case or back into the repository.
func copySample(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(sampleDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(sampleDir, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy sample: %v", err)
	}
	return dst
}

func edit(t *testing.T, dir, rel, old, replacement string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	text := string(data)
	if !strings.Contains(text, old) {
		t.Fatalf("%s does not contain %q", rel, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(text, old, replacement, 1)), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func deleteLines(t *testing.T, dir, rel, contains string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, contains) {
			kept = append(kept, line)
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func replaceAll(t *testing.T, dir, rel, old, replacement string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	text := string(data)
	if !strings.Contains(text, old) {
		t.Fatalf("%s does not contain %q", rel, old)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(text, old, replacement)), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// corruptDigestAfter flips one hex character of the first sha256 digest that follows marker.
// Digests legitimately change when the tree changes, so a test that pinned one would rot.
func corruptDigestAfter(t *testing.T, dir, rel, marker string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	text := string(data)

	at := strings.Index(text, marker)
	if at < 0 {
		t.Fatalf("%s does not contain marker %q", rel, marker)
	}
	rel2 := strings.Index(text[at:], "sha256:")
	if rel2 < 0 {
		t.Fatalf("no digest after %q in %s", marker, rel)
	}
	pos := at + rel2 + len("sha256:")
	if pos >= len(text) {
		t.Fatalf("digest after %q is truncated in %s", marker, rel)
	}
	replacement := byte('0')
	if text[pos] == '0' {
		replacement = '1'
	}
	edited := text[:pos] + string(replacement) + text[pos+1:]
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func codesOf(f *Findings) []string {
	out := []string{}
	for _, x := range f.Items() {
		out = append(out, x.Code)
	}
	return out
}

func hasCode(f *Findings, code string) bool {
	for _, x := range f.Items() {
		if x.Code == code {
			return true
		}
	}
	return false
}

func hasRuleFinding(f *Findings, code, ruleID string) bool {
	for _, x := range f.Items() {
		if x.Code == code && x.RuleID != nil && *x.RuleID == ruleID {
			return true
		}
	}
	return false
}
