package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Media types for the OCI layout this tool writes. The bundle archive is a layer, because
// that is what it is: a single blob carrying the manifest and the artefacts it names.
const (
	MediaTypeManifest = "application/vnd.oci.image.manifest.v1+json"
	MediaTypeIndex    = "application/vnd.oci.image.index.v1+json"
	MediaTypeConfig   = "application/vnd.oci.image.config.v1+json"
	MediaTypeLayer    = "application/vnd.oci.image.layer.v1.tar+gzip"
	MediaTypeLayout   = "application/vnd.oci.layout.header.v1+json"
)

// Descriptor is an OCI content descriptor. Field order is fixed by the struct, so the
// manifest that contains it marshals identically on every run.
type Descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// Manifest is the OCI image manifest for a bundle.
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Config        Descriptor        `json:"config"`
	Layers        []Descriptor      `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// Index points at the manifest, which is how a consumer finds it by tag.
type Index struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Manifests     []Descriptor `json:"manifests"`
}

// LayoutHeader is the oci-layout file.
type LayoutHeader struct {
	ImageLayoutVersion string `json:"imageLayoutVersion"`
}

// Published describes what was written.
type Published struct {
	Dir            string
	ManifestDigest string
	LayerDigest    string
	ManifestBytes  int
}

// Publish writes an OCI Image Layout for a bundle archive and returns the manifest digest —
// the value a binding pins. Nothing here contacts a registry: the layout is the artefact a
// registry would accept, and writing it is the part that can be verified offline.
func Publish(bundlePath, dir string, created time.Time) (Published, error) {
	layer, err := os.ReadFile(bundlePath)
	if err != nil {
		return Published{}, fmt.Errorf("read bundle: %w", err)
	}
	layerDigest := digestOf(layer)

	config := []byte("{}")
	configDigest := digestOf(config)

	m := Manifest{
		SchemaVersion: 2,
		MediaType:     MediaTypeManifest,
		Config:        Descriptor{MediaType: MediaTypeConfig, Digest: configDigest, Size: int64(len(config))},
		Layers:        []Descriptor{{MediaType: MediaTypeLayer, Digest: layerDigest, Size: int64(len(layer))}},
	}
	// The creation annotation is only added when a timestamp was supplied, so that two
	// publishes of the same bundle produce the same manifest digest.
	if !created.IsZero() {
		m.Annotations = map[string]string{
			"org.opencontainers.image.created": created.UTC().Format(time.RFC3339),
		}
	}

	manifest, err := json.Marshal(m)
	if err != nil {
		return Published{}, err
	}
	manifestDigest := digestOf(manifest)

	index, err := json.Marshal(Index{
		SchemaVersion: 2,
		MediaType:     MediaTypeIndex,
		Manifests:     []Descriptor{{MediaType: MediaTypeManifest, Digest: manifestDigest, Size: int64(len(manifest))}},
	})
	if err != nil {
		return Published{}, err
	}
	header, err := json.Marshal(LayoutHeader{ImageLayoutVersion: "1.0.0"})
	if err != nil {
		return Published{}, err
	}

	blobs := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobs, 0o755); err != nil {
		return Published{}, err
	}
	for digest, data := range map[string][]byte{
		configDigest:   config,
		layerDigest:    layer,
		manifestDigest: manifest,
	} {
		if err := os.WriteFile(filepath.Join(blobs, trimDigest(digest)), data, 0o644); err != nil {
			return Published{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), index, 0o644); err != nil {
		return Published{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "oci-layout"), header, 0o644); err != nil {
		return Published{}, err
	}

	return Published{
		Dir:            dir,
		ManifestDigest: manifestDigest,
		LayerDigest:    layerDigest,
		ManifestBytes:  len(manifest),
	}, nil
}

// Reference renders the pinned reference a binding would carry.
func (p Published) Reference(repository string) string {
	if repository == "" {
		return p.ManifestDigest
	}
	return repository + "@" + p.ManifestDigest
}

func trimDigest(digest string) string {
	if len(digest) > len("sha256:") {
		return digest[len("sha256:"):]
	}
	return digest
}
