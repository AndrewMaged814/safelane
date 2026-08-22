package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"
	"github.com/AndrewMaged814/safelane/internal/orchestrate"
	"github.com/AndrewMaged814/safelane/internal/policy"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/verify/ghcr"
	"github.com/AndrewMaged814/safelane/internal/verify/github"
)

// The check labels. They are the reader's index into the report -- the
// same four names appear under Detected, Failed or Unavailable depending
// on what happened -- so they are written once.
const (
	// checkMergedCommitPrefix is completed with the branch the
	// application releases from, because "on master" is part of the
	// claim: a merge into another branch is not this release's evidence.
	checkMergedCommitPrefix = "Merged commit on "
	checkPublish            = "Required publish check"
	checkDigest             = "Immutable GHCR digest"
)

// checkOwner names which of the three checks a GitHub verification reason
// is about. Verification stops at its first negative answer, so a reason
// belongs to exactly one check, and every check before it in evaluation
// order was proven on the way there.
type checkOwner int

const (
	ownerNone checkOwner = iota
	ownerMergedCommit
	ownerPublish
)

// inspection is the whole `release plan` report as values, before any
// of it is a string. Building it separately from rendering it is what
// lets --json carry the same content without a second derivation of the
// same facts from the same Release.
type inspection struct {
	release        *release.Release
	policy         policy.Policy
	checks         []evidenceCheck
	safety         []evidenceCheck
	history        []*release.Release
	liveState      release.State
	effectiveState release.State
	stateSource    string
}

// buildInspection derives the report from one submission pass.
func buildInspection(insp orchestrate.Inspection, cfg project.Config, pol policy.Policy, now time.Time) inspection {
	checks := evidenceChecks(insp, cfg, now)
	return inspection{
		release: insp.Release,
		policy:  pol,
		checks:  checks,
		safety:  safetySignals(insp, cfg), liveState: release.StateUnknown, effectiveState: insp.Release.State(), stateSource: "recorded",
	}
}

func persistedEvidenceChecks(r *release.Release, cfg project.Config) []evidenceCheck {
	if evidence, ok := r.Evidence().Verified(); ok {
		checks := []evidenceCheck{{label: checkMergedCommitPrefix + cfg.Repository.DefaultBranch, outcome: checkDetected, value: evidence.MergeCommitSHA()}}
		for _, run := range evidence.RequiredChecks() {
			checks = append(checks, evidenceCheck{label: checkPublish, outcome: checkDetected, value: run.Name, tail: "(" + run.Conclusion + ")"})
		}
		checks = append(checks, evidenceCheck{label: checkDigest, outcome: checkDetected, value: shortDigest(evidence.ArtifactDigest())})
		return checks
	}
	outcome := checkFailed
	if r.Evidence().Outcome() == release.EvidenceUnknown {
		outcome = checkUnavailable
	}
	var checks []evidenceCheck
	for _, reason := range r.Evidence().Reasons() {
		checks = append(checks, evidenceCheck{label: "Recorded evidence", outcome: outcome, value: reason.Code, detail: reason.Message, remedy: reason.Remedy})
	}
	return checks
}

// evidenceChecks derives the four evidence rows from the two verification
// results.
//
// The ordering rule that makes the report honest: a check reports
// unavailable, never failed, when it never ran. GitHub's evaluation stops
// at its first negative answer, so everything it did not reach is
// "skipped", and the reason it was skipped names the check that stopped
// it. Collapsing those into failures would claim SafeLane looked at
// evidence it never requested.
func evidenceChecks(insp orchestrate.Inspection, cfg project.Config, now time.Time) []evidenceCheck {
	gh, gr := insp.GitHub, insp.GHCR
	mergedLabel := checkMergedCommitPrefix + cfg.Repository.DefaultBranch

	var facts github.Facts
	if gh.Facts != nil {
		facts = *gh.Facts
	}
	owner := reasonOwner(gh)

	merged := mergedCommitCheck(mergedLabel, gh, facts, owner)
	var publishes []evidenceCheck
	for _, name := range cfg.Release.RequiredCheckNames() {
		publishes = append(publishes, publishCheck(name, gh, facts, owner, merged.outcome, now))
	}
	digest := digestCheck(cfg, gr, facts, merged.outcome)
	checks := []evidenceCheck{merged}
	checks = append(checks, publishes...)
	return append(checks, digest)
}

func safetySignals(insp orchestrate.Inspection, cfg project.Config) []evidenceCheck {
	if insp.GitHub.Facts == nil {
		return nil
	}
	mandatory := map[string]bool{}
	for _, name := range cfg.Release.RequiredCheckNames() {
		mandatory[name] = true
	}
	var out []evidenceCheck
	for _, run := range insp.GitHub.Facts.CheckRuns {
		if mandatory[run.Name] || (run.Conclusion != "failure" && run.Conclusion != "cancelled") {
			continue
		}
		out = append(out, evidenceCheck{label: run.Name, outcome: checkFailed, value: run.Conclusion, detail: "non-gating check"})
	}
	return out
}

// reasonOwner says which of the three checks a GitHub verification reason
// belongs to. A reason about the required check says nothing about the
// merge commit, which was already proven by the time evaluation reached
// it.
func reasonOwner(gh github.Result) checkOwner {
	switch gh.Reason {
	case github.ReasonRequiredCheckMissing, github.ReasonRequiredCheckFailed,
		github.ReasonRequiredCheckWrongSHA, github.ReasonRequiredCheckIncomplete:
		return ownerPublish
	case github.ReasonNone:
		return ownerNone
	default:
		return ownerMergedCommit
	}
}

func mergedCommitCheck(label string, gh github.Result, facts github.Facts, owner checkOwner) evidenceCheck {
	c := evidenceCheck{label: label}
	if owner != ownerMergedCommit {
		// Either everything verified, or the negative answer came from a
		// check that only runs once the merge commit is proven.
		c.outcome, c.value = checkDetected, facts.MergeCommitSHA
		return c
	}
	if gh.Status == github.StatusUnknown {
		c.outcome, c.value = checkUnavailable, gh.Detail
		return c
	}
	c.outcome = checkFailed
	switch gh.Reason {
	case github.ReasonNotMerged:
		c.value = fmt.Sprintf("pull request #%d is open", facts.Number)
		c.remedy = "merge the pull request, then retry"
	case github.ReasonBaseRefMismatch:
		c.value = fmt.Sprintf("merged into %s, not %s", facts.BaseRef, strings.TrimPrefix(label, checkMergedCommitPrefix))
		c.remedy = "merge into the branch this application releases from"
	default:
		c.value = gh.Detail
		c.remedy = "correct the pull request named above, then retry"
	}
	return c
}

func publishCheck(name string, gh github.Result, facts github.Facts, owner checkOwner, merged checkOutcome, now time.Time) evidenceCheck {
	c := evidenceCheck{label: checkPublish}
	if merged != checkDetected {
		c.outcome, c.value = checkUnavailable, "skipped (no merge commit)"
		return c
	}
	run, found := facts.CheckRun(name)
	if found && run.HeadSHA == facts.MergeCommitSHA && run.Status == "completed" && run.Conclusion == "success" {
		c.outcome, c.value, c.tail = checkDetected, name, "("+run.Conclusion+")"
		return c
	}
	if !found {
		c.outcome = checkUnavailable
		c.value = name + " (not found)"
		c.detail = "no such check ran for this exact commit"
		c.remedy = "run the mandatory check for the merge commit, then retry explicitly"
		return c
	}
	if gh.Status == github.StatusUnknown {
		c.outcome = checkUnavailable
		c.value = fmt.Sprintf("%s (%s, queued %s ago)", name, run.Status, since(run.StartedAt, now))
		return c
	}
	c.outcome = checkFailed
	c.value = fmt.Sprintf("%s (%s)", name, run.Conclusion)
	c.detail = "the publish workflow failed for this exact commit"
	c.remedy = "fix the build, merge again"
	return c
}

func digestCheck(cfg project.Config, gr ghcr.Result, facts github.Facts, merged checkOutcome) evidenceCheck {
	c := evidenceCheck{label: checkDigest}
	if merged != checkDetected || facts.MergeCommitSHA == "" {
		c.outcome, c.value = checkUnavailable, "skipped (no merge commit)"
		return c
	}
	switch gr.Status {
	case ghcr.StatusVerified:
		c.outcome, c.value = checkDetected, shortDigest(gr.ResolvedDigest)
	case ghcr.StatusUnknown:
		c.outcome = checkUnavailable
		c.value = "no manifest yet for tag " + project.ImageTag(cfg.Release.ImageTag, facts.MergeCommitSHA)
	default:
		c.outcome, c.value = checkFailed, gr.Detail
		c.remedy = "resolve the digest in the repository this application releases from"
	}
	return c
}

// since renders how long ago t was, in the `40s` / `3m10s` form the rest
// of SafeLane's durations use. An unset time reports "0s" rather than a
// nonsense interval since the zero year.
func since(t, now time.Time) string {
	if t.IsZero() || now.Before(t) {
		return "0s"
	}
	return now.Sub(t).Round(time.Second).String()
}

// Render writes the report Appendix A specifies: Target, Detected,
// Failed, Unavailable, Assessment, Rendered Manifest Bundle, Decision.
//
// The order is the order of the questions. What is this aimed at, what
// did I find, what did I fail to find, what could I not look at, what do
// I make of it, what exactly would I apply, and what happens next.
func (in inspection) Render() string {
	var b strings.Builder
	r := in.release

	fmt.Fprintf(&b, "\n%s%s\n\n", pad("SafeLane investigation", 42), r.ID)
	if len(in.history) > 0 {
		fmt.Fprintf(&b, "Recorded state: %s\n", r.State())
		b.WriteString("Attempt history:\n")
		for _, attempt := range in.history {
			fmt.Fprintf(&b, "  %d  %s  %-13s %s", attempt.AttemptNumber(), attempt.ID, attempt.State(), attempt.CreatedAt.Format(time.RFC3339))
			if attempt.RetryOf() != "" {
				fmt.Fprintf(&b, "  retry_of=%s", attempt.RetryOf())
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	target := in.targetSection()
	target.render(&b, labelWidth(target))
	b.WriteString("\n")

	width := checkLabelWidth(in.checks)
	renderChecks(&b, "Detected", "✓", in.checksWith(checkDetected), width, true)
	b.WriteString("\n")
	if failed := in.checksWith(checkFailed); len(failed) > 0 {
		renderChecks(&b, "Failed", "✗", failed, width, false)
		b.WriteString("\n")
	}
	if unavailable := in.checksWith(checkUnavailable); len(unavailable) > 0 {
		renderChecks(&b, "Unavailable", "–", unavailable, width, false)
		b.WriteString("\n")
	}
	if len(in.safety) > 0 {
		renderChecks(&b, "Safety signals (non-gating)", "!", in.safety, checkLabelWidth(in.safety), false)
		b.WriteString("\n")
	}

	assessment := in.assessmentSection()
	bundle := in.bundleSection()
	decision := in.decisionSection()
	width = labelWidth(assessment, bundle, decision)

	assessment.render(&b, width)
	b.WriteString("\n")
	if len(bundle.rows) > 0 {
		bundle.render(&b, width)
		// The resource listing is not a labelled row: it is a table under
		// one, indented past it.
		in.renderBundleResources(&b)
		b.WriteString("\n")
	}
	decision.render(&b, width)

	b.WriteString("\n")
	if next := nextCommand(r); next != "" {
		fmt.Fprintf(&b, "Nothing was changed.\nNext: %s\n", next)
	}
	if r.State() == release.StatePromoted {
		b.WriteString("This artifact was already released; no redeploy is offered.\n")
	}
	if r.State() == release.StateIneligible {
		b.WriteString("No rollout may start. This outcome is not retryable.\n")
	}
	if r.State() == release.StateIndeterminate {
		b.WriteString("SafeLane could not determine the answer. This is not a refusal. Retry.\n")
	}
	if r.State() == release.StateUnknown {
		b.WriteString("Live identity could not be proven. No transition is allowed.\n")
	}
	return b.String()
}

func nextCommand(r *release.Release) string {
	switch r.State() {
	case release.StateReady:
		return "safelane release run " + string(r.ID)
	case release.StateStarting, release.StateProgressing, release.StateAnalysing, release.StateAtGate, release.StatePaused:
		return "safelane release status " + string(r.ID)
	case release.StateAborted, release.StateFailed, release.StateBlocked:
		return "safelane release retry " + string(r.ID)
	case release.StateIndeterminate:
		if r.Eligibility().Retryable() {
			return "safelane release retry " + string(r.ID)
		}
	}
	return ""
}

func (in inspection) checksWith(outcome checkOutcome) []evidenceCheck {
	var out []evidenceCheck
	for _, c := range in.checks {
		if c.outcome == outcome {
			out = append(out, c)
		}
	}
	return out
}

func (in inspection) targetSection() labeledSection {
	t := in.release.Target()
	return labeledSection{
		title:     "Target",
		tailWidth: 16,
		rows: []labeledRow{
			{label: "application", value: t.Application},
			{label: "environment", value: t.Environment},
			{label: "cluster", value: t.Cluster, tail: "namespace " + t.Namespace},
		},
	}
}

// assessmentSection is the heart of the report: what SafeLane made of the
// change, and how far that lets it ship per step.
//
// A release that is not eligible has one row, and it is a refusal to
// speculate. Assessment is a question about a change that may ship at
// all; answering it for a change that may not would attach a lane to
// something that never earned one.
func (in inspection) assessmentSection() labeledSection {
	s := labeledSection{title: "Assessment", tailWidth: 8}
	a, ok := in.release.Assessment()
	if !ok {
		s.rows = []labeledRow{{
			label: "not performed",
			value: fmt.Sprintf("an %s release receives no lane", in.release.Eligibility().Status()),
		}}
		return s
	}

	s.rows = append(s.rows, changeRow(a.Facts), authorshipRow(a.Facts))
	s.rows = append(s.rows, heuristicRow(a.Heuristic, in.policy.Assessment.Heuristic))
	s.rows = append(s.rows, modelRow(a.Model))

	combinedBy := "(the worse of the two)"
	if a.HeuristicOnly() {
		combinedBy = "(heuristic only)"
	}
	s.rows = append(s.rows, labeledRow{label: "risk", value: string(a.Risk), tail: combinedBy})

	weights := in.laneWeights(a.Lane)
	gates := gateCount(weights)
	s.rows = append(s.rows, labeledRow{
		label: "lane",
		value: a.Lane,
		tail:  weightLadder(weights) + "   " + fmt.Sprintf("(%d %s)", gates, plural(gates, "gate", "gates")),
	})
	return s
}

// laneWeights prefers the envelope read back out of the rendered Rollout
// over the lane's declared weights. They should be identical; the one
// that was actually hashed and will actually be applied is the one worth
// printing.
func (in inspection) laneWeights(lane string) []int {
	if env, ok := in.release.Eligibility().Envelope(); ok {
		return env.Stages()
	}
	return in.policy.Lanes[lane].Weights
}

func changeRow(f assess.Facts) labeledRow {
	row := labeledRow{
		label: "change",
		value: fmt.Sprintf("%d %s, +%d −%d",
			len(f.Files), plural(len(f.Files), "file", "files"), f.TotalAdditions, f.TotalDeletions),
	}
	if len(f.Files) == 1 {
		// The one file's counts are already on the line above; repeating
		// them beside its path says nothing new.
		row.cont = []string{f.Files[0].Path}
		return row
	}
	widest := 0
	for _, file := range f.Files {
		if n := runeLen(file.Path); n > widest {
			widest = n
		}
	}
	for _, file := range f.Files {
		row.cont = append(row.cont, strings.TrimRight(fmt.Sprintf("%s%s −%d",
			pad(file.Path, widest+1), padLeft(fmt.Sprintf("+%d", file.Additions), 3), file.Deletions), " "))
	}
	return row
}

// authorshipRow says who wrote the change and shows the evidence for it,
// because "agent" is a claim that raises the risk floor and a reader is
// entitled to see what proved it.
func authorshipRow(f assess.Facts) labeledRow {
	if !f.AgentAuthored {
		return labeledRow{
			label: "authored by",
			value: "human",
			tail:  fmt.Sprintf("(no agent trailer on %s)", abbrev(f.MergeCommitSHA, 8)),
		}
	}
	return labeledRow{
		label:      "authored by",
		value:      "agent",
		tail:       f.AgentEvidence,
		cont:       []string{"on merge commit " + abbrev(f.MergeCommitSHA, 7) + "…"},
		contAtTail: true,
	}
}

func heuristicRow(v assess.Verdict, cfg assess.HeuristicConfig) labeledRow {
	row := labeledRow{label: "heuristic", value: string(v.Risk), contAtTail: true}
	if !v.Available {
		row.value, row.tail = "unavailable", ""
		row.cont = splitReasons(v.Reason)
		return row
	}
	if len(v.Rules) == 0 {
		row.tail = v.Rationale
		return row
	}
	widest := 0
	quoted := make([]string, len(v.Rules))
	for i, name := range v.Rules {
		quoted[i] = fmt.Sprintf("rule %q", name)
		if n := runeLen(quoted[i]); n > widest {
			widest = n
		}
	}
	lines := make([]string, len(v.Rules))
	for i, name := range v.Rules {
		floor, ok := cfg.MinimumFor(name)
		if !ok {
			// The rule fired under a configuration this build no longer
			// holds -- a record read back after policy.yml changed. Say
			// the rule fired; do not invent the floor it raised.
			lines[i] = quoted[i]
			continue
		}
		lines[i] = pad(quoted[i], widest+4) + "floor → " + string(floor)
	}
	row.tail, row.cont = lines[0], lines[1:]
	return row
}

// modelRow names the assessor that answered. Which model rated a change
// is part of the finding, not an implementation detail: a reader
// comparing two releases needs to know whether the same one looked at
// both.
func modelRow(v assess.Verdict) labeledRow {
	if !v.Available {
		return labeledRow{
			label:      "model",
			value:      "unavailable",
			cont:       splitReasons(v.Reason),
			contAtTail: true,
		}
	}
	label := "model"
	if v.Assessor != "" {
		label = "model  (" + v.Assessor + ")"
	}
	row := labeledRow{label: label, value: string(v.Risk), contAtTail: true}
	lines := wrapRationale(`"`+v.Rationale+`"`, 2+18+8)
	if len(lines) > 0 {
		row.tail, row.cont = lines[0], lines[1:]
	}
	return row
}

// splitReasons turns an assessor's joined reason string back into one
// line per attempt, so "claude failed and codex is not installed" reads
// as two facts rather than one sentence.
func splitReasons(reason string) []string {
	if reason == "" {
		return nil
	}
	parts := strings.Split(reason, "; ")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func (in inspection) bundleSection() labeledSection {
	bundle, ok := in.release.Bundle()
	if !ok {
		return labeledSection{title: "Rendered Manifest Bundle"}
	}
	return labeledSection{
		title:     "Rendered Manifest Bundle",
		tailWidth: 8,
		rows: []labeledRow{
			{label: "template digest", value: shortDigest(bundle.Template().ContentDigest)},
			{label: fmt.Sprintf("%d resources hashed", bundle.Len()), bare: true},
		},
	}
}

func (in inspection) renderBundleResources(b *strings.Builder) {
	bundle, ok := in.release.Bundle()
	if !ok {
		return
	}
	hashes := bundle.Hashes()
	nameWidth, kindWidth := 0, 0
	for _, h := range hashes {
		if n := runeLen(h.Ref.Name); n > nameWidth {
			nameWidth = n
		}
		if n := runeLen(h.Ref.Kind); n > kindWidth {
			kindWidth = n
		}
	}
	for _, h := range hashes {
		fmt.Fprintf(b, "    %s%s%s\n",
			pad(h.Ref.Name, nameWidth+2), pad(h.Ref.Kind, kindWidth+2), shortHash(h.Hash))
	}
}

// decisionSection is the answer, and it is deliberately terse: an
// eligible release states the envelope it earned and where it came from;
// anything else states why not and whether it is worth trying again.
func (in inspection) decisionSection() labeledSection {
	s := labeledSection{title: "Decision", tailWidth: 8}
	elig := in.release.Eligibility()

	env, eligible := elig.Envelope()
	if !eligible {
		s.rows = []labeledRow{
			{label: "eligibility", value: elig.Status().String()},
			{label: "reason", value: elig.ReasonCode()},
			{label: "retryable", value: fmt.Sprintf("%t", elig.Retryable())},
			{label: "envelope", value: "none"},
		}
		return s
	}

	weights := env.Stages()
	gates := gateCount(weights)
	ladder := weightLadder(weights)
	gap := 10
	if n := runeLen(ladder) + 2; n > gap {
		gap = n
	}

	lane := ""
	if a, ok := in.release.Assessment(); ok {
		lane = a.Lane
	}
	s.rows = []labeledRow{
		{label: "eligibility", value: elig.Status().String()},
		{label: "policy version", value: in.policy.Version},
		{label: "reason", value: elig.ReasonCode()},
		{
			label: "envelope",
			value: pad(ladder, gap) + fmt.Sprintf("(%d weights, %d %s)", len(weights), gates, plural(gates, "gate", "gates")),
			cont: []string{
				fmt.Sprintf("lane %q, selected by assessment,", lane),
				"read back from the hashed Rollout",
			},
		},
		{label: "next action", value: env.NextAction()},
	}
	return s
}

// abbrev shortens a commit SHA for prose. It never pads: a short SHA
// stays short rather than being reported as longer than it is.
func abbrev(sha string, n int) string {
	if len(sha) <= n {
		return sha
	}
	return sha[:n]
}
