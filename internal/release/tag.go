package release

import "strings"

// Tag is the Appendix C4 category a rejection is reported under: the
// bracketed word in `- [config] missing_project_config (project)`.
//
// It is deliberately not [Error.Category]. Category is the internal
// severity class a caller branches on (is this a forbidden field? is this
// unknown evidence?); Tag is the *subsystem* a reader is being pointed
// at, and the two do not map one to one. CategoryInvalidRequest, for
// instance, covers both an operator configuration file that is missing
// and a request value that is malformed, which are [config] and [schema]
// respectively. The reason code is what separates them, so the code is
// what this reads first.
func (e *Error) Tag() string {
	if e == nil {
		return ""
	}
	if tag, ok := codeTags[e.Code]; ok {
		return tag
	}
	switch e.Category {
	case CategoryForbiddenField, CategoryMalformedRequest, CategoryInvalidRequest:
		return TagSchema
	case CategoryEvidenceMissing, CategoryEvidenceFailed, CategoryEvidenceUnknown:
		// Artifact evidence is the registry's answer; everything else in
		// this bucket came from GitHub.
		if strings.HasPrefix(e.Field, "artifact") || strings.HasPrefix(e.Field, "evidence.artifact") {
			return TagGHCR
		}
		return TagGitHub
	case CategoryRenderFailed:
		return TagConfig
	default:
		return TagInternal
	}
}

// The Appendix C4 tags. There is no fourth source of truth for these
// strings: output, the record, and this table all use these constants.
const (
	TagConfig   = "config"
	TagSchema   = "schema"
	TagGitHub   = "github"
	TagGHCR     = "ghcr"
	TagPolicy   = "policy"
	TagAssess   = "assess"
	TagState    = "state"
	TagExecute  = "execute"
	TagInternal = "internal"
)

// codeTags is Appendix C4's reason-code catalogue, plus the codes this
// build emits that the catalogue groups under one of its rows. A code
// absent from here falls back to its category, which is the coarser but
// never-wrong answer.
var codeTags = map[string]string{
	// config: the operator's own files.
	"missing_project_config":      TagConfig,
	"unreadable_project_config":   TagConfig,
	"invalid_project_config":      TagConfig,
	"unsupported_project_version": TagConfig,
	"missing_project_field":       TagConfig,
	"unsafe_project_field":        TagConfig,
	"malformed_image_repository":  TagConfig,
	"missing_policy_config":       TagConfig,
	"unreadable_policy_config":    TagConfig,
	"invalid_policy_config":       TagConfig,
	"missing_policy_field":        TagConfig,
	"undeclared_lane":             TagConfig,
	"empty_lane":                  TagConfig,
	"invalid_risk_level":          TagConfig,
	"invalid_duration":            TagConfig,

	// schema: the shape of the request a caller sent.
	"unknown_field":              TagSchema,
	"invalid_json":               TagSchema,
	"invalid_request_shape":      TagSchema,
	"unsupported_schema_version": TagSchema,

	// github / ghcr: evidence SafeLane went and looked for.
	"pull_request_not_merged": TagGitHub,
	"required_check_failed":   TagGitHub,
	"required_check_missing":  TagGitHub,
	"github_unreachable":      TagGitHub,
	"rate_limited":            TagGitHub,
	"digest_not_found":        TagGHCR,
	"ghcr_unreachable":        TagGHCR,

	// policy: the decision itself.
	"all_mandatory_evidence_verified": TagPolicy,
	"requirement_failed":              TagPolicy,
	"verification_incomplete":         TagPolicy,
	"release_not_eligible":            TagPolicy,
	"transition_exceeds_envelope":     TagPolicy,
	"transition_not_permitted":        TagPolicy,

	// assess: the two assessors.
	"heuristic_failed":  TagAssess,
	"model_unavailable": TagAssess,
}
