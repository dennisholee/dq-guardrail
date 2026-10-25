package engine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

// realBundleArchive builds the sample's bundle the way the CLI does.
func realBundleArchive(t *testing.T) []byte {
	t.Helper()
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	manifest, err := BuildManifest(m, BuiltFrom{Commit: "test", Ref: "main", Epoch: 1793001600, Dqctl: "test"})
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	raw, err := MarshalManifest(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	files, err := BundleFiles(m, raw)
	if err != nil {
		t.Fatalf("bundle files: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteArchive(&buf, files, 1793001600); err != nil {
		t.Fatalf("archive: %v", err)
	}
	return buf.Bytes()
}

// TestReadArchiveRoundTrips: what WriteArchive writes, ReadArchive reads, byte for byte.
func TestReadArchiveRoundTrips(t *testing.T) {
	want := map[string][]byte{
		"bundle.yaml":               []byte("bundleFormat: 1\n"),
		"catalog/value-sets/a.yaml": []byte("name: a\n"),
		"plans/p.json":              []byte("{}\n"),
	}
	var buf bytes.Buffer
	if err := WriteArchive(&buf, want, 0); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadArchive(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("read %d files, wrote %d", len(got), len(want))
	}
	for name, data := range want {
		if !bytes.Equal(got[name], data) {
			t.Errorf("%s did not round-trip", name)
		}
	}
}

// TestVerifyBundleArchiveAcceptsTheSampleBundle. Since bundleFormat 2 every artefact the bundle
// carries has a field in the manifest, so nothing should arrive undescribed.
func TestVerifyBundleArchiveAcceptsTheSampleBundle(t *testing.T) {
	c, err := VerifyBundleArchive(realBundleArchive(t))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.Objects != 3 || c.Packs == 0 || c.Plans == 0 {
		t.Fatalf("unexpected counts: %+v", c)
	}
	t.Logf("%d files, %d objects, %d packs, %d plans, %d undeclared",
		c.Files, c.Objects, c.Packs, c.Plans, len(c.Undeclared))
	if len(c.Undeclared) != 0 {
		t.Errorf("%d files are carried but undescribed: %v. bundleFormat 2 is supposed to make "+
			"this impossible, so either the manifest lost a section or a new artefact kind "+
			"arrived without one.", len(c.Undeclared), c.Undeclared)
	}
}

// TestVerifyBundleArchiveRejectsATamperedArtefact: the case the check exists for. A contract is
// edited inside the archive and the manifest still claims the old digest.
func TestVerifyBundleArchiveRejectsATamperedArtefact(t *testing.T) {
	files, err := ReadArchive(bytes.NewReader(realBundleArchive(t)))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var target string
	for name := range files {
		if strings.HasSuffix(name, "party.json") {
			target = name
			break
		}
	}
	if target == "" {
		t.Fatal("the bundle has no party.json")
	}
	files[target] = append(files[target], []byte(" ")...)

	var buf bytes.Buffer
	if err := WriteArchive(&buf, files, 1793001600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := VerifyBundleArchive(buf.Bytes()); err == nil {
		t.Fatal("an edited contract inside the archive verified")
	} else if !strings.Contains(err.Error(), "does not describe") && !strings.Contains(err.Error(), "no file in the archive") {
		t.Errorf("the error should name the mismatch, got: %v", err)
	}
}

// TestVerifyBundleArchiveRejectsASmuggledFile: content of a governed kind that the manifest does
// not describe is content the bundle will act on with no provenance behind it.
func TestVerifyBundleArchiveRejectsASmuggledFile(t *testing.T) {
	files, err := ReadArchive(bytes.NewReader(realBundleArchive(t)))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	files["rules/extra.pack.yaml"] = []byte("severity: MINOR\n")

	var buf bytes.Buffer
	if err := WriteArchive(&buf, files, 1793001600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := VerifyBundleArchive(buf.Bytes()); err == nil {
		t.Fatal("an undeclared pack inside the archive verified")
	}
}

// TestVerifyBundleArchiveRejectsARemovedArtefact: the manifest claims a digest, the archive no
// longer answers to it.
func TestVerifyBundleArchiveRejectsARemovedArtefact(t *testing.T) {
	files, err := ReadArchive(bytes.NewReader(realBundleArchive(t)))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for name := range files {
		if strings.HasPrefix(name, "plans/") {
			delete(files, name)
			break
		}
	}

	var buf bytes.Buffer
	if err := WriteArchive(&buf, files, 1793001600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if _, err := VerifyBundleArchive(buf.Bytes()); err == nil {
		t.Fatal("an archive missing a plan it declares verified")
	}
}

// TestReadArchiveRejectsUnsafeNames: names are input from whoever built the archive.
func TestReadArchiveRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"../escape.yaml", "/absolute.yaml", "a/../../b.yaml", "a\\b.yaml", "a//b.yaml"} {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		body := []byte("x")
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("header: %v", err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("body: %v", err)
		}
		if err := tw.Close(); err != nil {
			t.Fatalf("close tar: %v", err)
		}
		if err := gz.Close(); err != nil {
			t.Fatalf("close gzip: %v", err)
		}

		if _, err := ReadArchive(bytes.NewReader(buf.Bytes())); err == nil {
			t.Errorf("archive entry %q was accepted", name)
		}
	}
}
