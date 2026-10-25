package engine

import (
	"bytes"
	"testing"
)

// buildOnce renders the archive for a tree into memory.
func buildOnce(t *testing.T, dir string, epoch int64) ([]byte, *Bundle) {
	t.Helper()
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	manifest, err := BuildManifest(m, BuiltFrom{
		Commit: "7c1f2ab9e4d5063c8a7b91d2e5f04a6c3b8d9e12",
		Ref:    "main",
		Epoch:  epoch,
		Dqctl:  "test",
	})
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	body, err := MarshalManifest(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	files, err := BundleFiles(m, body)
	if err != nil {
		t.Fatalf("files: %v", err)
	}
	var buf bytes.Buffer
	if err := WriteArchive(&buf, files, epoch); err != nil {
		t.Fatalf("archive: %v", err)
	}
	return buf.Bytes(), manifest
}

// TestBundleIsDeterministic is the property the audit story rests on: the same tree must
// produce the same bytes. Anything from the environment — clock, uid, file order, gzip
// header — has to be pinned, or every build looks like a change to whoever holds the pin.
func TestBundleIsDeterministic(t *testing.T) {
	a, _ := buildOnce(t, sampleDir, 1793001600)
	b, _ := buildOnce(t, sampleDir, 1793001600)
	if !bytes.Equal(a, b) {
		t.Fatalf("two builds differ: %s vs %s", DigestBytes(a), DigestBytes(b))
	}
	t.Logf("archive digest %s (%d bytes)", DigestBytes(a), len(a))

	c, _ := buildOnce(t, sampleDir, 1793001601)
	if bytes.Equal(a, c) {
		t.Fatal("a different SOURCE_DATE_EPOCH should change the digest")
	}
}

// TestBundleMatchesCommittedManifest is the falsifiability check: the manifest committed in
// evidence/ must describe the tree it sits in.
func TestBundleMatchesCommittedManifest(t *testing.T) {
	m, err := Load(sampleDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.Bundle == nil {
		t.Fatal("no committed manifest")
	}
	fresh, err := BuildManifest(m, BuiltFrom{
		Commit: m.BundleCommit(),
		Ref:    m.BundleRef(),
		Epoch:  m.BundleEpoch(),
		Dqctl:  m.BundleDqctl(),
	})
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if problems := CompareManifests(fresh, m.Bundle); len(problems) > 0 {
		t.Fatalf("committed manifest disagrees with the tree: %v", problems)
	}
}

// TestBundleDetectsDrift proves the comparison is not vacuous.
func TestBundleDetectsDrift(t *testing.T) {
	dir := copySample(t)
	edit(t, dir, "rules/object/party.pack.yaml", "severity: CRITICAL", "severity: MAJOR")

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fresh, err := BuildManifest(m, BuiltFrom{
		Commit: m.BundleCommit(),
		Ref:    m.BundleRef(),
		Epoch:  m.BundleEpoch(),
		Dqctl:  m.BundleDqctl(),
	})
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if problems := CompareManifests(fresh, m.Bundle); len(problems) == 0 {
		t.Fatal("a changed pack must produce a drift finding")
	}
}
