// Package proof is the Release Proof read model (#52). It projects an
// already-persisted Release into the four-section proof contract: Artifact,
// Decision, Execution, and Boundary.
//
// From never re-verifies GitHub or GHCR evidence and never re-evaluates
// policy. Execution and Boundary are always pending until #53 and #54
// supply real events. Evidence sources named here are GitHub and GHCR
// only; Decision proof is Release Eligibility, not a risk assessment.
package proof

import (
	"encoding/json"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
)

// SchemaVersion is the version of the machine-readable proof contract.
const SchemaVersion = "safelane.release.proof/v1"

const (
	sourceGitHub  = "github"
	sourceGHCR    = "ghcr"
	statusPending = "pending"
)

// Proof is the four-section Release Proof. Construct it with [From].
type Proof struct {
	releaseID   release.ReleaseID
	createdAt   time.Time
	application string
	environment string
	caller      release.CallerIdentity
	artifact    Artifact
	decision    Decision
	execution   PendingSection
	boundary    PendingSection
}

// Artifact is the Artifact-proof section.
type Artifact struct {
	Outcome        string                    `json:"outcome"`
	Sources        []string                  `json:"sources"`
	Target         release.Target            `json:"target"`
	Repository     string                    `json:"repository,omitempty"`
	Revision       string                    `json:"revision,omitempty"`
	PullRequest    *PullRequestProof         `json:"pull_request,omitempty"`
	CI             *CIProof                  `json:"ci,omitempty"`
	Digest         string                    `json:"digest,omitempty"`
	DigestSource   string                    `json:"digest_source,omitempty"`
	Template       *release.TemplateIdentity `json:"template,omitempty"`
	TemplateSource string                    `json:"template_source,omitempty"`
	BundleHashes   []release.ResourceHash    `json:"bundle_hashes,omitempty"`
	BundleDigest   string                    `json:"bundle_digest,omitempty"`
	BundleSource   string                    `json:"bundle_source,omitempty"`
	Reasons        release.Errors            `json:"reasons,omitempty"`
}

// PullRequestProof is the verified pull-request and reviewer evidence.
type PullRequestProof struct {
	Number   int    `json:"number"`
	URL      string `json:"url"`
	Author   string `json:"author"`
	Reviewer string `json:"reviewer,omitempty"`
	Source   string `json:"source"`
}

// CIProof is the verified required-check evidence.
type CIProof struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url,omitempty"`
	Source     string `json:"source"`
}

// Decision is the Decision-proof section: Release Eligibility, not risk.
type Decision struct {
	Eligibility   string                   `json:"eligibility"`
	PolicyVersion string                   `json:"policy_version"`
	ReasonCode    string                   `json:"reason_code"`
	Message       string                   `json:"message"`
	Retryable     bool                     `json:"retryable"`
	Envelope      *release.RolloutEnvelope `json:"rollout_envelope,omitempty"`
	Source        string                   `json:"source"`
}

// PendingSection is Execution or Boundary proof before real evidence exists.
type PendingSection struct {
	Status string `json:"status"`
}

// From projects proof from an already-persisted Release. It reads only what
// the Release already recorded.
func From(r *release.Release) Proof {
	elig := r.Eligibility()
	p := Proof{
		releaseID:   r.ID,
		createdAt:   r.CreatedAt,
		application: r.Target().Application,
		environment: r.Target().Environment,
		caller:      r.Caller(),
		artifact:    artifactFrom(r),
		decision: Decision{
			Eligibility:   elig.Status().String(),
			PolicyVersion: elig.PolicyVersion(),
			ReasonCode:    elig.ReasonCode(),
			Message:       elig.Message(),
			Retryable:     elig.Retryable(),
			Source:        "safelane",
		},
		execution: PendingSection{Status: statusPending},
		boundary:  PendingSection{Status: statusPending},
	}
	if env, ok := elig.Envelope(); ok {
		p.decision.Envelope = &env
	}
	return p
}

func artifactFrom(r *release.Release) Artifact {
	a := Artifact{
		Outcome: r.Evidence().Outcome().String(),
		Sources: []string{sourceGitHub, sourceGHCR},
		Target:  r.Target(),
	}
	if evidence, ok := r.Evidence().Verified(); ok {
		a.Repository = evidence.Repository().String()
		a.Revision = evidence.MergeCommitSHA()
		pr := evidence.PullRequest()
		a.PullRequest = &PullRequestProof{
			Number: pr.Number,
			URL:    pr.URL,
			Author: pr.Author,
			Source: sourceGitHub,
		}
		if evidence.IndependentApproval() {
			a.PullRequest.Reviewer = evidence.Approval().Reviewer
		}
		check := evidence.RequiredCheck()
		a.CI = &CIProof{
			Name:       check.Name,
			Conclusion: check.Conclusion,
			URL:        check.URL,
			Source:     sourceGitHub,
		}
		a.Digest = evidence.ArtifactDigest()
		a.DigestSource = sourceGHCR
	} else {
		a.Reasons = r.Evidence().Reasons()
	}
	if tmpl, ok := r.TemplateIdentity(); ok {
		a.Template = &tmpl
		a.TemplateSource = "safelane"
	}
	if hashes := r.BundleHashes(); len(hashes) > 0 {
		a.BundleHashes = hashes
	}
	if bundle, ok := r.Bundle(); ok {
		a.BundleDigest = bundle.Digest()
		a.BundleSource = "safelane"
	}
	return a
}

type proofJSON struct {
	SchemaVersion string                 `json:"schema_version"`
	ReleaseID     release.ReleaseID      `json:"release_id"`
	CreatedAt     time.Time              `json:"created_at"`
	Application   string                 `json:"application"`
	Environment   string                 `json:"environment"`
	Caller        release.CallerIdentity `json:"caller"`
	Artifact      Artifact               `json:"artifact"`
	Decision      Decision               `json:"decision"`
	Execution     PendingSection         `json:"execution"`
	Boundary      PendingSection         `json:"boundary"`
}

// MarshalJSON writes the machine-readable proof contract.
func (p Proof) MarshalJSON() ([]byte, error) {
	return json.Marshal(proofJSON{
		SchemaVersion: SchemaVersion,
		ReleaseID:     p.releaseID,
		CreatedAt:     p.createdAt,
		Application:   p.application,
		Environment:   p.environment,
		Caller:        p.caller,
		Artifact:      p.artifact,
		Decision:      p.decision,
		Execution:     p.execution,
		Boundary:      p.boundary,
	})
}
