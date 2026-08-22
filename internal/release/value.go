package release

import (
	"fmt"
	"strings"
)

// DigestAlgorithm is the only content-address algorithm SafeLane accepts, for both
// OCI image digests and its own content hashes.
const DigestAlgorithm = "sha256"

// digestHexLen is the hex length of a sha256 digest.
const digestHexLen = 64

// commitSHALen is the hex length of a git object ID.
const commitSHALen = 40

func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// IsCommitSHA reports whether s is a full lowercase 40-hex git object ID. SafeLane
// rejects abbreviated SHAs: an abbreviation is not an identity.
func IsCommitSHA(s string) bool { return isLowerHex(s, commitSHALen) }

// IsContentDigest reports whether s is a "sha256:<64 lowercase hex>" digest.
func IsContentDigest(s string) bool {
	algo, hex, ok := strings.Cut(s, ":")
	return ok && algo == DigestAlgorithm && isLowerHex(hex, digestHexLen)
}

// IsDNSLabel reports whether s is a lowercase RFC 1123 DNS label.
func IsDNSLabel(s string) bool { return isDNSLabel(s) }

// isDNSLabel reports whether s is a lowercase RFC 1123 DNS label.
//
// Every value SafeLane substitutes into the Release Template is checked against a
// strict character set before rendering. This is not cosmetic validation: rendered
// output is YAML built by text/template, which performs no escaping, so an
// unvalidated namespace or application name is a YAML-injection vector into the
// bundle that later reaches the cluster.
func isDNSLabel(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
			if i == 0 || i == len(s)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// isGitHubLogin reports whether s looks like a GitHub account name.
func isGitHubLogin(s string) bool {
	if s == "" || len(s) > 39 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}

// RepositoryRef identifies a GitHub repository as owner/name.
type RepositoryRef struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

func (r RepositoryRef) String() string { return r.Owner + "/" + r.Name }

// IsZero reports whether the reference is empty.
func (r RepositoryRef) IsZero() bool { return r.Owner == "" && r.Name == "" }

// ParseRepositoryRef parses "owner/name".
func ParseRepositoryRef(s string) (RepositoryRef, error) {
	owner, name, ok := strings.Cut(s, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return RepositoryRef{}, Invalid("malformed_repository", "source.repository",
			fmt.Sprintf("%q is not a repository reference", s),
			`Use "owner/name", for example "AndrewMaged814/safelane-demo-api".`)
	}
	if !isGitHubLogin(owner) {
		return RepositoryRef{}, Invalid("malformed_repository", "source.repository",
			fmt.Sprintf("%q is not a valid repository owner", owner), `Use the account login exactly as GitHub shows it.`)
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
		if !ok {
			return RepositoryRef{}, Invalid("malformed_repository", "source.repository",
				fmt.Sprintf("%q is not a valid repository name", name), "Use the repository name exactly as GitHub shows it.")
		}
	}
	return RepositoryRef{Owner: owner, Name: name}, nil
}

// ImageReference is an immutable OCI image reference, decomposed.
//
// SafeLane accepts digest references only. A tag is mutable, so a tagged reference
// is not an identity and is rejected at intake rather than resolved.
type ImageReference struct {
	Registry   string `json:"registry"`   // e.g. "ghcr.io"
	Repository string `json:"repository"` // e.g. "andrewmaged814/safelane-demo-api"
	Digest     string `json:"digest"`     // e.g. "sha256:<64 hex>"
}

// String rebuilds the canonical "<registry>/<repository>@<digest>" form.
func (r ImageReference) String() string {
	if r.Registry == "" && r.Repository == "" && r.Digest == "" {
		return ""
	}
	return r.Registry + "/" + r.Repository + "@" + r.Digest
}

// IsZero reports whether the reference is empty.
func (r ImageReference) IsZero() bool { return r.Digest == "" && r.Repository == "" }

// ParseImageReference parses an immutable OCI reference of the form
// "<registry>/<repository>@sha256:<64 hex>".
//
// The reference is deliberately the caller's *only* artifact field. SafeLane derives
// the expected registry and repository from it and compares them against operator
// configuration during verification; it does not accept a caller-declared "expected
// repository", because a claim cannot validate itself.
func ParseImageReference(s string) (ImageReference, error) {
	const field = "artifact.image_reference"
	if s == "" {
		return ImageReference{}, Invalid("missing_image_reference", field,
			"no image reference was supplied",
			"Supply the immutable digest reference published by CI, for example ghcr.io/owner/safelane-demo-api@sha256:<digest>.")
	}
	name, digest, ok := strings.Cut(s, "@")
	if !ok {
		return ImageReference{}, Invalid("mutable_image_reference", field,
			fmt.Sprintf("%q is not an immutable reference", s),
			"Supply a digest reference (repository@sha256:<digest>). Tags are mutable and are never accepted.")
	}
	if strings.Contains(digest, "@") {
		return ImageReference{}, Invalid("malformed_image_reference", field,
			fmt.Sprintf("%q contains more than one digest separator", s),
			"Supply exactly one repository@sha256:<digest> reference.")
	}
	if !IsContentDigest(digest) {
		return ImageReference{}, Invalid("malformed_image_digest", field,
			fmt.Sprintf("%q is not a sha256 digest", digest),
			"Use the sha256:<64 lowercase hex> digest emitted by the build/push step.")
	}
	registry, repository, ok := strings.Cut(name, "/")
	if !ok || repository == "" {
		return ImageReference{}, Invalid("malformed_image_reference", field,
			fmt.Sprintf("%q has no registry host", s),
			"Include the registry host, for example ghcr.io/owner/safelane-demo-api@sha256:<digest>.")
	}
	if strings.Contains(repository, ":") {
		return ImageReference{}, Invalid("mutable_image_reference", field,
			fmt.Sprintf("%q carries a tag as well as a digest", s),
			"Remove the tag. SafeLane binds a release to a digest only.")
	}
	if strings.Contains(registry, ":") {
		return ImageReference{}, Invalid("unsupported_registry_port", field,
			fmt.Sprintf("registry %q specifies a port", registry),
			"Use a registry host without a port. Port-qualified registries are out of scope for this phase.")
	}
	if !strings.Contains(registry, ".") && registry != "localhost" {
		return ImageReference{}, Invalid("malformed_image_reference", field,
			fmt.Sprintf("%q does not look like a registry host", registry),
			"Include the registry host, for example ghcr.io/owner/safelane-demo-api@sha256:<digest>.")
	}
	if err := validateImagePathSegment(registry, field); err != nil {
		return ImageReference{}, err
	}
	if err := validateImagePathSegment(repository, field); err != nil {
		return ImageReference{}, err
	}
	return ImageReference{Registry: registry, Repository: repository, Digest: digest}, nil
}

// validateImagePathSegment restricts registry/repository characters to the set that
// is safe to substitute into rendered YAML without escaping.
func validateImagePathSegment(s, field string) error {
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_' || c == '/'
		if !ok {
			return Invalid("unsafe_image_reference", field,
				fmt.Sprintf("%q contains a character SafeLane will not substitute into rendered YAML", s),
				"Use a lowercase registry/repository path.")
		}
	}
	return nil
}
