package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Verified is what a layout was proven to contain.
//
// The layer bytes are only returned after every digest in the chain has been checked against the
// bytes on disk, so a caller cannot reach unverified content by forgetting a step.
type Verified struct {
	Dir            string
	ManifestDigest string
	LayerDigest    string
	Layer          []byte
	Blobs          int
}

// VerifyLayout reads an OCI image layout from disk and checks its content-addressing: the header
// says what it is, the index points at a manifest, and every descriptor hashes to the bytes it
// names.
//
// This is the consumer's half of the claim the whole toolchain rests on. A binding pins a
// manifest digest; publishing writes a layout; but nothing else in this repository ever checks
// that the bytes a consumer received are the bytes that digest names. A digest nobody verifies is
// a comment.
func VerifyLayout(dir string) (Verified, error) {
	header, err := os.ReadFile(filepath.Join(dir, "oci-layout"))
	if err != nil {
		return Verified{}, fmt.Errorf("read layout header: %w", err)
	}
	var lh LayoutHeader
	if err := json.Unmarshal(header, &lh); err != nil {
		return Verified{}, fmt.Errorf("parse oci-layout: %w", err)
	}
	if lh.ImageLayoutVersion != "1.0.0" {
		return Verified{}, fmt.Errorf("layout version %q is not 1.0.0", lh.ImageLayoutVersion)
	}

	rawIndex, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return Verified{}, fmt.Errorf("read index: %w", err)
	}
	var index Index
	if err := json.Unmarshal(rawIndex, &index); err != nil {
		return Verified{}, fmt.Errorf("parse index.json: %w", err)
	}
	if index.SchemaVersion != 2 {
		return Verified{}, fmt.Errorf("index schemaVersion is %d, expected 2", index.SchemaVersion)
	}
	if len(index.Manifests) != 1 {
		return Verified{}, fmt.Errorf("index lists %d manifests; a bundle layout has exactly one", len(index.Manifests))
	}

	v := Verified{Dir: dir}
	manifestDesc := index.Manifests[0]
	if manifestDesc.MediaType != MediaTypeManifest {
		return Verified{}, fmt.Errorf("index points at %q, not an image manifest", manifestDesc.MediaType)
	}
	manifestBytes, err := verifyBlob(dir, manifestDesc)
	if err != nil {
		return Verified{}, fmt.Errorf("manifest: %w", err)
	}
	v.ManifestDigest = manifestDesc.Digest
	v.Blobs++

	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return Verified{}, fmt.Errorf("parse manifest: %w", err)
	}
	if manifest.SchemaVersion != 2 {
		return Verified{}, fmt.Errorf("manifest schemaVersion is %d, expected 2", manifest.SchemaVersion)
	}

	if _, err := verifyBlob(dir, manifest.Config); err != nil {
		return Verified{}, fmt.Errorf("config: %w", err)
	}
	v.Blobs++

	if len(manifest.Layers) != 1 {
		return Verified{}, fmt.Errorf("manifest lists %d layers; a bundle is one", len(manifest.Layers))
	}
	layerDesc := manifest.Layers[0]
	if layerDesc.MediaType != MediaTypeLayer {
		return Verified{}, fmt.Errorf("layer is %q, not a tar+gzip layer", layerDesc.MediaType)
	}
	layer, err := verifyBlob(dir, layerDesc)
	if err != nil {
		return Verified{}, fmt.Errorf("layer: %w", err)
	}
	v.LayerDigest = layerDesc.Digest
	v.Layer = layer
	v.Blobs++

	return v, nil
}

// verifyBlob reads the blob a descriptor names and proves it is that blob. Size is checked before
// the hash because it is free, and a size mismatch means the wrong bytes before any hashing.
func verifyBlob(dir string, d Descriptor) ([]byte, error) {
	if d.Digest == "" {
		return nil, fmt.Errorf("descriptor has no digest")
	}
	path := filepath.Join(dir, "blobs", "sha256", trimDigest(d.Digest))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("blob %s is missing: %w", d.Digest, err)
	}
	if int64(len(data)) != d.Size {
		return nil, fmt.Errorf("blob %s is %d bytes, the descriptor says %d", d.Digest, len(data), d.Size)
	}
	if actual := digestOf(data); actual != d.Digest {
		return nil, fmt.Errorf("blob %s hashes to %s: the content is not what the digest names", d.Digest, actual)
	}
	return data, nil
}
