package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPublishWritesValidLayout checks the layout is internally consistent: the index points
// at the manifest blob, and the layer blob is byte-for-byte the bundle archive.
func TestPublishWritesValidLayout(t *testing.T) {
	archive, _ := buildOnce(t, sampleDir, 1793001600)
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "dq-bundle.tar.gz")
	if err := os.WriteFile(bundlePath, archive, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	layout := filepath.Join(dir, "oci")
	published, err := Publish(bundlePath, layout, time.Unix(1793001600, 0))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	if published.LayerDigest != DigestBytes(archive) {
		t.Fatalf("layer digest %s is not the archive digest %s", published.LayerDigest, DigestBytes(archive))
	}

	raw, err := os.ReadFile(filepath.Join(layout, "index.json"))
	if err != nil {
		t.Fatalf("index.json: %v", err)
	}
	var idx Index
	if err := json.Unmarshal(raw, &idx); err != nil {
		t.Fatalf("index.json does not parse: %v", err)
	}
	if len(idx.Manifests) != 1 || idx.Manifests[0].Digest != published.ManifestDigest {
		t.Fatalf("index points at %v, published %s", idx.Manifests, published.ManifestDigest)
	}
	if idx.Manifests[0].Size != int64(published.ManifestBytes) {
		t.Fatalf("index size %d != manifest size %d", idx.Manifests[0].Size, published.ManifestBytes)
	}

	blob := filepath.Join(layout, "blobs", "sha256", trimDigest(published.ManifestDigest))
	manifestRaw, err := os.ReadFile(blob)
	if err != nil {
		t.Fatalf("manifest blob missing: %v", err)
	}
	if DigestBytes(manifestRaw) != published.ManifestDigest {
		t.Fatal("the manifest blob does not hash to its own digest")
	}
	var m Manifest
	if err := json.Unmarshal(manifestRaw, &m); err != nil {
		t.Fatalf("manifest does not parse: %v", err)
	}
	if len(m.Layers) != 1 || m.Layers[0].Digest != published.LayerDigest {
		t.Fatalf("manifest layer %v != %s", m.Layers, published.LayerDigest)
	}
	if _, err := os.Stat(filepath.Join(layout, "oci-layout")); err != nil {
		t.Fatalf("oci-layout missing: %v", err)
	}
}

// TestPublishIsDeterministic: the same bundle and timestamp must yield the same manifest
// digest, or a binding pin would change on every publish for no reason.
func TestPublishIsDeterministic(t *testing.T) {
	archive, _ := buildOnce(t, sampleDir, 1793001600)
	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "dq-bundle.tar.gz")
	if err := os.WriteFile(bundlePath, archive, 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	a, err := Publish(bundlePath, filepath.Join(dir, "a"), time.Unix(1793001600, 0))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	b, err := Publish(bundlePath, filepath.Join(dir, "b"), time.Unix(1793001600, 0))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if a.ManifestDigest != b.ManifestDigest {
		t.Fatalf("two publishes differ: %s vs %s", a.ManifestDigest, b.ManifestDigest)
	}

	// No timestamp means no creation annotation, which is what keeps local publishes
	// reproducible without an environment variable.
	c, err := Publish(bundlePath, filepath.Join(dir, "c"), time.Time{})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "c", "blobs", "sha256", trimDigest(c.ManifestDigest)))
	if err != nil {
		t.Fatalf("manifest blob: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if len(m.Annotations) != 0 {
		t.Fatalf("an unsupplied timestamp must not appear in the manifest: %v", m.Annotations)
	}
}
