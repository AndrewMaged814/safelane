package cli

import (
	"fmt"
	"strings"
)

// The `release plan` report is a fixed-column layout, and Appendix A
// is its specification down to the space. This file is the layout engine;
// inspect.go decides what goes in it.
//
// Two column families, and they are not the same:
//
//   - Labelled rows -- Target, Assessment, Rendered Manifest Bundle,
//     Decision -- put the label at column 3 and the value at the first
//     even column at least three past the longest label in the group,
//     never nearer than [minLabelWidth]. A group is a set of sections that
//     share one width so their values line up as one column down the page.
//   - Evidence rows -- Detected, Failed, Unavailable -- put a status mark
//     at column 3 and the label after it, and are wide enough that the
//     four check names in Appendix A all clear the same value column.
//
// Both are computed from content rather than hard-coded, so a longer
// application name or check name shifts the column instead of ragging the
// output.
const (
	// minLabelWidth is the narrowest label field. Target's labels are
	// short enough to want less; holding the floor is what keeps a
	// negative case's Decision block lined up with a positive one's
	// Target block.
	minLabelWidth = 16
	// labelGap is the whitespace between the longest label in a group and
	// the value column.
	labelGap = 3
	// minCheckLabelWidth is the evidence-row label field.
	minCheckLabelWidth = 30
	// checkValueWidth is the first column of a two-part evidence answer,
	// so "build-and-push" and its conclusion do not run together.
	checkValueWidth = 16
	// reportWidth is the column the report wraps prose at. Only model
	// rationale wraps; every other value is short by construction.
	reportWidth = 80
)

// labeledRow is one `label   value  tail` line plus any continuation
// lines beneath it.
type labeledRow struct {
	label string
	value string
	// tail is a second column after value, padded to the section's
	// tailWidth. Empty means the row is just label and value.
	tail string
	// cont are continuation lines. They align under value, or under tail
	// when contAtTail is set -- which is the difference between a wrapped
	// file list (under value) and a wrapped model rationale (under tail).
	cont       []string
	contAtTail bool
	// bare marks a row that is a sentence rather than a label and a
	// value -- "5 resources hashed", which introduces the table under
	// it. It is written at the label column and excluded from the
	// width calculation, so a long one does not push every value in
	// its group to the right.
	bare bool
}

// labeledSection is one titled block of labelled rows. tailWidth is the
// width of the second column: 8 in Assessment (risk words), 16 in Target
// (a cluster name before "namespace ...").
type labeledSection struct {
	title     string
	rows      []labeledRow
	tailWidth int
}

// labelWidth returns the field width a set of sections share, so their
// values form one column.
func labelWidth(sections ...labeledSection) int {
	widest := 0
	for _, s := range sections {
		for _, r := range s.rows {
			if r.bare {
				continue
			}
			if n := runeLen(r.label); n > widest {
				widest = n
			}
		}
	}
	width := widest + labelGap
	if width%2 == 1 {
		width++
	}
	if width < minLabelWidth {
		width = minLabelWidth
	}
	return width
}

func (s labeledSection) render(b *strings.Builder, width int) {
	if len(s.rows) == 0 {
		return
	}
	fmt.Fprintf(b, "%s\n", s.title)
	valueCol := 2 + width
	for _, r := range s.rows {
		if r.bare {
			fmt.Fprintf(b, "  %s\n", r.label)
			continue
		}
		line := "  " + padSep(r.label, width)
		if r.tail != "" {
			line += padSep(r.value, s.tailWidth) + r.tail
		} else {
			line += r.value
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")

		contCol := valueCol
		if r.contAtTail {
			contCol += s.tailWidth
		}
		for _, c := range r.cont {
			b.WriteString(strings.Repeat(" ", contCol) + c + "\n")
		}
	}
}

// checkOutcome is what SafeLane found when it went to look. The three are
// not interchangeable and the distinction is the point of the report:
// failed means "I looked and the answer is no", unavailable means "I could
// not look".
type checkOutcome int

const (
	checkDetected checkOutcome = iota
	checkFailed
	checkUnavailable
)

// evidenceCheck is one row of the Detected/Failed/Unavailable sections.
type evidenceCheck struct {
	outcome checkOutcome
	label   string
	value   string
	// tail is a second column after value, for a check whose answer has
	// two parts -- a check-run name and its conclusion. It is a layout
	// concern only: the machine form joins the two with one space rather
	// than carrying the padding.
	tail string
	// detail and remedy are the indented lines under a row. detail says
	// what is wrong in a sentence; remedy says what to do about it.
	detail string
	remedy string
}

func checkLabelWidth(checks []evidenceCheck) int {
	widest := 0
	for _, c := range checks {
		if n := runeLen(c.label); n > widest {
			widest = n
		}
	}
	if width := widest + labelGap; width > minCheckLabelWidth {
		return width
	}
	return minCheckLabelWidth
}

// renderChecks writes one evidence section. Detected always renders, with
// "(none)" when nothing was found -- an empty Detected block is itself the
// finding. Failed and Unavailable are omitted when empty, because a
// heading with nothing under it reads as a claim that something was
// checked.
func renderChecks(b *strings.Builder, title, mark string, checks []evidenceCheck, width int, showEmpty bool) {
	if len(checks) == 0 {
		if !showEmpty {
			return
		}
		fmt.Fprintf(b, "%s\n  (none)\n", title)
		return
	}
	fmt.Fprintf(b, "%s\n", title)
	for _, c := range checks {
		value := c.value
		if c.tail != "" {
			value = padSep(value, checkValueWidth) + c.tail
		}
		b.WriteString(strings.TrimRight("  "+mark+" "+padSep(c.label, width)+value, " ") + "\n")
		if c.detail != "" {
			fmt.Fprintf(b, "      %s\n", c.detail)
		}
		if c.remedy != "" {
			fmt.Fprintf(b, "      remedy: %s\n", c.remedy)
		}
	}
}

// pad left-aligns s in a field of width runes. It counts runes, not
// bytes: values in this report contain →, − and …, and a byte-counting
// pad would silently narrow every column that holds one.
func pad(s string, width int) string {
	if n := runeLen(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// padSep is pad for a column that another column butts up against. It
// never returns a value flush against the next one: a lane named
// "standard" is exactly as wide as the field it sits in, and without this
// the report reads "standard5 → 25 → 50 → 100". One space is the
// difference between a column and a collision.
func padSep(s string, width int) string {
	if runeLen(s) >= width {
		return s + " "
	}
	return pad(s, width)
}

// padLeft right-aligns s, for the addition counts in a file list, where
// "+1" and "+41" have to end in the same column.
func padLeft(s string, width int) string {
	if n := runeLen(s); n < width {
		return strings.Repeat(" ", width-n) + s
	}
	return s
}

func runeLen(s string) int { return len([]rune(s)) }

// clauseBackup is how far wrapRationale will give up to end a line on a
// clause boundary instead of mid-clause: about one short word. A comma
// three characters from the wrap point is worth breaking at; a semicolon
// twenty-six characters back is not, and reaching for it would leave a
// visibly short line.
const clauseBackup = 8

// wrapRationale word-wraps model prose to reportWidth at the given
// indent, preferring to end a line just after a comma or semicolon when
// one falls within [clauseBackup] characters of where a greedy wrap would
// have broken.
//
// The preference is what keeps a rationale readable as sentences rather
// than as a justified block: a line that ends "...no request path," reads
// as a finished thought, while the greedy "...no request path, no" reads
// as a truncation. It costs at most a few characters of line length.
func wrapRationale(text string, indent int) []string {
	limit := reportWidth - indent
	if limit < 20 {
		limit = 20
	}
	words := strings.Fields(text)
	var lines []string
	for len(words) > 0 {
		line := words[0]
		taken := 1
		for taken < len(words) && runeLen(line)+1+runeLen(words[taken]) <= limit {
			line += " " + words[taken]
			taken++
		}
		if taken < len(words) {
			if shorter, n := backUpToClause(line, words[:taken]); n > 0 {
				line, taken = shorter, n
			}
		}
		lines = append(lines, line)
		words = words[taken:]
	}
	return lines
}

// backUpToClause returns the longest prefix of the words on this line
// that ends in a clause boundary, if dropping the rest costs no more than
// clauseBackup characters. n is how many words the prefix used; zero
// means keep the greedy line.
func backUpToClause(line string, words []string) (string, int) {
	full := runeLen(line)
	for i := len(words) - 1; i > 0; i-- {
		w := words[i-1]
		if !strings.HasSuffix(w, ",") && !strings.HasSuffix(w, ";") {
			continue
		}
		candidate := strings.Join(words[:i], " ")
		if full-runeLen(candidate) > clauseBackup {
			return "", 0
		}
		return candidate, i
	}
	return "", 0
}

// shortDigest is the `sha256:xxxxxxxx…xxxx` form the report uses for a
// digest that has a line to itself.
func shortDigest(digest string) string {
	hex, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(hex) < 14 {
		return digest
	}
	return "sha256:" + hex[:8] + "…" + hex[len(hex)-4:]
}

// shortHash is the narrower `sha256:xxxxxx…` form the bundle listing
// uses, where five hashes are read against each other rather than copied.
func shortHash(digest string) string {
	hex, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(hex) < 6 {
		return digest
	}
	return "sha256:" + hex[:6] + "…"
}

// weightLadder renders a lane's weights the way the report says them out
// loud: 1 → 5 → 25 → 50 → 100.
func weightLadder(weights []int) string {
	parts := make([]string, len(weights))
	for i, w := range weights {
		parts[i] = fmt.Sprintf("%d", w)
	}
	return strings.Join(parts, " → ")
}

// gateCount is the number of pauses a ladder contains. N weights make
// N-1 gates: the final weight is reached when the rollout runs out of
// steps, not at a gate of its own (Appendix C5).
func gateCount(weights []int) int {
	if len(weights) == 0 {
		return 0
	}
	return len(weights) - 1
}

func plural(n int, singular, many string) string {
	if n == 1 {
		return singular
	}
	return many
}
