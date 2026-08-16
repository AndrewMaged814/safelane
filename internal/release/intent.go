package release

import "fmt"

// Intent is the caller-facing Release Request: change identity and an
// environment selector. It does not carry evidence. SafeLane collects and
// verifies evidence itself.
type Intent struct {
	SchemaVersion string `json:"schema_version"`
	Repository    string `json:"repository,omitempty"`
	PullRequest   int    `json:"pull_request"`
	Environment   string `json:"environment,omitempty"`
	// Image is an optional immutable digest pin. When set, SafeLane still
	// resolves the digest for the merge and rejects a mismatch.
	Image string `json:"image,omitempty"`
}

// Validate checks caller-supplied identity only. It verifies nothing.
func (in Intent) Validate() error {
	var errs Errors

	if in.SchemaVersion != RequestSchemaVersion {
		errs = append(errs, Malformed("unsupported_schema_version", "schema_version",
			fmt.Sprintf("schema version %q is not supported", in.SchemaVersion),
			fmt.Sprintf("Set schema_version to %q.", RequestSchemaVersion)))
	}

	if in.PullRequest <= 0 {
		errs = append(errs, Invalid("missing_pull_request", "pull_request",
			"no pull request number was supplied",
			"Identify the merged pull request. SafeLane collects evidence; do not author claims."))
	}

	if in.Repository != "" {
		if _, err := ParseRepositoryRef(in.Repository); err != nil {
			errs = append(errs, Invalid("malformed_repository", "repository",
				fmt.Sprintf("%q is not a repository reference", in.Repository),
				`Use "owner/name", for example "AndrewMaged814/podinfo".`))
		}
	}

	if in.Environment != "" && !isDNSLabel(in.Environment) {
		errs = append(errs, Invalid("unsafe_environment", "environment",
			fmt.Sprintf("%q is not a lowercase DNS label", in.Environment),
			"Use lowercase letters, digits and hyphens (RFC 1123 label)."))
	}

	if in.Image != "" {
		if _, err := ParseImageReference(in.Image); err != nil {
			errs = append(errs, flatten(err)...)
		}
	}

	return errs.OrNil()
}
