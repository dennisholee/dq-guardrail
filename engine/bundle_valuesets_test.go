package engine

import (
	"testing"
)

// TestValidateRecomputesValueSetDigests: since bundleFormat 2 the manifest claims a digest for
// every value set, so validate recomputes them. A check that cannot fail is not a check.
func TestValidateRecomputesValueSetDigests(t *testing.T) {
	dir := copySample(t)
	corruptDigestAfter(t, dir, "evidence/bundle.yaml", "valueSets:")

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	reg := loadRegistry(t)
	f := ValidateTree(m, reg)
	if !hasCode(f, CodeDigestMismatch) {
		t.Fatalf("want %s for a stale value-set digest, got %v", CodeDigestMismatch, codesOf(f))
	}
}

// TestValidateRecomputesIntentsDigest: the intent registry is a claim too.
func TestValidateRecomputesIntentsDigest(t *testing.T) {
	dir := copySample(t)
	corruptDigestAfter(t, dir, "evidence/bundle.yaml", "intents:")

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	reg := loadRegistry(t)
	f := ValidateTree(m, reg)
	if !hasCode(f, CodeDigestMismatch) {
		t.Fatalf("want %s for a stale intents digest, got %v", CodeDigestMismatch, codesOf(f))
	}
}

// TestBundleCheckNoticesAStaleValueSetDigest: bundle --check compares the manifest against the
// tree, so the new claims are guarded the same way the old ones are.
func TestBundleCheckNoticesAStaleValueSetDigest(t *testing.T) {
	dir := copySample(t)
	corruptDigestAfter(t, dir, "evidence/bundle.yaml", "valueSets:")

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fresh, err := BuildManifest(m, BuiltFrom{
		Commit: m.BundleCommit(), Ref: m.BundleRef(), Epoch: m.BundleEpoch(), Dqctl: m.BundleDqctl(),
	})
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	problems := CompareManifests(fresh, m.Bundle)
	if len(problems) == 0 {
		t.Fatal("a stale value-set digest survived bundle --check")
	}
	found := false
	for _, p := range problems {
		if p == "valueSets differ from the tree" {
			found = true
		}
	}
	if !found {
		t.Errorf("the comparison did not name the value sets: %v", problems)
	}
}
