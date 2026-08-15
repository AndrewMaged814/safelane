package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/AndrewMaged814/safelane/internal/release"
)

const (
	// ResourceSuffix marks a file in the Release Template as a resource template.
	// Every such file must render exactly one Kubernetes object.
	ResourceSuffix = ".yaml.tmpl"

	// MetadataFile is the optional Release Template metadata file. It holds
	// "key: value" lines; the recognized keys are "name" and "version". It is
	// optional because the template's content digest, not its label, is what a
	// Release pins.
	MetadataFile = "TEMPLATE"
)

// Template is a loaded, operator-owned Release Template.
//
// A Template is immutable once loaded and carries a content digest over every file in
// the template directory. SafeLane does not author templates and no caller may select
// or override one: the only way to change what SafeLane renders is to change the
// operator-owned files, which changes the digest recorded on every subsequent Release.
type Template struct {
	name      string
	version   string
	digest    string
	fileCount int
	resources []resourceTemplate
}

type resourceTemplate struct {
	path string
	body string
}

// LoadDir loads the Release Template from a directory on disk.
func LoadDir(dir string) (Template, error) {
	t, err := LoadFS(os.DirFS(dir))
	if err != nil {
		return Template{}, err
	}
	return t, nil
}

// LoadFS loads the Release Template from a filesystem.
//
// The content digest covers *every* regular file in the tree, not only the resource
// templates, because a Release pins its exact template content. An operator who does
// not want prose edits to change template identity should keep prose out of the
// template directory.
func LoadFS(fsys fs.FS) (Template, error) {
	type loadedFile struct {
		path string
		body string
	}
	var files []loadedFile

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return readErr
		}
		files = append(files, loadedFile{path: path, body: normalizeNewlines(string(raw))})
		return nil
	})
	if err != nil {
		return Template{}, release.RenderError("template_unreadable", "template",
			"the Release Template could not be read",
			"Point SafeLane at the operator-owned Release Template directory.").WithCause(err)
	}
	if len(files) == 0 {
		return Template{}, release.RenderError("empty_template", "template",
			"the Release Template directory is empty",
			"The Release Template must contain at least one "+ResourceSuffix+" file.")
	}

	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	h := sha256.New()
	fmt.Fprintf(h, "safelane.release-template.v1\n")
	var resources []resourceTemplate
	var meta metadata
	for _, f := range files {
		fmt.Fprintf(h, "%s\n%d\n", f.path, len(f.body))
		h.Write([]byte(f.body))

		switch {
		case strings.HasSuffix(f.path, ResourceSuffix):
			resources = append(resources, resourceTemplate{path: f.path, body: f.body})
		case f.path == MetadataFile:
			parsed, err := parseMetadata(f.body)
			if err != nil {
				return Template{}, err
			}
			meta = parsed
		}
	}
	if len(resources) == 0 {
		return Template{}, release.RenderError("no_resource_templates", "template",
			"the Release Template contains no "+ResourceSuffix+" files",
			"Each Kubernetes object in the bundle is one "+ResourceSuffix+" file in the Release Template.")
	}

	return Template{
		name:      meta.name,
		version:   meta.version,
		digest:    release.DigestAlgorithm + ":" + hex.EncodeToString(h.Sum(nil)),
		fileCount: len(files),
		resources: resources,
	}, nil
}

// Identity returns the template identity recorded on every Release rendered from it.
func (t Template) Identity() release.TemplateIdentity {
	return release.TemplateIdentity{
		Name:          t.name,
		Version:       t.version,
		ContentDigest: t.digest,
		FileCount:     t.fileCount,
	}
}

// ResourcePaths returns the resource template paths in render order.
func (t Template) ResourcePaths() []string {
	out := make([]string, 0, len(t.resources))
	for _, r := range t.resources {
		out = append(out, r.path)
	}
	return out
}

// IsZero reports whether the template was never loaded.
func (t Template) IsZero() bool { return t.digest == "" }

type metadata struct {
	name    string
	version string
}

func parseMetadata(body string) (metadata, error) {
	var m metadata
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return metadata{}, release.RenderError("malformed_template_metadata", MetadataFile,
				fmt.Sprintf("%s line %d is not a \"key: value\" pair", MetadataFile, i+1),
				`Use "name: <name>" and "version: <version>" lines, or delete the file.`)
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "name":
			m.name = value
		case "version":
			m.version = value
		default:
			return metadata{}, release.RenderError("unknown_template_metadata_key", MetadataFile,
				fmt.Sprintf("%s line %d has unknown key %q", MetadataFile, i+1, strings.TrimSpace(key)),
				`Only "name" and "version" are recognized. A typo here would silently drop template identity, so it is rejected.`)
		}
	}
	return m, nil
}

// normalizeNewlines converts CRLF and lone CR to LF.
//
// This runs on template bytes as they are read, before hashing and before rendering.
// Without it, the same template checked out on Windows and on a Linux CI runner would
// produce different template digests and different per-resource hashes for identical
// content, and the "same template version produces the same bytes" guarantee would be
// false across platforms.
func normalizeNewlines(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
