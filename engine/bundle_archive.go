package engine

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BundleFiles assembles the archive contents: the manifest plus every artefact it describes.
// Plans are stored under plans/ because that is how the manifest refers to them.
func BundleFiles(m *Model, manifest []byte) (map[string][]byte, error) {
	files := map[string][]byte{"bundle.yaml": manifest}
	add := func(rel, name string) error {
		data, err := os.ReadFile(filepath.Join(m.Root, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		files[name] = data
		return nil
	}

	for name := range m.Contracts {
		rel := m.Contracts[name].Path
		if err := add(rel, rel); err != nil {
			return nil, err
		}
	}
	for _, vs := range m.ValueSets {
		if err := add(vs.Path, vs.Path); err != nil {
			return nil, err
		}
	}
	if m.Registry != nil {
		if err := add(m.Registry.Path, m.Registry.Path); err != nil {
			return nil, err
		}
	}
	for _, p := range m.AllPacks() {
		if err := add(p.Path, p.Path); err != nil {
			return nil, err
		}
	}
	for rel := range m.Plans {
		if err := add(rel, strings.TrimPrefix(rel, "evidence/")); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// WriteArchive writes a gzipped tar that depends only on its inputs: entries are sorted,
// every header field is fixed, and the timestamp comes from the caller. Two builds of the
// same tree must produce identical bytes — that is the property the whole audit story
// rests on, because a fresh digest on every build makes a binding bump look like a change.
func WriteArchive(w io.Writer, files map[string][]byte, epoch int64) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	stamp := time.Unix(epoch, 0).UTC()

	gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	gz.Header.ModTime = stamp
	gz.Header.OS = 255 // unknown, rather than a platform that would differ

	tw := tar.NewWriter(gz)
	for _, name := range names {
		data := files[name]
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(data)),
			ModTime:  stamp,
			Typeflag: tar.TypeReg,
			Uid:      0,
			Gid:      0,
			Uname:    "",
			Gname:    "",
			Format:   tar.FormatUSTAR,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("header %s: %w", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			return fmt.Errorf("body %s: %w", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}
