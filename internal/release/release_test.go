package release_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
)

const (
	mergeSHA = "4f0c1b9e7ac2d5386b1d9f4a5c8e2b7d3a6f0e91"
	otherSHA = "aa0c1b9e7ac2d5386b1d9f4a5c8e2b7d3a6f0e91"
	digestA  = "sha256:3fbc1d9a7e42c8056d1f9b3e7a5c204d8e6b1f39a7c50d28e4b6f19a3c7d50e8"
	digestB  = "sha256:0011223344556677889900aabbccddeeff00112233445566778899aabbccddee"
	imageRef = "ghcr.io/andrewmaged814/podinfo@" + digestA
)

var testTime = time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)

func validTarget() release.Target {
	return release.Target{Application: "podinfo", Environment: "production", Cluster: "safelane-demo", Namespace: "podinfo"}
}

func validRequest() release.ReleaseRequest {
	return release.ReleaseRequest{
		SchemaVersion: release.RequestSchemaVersion,
		Target:        validTarget(),
		Source: release.ClaimedSource{
			Repository:     "AndrewMaged814/podinfo",
			BaseBranch:     "main",
			MergeCommitSHA: mergeSHA,
		},
		Review: release.ClaimedReview{
			PullRequestNumber: 1,
			PullRequestURL:    "https://github.com/AndrewMaged814/podinfo/pull/1",
			Author:            "AndrewMaged814",
			Approver:          "ahmed",
		},
		CI:       release.ClaimedCI{Workflow: "publish", CheckName: "publish / build-and-push", RunID: 16453210987},
		Artifact: release.ClaimedArtifact{ImageReference: imageRef},
		Caller:   release.CallerIdentity{Identity: "codex-cli", Kind: release.CallerAgent},
		Metadata: release.RequestMetadata{RequestID: "req-fixture-0001", SubmittedAt: testTime},
	}
}

func validEvidenceInput() release.EvidenceInput {
	return release.EvidenceInput{
		Repository: release.RepositoryRef{Owner: "AndrewMaged814", Name: "podinfo"},
		PullRequest: release.VerifiedPullRequest{
			Number: 1, URL: "https://github.com/AndrewMaged814/podinfo/pull/1",
			Author: "AndrewMaged814", BaseBranch: "main", MergedAt: testTime,
		},
		Approval:       release.VerifiedApproval{Reviewer: "ahmed", ApprovedAt: testTime},
		MergeCommitSHA: mergeSHA,
		RequiredCheck: release.VerifiedCheckRun{
			Name: "publish / build-and-push", HeadSHA: mergeSHA,
			Conclusion: release.CheckConclusionSuccess, RunID: 16453210987, CompletedAt: testTime,
		},
		Artifact: release.VerifiedArtifact{
			Reference:      release.ImageReference{Registry: "ghcr.io", Repository: "andrewmaged814/podinfo", Digest: digestA},
			ObservedDigest: digestA,
			ResolvedAt:     testTime,
		},
		VerifiedAt: testTime,
	}
}

func mustEvidence(t *testing.T, in release.EvidenceInput) release.ReleaseEvidence {
	t.Helper()
	ev, err := release.NewReleaseEvidence(in)
	if err != nil {
		t.Fatalf("NewReleaseEvidence: %v", err)
	}
	return ev
}

func mustVerified(t *testing.T, ev release.ReleaseEvidence) release.EvidenceResult {
	t.Helper()
	res, err := release.VerifiedEvidence(ev)
	if err != nil {
		t.Fatalf("VerifiedEvidence: %v", err)
	}
	return res
}

func mustBundle(t *testing.T, target release.Target, digest string) *release.RenderedBundle {
	t.Helper()
	body := []byte("apiVersion: argoproj.io/v1alpha1\nkind: Rollout\nmetadata:\n  name: podinfo\n  namespace: " +
		target.Namespace + "\nspec:\n  image: ghcr.io/andrewmaged814/podinfo@" + digest + "\n")
	res, err := release.NewRenderedResource(release.ResourceRef{
		TemplatePath: "40-rollout.yaml.tmpl", APIVersion: "argoproj.io/v1alpha1",
		Kind: "Rollout", Namespace: target.Namespace, Name: "podinfo",
	}, body)
	if err != nil {
		t.Fatalf("NewRenderedResource: %v", err)
	}
	tmplID := release.TemplateIdentity{Name: "podinfo-canary", Version: "v0.1.0-fixture", ContentDigest: digestB, FileCount: 7}
	bundle, err := release.NewRenderedBundle(tmplID, target, digest, []release.RenderedResource{res})
	if err != nil {
		t.Fatalf("NewRenderedBundle: %v", err)
	}
	return &bundle
}

func mustID(t *testing.T) release.ReleaseID {
	t.Helper()
	id, err := release.NewReleaseID(testTime, bytes.NewReader([]byte("0123456789")))
	if err != nil {
		t.Fatalf("NewReleaseID: %v", err)
	}
	return id
}

func eligibilityFor(t *testing.T, ev release.EvidenceResult) release.Eligibility {
	t.Helper()
	switch ev.Outcome() {
	case release.EvidenceVerified:
		env, err := release.NewRolloutEnvelope([]int{5, 25, 50, 100}, "start")
		if err != nil {
			t.Fatalf("NewRolloutEnvelope: %v", err)
		}
		elig, err := release.Eligible("1", "all_mandatory_evidence_verified",
			"All configured mandatory evidence verified.", env)
		if err != nil {
			t.Fatalf("Eligible: %v", err)
		}
		return elig
	case release.EvidenceUnknown:
		elig, err := release.Indeterminate("1", "verification_incomplete",
			"Verification could not be completed. Retry once GitHub and GHCR are reachable.")
		if err != nil {
			t.Fatalf("Indeterminate: %v", err)
		}
		return elig
	default:
		elig, err := release.Ineligible("1", "requirement_failed",
			"A mandatory evidence requirement failed.")
		if err != nil {
			t.Fatalf("Ineligible: %v", err)
		}
		return elig
	}
}

func newRelease(t *testing.T, p release.ReleaseParams) (*release.Release, error) {
	t.Helper()
	if p.Eligibility.IsZero() {
		p.Eligibility = eligibilityFor(t, p.Evidence)
	}
	return release.NewRelease(p)
}

// ---------------------------------------------------------------- release id

func TestReleaseIDIsSortableAndParseable(t *testing.T) {
	entropy := bytes.NewReader(bytes.Repeat([]byte{0x00}, 64))
	earlier, err := release.NewReleaseID(testTime, entropy)
	if err != nil {
		t.Fatalf("NewReleaseID: %v", err)
	}
	later, err := release.NewReleaseID(testTime.Add(time.Second), entropy)
	if err != nil {
		t.Fatalf("NewReleaseID: %v", err)
	}
	if !(string(earlier) < string(later)) {
		t.Errorf("ids do not sort by creation time: %s !< %s", earlier, later)
	}
	if _, err := release.ParseReleaseID(string(earlier)); err != nil {
		t.Errorf("ParseReleaseID(%s): %v", earlier, err)
	}
	got, err := earlier.Time()
	if err != nil {
		t.Fatalf("Time(): %v", err)
	}
	if !got.Equal(testTime) {
		t.Errorf("embedded time = %s, want %s", got, testTime)
	}
	if !strings.HasPrefix(string(earlier), release.ReleaseIDPrefix) {
		t.Errorf("%s does not carry the %q prefix", earlier, release.ReleaseIDPrefix)
	}
}

func TestReleaseIDsDoNotCollideForTheSameTarget(t *testing.T) {
	seen := make(map[release.ReleaseID]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id, err := release.MintReleaseID()
		if err != nil {
			t.Fatalf("MintReleaseID: %v", err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate release id %s after %d mints", id, i)
		}
		seen[id] = struct{}{}
	}
}

func TestParseReleaseIDRejectsMalformed(t *testing.T) {
	for _, bad := range []string{
		"", "rel_", "01ARZ3NDEKTSV4RRFFQ69G5FAV", "rel_01ARZ3NDEKTSV4RRFFQ69G5FA",
		"rel_01ARZ3NDEKTSV4RRFFQ69G5FAVX", "rel_01ARZ3NDEKTSV4RRFFQ69G5FAU",
		"rel_81ARZ3NDEKTSV4RRFFQ69G5FAV", "release_01ARZ3NDEKTSV4RRFFQ69G5FAV",
	} {
		if _, err := release.ParseReleaseID(bad); err == nil {
			t.Errorf("ParseReleaseID(%q) = nil error, want rejection", bad)
		} else if !errors.Is(err, release.ErrInvalidRequest) {
			t.Errorf("ParseReleaseID(%q): category = %q, want invalid_request", bad, release.Categorize(err))
		}
	}
}

// ---------------------------------------------------------------- request

func TestValidRequestPasses(t *testing.T) {
	if err := validRequest().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestRequestValidationRejects(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*release.ReleaseRequest)
		wantCode string
		wantErr  error
	}{
		{"mutable tag", func(r *release.ReleaseRequest) {
			r.Artifact.ImageReference = "ghcr.io/andrewmaged814/podinfo:1.2.3"
		}, "mutable_image_reference", release.ErrInvalidRequest},
		{"tag and digest", func(r *release.ReleaseRequest) {
			r.Artifact.ImageReference = "ghcr.io/andrewmaged814/podinfo:1.2.3@" + digestA
		}, "mutable_image_reference", release.ErrInvalidRequest},
		{"no registry host", func(r *release.ReleaseRequest) {
			r.Artifact.ImageReference = "podinfo@" + digestA
		}, "malformed_image_reference", release.ErrInvalidRequest},
		{"missing namespace", func(r *release.ReleaseRequest) {
			r.Target.Namespace = ""
		}, "missing_target_component", release.ErrInvalidRequest},
		{"unsafe namespace", func(r *release.ReleaseRequest) {
			r.Target.Namespace = "podinfo\n  privileged: true"
		}, "unsafe_target_component", release.ErrInvalidRequest},
		{"abbreviated merge sha", func(r *release.ReleaseRequest) {
			r.Source.MergeCommitSHA = "4f0c1b9"
		}, "malformed_merge_commit_sha", release.ErrInvalidRequest},
		{"missing reviewer", func(r *release.ReleaseRequest) {
			r.Review.Approver = ""
		}, "missing_reviewer", release.ErrInvalidRequest},
		{"missing required check", func(r *release.ReleaseRequest) {
			r.CI.CheckName = ""
		}, "missing_required_check", release.ErrInvalidRequest},
		{"missing caller", func(r *release.ReleaseRequest) {
			r.Caller.Identity = ""
		}, "missing_caller_identity", release.ErrInvalidRequest},
		{"wrong schema version", func(r *release.ReleaseRequest) {
			r.SchemaVersion = "safelane.release.request/v0"
		}, "unsupported_schema_version", release.ErrMalformedRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			tc.mutate(&req)
			err := req.Validate()
			if err == nil {
				t.Fatal("expected rejection")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("category = %q, want %v", release.Categorize(err), tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantCode) {
				t.Errorf("error = %v, want code %q", err, tc.wantCode)
			}
		})
	}
}

// TestRequestReportsEveryProblemAtOnce matters for the "typed, actionable errors"
// requirement: an agent should be able to fix everything in one pass.
func TestRequestReportsEveryProblemAtOnce(t *testing.T) {
	req := validRequest()
	req.Target.Namespace = ""
	req.Source.MergeCommitSHA = ""
	req.Artifact.ImageReference = "ghcr.io/andrewmaged814/podinfo:latest"

	err := req.Validate()
	var errs release.Errors
	if !errors.As(err, &errs) {
		t.Fatalf("Validate returned %T, want release.Errors", err)
	}
	if len(errs) < 3 {
		t.Errorf("got %d rejections, want at least 3: %v", len(errs), errs)
	}
	for _, e := range errs {
		if e.Remedy == "" {
			t.Errorf("rejection %s has no remedy", e.Code)
		}
		if e.Field == "" {
			t.Errorf("rejection %s names no field", e.Code)
		}
	}
}

// TestForbiddenRequestKeysCoverTheFourForbiddenClasses checks that the denylist intake
// screens against actually names Kubernetes objects, patches, template selection and
// policy selection.
func TestForbiddenRequestKeysCoverTheFourForbiddenClasses(t *testing.T) {
	keys := release.ForbiddenRequestKeys()
	index := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		index[k] = struct{}{}
	}
	for _, required := range []string{
		"manifests", "kubernetes", "apiVersion", "kind", "spec", // Kubernetes objects
		"patch", "patches", "json_patch", "overlay", // patches
		"template", "template_version", "release_template", // template selection
		"policy", "policy_version", "risk_override", // policy selection
	} {
		if _, ok := index[required]; !ok {
			t.Errorf("ForbiddenRequestKeys() does not include %q", required)
		}
	}
}

// TestReleaseRequestCannotCarryKubernetesConfiguration is the structural half of the
// guarantee: unknown keys have nowhere to land, so even a decoder that tolerates them
// cannot smuggle configuration into the type.
func TestReleaseRequestCannotCarryKubernetesConfiguration(t *testing.T) {
	payload := `{
	  "schema_version": "safelane.release.request/v1",
	  "manifests": [{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"evil"}}],
	  "patch": {"spec":{"replicas":100}},
	  "template": "attacker-owned",
	  "policy": "permit-everything"
	}`

	// A tolerant decoder (encoding/json's default) drops the forbidden keys because
	// ReleaseRequest has no field, map or raw-JSON member they could bind to.
	var tolerant release.ReleaseRequest
	if err := json.Unmarshal([]byte(payload), &tolerant); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, err := json.Marshal(tolerant); err != nil {
		t.Fatalf("re-marshal: %v", err)
	} else {
		for _, forbidden := range []string{"evil", "attacker-owned", "permit-everything", "replicas"} {
			if bytes.Contains(got, []byte(forbidden)) {
				t.Errorf("ReleaseRequest retained forbidden content %q: %s", forbidden, got)
			}
		}
	}

	// The strict decoder intake is required to use rejects them instead of dropping
	// them, which is what makes the rejection visible to the caller.
	dec := json.NewDecoder(strings.NewReader(payload))
	dec.DisallowUnknownFields()
	var strict release.ReleaseRequest
	if err := dec.Decode(&strict); err == nil {
		t.Error("DisallowUnknownFields accepted a payload carrying Kubernetes configuration")
	}
}

// TestFixtureReleaseIntentIsValid keeps testdata/release-request.json honest.
func TestFixtureReleaseIntentIsValid(t *testing.T) {
	const path = "../../testdata/release-request.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var intent release.Intent
	if err := dec.Decode(&intent); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if err := intent.Validate(); err != nil {
		t.Fatalf("%s is not a valid Release Request: %v", path, err)
	}
	if intent.PullRequest != 1 {
		t.Errorf("fixture pull_request = %d, want 1", intent.PullRequest)
	}
}

// ---------------------------------------------------------------- evidence

func TestNewReleaseEvidenceAcceptsCompleteEvidence(t *testing.T) {
	ev := mustEvidence(t, validEvidenceInput())
	if ev.IsZero() {
		t.Fatal("constructed evidence reports itself as unset")
	}
	if ev.MergeCommitSHA() != mergeSHA {
		t.Errorf("MergeCommitSHA = %q", ev.MergeCommitSHA())
	}
	if ev.ArtifactDigest() != digestA {
		t.Errorf("ArtifactDigest = %q", ev.ArtifactDigest())
	}
	if !ev.IndependentApproval() {
		t.Error("IndependentApproval = false for an approver who is not the author")
	}
}

func TestNewReleaseEvidence_AllowsMissingApproval(t *testing.T) {
	in := validEvidenceInput()
	in.Approval = release.VerifiedApproval{}
	ev := mustEvidence(t, in)
	if ev.IndependentApproval() {
		t.Error("no recorded approval is not an independent approval")
	}
}

func TestNewReleaseEvidenceRejects(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*release.EvidenceInput)
		wantCode string
	}{
		{"self approval", func(in *release.EvidenceInput) {
			in.Approval.Reviewer = in.PullRequest.Author
		}, "self_approval"},
		{"self approval differing case", func(in *release.EvidenceInput) {
			in.Approval.Reviewer = strings.ToUpper(in.PullRequest.Author)
		}, "self_approval"},
		{"unmerged pull request", func(in *release.EvidenceInput) {
			in.PullRequest.MergedAt = time.Time{}
		}, "pull_request_not_merged"},
		{"check ran on the pull request head", func(in *release.EvidenceInput) {
			in.RequiredCheck.HeadSHA = otherSHA
		}, "check_run_commit_mismatch"},
		{"check failed", func(in *release.EvidenceInput) {
			in.RequiredCheck.Conclusion = "failure"
		}, "required_check_not_successful"},
		{"check still running", func(in *release.EvidenceInput) {
			in.RequiredCheck.Conclusion = ""
		}, "required_check_not_successful"},
		{"no check", func(in *release.EvidenceInput) {
			in.RequiredCheck = release.VerifiedCheckRun{}
		}, "missing_required_check"},
		{"digest mismatch", func(in *release.EvidenceInput) {
			in.Artifact.ObservedDigest = digestB
		}, "artifact_digest_mismatch"},
		{"mutable artifact", func(in *release.EvidenceInput) {
			in.Artifact.Reference.Digest = "1.2.3"
			in.Artifact.ObservedDigest = "1.2.3"
		}, "mutable_artifact_reference"},
		{"artifact never resolved", func(in *release.EvidenceInput) {
			in.Artifact.ResolvedAt = time.Time{}
		}, "artifact_not_resolved"},
		{"abbreviated merge sha", func(in *release.EvidenceInput) {
			in.MergeCommitSHA = "4f0c1b9"
			in.RequiredCheck.HeadSHA = "4f0c1b9"
		}, "malformed_merge_commit_sha"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validEvidenceInput()
			tc.mutate(&in)
			ev, err := release.NewReleaseEvidence(in)
			if err == nil {
				t.Fatal("expected rejection")
			}
			if !ev.IsZero() {
				t.Error("a rejected input produced non-zero evidence")
			}
			if !strings.Contains(err.Error(), tc.wantCode) {
				t.Errorf("error = %v, want code %q", err, tc.wantCode)
			}
		})
	}
}

// TestUnresolvedArtifactIsUnknownNotMissing checks the category, not just the failure:
// an operational gap must land in evidence_unknown so it can never be reported as the
// milder, more specific outcome.
func TestUnresolvedArtifactIsUnknownNotMissing(t *testing.T) {
	in := validEvidenceInput()
	in.Artifact.ResolvedAt = time.Time{}
	_, err := release.NewReleaseEvidence(in)
	if err == nil {
		t.Fatal("expected rejection")
	}
	if got := release.Categorize(err); got != release.CategoryEvidenceUnknown {
		t.Errorf("category = %q, want %q", got, release.CategoryEvidenceUnknown)
	}
}

// TestEvidenceJSONRoundTripRevalidates proves a persisted record cannot be edited into
// verified evidence.
func TestEvidenceJSONRoundTripRevalidates(t *testing.T) {
	ev := mustEvidence(t, validEvidenceInput())
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var back release.ReleaseEvidence
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.MergeCommitSHA() != ev.MergeCommitSHA() || back.ArtifactDigest() != ev.ArtifactDigest() {
		t.Error("round trip lost evidence values")
	}

	forged := strings.Replace(string(raw), `"reviewer":"ahmed"`, `"reviewer":"AndrewMaged814"`, 1)
	if forged == string(raw) {
		t.Fatal("test setup: reviewer field not found in marshalled evidence")
	}
	var tampered release.ReleaseEvidence
	err = json.Unmarshal([]byte(forged), &tampered)
	if err == nil {
		t.Fatal("a self-approved record decoded into verified evidence")
	}
	if !strings.Contains(err.Error(), "self_approval") {
		t.Errorf("error = %v, want self_approval", err)
	}
}

// TestZeroEvidenceMarshalsToNull proves an unset value cannot masquerade as a hollow
// but present evidence object.
func TestZeroEvidenceMarshalsToNull(t *testing.T) {
	raw, err := json.Marshal(release.ReleaseEvidence{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != "null" {
		t.Errorf("zero evidence marshalled to %s, want null", raw)
	}
}

// ---------------------------------------------------------------- evidence result

// TestZeroEvidenceResultIsUnknown is the non-negotiable from the spec: nothing defaults
// to verified.
func TestZeroEvidenceResultIsUnknown(t *testing.T) {
	var zero release.EvidenceResult
	if zero.Outcome() != release.EvidenceUnknown {
		t.Errorf("zero outcome = %v, want unknown", zero.Outcome())
	}
	if zero.IsVerified() {
		t.Error("zero EvidenceResult reports itself verified")
	}
	if ev, ok := zero.Verified(); ok || !ev.IsZero() {
		t.Error("zero EvidenceResult yielded evidence")
	}
}

func TestNonVerifiedResultsCarryNoEvidence(t *testing.T) {
	reason := release.FailedEvidenceError("required_check_not_successful", "evidence.required_check", "ci failed", "fix ci")
	for _, res := range []release.EvidenceResult{
		release.MissingEvidence(reason),
		release.FailedEvidence(reason),
		release.UnknownEvidence(reason),
	} {
		if ev, ok := res.Verified(); ok || !ev.IsZero() {
			t.Errorf("%s result yielded evidence", res.Outcome())
		}
		if len(res.Reasons()) == 0 {
			t.Errorf("%s result recorded no reason", res.Outcome())
		}
	}
}

// TestNonVerifiedResultAlwaysHasAReason prevents a silent, unexplained withholding.
func TestNonVerifiedResultAlwaysHasAReason(t *testing.T) {
	res := release.UnknownEvidence()
	if len(res.Reasons()) != 1 {
		t.Fatalf("reasons = %v, want a synthesized reason", res.Reasons())
	}
	if release.Categorize(res.Reasons()[0]) != release.CategoryEvidenceUnknown {
		t.Errorf("synthesized reason category = %q", release.Categorize(res.Reasons()[0]))
	}
}

func TestVerifiedEvidenceRejectsUnsetEvidence(t *testing.T) {
	if _, err := release.VerifiedEvidence(release.ReleaseEvidence{}); err == nil {
		t.Fatal("VerifiedEvidence accepted unset evidence")
	} else if !errors.Is(err, release.ErrInternal) {
		t.Errorf("category = %q, want internal", release.Categorize(err))
	}
}

// TestEvidenceResultJSONRejectsVerifiedWithoutEvidence closes the persistence-side
// version of the coercion: a stored record cannot claim "verified" with nothing behind
// it.
func TestEvidenceResultJSONRejectsVerifiedWithoutEvidence(t *testing.T) {
	var res release.EvidenceResult
	err := json.Unmarshal([]byte(`{"outcome":"verified","evidence":null}`), &res)
	if err == nil {
		t.Fatal("a verified outcome with no evidence was accepted")
	}
	if !strings.Contains(err.Error(), "verified_without_evidence") {
		t.Errorf("error = %v, want verified_without_evidence", err)
	}
}

func TestEvidenceResultJSONRejectsEvidenceUnderNonVerifiedOutcome(t *testing.T) {
	ev := mustEvidence(t, validEvidenceInput())
	raw, err := json.Marshal(mustVerified(t, ev))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	forged := strings.Replace(string(raw), `"outcome":"verified"`, `"outcome":"failed"`, 1)
	var res release.EvidenceResult
	if err := json.Unmarshal([]byte(forged), &res); err == nil ||
		!strings.Contains(err.Error(), "evidence_under_non_verified_outcome") {
		t.Errorf("error = %v, want evidence_under_non_verified_outcome", err)
	}
}

func TestUnrecognizedOutcomeDecodesToUnknown(t *testing.T) {
	var o release.EvidenceOutcome = release.EvidenceVerified
	if err := json.Unmarshal([]byte(`"totally-fine-honestly"`), &o); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if o != release.EvidenceUnknown {
		t.Errorf("outcome = %v, want unknown", o)
	}
}

// ---------------------------------------------------------------- rendered bundle

func TestRenderedResourceHashIsDerivedFromItsBytes(t *testing.T) {
	ref := release.ResourceRef{TemplatePath: "a.yaml.tmpl", APIVersion: "v1", Kind: "Service", Namespace: "podinfo", Name: "podinfo-stable"}
	one, err := release.NewRenderedResource(ref, []byte("apiVersion: v1\n"))
	if err != nil {
		t.Fatalf("NewRenderedResource: %v", err)
	}
	two, err := release.NewRenderedResource(ref, []byte("apiVersion: v2\n"))
	if err != nil {
		t.Fatalf("NewRenderedResource: %v", err)
	}
	if one.Hash() == two.Hash() {
		t.Error("different bytes produced the same hash")
	}
	// Independently computed: printf 'apiVersion: v1\n' | sha256sum
	const want = "sha256:776ae142428e754b67d7d6e3dfdbe1b448f0bac355d8fa24ec9471e21d90b432"
	if one.Hash() != want {
		t.Errorf("hash = %s, want %s (sha256sum of the exact bytes)", one.Hash(), want)
	}
}

func TestRenderedResourceRejectsIncompleteRef(t *testing.T) {
	_, err := release.NewRenderedResource(release.ResourceRef{TemplatePath: "a.yaml.tmpl", APIVersion: "v1", Kind: "Service"}, []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "missing_name") {
		t.Errorf("error = %v, want missing_name", err)
	}
}

func TestRenderedBundleRejectsUnpinnedDigest(t *testing.T) {
	res, err := release.NewRenderedResource(release.ResourceRef{
		TemplatePath: "a.yaml.tmpl", APIVersion: "v1", Kind: "Service", Name: "x",
	}, []byte("apiVersion: v1\n"))
	if err != nil {
		t.Fatalf("NewRenderedResource: %v", err)
	}
	id := release.TemplateIdentity{ContentDigest: digestB, FileCount: 1}
	if _, err := release.NewRenderedBundle(id, validTarget(), "latest", []release.RenderedResource{res}); err == nil ||
		!strings.Contains(err.Error(), "unpinned_bundle") {
		t.Errorf("error = %v, want unpinned_bundle", err)
	}
}

func TestRenderedBundleJSONDetectsTamperedBytes(t *testing.T) {
	bundle := mustBundle(t, validTarget(), digestA)
	raw, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back release.RenderedBundle
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Digest() != bundle.Digest() {
		t.Errorf("bundle digest changed on round trip: %s -> %s", bundle.Digest(), back.Digest())
	}

	// Rewrite the recorded bytes while leaving every recorded hash alone. This is the
	// attack the design exists to detect: a record edited after rendering.
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	resources, ok := record["resources"].([]any)
	if !ok || len(resources) == 0 {
		t.Fatalf("record has no resources: %v", record)
	}
	first, ok := resources[0].(map[string]any)
	if !ok {
		t.Fatalf("resource 0 is %T", resources[0])
	}
	original, ok := first["bytes"].(string)
	if !ok {
		t.Fatalf("resource bytes are %T", first["bytes"])
	}
	first["bytes"] = base64.StdEncoding.EncodeToString(
		[]byte("apiVersion: argoproj.io/v1alpha1\nkind: Rollout\nmetadata:\n  name: podinfo\n  namespace: podinfo\nspec:\n  image: evil\n"))
	if first["bytes"] == original {
		t.Fatal("test setup: tampered bytes are identical to the originals")
	}
	tampered, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}

	var broken release.RenderedBundle
	err = json.Unmarshal(tampered, &broken)
	if err == nil {
		t.Fatal("tampered bundle bytes were accepted")
	}
	if !strings.Contains(err.Error(), "rendered_resource_hash_mismatch") {
		t.Errorf("error = %v, want rendered_resource_hash_mismatch", err)
	}
}

// ---------------------------------------------------------------- release record

func TestNewReleaseBindsEvidenceArtifactTargetAndBundle(t *testing.T) {
	req := validRequest()
	ev := mustEvidence(t, validEvidenceInput())
	rel, err := newRelease(t, release.ReleaseParams{
		ID:        mustID(t),
		Request:   req,
		Evidence:  mustVerified(t, ev),
		Bundle:    mustBundle(t, req.Target, digestA),
		CreatedAt: testTime,
	})
	if err != nil {
		t.Fatalf("NewRelease: %v", err)
	}
	if rel.Target() != req.Target {
		t.Errorf("Target() = %v", rel.Target())
	}
	if rel.SourceRevision() != mergeSHA {
		t.Errorf("SourceRevision() = %q", rel.SourceRevision())
	}
	if rel.ArtifactDigest() != digestA {
		t.Errorf("ArtifactDigest() = %q", rel.ArtifactDigest())
	}
	if id, ok := rel.TemplateIdentity(); !ok || id.ContentDigest == "" {
		t.Error("no template identity recorded on the Release")
	}
	if len(rel.BundleHashes()) != 1 {
		t.Errorf("BundleHashes() = %d entries, want 1", len(rel.BundleHashes()))
	}
	if _, ok := rel.Bundle(); !ok {
		t.Error("no bundle recorded on the Release")
	}
}

func TestReleaseWithoutVerifiedEvidenceHasNoBundle(t *testing.T) {
	rel, err := newRelease(t, release.ReleaseParams{
		ID:      mustID(t),
		Request: validRequest(),
		Evidence: release.UnknownEvidence(release.UnknownEvidenceError(
			"github_unreachable", "evidence", "GitHub did not answer", "Retry once GitHub is reachable.")),
		CreatedAt: testTime,
	})
	if err != nil {
		t.Fatalf("NewRelease: %v", err)
	}
	if rel.Evidence().Outcome() != release.EvidenceUnknown {
		t.Errorf("outcome = %v, want unknown", rel.Evidence().Outcome())
	}
	if _, ok := rel.Bundle(); ok {
		t.Error("a release with unknown evidence carries a bundle")
	}
	if rel.SourceRevision() != "" {
		t.Errorf("SourceRevision() = %q; a claimed revision is not a source revision", rel.SourceRevision())
	}
	if rel.ArtifactDigest() != "" {
		t.Errorf("ArtifactDigest() = %q for unverified evidence", rel.ArtifactDigest())
	}
}

func TestNewReleaseRejectsBundleWithoutVerifiedEvidence(t *testing.T) {
	req := validRequest()
	_, err := newRelease(t, release.ReleaseParams{
		ID:        mustID(t),
		Request:   req,
		Evidence:  release.UnknownEvidence(),
		Bundle:    mustBundle(t, req.Target, digestA),
		CreatedAt: testTime,
	})
	if err == nil || !strings.Contains(err.Error(), "bundle_without_verified_evidence") {
		t.Errorf("error = %v, want bundle_without_verified_evidence", err)
	}
}

func TestNewReleaseRejectsCrossReleaseAndCrossTargetCombination(t *testing.T) {
	otherTarget := validTarget()
	otherTarget.Namespace = "staging"

	cases := []struct {
		name     string
		params   func() release.ReleaseParams
		wantCode string
	}{
		{"bundle rendered for another artifact", func() release.ReleaseParams {
			req := validRequest()
			return release.ReleaseParams{
				ID: mustID(t), Request: req,
				Evidence:  mustVerified(t, mustEvidence(t, validEvidenceInput())),
				Bundle:    mustBundle(t, req.Target, digestB),
				CreatedAt: testTime,
			}
		}, "bundle_artifact_mismatch"},
		{"bundle rendered for another target", func() release.ReleaseParams {
			req := validRequest()
			return release.ReleaseParams{
				ID: mustID(t), Request: req,
				Evidence:  mustVerified(t, mustEvidence(t, validEvidenceInput())),
				Bundle:    mustBundle(t, otherTarget, digestA),
				CreatedAt: testTime,
			}
		}, "bundle_target_mismatch"},
		{"evidence verified for another change", func() release.ReleaseParams {
			in := validEvidenceInput()
			in.MergeCommitSHA = otherSHA
			in.RequiredCheck.HeadSHA = otherSHA
			return release.ReleaseParams{
				ID: mustID(t), Request: validRequest(),
				Evidence:  mustVerified(t, mustEvidence(t, in)),
				CreatedAt: testTime,
			}
		}, "source_revision_mismatch"},
		{"evidence verified for another artifact", func() release.ReleaseParams {
			in := validEvidenceInput()
			in.Artifact.Reference.Digest = digestB
			in.Artifact.ObservedDigest = digestB
			return release.ReleaseParams{
				ID: mustID(t), Request: validRequest(),
				Evidence:  mustVerified(t, mustEvidence(t, in)),
				CreatedAt: testTime,
			}
		}, "artifact_mismatch"},
		{"evidence verified in another repository", func() release.ReleaseParams {
			in := validEvidenceInput()
			in.Repository = release.RepositoryRef{Owner: "someone-else", Name: "podinfo"}
			return release.ReleaseParams{
				ID: mustID(t), Request: validRequest(),
				Evidence:  mustVerified(t, mustEvidence(t, in)),
				CreatedAt: testTime,
			}
		}, "repository_mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newRelease(t, tc.params())
			if err == nil {
				t.Fatal("expected rejection")
			}
			if !strings.Contains(err.Error(), tc.wantCode) {
				t.Errorf("error = %v, want code %q", err, tc.wantCode)
			}
		})
	}
}

func TestNewReleaseRequiresAValidID(t *testing.T) {
	_, err := newRelease(t, release.ReleaseParams{
		ID: "podinfo-2026-08-15", Request: validRequest(),
		Evidence:  mustVerified(t, mustEvidence(t, validEvidenceInput())),
		CreatedAt: testTime,
	})
	if err == nil || !strings.Contains(err.Error(), "malformed_release_id") {
		t.Errorf("error = %v, want malformed_release_id", err)
	}
}

func TestReleaseJSONRoundTripRechecksBindings(t *testing.T) {
	req := validRequest()
	rel, err := newRelease(t, release.ReleaseParams{
		ID:        mustID(t),
		Request:   req,
		Evidence:  mustVerified(t, mustEvidence(t, validEvidenceInput())),
		Bundle:    mustBundle(t, req.Target, digestA),
		CreatedAt: testTime,
	})
	if err != nil {
		t.Fatalf("NewRelease: %v", err)
	}
	raw, err := json.Marshal(rel)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(raw, []byte(release.RecordSchemaVersion)) {
		t.Errorf("record does not carry its schema version: %s", raw)
	}

	var back release.Release
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.ID != rel.ID || !back.CreatedAt.Equal(rel.CreatedAt) {
		t.Error("round trip lost release identity")
	}
	original, _ := rel.Bundle()
	restored, ok := back.Bundle()
	if !ok {
		t.Fatal("round trip lost the bundle")
	}
	if restored.Digest() != original.Digest() {
		t.Errorf("bundle digest changed on round trip: %s -> %s", original.Digest(), restored.Digest())
	}

	// A record whose bundle was re-pointed at a different digest must not reload.
	broken := strings.Replace(string(raw), `"pinned_digest":"`+digestA+`"`, `"pinned_digest":"`+digestB+`"`, 1)
	if broken == string(raw) {
		t.Fatal("test setup: pinned_digest not found in the record")
	}
	var tampered release.Release
	if err := json.Unmarshal([]byte(broken), &tampered); err == nil {
		t.Error("a record whose bundle pins a different digest was accepted")
	}
}

// TestReleaseRecordToleratesFutureSections proves #50 and #52 can add sections without
// breaking readers of this schema version.
func TestReleaseRecordToleratesFutureSections(t *testing.T) {
	req := validRequest()
	rel, err := newRelease(t, release.ReleaseParams{
		ID:        mustID(t),
		Request:   req,
		Evidence:  mustVerified(t, mustEvidence(t, validEvidenceInput())),
		Bundle:    mustBundle(t, req.Target, digestA),
		CreatedAt: testTime,
	})
	if err != nil {
		t.Fatalf("NewRelease: %v", err)
	}
	raw, err := json.Marshal(rel)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	future := strings.TrimSuffix(string(raw), "}") +
		`,"decision":{"normalized_risk":"medium","policy_version":"1"},"proof":{"execution":"pending"}}`
	var back release.Release
	if err := json.Unmarshal([]byte(future), &back); err != nil {
		t.Errorf("a record carrying future sections failed to load: %v", err)
	}
}

// TestNoRiskVocabularyOnTheReleaseRecord: eligibility is not risk. The record
// may carry an eligibility section; it must not carry risk tiers.
func TestNoRiskVocabularyOnTheReleaseRecord(t *testing.T) {
	req := validRequest()
	rel, err := newRelease(t, release.ReleaseParams{
		ID:        mustID(t),
		Request:   req,
		Evidence:  mustVerified(t, mustEvidence(t, validEvidenceInput())),
		Bundle:    mustBundle(t, req.Target, digestA),
		CreatedAt: testTime,
	})
	if err != nil {
		t.Fatalf("NewRelease: %v", err)
	}
	raw, err := json.Marshal(rel)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := record["eligibility"]; !ok {
		t.Fatal("the Release record must persist eligibility")
	}
	for _, forbidden := range []string{"risk", "severity", "normalized_risk", "decision"} {
		if _, present := record[forbidden]; present {
			t.Errorf("the Release record carries %q; evidence is not a risk policy", forbidden)
		}
	}
}

func TestNewRelease_EligibleVerified_PersistsStaticEnvelope(t *testing.T) {
	req := validRequest()
	ev := mustVerified(t, mustEvidence(t, validEvidenceInput()))
	env, err := release.NewRolloutEnvelope([]int{5, 25, 50, 100}, "start")
	if err != nil {
		t.Fatalf("NewRolloutEnvelope: %v", err)
	}
	elig, err := release.Eligible("1", "all_mandatory_evidence_verified",
		"All configured mandatory evidence verified.", env)
	if err != nil {
		t.Fatalf("Eligible: %v", err)
	}

	rel, err := release.NewRelease(release.ReleaseParams{
		ID:          mustID(t),
		Request:     req,
		Evidence:    ev,
		Bundle:      mustBundle(t, req.Target, digestA),
		Eligibility: elig,
		CreatedAt:   testTime,
	})
	if err != nil {
		t.Fatalf("NewRelease: %v", err)
	}
	got := rel.Eligibility()
	if got.Status() != release.EligibilityEligible {
		t.Fatalf("status = %s, want eligible", got.Status())
	}
	gotEnv, ok := got.Envelope()
	if !ok {
		t.Fatal("eligible release lost its envelope")
	}
	if gotEnv.NextAction() != "start" {
		t.Errorf("next action = %q, want start", gotEnv.NextAction())
	}
}

func TestNewRelease_RejectsEligibleWithoutVerifiedEvidence(t *testing.T) {
	env, err := release.NewRolloutEnvelope([]int{5, 25, 50, 100}, "start")
	if err != nil {
		t.Fatalf("NewRolloutEnvelope: %v", err)
	}
	elig, err := release.Eligible("1", "all_mandatory_evidence_verified", "ok", env)
	if err != nil {
		t.Fatalf("Eligible: %v", err)
	}
	_, err = release.NewRelease(release.ReleaseParams{
		ID:          mustID(t),
		Request:     validRequest(),
		Evidence:    release.UnknownEvidence(),
		Eligibility: elig,
		CreatedAt:   testTime,
	})
	if err == nil || !strings.Contains(err.Error(), "eligible_without_verified_evidence") {
		t.Errorf("error = %v, want eligible_without_verified_evidence", err)
	}
}

func TestNewRelease_RequiresEligibility(t *testing.T) {
	req := validRequest()
	_, err := release.NewRelease(release.ReleaseParams{
		ID:        mustID(t),
		Request:   req,
		Evidence:  mustVerified(t, mustEvidence(t, validEvidenceInput())),
		Bundle:    mustBundle(t, req.Target, digestA),
		CreatedAt: testTime,
	})
	if err == nil || !strings.Contains(err.Error(), "missing_eligibility") {
		t.Errorf("error = %v, want missing_eligibility", err)
	}
}

func TestReleaseJSONRoundTrip_PreservesEligibility(t *testing.T) {
	req := validRequest()
	rel, err := newRelease(t, release.ReleaseParams{
		ID:        mustID(t),
		Request:   req,
		Evidence:  mustVerified(t, mustEvidence(t, validEvidenceInput())),
		Bundle:    mustBundle(t, req.Target, digestA),
		CreatedAt: testTime,
	})
	if err != nil {
		t.Fatalf("NewRelease: %v", err)
	}
	raw, err := json.Marshal(rel)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back release.Release
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Eligibility().Status() != rel.Eligibility().Status() {
		t.Errorf("status %s -> %s", rel.Eligibility().Status(), back.Eligibility().Status())
	}
	if back.Eligibility().ReasonCode() != rel.Eligibility().ReasonCode() {
		t.Errorf("reason %q -> %q", rel.Eligibility().ReasonCode(), back.Eligibility().ReasonCode())
	}
	origEnv, _ := rel.Eligibility().Envelope()
	backEnv, ok := back.Eligibility().Envelope()
	if !ok || backEnv.NextAction() != origEnv.NextAction() {
		t.Error("envelope did not survive round-trip")
	}

	// Re-running assessment is not represented as a rewrite: the loaded
	// record keeps the original eligibility, including policy version.
	if back.Eligibility().PolicyVersion() != "1" {
		t.Errorf("policy version = %q", back.Eligibility().PolicyVersion())
	}
}
