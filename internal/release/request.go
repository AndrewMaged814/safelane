package release

import (
	"fmt"
	"time"
)

// RequestSchemaVersion is the value a Release Request must carry in its
// "schema_version" field. Intake rejects any other value as malformed rather than
// guessing at an older or newer shape.
const RequestSchemaVersion = "safelane.release.request/v1"

// Target identifies exactly one release destination.
//
// SafeLane never infers a target. All four components are required, because a
// release bound to three of them could be replayed against a fourth.
type Target struct {
	Application string `json:"application"`
	Environment string `json:"environment"`
	Cluster     string `json:"cluster"`
	Namespace   string `json:"namespace"`
	Rollout     string `json:"rollout,omitempty"`
}

func (t Target) String() string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", t.Application, t.Environment, t.Cluster, t.Namespace, t.Rollout)
}

// IsZero reports whether no component was supplied.
func (t Target) IsZero() bool { return t == Target{} }

// Validate checks that every component is present and is a safe DNS label. Every
// component is substituted into rendered YAML, so the character set is enforced here
// rather than at the renderer's discretion.
func (t Target) Validate() error {
	var errs Errors
	for _, f := range []struct {
		name  string
		value string
	}{
		{"target.application", t.Application},
		{"target.environment", t.Environment},
		{"target.cluster", t.Cluster},
		{"target.namespace", t.Namespace},
	} {
		if f.value == "" {
			errs = append(errs, Invalid("missing_target_component", f.name,
				"target identity is incomplete",
				"Supply application, environment, cluster and namespace. SafeLane will not infer a release target."))
			continue
		}
		if !isDNSLabel(f.value) {
			errs = append(errs, Invalid("unsafe_target_component", f.name,
				fmt.Sprintf("%q is not a lowercase DNS label", f.value),
				"Use lowercase letters, digits and hyphens (RFC 1123 label)."))
		}
	}
	return errs.OrNil()
}

// ClaimedSource is the caller's assertion about the reviewed source revision.
// Unverified. SafeLane checks it against GitHub before any of it is believed.
type ClaimedSource struct {
	// Repository is "owner/name" on GitHub.
	Repository string `json:"repository"`
	// BaseBranch is the branch the pull request merged into. SafeLane's verified
	// source revision is the merge commit on this branch, never a PR head.
	BaseBranch string `json:"base_branch"`
	// MergeCommitSHA is the claimed merge commit produced by the pull request.
	MergeCommitSHA string `json:"merge_commit_sha"`
}

// ClaimedReview is the caller's assertion about pull-request review evidence.
// Unverified.
type ClaimedReview struct {
	PullRequestNumber int    `json:"pull_request_number"`
	PullRequestURL    string `json:"pull_request_url"`
	// Author and Approver are claims recorded so a rejection can name them.
	// SafeLane re-reads both from GitHub and enforces approver != author itself.
	Author   string `json:"author"`
	Approver string `json:"approver"`
}

// ClaimedCI is the caller's assertion about the required check run. Unverified.
type ClaimedCI struct {
	// Workflow is the workflow file or display name, e.g. "publish".
	Workflow string `json:"workflow"`
	// CheckName is the required check run name SafeLane must find *for the merge
	// commit SHA*. A passing check on the pull request head does not satisfy it.
	CheckName string `json:"check_name"`
	RunID     int64  `json:"run_id,omitempty"`
	RunURL    string `json:"run_url,omitempty"`
}

// ClaimedArtifact is the caller's assertion about the deployable artifact.
// Unverified.
//
// It carries one field on purpose. A caller does not get to declare the "expected"
// registry or repository alongside the reference it is asserting; the expected
// repository is operator configuration, checked during verification.
type ClaimedArtifact struct {
	ImageReference string `json:"image_reference"`
}

// CallerKind labels the sort of caller, for proof and audit only. It has no effect
// on release logic: Codex, Claude Code, CI, another agent and a human are
// interchangeable callers.
type CallerKind string

const (
	CallerAgent   CallerKind = "agent"
	CallerCI      CallerKind = "ci"
	CallerHuman   CallerKind = "human"
	CallerUnknown CallerKind = "unknown"
)

// CallerIdentity is the caller's self-declared submitter identity: an
// unverified claim recorded for audit and Release Proof, carrying no
// authority of its own. This is distinct from CONTEXT.md's "Restricted
// Caller Identity" - the caller's namespace-scoped Kubernetes identity,
// used for observability only and enforced by the cluster, not by this
// struct.
type CallerIdentity struct {
	// Identity is the caller's self-declared name, e.g. "codex-cli".
	Identity string     `json:"identity"`
	Kind     CallerKind `json:"kind"`
	// Tool and ToolVersion describe the client, for reproducibility of the record.
	Tool        string `json:"tool,omitempty"`
	ToolVersion string `json:"tool_version,omitempty"`
}

// RequestMetadata carries request bookkeeping.
//
// There is deliberately no free-form map here. A labels or annotations map would be
// a hole straight through the "no caller-supplied Kubernetes configuration" rule:
// arbitrary caller strings could reach the rendered bundle. Every field is typed and
// none of them is substituted into rendered YAML.
type RequestMetadata struct {
	// RequestID is the caller's idempotency/correlation handle. It is not the
	// release ID; SafeLane mints that itself (see ReleaseID).
	RequestID string `json:"request_id"`
	// SubmittedAt is a caller claim about submission time and is recorded as such.
	// SafeLane stamps its own CreatedAt on the Release.
	SubmittedAt time.Time `json:"submitted_at"`
	// Reason is free human text for the audit trail. It is never parsed, never
	// rendered into the bundle, and never affects a decision.
	Reason string `json:"reason,omitempty"`
}

// ReleaseRequest is the caller-submitted shape: release identity and evidence only.
//
// # Why this type cannot carry Kubernetes configuration
//
// Every field is a scalar or a closed struct of scalars. There is no map[string]any,
// no json.RawMessage, no []byte, no interface, and no free-form label map anywhere in
// the graph. Manifests, patches, template selection and policy selection therefore
// have no representation here - not "are validated away", but have nowhere to live.
//
// That alone would let an intake layer *silently drop* such fields, which the
// specification forbids. So the intake boundary (a separate ticket item) must:
//
//  1. decode the raw JSON with (*json.Decoder).DisallowUnknownFields, and
//  2. screen the raw payload against [ForbiddenRequestKeys] first, so that a caller
//     who sent "manifests" or "patch" gets a CategoryForbiddenField rejection naming
//     the field, rather than a generic "unknown field" message.
//
// Step 2 exists for actionability, not for safety; step 1 plus this type's shape is
// what makes the guarantee structural.
type ReleaseRequest struct {
	SchemaVersion string          `json:"schema_version"`
	Target        Target          `json:"target"`
	Source        ClaimedSource   `json:"source"`
	Review        ClaimedReview   `json:"review"`
	CI            ClaimedCI       `json:"ci"`
	Artifact      ClaimedArtifact `json:"artifact"`
	Caller        CallerIdentity  `json:"caller"`
	Metadata      RequestMetadata `json:"metadata"`
}

// ForbiddenRequestKeys lists the JSON keys intake screens for at the request's top
// level and rejects with CategoryForbiddenField if a caller includes them.
//
// The screen is scoped to the top level, not "any depth": this list includes "kind",
// which is both a Kubernetes object field SafeLane must reject and the JSON key of
// [CallerIdentity.Kind] - a legitimate field nested under "caller" on every valid
// request. Screening at any depth would reject "caller.kind" on every real
// submission, including the fixture. A forbidden field nested inside a legitimate
// sub-object instead has nowhere to decode into and is rejected as malformed by
// intake's strict, recursive (*json.Decoder).DisallowUnknownFields decode - a
// different category, but rejected either way, never silently dropped.
//
// Intake screens raw JSON against this list *before* decoding into [ReleaseRequest],
// so a caller who put a forbidden field at the top level is told exactly what it may
// not send, rather than getting a generic "unknown field" message. Extend this list
// rather than inventing a second denylist elsewhere.
func ForbiddenRequestKeys() []string {
	return []string{
		// Kubernetes objects, in any of the shapes a caller might reach for.
		"manifest", "manifests", "resources", "objects", "kubernetes", "k8s",
		"rollout", "deployment", "service", "ingress", "analysistemplate",
		"apiVersion", "kind", "spec", "podTemplate", "containers", "image",
		// Patches.
		"patch", "patches", "jsonPatch", "json_patch", "strategicMergePatch",
		"strategic_merge_patch", "overlay", "overlays", "kustomization",
		"values", "helmValues", "helm_values",
		// Template selection.
		"template", "templates", "templateRef", "template_ref", "templateVersion",
		"template_version", "releaseTemplate", "release_template", "chart",
		// Policy selection.
		"policy", "policies", "policyRef", "policy_ref", "policyVersion",
		"policy_version", "riskOverride", "risk_override", "severity", "risk",
		"approval", "approvals", "waiver", "exception",
		// Lane selection. The lane is selected by assessment, never
		// requested: a caller naming one is the schema-level form of asking
		// for a wider rollout than its change earned.
		"lane", "lanes", "weights",
		// Execution shaping. A caller does not choose its own rollout envelope.
		"stages", "trafficWeight", "traffic_weight", "steps", "envelope",
		"autoPromote", "auto_promote",
		// Evidence and operator facts. The caller submits intent only;
		// SafeLane collects these itself.
		"target", "source", "review", "ci", "artifact", "caller", "metadata",
		"approver", "merge_commit_sha", "check_name", "run_id", "cluster",
		"namespace", "evidence", "checks", "digest", "approved",
	}
}

// Validate reports every structural problem with the request at once, so an agent
// can correct all of them in a single pass.
//
// Validate checks shape and internal consistency only. It verifies nothing: no
// GitHub call, no registry call, no claim believed. Verification is a separate step
// whose only output is a [ReleaseEvidence].
func (r ReleaseRequest) Validate() error {
	var errs Errors

	if r.SchemaVersion != RequestSchemaVersion {
		errs = append(errs, Malformed("unsupported_schema_version", "schema_version",
			fmt.Sprintf("schema version %q is not supported", r.SchemaVersion),
			fmt.Sprintf("Set schema_version to %q.", RequestSchemaVersion)))
	}

	if err := r.Target.Validate(); err != nil {
		errs = append(errs, flatten(err)...)
	}

	if _, err := ParseRepositoryRef(r.Source.Repository); err != nil {
		errs = append(errs, flatten(err)...)
	}
	if r.Source.BaseBranch == "" {
		errs = append(errs, Invalid("missing_base_branch", "source.base_branch",
			"no base branch was supplied",
			`Set base_branch to the branch the pull request merged into, normally "main".`))
	}
	if !IsCommitSHA(r.Source.MergeCommitSHA) {
		errs = append(errs, Invalid("malformed_merge_commit_sha", "source.merge_commit_sha",
			fmt.Sprintf("%q is not a full 40-character commit SHA", r.Source.MergeCommitSHA),
			"Supply the full merge commit SHA on the base branch. The pull request head SHA is not the verified source revision."))
	}

	if r.Review.PullRequestNumber <= 0 {
		errs = append(errs, Invalid("missing_pull_request", "review.pull_request_number",
			"no pull request number was supplied",
			"Supply the number of the merged pull request that produced the merge commit."))
	}
	if !isGitHubLogin(r.Review.Author) {
		errs = append(errs, Invalid("missing_pull_request_author", "review.author",
			"no pull request author was supplied",
			"Supply the pull request author's GitHub login."))
	}
	if !isGitHubLogin(r.Review.Approver) {
		errs = append(errs, Invalid("missing_reviewer", "review.approver",
			"no approving reviewer was supplied",
			"Supply the approving reviewer's GitHub login. Self-approval is not review evidence."))
	}

	if r.CI.CheckName == "" {
		errs = append(errs, Invalid("missing_required_check", "ci.check_name",
			"no required check name was supplied",
			"Supply the name of the required check run that must have succeeded for the merge commit SHA."))
	}

	if _, err := ParseImageReference(r.Artifact.ImageReference); err != nil {
		errs = append(errs, flatten(err)...)
	}

	if r.Caller.Identity == "" {
		errs = append(errs, Invalid("missing_caller_identity", "caller.identity",
			"no caller identity was supplied",
			"Identify the caller. Caller identity does not change release logic, but it is recorded in Release Proof."))
	}
	if r.Metadata.RequestID == "" {
		errs = append(errs, Invalid("missing_request_id", "metadata.request_id",
			"no request id was supplied",
			"Supply a caller-generated request id for correlation. It is not the release id; SafeLane mints that."))
	}

	return errs.OrNil()
}

// ValidateIdentity checks the fields SafeLane records on every Release:
// schema, target, repository, pull request, caller, and request id.
// Merge SHA, approver, check name, and image are evidence, not identity.
func (r ReleaseRequest) ValidateIdentity() error {
	var errs Errors

	if r.SchemaVersion != RequestSchemaVersion {
		errs = append(errs, Malformed("unsupported_schema_version", "schema_version",
			fmt.Sprintf("schema version %q is not supported", r.SchemaVersion),
			fmt.Sprintf("Set schema_version to %q.", RequestSchemaVersion)))
	}
	if err := r.Target.Validate(); err != nil {
		errs = append(errs, flatten(err)...)
	}
	if r.Source.Repository != "" {
		if _, err := ParseRepositoryRef(r.Source.Repository); err != nil {
			errs = append(errs, flatten(err)...)
		}
	}
	if r.Review.PullRequestNumber <= 0 {
		errs = append(errs, Invalid("missing_pull_request", "review.pull_request_number",
			"no pull request number was supplied",
			"Supply the number of the merged pull request."))
	}
	if r.Caller.Identity == "" {
		errs = append(errs, Invalid("missing_caller_identity", "caller.identity",
			"no caller identity was supplied",
			"Identify the caller. Caller identity does not change release logic, but it is recorded in Release Proof."))
	}
	if r.Metadata.RequestID == "" {
		errs = append(errs, Invalid("missing_request_id", "metadata.request_id",
			"no request id was supplied",
			"Supply a caller-generated request id for correlation. It is not the release id; SafeLane mints that."))
	}
	return errs.OrNil()
}

// ImageReference returns the parsed claimed artifact reference.
func (r ReleaseRequest) ImageReference() (ImageReference, error) {
	return ParseImageReference(r.Artifact.ImageReference)
}

// Repository returns the parsed claimed source repository.
func (r ReleaseRequest) Repository() (RepositoryRef, error) {
	return ParseRepositoryRef(r.Source.Repository)
}

// flatten is a package-local alias for Flatten, kept so call sites inside
// this package read the same as before Flatten was exported for intake.
func flatten(err error) Errors { return Flatten(err) }
