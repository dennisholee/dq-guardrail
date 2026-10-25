package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// publishForTest builds a layout in a temp dir and returns the directory and the layer bytes.
func publishForTest(t *testing.T) (string, []byte) {
	t.Helper()
	var archive bytes.Buffer
	if err := WriteArchive(&archive, map[string][]byte{
		"bundle.yaml": []byte("apiVersion: dq/v1\nkind: Bundle\n"),
	}, 0); err != nil {
		t.Fatalf("archive: %v", err)
	}

	dir := t.TempDir()
	bundlePath := filepath.Join(dir, "bundle.tar.gz")
	if err := os.WriteFile(bundlePath, archive.Bytes(), 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	layout := filepath.Join(dir, "oci")
	published, err := Publish(bundlePath, layout, time.Time{})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.ManifestDigest == "" || published.LayerDigest == "" {
		t.Fatal("publish reported no digests")
	}
	return layout, archive.Bytes()
}

// TestVerifyLayoutAcceptsWhatPublishWrote: the layout a consumer receives verifies, and the layer
// it yields is byte-identical to the bundle that was published.
func TestVerifyLayoutAcceptsWhatPublishWrote(t *testing.T) {
	layout, wantLayer := publishForTest(t)

	v, err := VerifyLayout(layout)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !bytes.Equal(v.Layer, wantLayer) {
		t.Fatalf("verified layer is not the published bundle: %d vs %d bytes", len(v.Layer), len(wantLayer))
	}
	if v.ManifestDigest == "" || v.LayerDigest == "" {
		t.Fatal("verify reported no digests")
	}
	if v.Blobs != 3 {
		t.Errorf("verified %d blobs, expected manifest, config and layer", v.Blobs)
	}
}

// TestVerifyLayoutRejectsACorruptedBlob is the case the check exists for: the bytes changed and
// the digest did not.
func TestVerifyLayoutRejectsACorruptedBlob(t *testing.T) {
	layout, _ := publishForTest(t)

	v, err := VerifyLayout(layout)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	path := filepath.Join(layout, "blobs", "sha256", trimDigest(v.LayerDigest))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("corrupt blob: %v", err)
	}

	if _, err := VerifyLayout(layout); err == nil {
		t.Fatal("a corrupted layer blob verified")
	} else if !strings.Contains(err.Error(), v.LayerDigest) {
		t.Errorf("the error should name the digest that failed, got: %v", err)
	}
}

// TestVerifyLayoutRejectsATamperedIndex: a consumer who is pointed at a different manifest must
// fail, not silently verify something else.
func TestVerifyLayoutRejectsATamperedIndex(t *testing.T) {
	layout, _ := publishForTest(t)

	// Point the index at a well-formed digest that no blob answers to.
	indexPath := filepath.Join(layout, "index.json")
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	tampered := strings.ReplaceAll(string(raw),
		"sha256:", "sha256:0")
	if tampered == string(raw) {
		t.Fatal("the tamper did not change the index")
	}
	if err := os.WriteFile(indexPath, []byte(tampered), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	if _, err := VerifyLayout(layout); err == nil {
		t.Fatal("an index pointing at a missing blob verified")
	}
}

// TestVerifyLayoutRejectsAMissingHeader: a directory that is not a layout is not a layout.
func TestVerifyLayoutRejectsAMissingHeader(t *testing.T) {
	layout, _ := publishForTest(t)
	if err := os.Remove(filepath.Join(layout, "oci-layout")); err != nil {
		t.Fatalf("remove header: %v", err)
	}
	if _, err := VerifyLayout(layout); err == nil {
		t.Fatal("a directory without oci-layout verified")
	}
}
