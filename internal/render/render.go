package render

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/AndrewMaged814/safelane/internal/release"
)

// TemplateData is the complete, explicitly ordered set of values SafeLane substitutes
// into the Release Template. It is a struct, not a map, so there is no iteration order
// to leak into rendered bytes and a template typo is an execution error rather than an
// empty string.
//
// Every field here is derived from the release target or from verified evidence.
// Nothing on it comes from a caller's free-form input, and nothing on it is
// time-dependent or random.
//
// A real Release Template may use any subset of these. It must use [TemplateData.ImageReference]
// (or [TemplateData.ImageDigest]) in its pod template, or [Render] fails - see
// "unpinned_template" in [Render].
type TemplateData struct {
	// Target identity.
	Application string
	Environment string
	Cluster     string
	Namespace   string

	// Verified artifact. ImageReference is the full immutable reference and is what a
	// pod template should use.
	ImageReference  string // ghcr.io/owner/podinfo@sha256:<hex>
	ImageRegistry   string // ghcr.io
	ImageRepository string // owner/podinfo
	ImageDigest     string // sha256:<hex>

	// Verified source identity, for traceability annotations. Deterministic: both
	// come from verified evidence, not from a clock.
	SourceRepository string // owner/repo
	SourceRevision   string // merge commit SHA on the base branch
	SourceBranch     string

	// SafeLane-derived resource names. These are conventions, not policy: a real
	// Release Template may hard-code its own names instead. If the pre-created Rollout
	// (#55) uses different names, change the derivation in newTemplateData - the names
	// must match the operator's cluster exactly.
	RolloutName          string // <application>
	StableServiceName    string // <application>-stable
	CanaryServiceName    string // <application>-canary
	AnalysisTemplateName string // <application>-success-rate
	IngressName          string // <application>
}

// Render produces the single Rendered Manifest Bundle for one Release.
//
// It is a pure function of its arguments: same template, target and evidence produce
// byte-identical output every time. It accepts no clock, no entropy, and no release ID,
// so no non-deterministic value can reach the rendered bytes.
//
// The digest pinned into the bundle is read from the verified evidence, never from a
// caller's claim. Render fails with "unpinned_template" if the rendered bundle does not
// contain that digest anywhere, which is the guard that catches a real Release Template
// whose pod template forgot to reference the image: SafeLane would rather refuse to
// release than record a bundle that is not pinned to the verified artifact.
func Render(t Template, target release.Target, evidence release.ReleaseEvidence) (release.RenderedBundle, error) {
	if t.IsZero() {
		return release.RenderedBundle{}, release.RenderError("template_not_loaded", "template",
			"no Release Template was loaded",
			"Load the operator-owned Release Template with LoadDir or LoadFS before rendering.")
	}
	if evidence.IsZero() {
		return release.RenderedBundle{}, release.RenderError("render_without_verified_evidence", "evidence",
			"rendering was attempted without verified evidence",
			"Verify evidence first. SafeLane pins the pod template to the verified immutable digest, never to a caller's claim.")
	}
	if err := target.Validate(); err != nil {
		return release.RenderedBundle{}, err
	}

	data, err := newTemplateData(target, evidence)
	if err != nil {
		return release.RenderedBundle{}, err
	}

	resources := make([]release.RenderedResource, 0, len(t.resources))
	pinned := false
	for _, rt := range t.resources {
		tmpl, err := template.New(rt.path).Option("missingkey=error").Parse(rt.body)
		if err != nil {
			return release.RenderedBundle{}, release.RenderError("template_parse_failed", rt.path,
				fmt.Sprintf("%s is not a valid template", rt.path),
				"Fix the template syntax in the operator-owned Release Template.").WithCause(err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return release.RenderedBundle{}, release.RenderError("template_execute_failed", rt.path,
				fmt.Sprintf("%s could not be rendered", rt.path),
				"The template references a value SafeLane does not supply. See render.TemplateData for the available values.").WithCause(err)
		}

		rendered := finalizeDocument(buf.Bytes())
		ref, err := identifyResource(rt.path, rendered)
		if err != nil {
			return release.RenderedBundle{}, err
		}
		res, err := release.NewRenderedResource(ref, rendered)
		if err != nil {
			return release.RenderedBundle{}, err
		}
		if bytes.Contains(rendered, []byte(data.ImageDigest)) {
			pinned = true
		}
		resources = append(resources, res)
	}

	if !pinned {
		return release.RenderedBundle{}, release.RenderError("unpinned_template", "template",
			fmt.Sprintf("no rendered resource references the verified digest %s", data.ImageDigest),
			"The Release Template's pod template must use {{ .ImageReference }} (or {{ .ImageDigest }}). SafeLane will not record a bundle that is not pinned to the verified artifact.")
	}

	return release.NewRenderedBundle(t.Identity(), target, data.ImageDigest, resources)
}

// newTemplateData derives the substitution values and re-validates every one of them.
//
// The values already passed validation on their way into the target and the verified
// evidence. They are checked again here because this is the last point before they are
// interpolated into YAML by text/template, which performs no escaping: a value
// containing a newline or a quote would not merely look wrong, it would change the
// structure of an object SafeLane is about to apply.
func newTemplateData(target release.Target, evidence release.ReleaseEvidence) (TemplateData, error) {
	ref := evidence.Artifact().Reference
	repo := evidence.Repository()

	data := TemplateData{
		Application: target.Application,
		Environment: target.Environment,
		Cluster:     target.Cluster,
		Namespace:   target.Namespace,

		ImageReference:  ref.String(),
		ImageRegistry:   ref.Registry,
		ImageRepository: ref.Repository,
		ImageDigest:     ref.Digest,

		SourceRepository: repo.String(),
		SourceRevision:   evidence.MergeCommitSHA(),
		SourceBranch:     evidence.PullRequest().BaseBranch,

		RolloutName:          target.Application,
		StableServiceName:    target.Application + "-stable",
		CanaryServiceName:    target.Application + "-canary",
		AnalysisTemplateName: target.Application + "-success-rate",
		IngressName:          target.Application,
	}

	for _, v := range []struct {
		name  string
		value string
	}{
		{"Application", data.Application},
		{"Environment", data.Environment},
		{"Cluster", data.Cluster},
		{"Namespace", data.Namespace},
		{"ImageReference", data.ImageReference},
		{"ImageRegistry", data.ImageRegistry},
		{"ImageRepository", data.ImageRepository},
		{"ImageDigest", data.ImageDigest},
		{"SourceRepository", data.SourceRepository},
		{"SourceRevision", data.SourceRevision},
		{"SourceBranch", data.SourceBranch},
	} {
		if !isSafeScalar(v.value) {
			return TemplateData{}, release.RenderError("unsafe_template_value", "template_data."+v.name,
				fmt.Sprintf("%s value %q contains a character SafeLane will not interpolate into YAML", v.name, v.value),
				"This is a SafeLane defect or a malformed verified value; rendering is refused rather than emitting structurally altered YAML.")
		}
	}
	if !release.IsContentDigest(data.ImageDigest) {
		return TemplateData{}, release.RenderError("unpinned_artifact", "template_data.ImageDigest",
			fmt.Sprintf("%q is not a sha256 digest", data.ImageDigest),
			"Render only against a verified immutable digest.")
	}
	if !release.IsCommitSHA(data.SourceRevision) {
		return TemplateData{}, release.RenderError("malformed_source_revision", "template_data.SourceRevision",
			fmt.Sprintf("%q is not a full commit SHA", data.SourceRevision),
			"Render only against a verified merge commit SHA.")
	}
	return data, nil
}

// isSafeScalar restricts substituted values to characters that cannot alter YAML
// structure: no newlines, quotes, colons, braces, brackets, hashes or backslashes.
func isSafeScalar(s string) bool {
	if s == "" {
		return true
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == '/', c == '@':
		case c == ':':
			// Permitted only inside a digest ("sha256:<hex>") or a reference that
			// contains one; a bare colon would open a mapping.
			if !strings.Contains(s, "@sha256:") && !strings.HasPrefix(s, "sha256:") {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// finalizeDocument normalizes a rendered document: LF line endings and exactly one
// trailing newline. Normalization happens *before* hashing, so the hash covers the
// bytes as they will be recorded, handed to a provider, and applied.
func finalizeDocument(b []byte) []byte {
	s := normalizeNewlines(string(b))
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return []byte(s + "\n")
}

// identify reads the Kubernetes coordinates back out of the rendered bytes.
//
// It is a strict, deliberately small scanner rather than a YAML parser: it recognizes
// top-level apiVersion/kind and a top-level metadata block with name/namespace at one
// level of indentation, which is what a single-object manifest looks like. Anything it
// cannot identify is an error, so a template that renders something unexpected fails
// loudly instead of being recorded with a blank identity.
//
// It never influences the hashed bytes.
func identifyResource(path string, rendered []byte) (release.ResourceRef, error) {
	ref := release.ResourceRef{TemplatePath: path}
	inMetadata := false
	started := false

	for _, raw := range strings.Split(string(rendered), "\n") {
		line := strings.TrimRight(raw, " \t")
		if line == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if line == "---" {
			if started {
				return release.ResourceRef{}, release.RenderError("multi_document_template", path,
					fmt.Sprintf("%s rendered more than one YAML document", path),
					"Each "+ResourceSuffix+" file must render exactly one Kubernetes object, so SafeLane can hash and identify it individually.")
			}
			continue
		}
		started = true

		if indent == 0 {
			inMetadata = false
			key, value, ok := strings.Cut(trimmed, ":")
			if !ok {
				continue
			}
			switch key {
			case "apiVersion":
				ref.APIVersion = unquote(strings.TrimSpace(value))
			case "kind":
				ref.Kind = unquote(strings.TrimSpace(value))
			case "metadata":
				inMetadata = true
			}
			continue
		}
		if inMetadata && indent == 2 {
			key, value, ok := strings.Cut(trimmed, ":")
			if !ok {
				continue
			}
			switch key {
			case "name":
				ref.Name = unquote(strings.TrimSpace(value))
			case "namespace":
				ref.Namespace = unquote(strings.TrimSpace(value))
			}
		}
	}

	if err := ref.Validate(); err != nil {
		return release.ResourceRef{}, err
	}
	return ref, nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
