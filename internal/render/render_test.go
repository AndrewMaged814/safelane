package render_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
)

const (
	fixtureTemplateDir = "testdata/release-template"

	digestA = "sha256:3fbc1d9a7e42c8056d1f9b3e7a5c204d8e6b1f39a7c50d28e4b6f19a3c7d50e8"
	digestB = "sha256:0011223344556677889900aabbccddeeff00112233445566778899aabbccddee"

	mergeSHA = "4f0c1b9e7ac2d5386b1d9f4a5c8e2b7d3a6f0e91"
)

// testWeights matches the four stages the fixture Rollout template
// rendered before lanes existed, so every existing byte/hash assertion
// in this file keeps holding unchanged.
var testWeights = []int{5, 25, 50, 100}

func testTarget() release.Target {
	return release.Target{
		Application: "safelane-demo-api",
		Environment: "production",
		Cluster:     "safelane-demo",
		Namespace:   "safelane-demo-api",
	}
}

// testEvidence builds verified evidence pinned to digest. It goes through the real
// constructor on purpose: if the constructor's invariants change, these tests must
// change with them.
func testEvidence(t *testing.T, digest string) release.ReleaseEvidence {
	t.Helper()
	now := time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)
	ev, err := release.NewReleaseEvidence(release.EvidenceInput{
		Repository: release.RepositoryRef{Owner: "AndrewMaged814", Name: "safelane-demo-api"},
		PullRequest: release.VerifiedPullRequest{
			Number:     1,
			URL:        "https://github.com/AndrewMaged814/safelane-demo-api/pull/1",
			Author:     "AndrewMaged814",
			BaseBranch: "main",
			MergedAt:   now,
		},
		MergeCommitSHA: mergeSHA,
		RequiredCheck: release.VerifiedCheckRun{
			Name:        "publish / build-and-push",
			HeadSHA:     mergeSHA,
			Conclusion:  release.CheckConclusionSuccess,
			RunID:       16453210987,
			CompletedAt: now,
		},
		Artifact: release.VerifiedArtifact{
			Reference:      release.ImageReference{Registry: "ghcr.io", Repository: "andrewmaged814/safelane-demo-api", Digest: digest},
			ObservedDigest: digest,
			ResolvedAt:     now,
		},
		VerifiedAt: now,
	})
	if err != nil {
		t.Fatalf("NewReleaseEvidence: %v", err)
	}
	return ev
}

func loadFixture(t *testing.T) render.Template {
	t.Helper()
	tmpl, err := render.LoadDir(fixtureTemplateDir)
	if err != nil {
		t.Fatalf("LoadDir(%s): %v", fixtureTemplateDir, err)
	}
	return tmpl
}

func renderFixture(t *testing.T, digest string) release.RenderedBundle {
	t.Helper()
	bundle, err := render.Render(loadFixture(t), testTarget(), testEvidence(t, digest), testWeights)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return bundle
}

// TestRenderIsDeterministic is the property the entire security claim rests on: the
// same template, target and verified digest must produce byte-identical output, so
// "the bytes that were assessed are the bytes that reach the cluster" is checkable by
// comparison rather than by trust.
func TestRenderIsDeterministic(t *testing.T) {
	first := renderFixture(t, digestA)
	// Reload the template as well, so a second load of the same files is proven to
	// produce the same template identity.
	second := renderFixture(t, digestA)

	if first.Template() != second.Template() {
		t.Errorf("template identity differs between loads:\n first=%+v\nsecond=%+v", first.Template(), second.Template())
	}
	if first.Digest() != second.Digest() {
		t.Errorf("bundle digest differs: %s vs %s", first.Digest(), second.Digest())
	}
	if first.Len() != second.Len() {
		t.Fatalf("resource count differs: %d vs %d", first.Len(), second.Len())
	}
	a, b := first.Resources(), second.Resources()
	for i := range a {
		if a[i].Ref() != b[i].Ref() {
			t.Errorf("resource %d identity differs: %+v vs %+v", i, a[i].Ref(), b[i].Ref())
		}
		if !bytes.Equal(a[i].Bytes(), b[i].Bytes()) {
			t.Errorf("resource %d (%s) bytes differ between renders", i, a[i].Ref())
		}
		if a[i].Hash() != b[i].Hash() {
			t.Errorf("resource %d (%s) hash differs: %s vs %s", i, a[i].Ref(), a[i].Hash(), b[i].Hash())
		}
	}
}

// TestRenderOrderIsStable pins the resource order to the lexicographic order of
// template paths, since apply order is part of what was rendered.
func TestRenderOrderIsStable(t *testing.T) {
	bundle := renderFixture(t, digestA)
	want := []string{
		"10-service-stable.yaml.tmpl",
		"20-service-canary.yaml.tmpl",
		"30-analysistemplate.yaml.tmpl",
		"35-ingress.yaml.tmpl",
		"40-rollout.yaml.tmpl",
	}
	got := make([]string, 0, bundle.Len())
	for _, res := range bundle.Resources() {
		got = append(got, res.Ref().TemplatePath)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("render order = %v, want %v", got, want)
	}
}

// TestRenderedResourceHashMatchesSHA256OfBytes checks the per-resource hash against an
// independent sha256 of the exact bytes, i.e. what `sha256sum` would report for the
// same file.
func TestRenderedResourceHashMatchesSHA256OfBytes(t *testing.T) {
	for _, res := range renderFixture(t, digestA).Resources() {
		raw := res.Bytes()
		sum := sha256.Sum256(raw)
		want := "sha256:" + hex.EncodeToString(sum[:])
		if res.Hash() != want {
			t.Errorf("%s: hash = %s, want %s", res.Ref(), res.Hash(), want)
		}
		if res.Size() != len(raw) {
			t.Errorf("%s: Size() = %d, want %d", res.Ref(), res.Size(), len(raw))
		}
	}
}

// TestRenderedBytesCannotBeMutatedAfterHashing proves the hash keeps describing the
// stored bytes even if a consumer scribbles on what it was handed.
func TestRenderedBytesCannotBeMutatedAfterHashing(t *testing.T) {
	bundle := renderFixture(t, digestA)
	res := bundle.Resources()[0]
	before := res.Hash()

	handed := res.Bytes()
	for i := range handed {
		handed[i] = 'x'
	}

	if res.Hash() != before {
		t.Errorf("hash changed after mutating the returned byte slice: %s -> %s", before, res.Hash())
	}
	again := bundle.Resources()[0]
	if again.Hash() != before || bytes.Equal(again.Bytes(), handed) {
		t.Error("bundle resource bytes were mutated through Bytes()")
	}
}

// TestDigestPinningChangesOnlyTheImage is the digest-pinning property: swapping only the
// verified digest must change only the pod template's image line, and therefore only the
// Rollout's hash. Anything else changing would mean the digest is influencing structure
// it has no business touching.
func TestDigestPinningChangesOnlyTheImage(t *testing.T) {
	a := renderFixture(t, digestA)
	b := renderFixture(t, digestB)

	if a.PinnedDigest() != digestA || b.PinnedDigest() != digestB {
		t.Fatalf("pinned digests = %s / %s", a.PinnedDigest(), b.PinnedDigest())
	}
	if a.Digest() == b.Digest() {
		t.Error("bundle digest did not change when the pinned artifact digest changed")
	}
	if a.Template() != b.Template() {
		t.Error("template identity changed when only the artifact digest changed")
	}

	resA, resB := a.Resources(), b.Resources()
	if len(resA) != len(resB) {
		t.Fatalf("resource count changed: %d vs %d", len(resA), len(resB))
	}

	changed := []string{}
	for i := range resA {
		if resA[i].Hash() == resB[i].Hash() {
			if !bytes.Equal(resA[i].Bytes(), resB[i].Bytes()) {
				t.Errorf("%s: equal hashes but different bytes", resA[i].Ref())
			}
			continue
		}
		changed = append(changed, resA[i].Ref().String())

		linesA := strings.Split(string(resA[i].Bytes()), "\n")
		linesB := strings.Split(string(resB[i].Bytes()), "\n")
		if len(linesA) != len(linesB) {
			t.Fatalf("%s: line count changed (%d -> %d); the digest must not change structure",
				resA[i].Ref(), len(linesA), len(linesB))
		}
		diffs := 0
		for n := range linesA {
			if linesA[n] == linesB[n] {
				continue
			}
			diffs++
			if !strings.Contains(strings.TrimSpace(linesA[n]), "image:") {
				t.Errorf("%s: line %d changed but is not the image field: %q -> %q",
					resA[i].Ref(), n+1, linesA[n], linesB[n])
			}
			if !strings.Contains(linesB[n], digestB) {
				t.Errorf("%s: line %d does not carry the new digest: %q", resA[i].Ref(), n+1, linesB[n])
			}
		}
		if diffs != 1 {
			t.Errorf("%s: %d lines changed, want exactly 1 (the pod template image)", resA[i].Ref(), diffs)
		}
	}

	if len(changed) != 1 || !strings.HasPrefix(changed[0], "Rollout/") {
		t.Errorf("changed resources = %v, want exactly the Rollout", changed)
	}
}

// TestBundleManifestConcatenatesTheHashedBytes checks that the multi-document view
// contains exactly the bytes the recorded hashes cover - no re-serialization
// anywhere in the path.
func TestBundleManifestConcatenatesTheHashedBytes(t *testing.T) {
	bundle := renderFixture(t, digestA)
	manifest := bundle.Manifest()
	for _, res := range bundle.Resources() {
		if !bytes.Contains(manifest, res.Bytes()) {
			t.Errorf("%s: manifest does not contain the exact hashed bytes", res.Ref())
		}
	}
	if want := strings.Count(string(manifest), "\n---\n") + 1; want != bundle.Len() {
		t.Errorf("manifest has %d documents, want %d", want, bundle.Len())
	}
}

// TestRenderedBytesCarryNoNonDeterministicValues guards the values that would silently
// break determinism if a future template author reached for them.
func TestRenderedBytesCarryNoNonDeterministicValues(t *testing.T) {
	manifest := string(renderFixture(t, digestA).Manifest())
	for _, banned := range []string{
		release.ReleaseIDPrefix,      // a release ID embeds a timestamp and randomness
		time.Now().Format("2006-01"), // no wall-clock date
		"<no value>",                 // an unresolved template field
	} {
		if strings.Contains(manifest, banned) {
			t.Errorf("rendered bundle contains %q", banned)
		}
	}
}

// TestRenderRecordsResourceIdentityFromRenderedBytes checks the identity SafeLane
// records describes what was actually produced.
func TestRenderRecordsResourceIdentityFromRenderedBytes(t *testing.T) {
	bundle := renderFixture(t, digestA)
	want := map[string]string{
		"10-service-stable.yaml.tmpl":   "Service/safelane-demo-api/safelane-demo-api-stable",
		"20-service-canary.yaml.tmpl":   "Service/safelane-demo-api/safelane-demo-api-canary",
		"30-analysistemplate.yaml.tmpl": "AnalysisTemplate/safelane-demo-api/safelane-demo-api-demo-behavior",
		"35-ingress.yaml.tmpl":          "Ingress/safelane-demo-api/safelane-demo-api",
		"40-rollout.yaml.tmpl":          "Rollout/safelane-demo-api/safelane-demo-api",
	}
	for _, res := range bundle.Resources() {
		ref := res.Ref()
		if got := ref.String(); got != want[ref.TemplatePath] {
			t.Errorf("%s identified as %q, want %q", ref.TemplatePath, got, want[ref.TemplatePath])
		}
		if ref.APIVersion == "" {
			t.Errorf("%s recorded no apiVersion", ref.TemplatePath)
		}
	}
}

// TestRenderRequiresVerifiedEvidence proves rendering against an unverified artifact is
// not expressible: the zero ReleaseEvidence is the only value a caller outside the
// release package can fabricate, and it is refused.
func TestRenderRequiresVerifiedEvidence(t *testing.T) {
	_, err := render.Render(loadFixture(t), testTarget(), release.ReleaseEvidence{}, testWeights)
	if err == nil {
		t.Fatal("expected rendering without verified evidence to fail")
	}
	if got := release.Categorize(err); got != release.CategoryRenderFailed {
		t.Errorf("category = %q, want %q", got, release.CategoryRenderFailed)
	}
}

func TestRenderRejectsInvalidTarget(t *testing.T) {
	bad := testTarget()
	bad.Namespace = "Not A Namespace"
	if _, err := render.Render(loadFixture(t), bad, testEvidence(t, digestA), testWeights); err == nil {
		t.Fatal("expected an invalid target to be rejected")
	}
}

// TestRenderRejectsTemplateThatDoesNotPinTheDigest is the guard that catches a real
// operator template whose pod template forgot the image. SafeLane refuses rather than
// record an unpinned bundle.
func TestRenderRejectsTemplateThatDoesNotPinTheDigest(t *testing.T) {
	fsys := fstest.MapFS{
		"10-service.yaml.tmpl": &fstest.MapFile{Data: []byte(
			"apiVersion: v1\nkind: Service\nmetadata:\n  name: {{ .Application }}\n  namespace: {{ .Namespace }}\nspec:\n  type: ClusterIP\n")},
	}
	tmpl, err := render.LoadFS(fsys)
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	_, err = render.Render(tmpl, testTarget(), testEvidence(t, digestA), testWeights)
	if err == nil {
		t.Fatal("expected a template that does not pin the digest to be rejected")
	}
	if !strings.Contains(err.Error(), "unpinned_template") {
		t.Errorf("error = %v, want unpinned_template", err)
	}
}

func TestRenderRejectsMultiDocumentTemplateFile(t *testing.T) {
	fsys := fstest.MapFS{
		"10-two.yaml.tmpl": &fstest.MapFile{Data: []byte(
			"apiVersion: v1\nkind: Service\nmetadata:\n  name: a\n---\napiVersion: v1\nkind: Service\nmetadata:\n  name: b\n")},
	}
	if _, err := render.LoadFS(fsys); err == nil ||
		!strings.Contains(err.Error(), "multi_document_template") {
		t.Errorf("error = %v, want multi_document_template", err)
	}
}

func TestRenderRejectsUnidentifiableResource(t *testing.T) {
	fsys := fstest.MapFS{
		"10-no-kind.yaml.tmpl": &fstest.MapFile{Data: []byte(
			"apiVersion: v1\nmetadata:\n  name: a\nimage: {{ .ImageReference }}\n")},
	}
	tmpl, err := render.LoadFS(fsys)
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if _, err := render.Render(tmpl, testTarget(), testEvidence(t, digestA), testWeights); err == nil ||
		!strings.Contains(err.Error(), "missing_kind") {
		t.Errorf("error = %v, want missing_kind", err)
	}
}

// TestRenderRejectsUnknownTemplateValue proves a template typo fails loudly instead of
// rendering an empty string into an object SafeLane would then apply.
func TestRenderRejectsUnknownTemplateValue(t *testing.T) {
	fsys := fstest.MapFS{
		"10-typo.yaml.tmpl": &fstest.MapFile{Data: []byte(
			"apiVersion: v1\nkind: Service\nmetadata:\n  name: {{ .Aplication }}\n")},
	}
	tmpl, err := render.LoadFS(fsys)
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if _, err := render.Render(tmpl, testTarget(), testEvidence(t, digestA), testWeights); err == nil ||
		!strings.Contains(err.Error(), "template_execute_failed") {
		t.Errorf("error = %v, want template_execute_failed", err)
	}
}

// TestTemplateFunctionsAreUnavailable proves a template author cannot reach for a
// non-deterministic helper: no functions are registered at all.
func TestTemplateFunctionsAreUnavailable(t *testing.T) {
	fsys := fstest.MapFS{
		"10-now.yaml.tmpl": &fstest.MapFile{Data: []byte(
			"apiVersion: v1\nkind: Service\nmetadata:\n  name: {{ now }}\n")},
	}
	tmpl, err := render.LoadFS(fsys)
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	if _, err := render.Render(tmpl, testTarget(), testEvidence(t, digestA), testWeights); err == nil ||
		!strings.Contains(err.Error(), "template_parse_failed") {
		t.Errorf("error = %v, want template_parse_failed", err)
	}
}

// TestTemplateDigestIsNewlineNormalized proves the same template content produces the
// same identity from a CRLF checkout (Windows) and an LF checkout (CI).
func TestTemplateDigestIsNewlineNormalized(t *testing.T) {
	lf := "apiVersion: v1\nkind: Service\nmetadata:\n  name: {{ .Application }}\n  namespace: {{ .Namespace }}\nspec:\n  image: {{ .ImageReference }}\n"
	crlf := strings.ReplaceAll(lf, "\n", "\r\n")

	loadOne := func(body string) render.Template {
		tmpl, err := render.LoadFS(fstest.MapFS{"10-svc.yaml.tmpl": &fstest.MapFile{Data: []byte(body)}})
		if err != nil {
			t.Fatalf("LoadFS: %v", err)
		}
		return tmpl
	}
	a, b := loadOne(lf), loadOne(crlf)
	if a.Identity() != b.Identity() {
		t.Errorf("template identity differs across line endings:\n LF=%+v\nCRLF=%+v", a.Identity(), b.Identity())
	}

	ev := testEvidence(t, digestA)
	ra, err := render.Render(a, testTarget(), ev, testWeights)
	if err != nil {
		t.Fatalf("render LF: %v", err)
	}
	rb, err := render.Render(b, testTarget(), ev, testWeights)
	if err != nil {
		t.Fatalf("render CRLF: %v", err)
	}
	if ra.Digest() != rb.Digest() {
		t.Errorf("bundle digest differs across line endings: %s vs %s", ra.Digest(), rb.Digest())
	}
}

// TestTemplateIdentityChangesWithContent proves the pinned identity actually tracks
// content, so an operator edit cannot pass unnoticed under a reused version label.
func TestTemplateIdentityChangesWithContent(t *testing.T) {
	base := "apiVersion: v1\nkind: Service\nmetadata:\n  name: {{ .Application }}\nspec:\n  image: {{ .ImageReference }}\n"
	load := func(body, meta string) render.Template {
		files := fstest.MapFS{"10-svc.yaml.tmpl": &fstest.MapFile{Data: []byte(body)}}
		if meta != "" {
			files["TEMPLATE"] = &fstest.MapFile{Data: []byte(meta)}
		}
		tmpl, err := render.LoadFS(files)
		if err != nil {
			t.Fatalf("LoadFS: %v", err)
		}
		return tmpl
	}
	a := load(base, "name: x\nversion: v1\n")
	b := load(base+"  replicas: 2\n", "name: x\nversion: v1\n")
	if a.Identity().ContentDigest == b.Identity().ContentDigest {
		t.Error("template content digest did not change when a resource file changed")
	}
	if a.Identity().Version != b.Identity().Version {
		t.Error("expected the reused version label to be identical; the digest is what distinguishes them")
	}
}

func TestMetadataFileRejectsUnknownKey(t *testing.T) {
	_, err := render.LoadFS(fstest.MapFS{
		"10-svc.yaml.tmpl": &fstest.MapFile{Data: []byte("apiVersion: v1\nkind: Service\nmetadata:\n  name: a\n")},
		"TEMPLATE":         &fstest.MapFile{Data: []byte("verison: v1\n")},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown_template_metadata_key") {
		t.Errorf("error = %v, want unknown_template_metadata_key", err)
	}
}

func TestLoadRejectsTemplateWithNoResourceFiles(t *testing.T) {
	_, err := render.LoadFS(fstest.MapFS{"README.md": &fstest.MapFile{Data: []byte("# nothing here\n")}})
	if err == nil || !strings.Contains(err.Error(), "no_resource_templates") {
		t.Errorf("error = %v, want no_resource_templates", err)
	}
}

// TestLaneWeightsChangeOnlyTheRolloutHash is ticket 07's core render
// property: rendering the same target and evidence under two different
// lanes (the "low" risk fast lane vs the "high" risk guarded lane) must
// change exactly one resource's hash -- the Rollout, since it is the
// only resource whose bytes carry `steps:`. Nothing else in the bundle
// may depend on which lane was selected.
func TestLaneWeightsChangeOnlyTheRolloutHash(t *testing.T) {
	fast := []int{5, 100}               // "low" risk lane: 1 gate
	guarded := []int{1, 5, 25, 50, 100} // "high" risk lane: 4 gates

	tmpl := loadFixture(t)
	ev := testEvidence(t, digestA)

	a, err := render.Render(tmpl, testTarget(), ev, fast)
	if err != nil {
		t.Fatalf("Render(fast): %v", err)
	}
	b, err := render.Render(tmpl, testTarget(), ev, guarded)
	if err != nil {
		t.Fatalf("Render(guarded): %v", err)
	}

	resA, resB := a.Resources(), b.Resources()
	if len(resA) != len(resB) {
		t.Fatalf("resource count changed: %d vs %d", len(resA), len(resB))
	}

	var changed []string
	for i := range resA {
		if resA[i].Ref() != resB[i].Ref() {
			t.Fatalf("resource %d identity differs: %+v vs %+v", i, resA[i].Ref(), resB[i].Ref())
		}
		if resA[i].Hash() != resB[i].Hash() {
			changed = append(changed, resA[i].Ref().String())
		}
	}
	if len(changed) != 1 || !strings.HasPrefix(changed[0], "Rollout/") {
		t.Errorf("changed resources = %v, want exactly the Rollout", changed)
	}
}

// TestGateCountingIsWeightsMinusOne is the other half of ticket 07's gate
// arithmetic: N configured weights render N-1 explicit (setWeight, pause)
// step pairs, never N -- the final weight is reached automatically once
// the rollout runs out of steps, and is never itself a step.
func TestGateCountingIsWeightsMinusOne(t *testing.T) {
	tests := []struct {
		weights   []int
		wantGates int
	}{
		{[]int{5, 100}, 1},
		{[]int{5, 25, 50, 100}, 3},
		{[]int{1, 5, 25, 50, 100}, 4},
	}
	tmpl := loadFixture(t)
	ev := testEvidence(t, digestA)
	for _, tc := range tests {
		bundle, err := render.Render(tmpl, testTarget(), ev, tc.weights)
		if err != nil {
			t.Fatalf("Render(%v): %v", tc.weights, err)
		}
		var rollout release.RenderedResource
		for _, res := range bundle.Resources() {
			if res.Ref().Kind == "Rollout" {
				rollout = res
			}
		}
		if rollout.IsZero() {
			t.Fatalf("no Rollout resource in the rendered bundle")
		}
		gotGates := strings.Count(string(rollout.Bytes()), "- pause:")
		if gotGates != tc.wantGates {
			t.Errorf("weights %v: %d pause entries, want %d gates", tc.weights, gotGates, tc.wantGates)
		}
		gotWeights := strings.Count(string(rollout.Bytes()), "- setWeight:")
		if wantSteps := len(tc.weights) - 1; gotWeights != wantSteps {
			t.Errorf("weights %v: %d explicit setWeight steps, want %d (all but the final, implicit weight)",
				tc.weights, gotWeights, wantSteps)
		}
	}
}

// TestPausesAreIndefinite is ticket 09's checklist item made executable:
// nothing about the Rollout self-resumes. A `pause: { duration: ... }`
// step resumes on its own after the clock runs out; only `pause: {}` waits
// for an explicit `argo rollouts promote`, which is the whole point of a
// gate SafeLane controls.
func TestPausesAreIndefinite(t *testing.T) {
	tmpl := loadFixture(t)
	ev := testEvidence(t, digestA)
	bundle, err := render.Render(tmpl, testTarget(), ev, []int{1, 5, 25, 50, 100})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var rollout release.RenderedResource
	for _, res := range bundle.Resources() {
		if res.Ref().Kind == "Rollout" {
			rollout = res
		}
	}
	if rollout.IsZero() {
		t.Fatal("no Rollout resource in the rendered bundle")
	}
	body := string(rollout.Bytes())
	if strings.Contains(body, "duration") {
		t.Errorf("rendered Rollout contains a pause duration; every pause must be indefinite (pause: {}), got:\n%s", body)
	}
	if gotPauses := strings.Count(body, "- pause: {}"); gotPauses != 4 {
		t.Errorf("got %d bare `- pause: {}` entries, want 4 (one per gate, none of them timed)", gotPauses)
	}
}

// TestDeriveEnvelope_RoundTripsThroughTheRealTemplate is ticket 07's
// full round trip against the actual fixture Release Template, not a
// hand-built stand-in: for both a narrow and a wide lane, the envelope
// read back out of the rendered bytes must equal the lane that was
// selected, exactly.
func TestDeriveEnvelope_RoundTripsThroughTheRealTemplate(t *testing.T) {
	tmpl := loadFixture(t)
	ev := testEvidence(t, digestA)

	for _, weights := range [][]int{
		{5, 100},
		{5, 25, 50, 100},
		{1, 5, 25, 50, 100},
	} {
		bundle, err := render.Render(tmpl, testTarget(), ev, weights)
		if err != nil {
			t.Fatalf("Render(%v): %v", weights, err)
		}
		env, templateDigest, err := release.DeriveEnvelope(bundle)
		if err != nil {
			t.Fatalf("DeriveEnvelope(%v): %v", weights, err)
		}
		if got := env.Stages(); !intSlicesEqual(got, weights) {
			t.Errorf("weights %v: derived stages = %v, want exactly the selected lane", weights, got)
		}
		if templateDigest != bundle.Template().ContentDigest {
			t.Errorf("weights %v: derived template digest = %q, want %q", weights, templateDigest, bundle.Template().ContentDigest)
		}
	}
}

func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestFixtureTemplateIdentityIsRecorded checks the fixture's metadata reaches the
// Release, since #48 requires the template version or content digest on the record.
func TestFixtureTemplateIdentityIsRecorded(t *testing.T) {
	id := renderFixture(t, digestA).Template()
	if id.Name != "safelane-demo-api-canary" {
		t.Errorf("template name = %q, want safelane-demo-api-canary", id.Name)
	}
	if id.Version != "v0.1.0-fixture" {
		t.Errorf("template version = %q, want v0.1.0-fixture", id.Version)
	}
	if !release.IsContentDigest(id.ContentDigest) {
		t.Errorf("template content digest = %q, want a sha256 digest", id.ContentDigest)
	}
	if id.FileCount != 7 {
		t.Errorf("template file count = %d, want 7 (5 resources + TEMPLATE + README.md)", id.FileCount)
	}
}
