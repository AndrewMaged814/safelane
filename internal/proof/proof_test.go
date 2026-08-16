package proof

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/orchestrate"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
	"github.com/AndrewMaged814/safelane/internal/store"
	"github.com/AndrewMaged814/safelane/internal/verify/github"
)

const (
	fixtureDigest    = "sha256:3fbc1d9a7e42c8056d1f9b3e7a5c204d8e6b1f39a7c50d28e4b6f19a3c7d50e8"
	fixtureMergeSHA  = "4f0c1b9e7ac2d5386b1d9f4a5c8e2b7d3a6f0e91"
	fixtureReleaseID = "rel_00000000000000000000000000"
)

type fakeFetcher struct {
	facts github.Facts
	err   error
}

func (f fakeFetcher) FetchPullRequestFacts(ctx context.Context, owner, repo string, number int) (github.Facts, error) {
	if f.err != nil {
		return github.Facts{}, f.err
	}
	return f.facts, nil
}

type fakeResolver struct {
	digest string
	err    error
}

func (f fakeResolver) ResolveDigest(ctx context.Context, ref release.ImageReference) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.digest, nil
}

func (f fakeResolver) ResolveTag(ctx context.Context, repository, tag string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.digest, nil
}

func verifiedFacts() github.Facts {
	return github.Facts{
		Repository:     "AndrewMaged814/podinfo",
		Number:         1,
		URL:            "https://github.com/AndrewMaged814/podinfo/pull/1",
		Merged:         true,
		MergedAt:       time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC),
		BaseRef:        "main",
		MergeCommitSHA: fixtureMergeSHA,
		AuthorLogin:    "AndrewMaged814",
		Approvals: []github.Approval{
			{Reviewer: "ahmed-placeholder", State: "APPROVED", ApprovedAt: time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)},
		},
		CheckRuns: []github.CheckRun{
			{
				Name: "publish / build-and-push", Conclusion: "success", HeadSHA: fixtureMergeSHA,
				RunID: 16453210987, URL: "https://github.com/AndrewMaged814/podinfo/actions/runs/16453210987",
				CompletedAt: time.Date(2026, 8, 15, 8, 30, 0, 0, time.UTC),
			},
		},
	}
}

func fixtureIntent() release.Intent {
	return release.Intent{
		SchemaVersion: release.RequestSchemaVersion,
		Repository:    "AndrewMaged814/podinfo",
		PullRequest:   1,
		Environment:   "production",
	}
}

func fixtureProject() project.Config {
	return project.Config{
		Version:     1,
		Application: "podinfo",
		Repository:  project.Repository{Name: "AndrewMaged814/podinfo", DefaultBranch: "main"},
		Release: project.Release{
			Environment:     "production",
			ImageRepository: "ghcr.io/andrewmaged814/podinfo",
			ImageTag:        "sha-{{merge_sha_short8}}",
			RequiredCheck:   "publish / build-and-push",
			TemplatePath:    ".safelane/release-template",
		},
		Target: project.Target{Cluster: "safelane-demo", Namespace: "podinfo", Rollout: "podinfo"},
	}
}

func loadTemplate(t *testing.T) render.Template {
	t.Helper()
	tmpl, err := render.LoadDir(filepath.Join("..", "render", "testdata", "release-template"))
	if err != nil {
		t.Fatalf("could not load template fixture: %v", err)
	}
	return tmpl
}

func persist(t *testing.T, mutate func(*orchestrate.Deps)) *release.Release {
	t.Helper()
	dir := t.TempDir()
	fs := &store.FileStore{Dir: dir}
	id, err := release.ParseReleaseID(fixtureReleaseID)
	if err != nil {
		t.Fatalf("ParseReleaseID: %v", err)
	}
	deps := orchestrate.Deps{
		GitHub:   fakeFetcher{facts: verifiedFacts()},
		GHCR:     fakeResolver{digest: fixtureDigest},
		Template: loadTemplate(t),
		Store:    fs,
		Project:  fixtureProject(),
		Now:      func() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) },
		NewID:    func() (release.ReleaseID, error) { return id, nil },
	}
	if mutate != nil {
		mutate(&deps)
	}
	if _, err := orchestrate.SubmitRelease(context.Background(), fixtureIntent(), deps); err != nil {
		t.Fatalf("SubmitRelease: %v", err)
	}
	loaded, err := fs.Load(id)
	if err != nil {
		t.Fatalf("Load persisted release: %v", err)
	}
	return loaded
}

func decodeProofJSON(t *testing.T, p Proof) map[string]any {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal proof: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("Unmarshal proof JSON: %v", err)
	}
	return obj
}

func object(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("want %q to be an object, got %T (%v)", key, parent[key], parent[key])
	}
	return v
}

func assertNoForbiddenVocab(t *testing.T, text string) {
	t.Helper()
	lower := strings.ToLower(text)
	for _, word := range []string{"deploywhisper", "normalized_risk", "normalized risk", "contributing provider", "aggregation", "low risk", "risk tier"} {
		if strings.Contains(lower, word) {
			t.Errorf("proof must not mention %q:\n%s", word, text)
		}
	}
}

func TestFrom_EligibleRelease_JSONDecisionAndPendingSections(t *testing.T) {
	r := persist(t, nil)
	p := From(r)
	obj := decodeProofJSON(t, p)

	if got := obj["release_id"]; got != fixtureReleaseID {
		t.Errorf("release_id = %v, want %s", got, fixtureReleaseID)
	}
	if got := obj["application"]; got != "podinfo" {
		t.Errorf("application = %v, want podinfo", got)
	}
	if got := obj["environment"]; got != "production" {
		t.Errorf("environment = %v, want production", got)
	}

	decision := object(t, obj, "decision")
	if got := decision["eligibility"]; got != "eligible" {
		t.Errorf("eligibility = %v, want eligible", got)
	}
	if got := decision["policy_version"]; got != "1" {
		t.Errorf("policy_version = %v, want 1", got)
	}
	if got := decision["reason_code"]; got != "all_mandatory_evidence_verified" {
		t.Errorf("reason_code = %v, want all_mandatory_evidence_verified", got)
	}
	if decision["retryable"] != false {
		t.Errorf("retryable = %v, want false", decision["retryable"])
	}
	env := object(t, decision, "rollout_envelope")
	stages, _ := env["stages"].([]any)
	if len(stages) != 4 || stages[0] != float64(5) || stages[1] != float64(25) || stages[2] != float64(50) || stages[3] != float64(100) {
		t.Errorf("stages = %v, want [5 25 50 100]", stages)
	}
	if got := env["next_action"]; got != "start" {
		t.Errorf("next_action = %v, want start", got)
	}

	if got := object(t, obj, "execution")["status"]; got != "pending" {
		t.Errorf("execution.status = %v, want pending", got)
	}
	if got := object(t, obj, "boundary")["status"]; got != "pending" {
		t.Errorf("boundary.status = %v, want pending", got)
	}

	raw, _ := json.Marshal(p)
	assertNoForbiddenVocab(t, string(raw))
}

func TestFrom_EligibleRelease_JSONArtifactFields(t *testing.T) {
	r := persist(t, nil)
	obj := decodeProofJSON(t, From(r))
	artifact := object(t, obj, "artifact")

	if got := artifact["outcome"]; got != "verified" {
		t.Errorf("artifact.outcome = %v, want verified", got)
	}
	sources, _ := artifact["sources"].([]any)
	if len(sources) != 2 || sources[0] != "github" || sources[1] != "ghcr" {
		t.Errorf("sources = %v, want [github ghcr]", sources)
	}
	if got := artifact["repository"]; got != "AndrewMaged814/podinfo" {
		t.Errorf("repository = %v", got)
	}
	if got := artifact["revision"]; got != fixtureMergeSHA {
		t.Errorf("revision = %v, want %s", got, fixtureMergeSHA)
	}
	if got := artifact["digest"]; got != fixtureDigest {
		t.Errorf("digest = %v, want %s", got, fixtureDigest)
	}
	if got := artifact["digest_source"]; got != "ghcr" {
		t.Errorf("digest_source = %v, want ghcr", got)
	}

	pr := object(t, artifact, "pull_request")
	if pr["number"] != float64(1) || pr["reviewer"] != "ahmed-placeholder" || pr["source"] != "github" {
		t.Errorf("pull_request = %v", pr)
	}
	ci := object(t, artifact, "ci")
	if ci["name"] != "publish / build-and-push" || ci["conclusion"] != "success" || ci["source"] != "github" {
		t.Errorf("ci = %v", ci)
	}
	target := object(t, artifact, "target")
	if target["application"] != "podinfo" || target["cluster"] != "safelane-demo" || target["namespace"] != "podinfo" {
		t.Errorf("target = %v", target)
	}

	tmpl := object(t, artifact, "template")
	wantTmpl, ok := r.TemplateIdentity()
	if !ok {
		t.Fatal("persisted eligible release must have a template identity")
	}
	if tmpl["name"] != wantTmpl.Name || tmpl["content_digest"] != wantTmpl.ContentDigest {
		t.Errorf("template = %v, want %+v", tmpl, wantTmpl)
	}
	if artifact["template_source"] != "safelane" || artifact["bundle_source"] != "safelane" {
		t.Errorf("SafeLane-owned artifact fields missing source, got template_source=%v bundle_source=%v", artifact["template_source"], artifact["bundle_source"])
	}
	if object(t, obj, "decision")["source"] != "safelane" {
		t.Errorf("decision.source = %v, want safelane", object(t, obj, "decision")["source"])
	}
	hashes, _ := artifact["bundle_hashes"].([]any)
	if len(hashes) != len(r.BundleHashes()) || len(hashes) == 0 {
		t.Errorf("bundle_hashes len = %d, want %d", len(hashes), len(r.BundleHashes()))
	}

	caller := object(t, obj, "caller")
	if caller["identity"] != "safelane-cli" || caller["kind"] != "agent" {
		t.Errorf("caller = %v", caller)
	}
}

func TestFrom_IneligibleRelease_HasNoEnvelope(t *testing.T) {
	r := persist(t, func(d *orchestrate.Deps) {
		facts := verifiedFacts()
		facts.CheckRuns = []github.CheckRun{
			{Name: "publish / build-and-push", Conclusion: "failure", HeadSHA: fixtureMergeSHA},
		}
		d.GitHub = fakeFetcher{facts: facts}
	})
	p := From(r)
	obj := decodeProofJSON(t, p)
	decision := object(t, obj, "decision")

	if decision["eligibility"] != "ineligible" {
		t.Errorf("eligibility = %v, want ineligible", decision["eligibility"])
	}
	if decision["retryable"] != false {
		t.Errorf("retryable = %v, want false", decision["retryable"])
	}
	if _, ok := decision["rollout_envelope"]; ok {
		t.Errorf("ineligible must not carry an envelope, got %v", decision["rollout_envelope"])
	}
	if object(t, obj, "execution")["status"] != "pending" || object(t, obj, "boundary")["status"] != "pending" {
		t.Error("execution and boundary must stay pending")
	}
	artifact := object(t, obj, "artifact")
	if artifact["outcome"] == "verified" || artifact["digest"] != nil {
		t.Errorf("failed evidence must not present a verified digest, got %v", artifact)
	}
	raw, _ := json.Marshal(p)
	assertNoForbiddenVocab(t, string(raw))
}

func TestFrom_IndeterminateRelease_IsRetryableUnknownNotSuccess(t *testing.T) {
	r := persist(t, func(d *orchestrate.Deps) {
		d.GitHub = fakeFetcher{err: errUnreachable}
	})
	p := From(r)
	obj := decodeProofJSON(t, p)
	decision := object(t, obj, "decision")

	if decision["eligibility"] != "indeterminate" {
		t.Errorf("eligibility = %v, want indeterminate", decision["eligibility"])
	}
	if decision["retryable"] != true {
		t.Errorf("retryable = %v, want true", decision["retryable"])
	}
	if _, ok := decision["rollout_envelope"]; ok {
		t.Errorf("indeterminate must not carry an envelope, got %v", decision["rollout_envelope"])
	}
	artifact := object(t, obj, "artifact")
	if artifact["outcome"] != "unknown" {
		t.Errorf("artifact.outcome = %v, want unknown", artifact["outcome"])
	}
	if artifact["digest"] != nil || artifact["revision"] != nil {
		t.Errorf("unknown evidence must not present verified artifact fields, got %v", artifact)
	}
	raw, _ := json.Marshal(p)
	assertNoForbiddenVocab(t, string(raw))
	if strings.Contains(strings.ToLower(string(raw)), `"outcome":"verified"`) {
		t.Error("indeterminate proof must not claim verified evidence")
	}
}

func TestFrom_EligibleRelease_ConciseMatchesJSON(t *testing.T) {
	r := persist(t, nil)
	p := From(r)
	out := p.Concise()
	assertNoForbiddenVocab(t, out)
	for _, want := range []string{
		"release_id: " + fixtureReleaseID,
		"created_at: 2026-08-15T12:00:00Z",
		"application: podinfo  environment: production",
		"caller: safelane-cli (agent)",
		"#1",
		"ahmed-placeholder",
		fixtureMergeSHA,
		fixtureDigest,
		"eligibility: eligible",
		"policy_version: 1",
		"All configured mandatory evidence verified",
		"rollout_envelope: 5 → 25 → 50 → 100",
		"next_action: start",
		"execution: pending",
		"boundary: pending",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("concise missing %q\n%s", want, out)
		}
	}
}

func TestFrom_IneligibleRelease_ConciseOmitsEnvelope(t *testing.T) {
	r := persist(t, func(d *orchestrate.Deps) {
		facts := verifiedFacts()
		facts.CheckRuns = []github.CheckRun{
			{Name: "publish / build-and-push", Conclusion: "failure", HeadSHA: fixtureMergeSHA},
		}
		d.GitHub = fakeFetcher{facts: facts}
	})
	out := From(r).Concise()
	assertNoForbiddenVocab(t, out)
	if !strings.Contains(out, "eligibility: ineligible") {
		t.Errorf("want ineligible\n%s", out)
	}
	if strings.Contains(out, "rollout_envelope:") || strings.Contains(out, "next_action:") {
		t.Errorf("ineligible concise must not print an envelope\n%s", out)
	}
	if !strings.Contains(out, "execution: pending") || !strings.Contains(out, "boundary: pending") {
		t.Errorf("want pending execution and boundary\n%s", out)
	}
}

func TestFrom_EligibleRelease_DetailsHasFourSections(t *testing.T) {
	r := persist(t, nil)
	p := From(r)
	out := p.Details()
	assertNoForbiddenVocab(t, out)
	for _, want := range []string{
		"Artifact",
		"Decision",
		"Execution",
		"Boundary",
		fixtureMergeSHA,
		fixtureDigest,
		"eligibility: eligible",
		"5 → 25 → 50 → 100",
		"github",
		"ghcr",
		"safelane",
		"pending",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("details missing %q\n%s", want, out)
		}
	}
	hashes := r.BundleHashes()
	if len(hashes) == 0 {
		t.Fatal("eligible release must have bundle hashes")
	}
	if !strings.Contains(out, hashes[0].Hash) {
		t.Errorf("details missing first bundle hash %s\n%s", hashes[0].Hash, out)
	}
	if bundle, ok := r.Bundle(); ok && !strings.Contains(out, bundle.Digest()) {
		t.Errorf("details missing bundle digest %s\n%s", bundle.Digest(), out)
	}
}

func TestForms_DoNotContradict(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*orchestrate.Deps)
		elig   string
		env    bool
	}{
		{name: "eligible", elig: "eligible", env: true},
		{name: "ineligible", mutate: func(d *orchestrate.Deps) {
			facts := verifiedFacts()
			facts.CheckRuns = []github.CheckRun{
				{Name: "publish / build-and-push", Conclusion: "failure", HeadSHA: fixtureMergeSHA},
			}
			d.GitHub = fakeFetcher{facts: facts}
		}, elig: "ineligible"},
		{name: "indeterminate", mutate: func(d *orchestrate.Deps) {
			d.GitHub = fakeFetcher{err: errUnreachable}
		}, elig: "indeterminate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := From(persist(t, tc.mutate))
			concise := p.Concise()
			details := p.Details()
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			jsonText := string(raw)
			assertNoForbiddenVocab(t, concise)
			assertNoForbiddenVocab(t, details)
			assertNoForbiddenVocab(t, jsonText)

			wantElig := "eligibility: " + tc.elig
			if !strings.Contains(concise, wantElig) || !strings.Contains(details, wantElig) {
				t.Errorf("human forms missing %s", wantElig)
			}
			if !strings.Contains(jsonText, `"eligibility":"`+tc.elig+`"`) {
				t.Errorf("JSON missing eligibility %s\n%s", tc.elig, jsonText)
			}
			hasEnvConcise := strings.Contains(concise, "rollout_envelope:")
			hasEnvDetails := strings.Contains(details, "rollout_envelope:")
			hasEnvJSON := strings.Contains(jsonText, `"rollout_envelope"`)
			if hasEnvConcise != tc.env || hasEnvDetails != tc.env || hasEnvJSON != tc.env {
				t.Errorf("envelope present concise=%v details=%v json=%v, want %v", hasEnvConcise, hasEnvDetails, hasEnvJSON, tc.env)
			}
			for _, form := range []string{concise, details, jsonText} {
				if !strings.Contains(form, "pending") {
					t.Errorf("form missing pending execution/boundary:\n%s", form)
				}
			}
		})
	}
}

var errUnreachable = errUnreachableSentinel{}

type errUnreachableSentinel struct{}

func (errUnreachableSentinel) Error() string { return "connection reset" }
