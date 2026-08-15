package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// TemplateIdentity pins the operator-owned Release Template a bundle was rendered
// from.
//
// ContentDigest is authoritative. Version is a human-readable label the operator may
// set; it is recorded for proof but never trusted for equality, because a label can be
// reused across edits while a content digest cannot.
type TemplateIdentity struct {
	Name          string `json:"name,omitempty"`
	Version       string `json:"version,omitempty"`
	ContentDigest string `json:"content_digest"`
	// FileCount is the number of template files covered by ContentDigest.
	FileCount int `json:"file_count"`
}

// Validate checks that the identity pins real content.
func (t TemplateIdentity) Validate() error {
	if !IsContentDigest(t.ContentDigest) {
		return RenderError("missing_template_digest", "bundle.template.content_digest",
			fmt.Sprintf("%q is not a sha256 content digest", t.ContentDigest),
			"Render from a loaded Release Template so its content digest is pinned to the Release.")
	}
	if t.FileCount <= 0 {
		return RenderError("empty_template", "bundle.template.file_count",
			"the Release Template contained no files",
			"Point SafeLane at the operator-owned Release Template directory.")
	}
	return nil
}

// ResourceRef identifies one rendered Kubernetes resource.
//
// TemplatePath is the path of the template file inside the Release Template that
// produced it, which is also the bundle's ordering key. The Kubernetes coordinates
// are read back out of the rendered bytes, so they describe what was actually
// produced rather than what a template author intended.
type ResourceRef struct {
	TemplatePath string `json:"template_path"`
	APIVersion   string `json:"api_version"`
	Kind         string `json:"kind"`
	Namespace    string `json:"namespace,omitempty"`
	Name         string `json:"name"`
}

func (r ResourceRef) String() string {
	if r.Namespace == "" {
		return fmt.Sprintf("%s/%s", r.Kind, r.Name)
	}
	return fmt.Sprintf("%s/%s/%s", r.Kind, r.Namespace, r.Name)
}

// Validate checks that the reference identifies a resource.
func (r ResourceRef) Validate() error {
	switch {
	case r.TemplatePath == "":
		return RenderError("missing_template_path", "bundle.resources[].template_path",
			"a rendered resource has no source template path", "This is a SafeLane defect; the renderer must record the source path.")
	case r.APIVersion == "":
		return RenderError("missing_api_version", "bundle.resources[].api_version",
			fmt.Sprintf("%s rendered no apiVersion", r.TemplatePath), "Every template file must render exactly one Kubernetes object with a top-level apiVersion.")
	case r.Kind == "":
		return RenderError("missing_kind", "bundle.resources[].kind",
			fmt.Sprintf("%s rendered no kind", r.TemplatePath), "Every template file must render exactly one Kubernetes object with a top-level kind.")
	case r.Name == "":
		return RenderError("missing_name", "bundle.resources[].name",
			fmt.Sprintf("%s rendered no metadata.name", r.TemplatePath), "Every template file must render exactly one Kubernetes object with metadata.name.")
	}
	return nil
}

// RenderedResource is one rendered Kubernetes object: its identity, the exact bytes
// SafeLane rendered, and the content hash of those exact bytes.
//
// # Why the hash cannot disagree with the bytes
//
// The bytes and the hash are unexported, and [NewRenderedResource] takes no hash
// argument - it computes the hash from the bytes it is given, in the same statement
// that stores them. There is no setter, no exported field, and no way to construct a
// value whose hash describes anything other than its own bytes.
//
// [RenderedResource.Bytes] returns a copy, so the bytes cannot be mutated after
// hashing either. That copy is the reason this type is safe to hand to proof
// rendering and to execution: both see the same bytes the hash covers.
type RenderedResource struct {
	ref   ResourceRef
	bytes []byte
	hash  string
}

// NewRenderedResource stores the exact rendered bytes and derives their content hash.
// The bytes are copied on the way in, so a renderer reusing a scratch buffer cannot
// invalidate the hash afterwards.
func NewRenderedResource(ref ResourceRef, rendered []byte) (RenderedResource, error) {
	if err := ref.Validate(); err != nil {
		return RenderedResource{}, err
	}
	if len(rendered) == 0 {
		return RenderedResource{}, RenderError("empty_rendered_resource", "bundle.resources[]",
			fmt.Sprintf("%s rendered no bytes", ref.TemplatePath),
			"A template file that renders nothing cannot be part of the bundle SafeLane will apply.")
	}
	stored := make([]byte, len(rendered))
	copy(stored, rendered)
	sum := sha256.Sum256(stored)
	return RenderedResource{
		ref:   ref,
		bytes: stored,
		hash:  DigestAlgorithm + ":" + hex.EncodeToString(sum[:]),
	}, nil
}

// Ref returns the resource identity.
func (r RenderedResource) Ref() ResourceRef { return r.ref }

// Bytes returns a copy of the exact rendered bytes that [RenderedResource.Hash]
// covers.
func (r RenderedResource) Bytes() []byte {
	out := make([]byte, len(r.bytes))
	copy(out, r.bytes)
	return out
}

// Hash returns "sha256:<hex>" over the exact rendered bytes.
func (r RenderedResource) Hash() string { return r.hash }

// Size returns the length in bytes of the rendered resource.
func (r RenderedResource) Size() int { return len(r.bytes) }

// IsZero reports whether this is the unset zero value.
func (r RenderedResource) IsZero() bool { return r.hash == "" }

type renderedResourceJSON struct {
	Ref   ResourceRef `json:"ref"`
	Bytes []byte      `json:"bytes"`
	Hash  string      `json:"hash"`
}

// MarshalJSON records identity, bytes and hash. The bytes are persisted because
// execution (#55) consumes the already-rendered bundle rather than re-rendering it.
func (r RenderedResource) MarshalJSON() ([]byte, error) {
	return json.Marshal(renderedResourceJSON{Ref: r.ref, Bytes: r.bytes, Hash: r.hash})
}

// UnmarshalJSON rebuilds the resource through [NewRenderedResource] and then checks
// the stored hash against the recomputed one. A record whose hash does not match its
// bytes is rejected: that is exactly the tampering this design exists to detect.
func (r *RenderedResource) UnmarshalJSON(data []byte) error {
	var w renderedResourceJSON
	if err := json.Unmarshal(data, &w); err != nil {
		return Malformed("malformed_rendered_resource", "bundle.resources[]",
			"a stored rendered resource could not be decoded",
			"The Release record is corrupt or was written by an incompatible version.").WithCause(err)
	}
	built, err := NewRenderedResource(w.Ref, w.Bytes)
	if err != nil {
		return err
	}
	if w.Hash != built.hash {
		return FailedEvidenceError("rendered_resource_hash_mismatch", "bundle.resources[].hash",
			fmt.Sprintf("%s: recorded hash %s does not cover the recorded bytes (%s)", w.Ref, w.Hash, built.hash),
			"The Release record was altered after rendering. Do not release from it.")
	}
	*r = built
	return nil
}

// RenderedBundle is the Rendered Manifest Bundle for one Release: the exact final
// Kubernetes object bytes SafeLane rendered from the operator-owned Release Template
// and intends to apply.
//
// # One rendering, reused everywhere
//
// A bundle is produced exactly once per Release, by
// github.com/AndrewMaged814/safelane/internal/render. This type has no method that
// renders, re-renders, or mutates anything: no Rerender, no template reference beyond
// an identity struct, no renderer handle, no exported field. The value a caller holds
// is the only bundle in play, and it is the value whose hashes are recorded on the
// Release and whose bytes execution later applies.
//
// # What it binds
//
// A bundle records the [TemplateIdentity] it came from, the [Target] it was rendered
// for, and the verified digest that was pinned into the pod template. [NewRelease]
// checks all three against the Release's own target and verified evidence, so a
// bundle rendered for one change or one target cannot be attached to another.
type RenderedBundle struct {
	template     TemplateIdentity
	target       Target
	pinnedDigest string
	resources    []RenderedResource
	digest       string
}

// NewRenderedBundle assembles a bundle from already-rendered resources and derives
// the bundle digest from them.
//
// The renderer is the intended caller. Resource order is the caller's order and is
// preserved verbatim, because apply order is part of what was rendered.
func NewRenderedBundle(template TemplateIdentity, target Target, pinnedDigest string, resources []RenderedResource) (RenderedBundle, error) {
	if err := template.Validate(); err != nil {
		return RenderedBundle{}, err
	}
	if err := target.Validate(); err != nil {
		return RenderedBundle{}, err
	}
	if !IsContentDigest(pinnedDigest) {
		return RenderedBundle{}, RenderError("unpinned_bundle", "bundle.pinned_digest",
			fmt.Sprintf("%q is not a sha256 digest", pinnedDigest),
			"Render only against a verified immutable OCI digest.")
	}
	if len(resources) == 0 {
		return RenderedBundle{}, RenderError("empty_bundle", "bundle.resources",
			"the Release Template rendered no resources",
			"The Release Template must render at least the Rollout and its Services.")
	}
	seenPath := make(map[string]struct{}, len(resources))
	seenRef := make(map[string]struct{}, len(resources))
	stored := make([]RenderedResource, 0, len(resources))
	for _, res := range resources {
		if res.IsZero() {
			return RenderedBundle{}, Internal("unset_rendered_resource",
				"a bundle resource was never rendered; build every resource with NewRenderedResource")
		}
		path := res.ref.TemplatePath
		if _, dup := seenPath[path]; dup {
			return RenderedBundle{}, RenderError("duplicate_template_path", "bundle.resources[].template_path",
				fmt.Sprintf("template path %q rendered twice", path), "Each template file renders exactly one resource.")
		}
		seenPath[path] = struct{}{}
		key := res.ref.APIVersion + "|" + res.ref.String()
		if _, dup := seenRef[key]; dup {
			return RenderedBundle{}, RenderError("duplicate_resource_identity", "bundle.resources[].name",
				fmt.Sprintf("two rendered resources share the identity %s", res.ref),
				"Give each rendered resource a distinct kind/namespace/name.")
		}
		seenRef[key] = struct{}{}
		stored = append(stored, res)
	}

	b := RenderedBundle{
		template:     template,
		target:       target,
		pinnedDigest: pinnedDigest,
		resources:    stored,
	}
	b.digest = b.computeDigest()
	return b, nil
}

// computeDigest derives one digest for the whole bundle from the per-resource hashes
// and their order, plus the template identity, target and pinned digest.
//
// It hashes the resource *hashes* rather than re-serializing the resources, so the
// bundle digest inherits the per-resource guarantee instead of introducing a second,
// possibly divergent, serialization of the same bytes.
func (b RenderedBundle) computeDigest() string {
	h := sha256.New()
	writeField := func(k, v string) {
		fmt.Fprintf(h, "%s=%d:%s\n", k, len(v), v)
	}
	writeField("safelane.bundle.v1", "")
	writeField("template.digest", b.template.ContentDigest)
	writeField("target", b.target.String())
	writeField("pinned.digest", b.pinnedDigest)
	for i, res := range b.resources {
		writeField(fmt.Sprintf("resource.%d.path", i), res.ref.TemplatePath)
		writeField(fmt.Sprintf("resource.%d.identity", i), res.ref.APIVersion+"|"+res.ref.String())
		writeField(fmt.Sprintf("resource.%d.hash", i), res.hash)
	}
	sum := h.Sum(nil)
	return DigestAlgorithm + ":" + hex.EncodeToString(sum)
}

// Template returns the pinned Release Template identity.
func (b RenderedBundle) Template() TemplateIdentity { return b.template }

// Target returns the target the bundle was rendered for.
func (b RenderedBundle) Target() Target { return b.target }

// PinnedDigest returns the verified immutable OCI digest baked into the pod template.
func (b RenderedBundle) PinnedDigest() string { return b.pinnedDigest }

// Resources returns the ordered rendered resources. The returned slice is a copy, so
// the bundle's contents and its digest cannot be changed after the fact.
func (b RenderedBundle) Resources() []RenderedResource {
	out := make([]RenderedResource, len(b.resources))
	copy(out, b.resources)
	return out
}

// Len returns the number of rendered resources.
func (b RenderedBundle) Len() int { return len(b.resources) }

// Digest returns one "sha256:<hex>" digest over the whole bundle: template identity,
// target, pinned artifact digest, and the ordered per-resource hashes.
func (b RenderedBundle) Digest() string { return b.digest }

// Hashes returns the per-resource hashes in bundle order, keyed by template path.
func (b RenderedBundle) Hashes() []ResourceHash {
	out := make([]ResourceHash, 0, len(b.resources))
	for _, res := range b.resources {
		out = append(out, ResourceHash{Ref: res.ref, Hash: res.hash, Size: len(res.bytes)})
	}
	return out
}

// Manifest returns the rendered resources concatenated as one multi-document YAML
// stream, in bundle order.
//
// This is a *view* for writing the bundle to disk or applying it.
// It concatenates the same stored bytes the per-resource hashes cover; it
// does not re-render or re-serialize anything.
func (b RenderedBundle) Manifest() []byte {
	var sb strings.Builder
	for _, res := range b.resources {
		sb.WriteString("---\n")
		sb.Write(res.bytes)
		if len(res.bytes) > 0 && res.bytes[len(res.bytes)-1] != '\n' {
			sb.WriteString("\n")
		}
	}
	return []byte(sb.String())
}

// IsZero reports whether this is the unset zero value.
func (b RenderedBundle) IsZero() bool { return b.digest == "" }

// ResourceHash is a per-resource hash record, for proof rendering.
type ResourceHash struct {
	Ref  ResourceRef `json:"ref"`
	Hash string      `json:"hash"`
	Size int         `json:"size"`
}

type renderedBundleJSON struct {
	Template     TemplateIdentity   `json:"template"`
	Target       Target             `json:"target"`
	PinnedDigest string             `json:"pinned_digest"`
	Resources    []RenderedResource `json:"resources"`
	Digest       string             `json:"digest"`
}

// MarshalJSON records the bundle.
func (b RenderedBundle) MarshalJSON() ([]byte, error) {
	return json.Marshal(renderedBundleJSON{
		Template:     b.template,
		Target:       b.target,
		PinnedDigest: b.pinnedDigest,
		Resources:    b.resources,
		Digest:       b.digest,
	})
}

// UnmarshalJSON rebuilds the bundle through [NewRenderedBundle] - which re-derives
// the bundle digest from the per-resource hashes - and rejects a record whose stored
// bundle digest does not match.
func (b *RenderedBundle) UnmarshalJSON(data []byte) error {
	var w renderedBundleJSON
	if err := json.Unmarshal(data, &w); err != nil {
		return Malformed("malformed_bundle_record", "bundle", "the stored rendered bundle could not be decoded",
			"The Release record is corrupt or was written by an incompatible version.").WithCause(err)
	}
	built, err := NewRenderedBundle(w.Template, w.Target, w.PinnedDigest, w.Resources)
	if err != nil {
		return err
	}
	if w.Digest != built.digest {
		return FailedEvidenceError("bundle_digest_mismatch", "bundle.digest",
			fmt.Sprintf("recorded bundle digest %s does not cover the recorded resources (%s)", w.Digest, built.digest),
			"The Release record was altered after rendering. Do not release from it.")
	}
	*b = built
	return nil
}
