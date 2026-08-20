// Package proof projects a persisted Release Record into human and machine proof.
// It never re-verifies evidence, re-runs assessment, or consults Kubernetes.
package proof

import (
	"encoding/json"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
)

const SchemaVersion = "safelane.release.proof/v2"

const (
	sourceGitHub = "github"
	sourceGHCR   = "ghcr"
)

type Proof struct {
	releaseID  release.ReleaseID
	createdAt  time.Time
	artifact   Artifact
	assessment *release.AssessmentRecord
	decision   Decision
	execution  []release.ExecutionEntry
	boundary   *release.Boundary
	outcome    string
}

type Artifact struct {
	Outcome        string                    `json:"outcome"`
	Sources        []string                  `json:"sources"`
	Target         release.Target            `json:"target"`
	Repository     string                    `json:"repository,omitempty"`
	Revision       string                    `json:"revision,omitempty"`
	PullRequest    *PullRequestProof         `json:"pull_request,omitempty"`
	CI             *CIProof                  `json:"ci,omitempty"`
	Digest         string                    `json:"digest,omitempty"`
	Image          string                    `json:"image,omitempty"`
	DigestSource   string                    `json:"digest_source,omitempty"`
	Template       *release.TemplateIdentity `json:"template,omitempty"`
	TemplateSource string                    `json:"template_source,omitempty"`
	BundleHashes   []release.ResourceHash    `json:"bundle_hashes,omitempty"`
	BundleDigest   string                    `json:"bundle_digest,omitempty"`
	BundleSource   string                    `json:"bundle_source,omitempty"`
	Reasons        release.Errors            `json:"reasons,omitempty"`
	Evidence       EvidenceSummary           `json:"evidence_summary,omitempty"`
}

type EvidenceSummary struct {
	Verified    int `json:"verified"`
	Failed      int `json:"failed"`
	Unavailable int `json:"unavailable"`
}

type PullRequestProof struct {
	Number     int    `json:"number"`
	URL        string `json:"url"`
	Author     string `json:"author"`
	Reviewer   string `json:"reviewer,omitempty"`
	BaseBranch string `json:"base_branch"`
	Source     string `json:"source"`
}

type CIProof struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url,omitempty"`
	Source     string `json:"source"`
}

type Decision struct {
	Eligibility   string                   `json:"eligibility"`
	PolicyVersion string                   `json:"policy_version"`
	ReasonCode    string                   `json:"reason_code"`
	Message       string                   `json:"message"`
	Retryable     bool                     `json:"retryable"`
	Envelope      *release.RolloutEnvelope `json:"rollout_envelope,omitempty"`
	Source        string                   `json:"source"`
}

func From(r *release.Release) Proof {
	elig := r.Eligibility()
	execution := r.Execution()
	var envelope *release.RolloutEnvelope
	if env, ok := elig.Envelope(); ok {
		envelope = &env
	}
	p := Proof{
		releaseID: r.ID, createdAt: r.CreatedAt, artifact: artifactFrom(r), execution: execution,
		outcome: outcome(execution, envelope),
		decision: Decision{
			Eligibility: elig.Status().String(), PolicyVersion: elig.PolicyVersion(),
			ReasonCode: elig.ReasonCode(), Message: elig.Message(), Retryable: elig.Retryable(), Envelope: envelope, Source: "safelane",
		},
	}
	if a, ok := r.RecordedAssessment(); ok {
		p.assessment = &a
	}
	if boundary, ok := r.Boundary(); ok {
		p.boundary = &boundary
	}
	return p
}

func outcome(entries []release.ExecutionEntry, envelope *release.RolloutEnvelope) string {
	if len(entries) == 0 {
		return "pending"
	}
	last := entries[len(entries)-1]
	if last.Outcome == release.OutcomeAborted || last.Verb == release.VerbAbort || last.Verb == release.VerbArgoAbort {
		return "aborted"
	}
	if last.Verb == release.VerbPause {
		return "paused"
	}
	if envelope != nil && last.Outcome == release.OutcomeGranted {
		stages := envelope.Stages()
		if len(stages) > 0 && last.RequestedWeight == stages[len(stages)-1] {
			return "complete"
		}
	}
	return "in_progress"
}

func artifactFrom(r *release.Release) Artifact {
	a := Artifact{Outcome: r.Evidence().Outcome().String(), Sources: []string{sourceGitHub, sourceGHCR}, Target: r.Target()}
	if evidence, ok := r.Evidence().Verified(); ok {
		a.Repository, a.Revision = evidence.Repository().String(), evidence.MergeCommitSHA()
		pr := evidence.PullRequest()
		a.PullRequest = &PullRequestProof{Number: pr.Number, URL: pr.URL, Author: pr.Author, BaseBranch: pr.BaseBranch, Source: sourceGitHub}
		if evidence.IndependentApproval() {
			a.PullRequest.Reviewer = evidence.Approval().Reviewer
		}
		check := evidence.RequiredCheck()
		a.CI = &CIProof{Name: check.Name, Conclusion: check.Conclusion, URL: check.URL, Source: sourceGitHub}
		a.Digest, a.Image, a.DigestSource = evidence.ArtifactDigest(), evidence.Artifact().Reference.String(), sourceGHCR
		a.Evidence = EvidenceSummary{Verified: 3, Unavailable: 1}
		if evidence.IndependentApproval() {
			a.Evidence.Verified++
			a.Evidence.Unavailable = 0
		}
	} else {
		a.Reasons = r.Evidence().Reasons()
	}
	if tmpl, ok := r.TemplateIdentity(); ok {
		a.Template, a.TemplateSource = &tmpl, "safelane"
	}
	a.BundleHashes = r.BundleHashes()
	if bundle, ok := r.Bundle(); ok {
		a.BundleDigest, a.BundleSource = bundle.Digest(), "safelane"
	}
	return a
}

type proofJSON struct {
	SchemaVersion string                    `json:"schema_version"`
	ReleaseID     release.ReleaseID         `json:"release_id"`
	CreatedAt     time.Time                 `json:"created_at"`
	Artifact      Artifact                  `json:"artifact"`
	Assessment    *release.AssessmentRecord `json:"assessment,omitempty"`
	Decision      Decision                  `json:"decision"`
	Execution     []release.ExecutionEntry  `json:"execution"`
	Boundary      *release.Boundary         `json:"boundary,omitempty"`
	Outcome       string                    `json:"outcome"`
}

func (p Proof) MarshalJSON() ([]byte, error) {
	return json.Marshal(proofJSON{SchemaVersion: SchemaVersion, ReleaseID: p.releaseID, CreatedAt: p.createdAt,
		Artifact: p.artifact, Assessment: p.assessment, Decision: p.decision, Execution: p.execution, Boundary: p.boundary, Outcome: p.outcome})
}
