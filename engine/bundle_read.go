package engine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// A bundle archive is read back by a consumer who did not build it, so its contents are input:
// names are checked before they are used, and sizes are capped. A decompression bomb is a denial
// of service on the consumer, and a name like ../x is how an archive escapes the directory it is
// extracted into.
const (
	maxArchiveFile  = 64 << 20
	maxArchiveTotal = 256 << 20
)

// ReadArchive is the inverse of WriteArchive: it returns every regular file in a gzipped tar,
// keyed by its archive name.
func ReadArchive(r io.Reader) (map[string][]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("read gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	var total int64

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar header: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("archive entry %q is not a regular file (type %q)", hdr.Name, hdr.Typeflag)
		}
		name, err := safeArchiveName(hdr.Name)
		if err != nil {
			return nil, err
		}
		if _, duplicate := files[name]; duplicate {
			return nil, fmt.Errorf("archive contains %q twice", name)
		}
		if hdr.Size > maxArchiveFile {
			return nil, fmt.Errorf("archive entry %q is %d bytes; the limit is %d", name, hdr.Size, maxArchiveFile)
		}

		data, err := io.ReadAll(io.LimitReader(tr, maxArchiveFile+1))
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", name, err)
		}
		total += int64(len(data))
		if total > maxArchiveTotal {
			return nil, fmt.Errorf("archive expands past %d bytes", maxArchiveTotal)
		}
		files[name] = data
	}

	if len(files) == 0 {
		return nil, errors.New("archive contains no files")
	}
	return files, nil
}

// safeArchiveName rejects names that would escape an extraction directory or that are ambiguous
// across platforms.
func safeArchiveName(name string) (string, error) {
	if name == "" {
		return "", errors.New("archive contains an entry with an empty name")
	}
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("archive entry %q is absolute", name)
	}
	if strings.Contains(name, "\\") {
		return "", fmt.Errorf("archive entry %q contains a backslash", name)
	}
	if path.Clean(name) != name {
		return "", fmt.Errorf("archive entry %q is not a clean relative path", name)
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "../") {
		return "", fmt.Errorf("archive entry %q escapes the extraction directory", name)
	}
	return name, nil
}

// ArchiveContents is what a bundle archive was proven to contain.
type ArchiveContents struct {
	Files   int
	Objects int
	Packs   int
	Plans   int
	// Undeclared lists files the archive carries that the manifest does not name by digest.
	// Since bundleFormat 2 this is expected to be empty: every artefact kind the bundle carries
	// has a field in the manifest. A non-empty list is a gap in the manifest, not in the check.
	Undeclared []string
}

// governedKind reports whether a path is one of the kinds the manifest must describe. A file of
// one of these kinds that the manifest does not name is content the bundle will act on, and its
// provenance is unaccounted for.
func governedKind(name string) bool {
	return strings.HasPrefix(name, "rules/") ||
		strings.HasPrefix(name, "plans/") ||
		strings.HasPrefix(name, "catalog/business-objects/") ||
		strings.HasPrefix(name, "catalog/value-sets/") ||
		name == "catalog/intents.yaml"
}

// VerifyBundleArchive proves that the manifest inside a bundle archive still describes the files
// inside that archive.
//
// The check is by digest rather than by path, because the archive is a flat set of artefacts and
// the manifest names them by identity. Every digest the manifest declares must belong to a file
// the archive carries — that catches tampering and removal. A carried file of a governed kind
// that the manifest does not declare is a failure too, because that is provenance nobody
// asserted. A file renamed is not caught here, and does not need to be: renaming changes the
// layer digest the binding pins.
func VerifyBundleArchive(archive []byte) (ArchiveContents, error) {
	files, err := ReadArchive(bytes.NewReader(archive))
	if err != nil {
		return ArchiveContents{}, err
	}

	rawManifest, ok := files["bundle.yaml"]
	if !ok {
		return ArchiveContents{}, errors.New("archive has no bundle.yaml, so it describes nothing")
	}
	var b Bundle
	if err := yaml.Unmarshal(rawManifest, &b); err != nil {
		return ArchiveContents{}, fmt.Errorf("parse bundle.yaml: %w", err)
	}
	if b.BundleFormat != 2 {
		return ArchiveContents{}, fmt.Errorf(
			"bundleFormat is %d; this verifier understands 2, and a bundle whose provenance it "+
				"cannot read is not one it will accept", b.BundleFormat)
	}

	declared := map[string]string{} // digest -> what claims it
	for _, o := range b.Objects {
		if o.ContractDigest == "" {
			return ArchiveContents{}, fmt.Errorf("object %q declares no contractDigest", o.Name)
		}
		declared[o.ContractDigest] = "contract " + o.Name
	}
	// Value sets and the registry are checked by path as well as by digest, so a file renamed
	// inside the archive is caught: a digest alone cannot tell a rename from the published file.
	byPath := map[string]string{} // archive path -> digest claimed for it
	for _, vs := range b.ValueSets {
		if vs.Digest == "" || vs.Path == "" {
			return ArchiveContents{}, fmt.Errorf("value set %q declares no path or digest", vs.Name)
		}
		declared[vs.Digest] = "value set " + vs.Name
		byPath[vs.Path] = vs.Digest
	}
	if b.Intents != nil {
		if b.Intents.Digest == "" || b.Intents.Path == "" {
			return ArchiveContents{}, errors.New("intents declares no path or digest")
		}
		declared[b.Intents.Digest] = "the intent registry"
		byPath[b.Intents.Path] = b.Intents.Digest
	}
	for _, p := range b.Packs {
		if p.Digest == "" {
			return ArchiveContents{}, fmt.Errorf("pack %q declares no digest", p.Name)
		}
		declared[p.Digest] = "pack " + p.Name
	}
	for _, p := range b.Plans {
		if p.Digest == "" {
			return ArchiveContents{}, fmt.Errorf("plan %q declares no digest", p.Plan)
		}
		declared[p.Digest] = "plan " + p.Plan
	}

	present := map[string]string{} // digest -> file name
	undeclared := []string{}
	for name, data := range files {
		if name == "bundle.yaml" {
			continue
		}
		digest := digestOf(data)
		present[digest] = name
		if _, ok := declared[digest]; !ok {
			if governedKind(name) {
				return ArchiveContents{}, fmt.Errorf(
					"the archive carries %q, which the manifest does not describe: its provenance is unasserted", name)
			}
			undeclared = append(undeclared, name)
		}
	}

	for digest, claim := range declared {
		if _, ok := present[digest]; !ok {
			return ArchiveContents{}, fmt.Errorf(
				"the manifest's %s claims %s, but no file in the archive has that digest", claim, digest)
		}
	}

	for path, digest := range byPath {
		data, ok := files[path]
		if !ok {
			return ArchiveContents{}, fmt.Errorf("the manifest describes %q, which the archive does not carry", path)
		}
		if actual := digestOf(data); actual != digest {
			return ArchiveContents{}, fmt.Errorf(
				"%q hashes to %s; the manifest claims %s for that path", path, actual, digest)
		}
	}

	sort.Strings(undeclared)
	return ArchiveContents{
		Files:      len(files),
		Objects:    len(b.Objects),
		Packs:      len(b.Packs),
		Plans:      len(b.Plans),
		Undeclared: undeclared,
	}, nil
}
