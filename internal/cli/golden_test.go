package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Golden files are copied out of PLAN.md Appendix A verbatim and are the
// specification, not a record of what the code happened to print. Nothing
// here regenerates them: a mismatch is a defect in the code or a finding
// against the plan, never a reason to overwrite the file.
//
// Volatile values are normalised on both sides rather than deleted, so the
// golden still shows a release id, a digest and a commit where the output
// has one. Measured values that are evidence -- weights, gate counts, file
// counts, +64 −12 -- are compared literally. They are the output.
var normalisers = []struct {
	pattern *regexp.Regexp
	replace string
}{
	{regexp.MustCompile(`rel_[0-9A-Z]{26}`), "rel_<ID>"},
	{regexp.MustCompile(`sha256:[0-9a-f]{64}`), "sha256:<DIGEST>"},
	{regexp.MustCompile(`sha256:[0-9a-f]+…[0-9a-f]*`), "sha256:<DIGEST>"},
	{regexp.MustCompile(`\b[0-9a-f]{40}\b`), "<SHA>"},
	// The truncated commit form, the same courtesy the digest short form
	// already gets. Appendix A abbreviates a merge commit in three
	// different widths across its blocks; normalising the shape rather
	// than one width is what lets all three be compared.
	{regexp.MustCompile(`\b[0-9a-f]{7,12}…`), "<SHA>"},
	{regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T[\d:.]+Z\b`), "<TIME>"},
	{regexp.MustCompile(`\b\d{2}:\d{2}:\d{2}Z\b`), "<TIME>"},
	{regexp.MustCompile(`\b(\d+m)?\d+s\b`), "<DURATION>"},
	{regexp.MustCompile(`safelane-demo-api-success-rate-\d+`), "safelane-demo-api-success-rate-<N>"},
}

func normalise(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for _, n := range normalisers {
		s = n.pattern.ReplaceAllString(s, n.replace)
	}
	return s
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return normalise(string(raw))
}

// assertGolden compares a whole output against a whole Appendix A block.
func assertGolden(t *testing.T, name, actual string) {
	t.Helper()
	want := readGolden(t, name)
	got := normalise(actual)
	if got != want {
		t.Errorf("output does not match %s\n--- want ---\n%s\n--- got ---\n%s\n%s",
			name, want, got, firstDifference(want, got))
	}
}

// assertGoldenFragment compares against an Appendix A block that shows
// only some of the sections -- N4 through N7 print a whole report but the
// plan quotes the part that case is about.
//
// Each blank-line-separated chunk of the golden must appear intact in the
// output, in order. That is stricter than it sounds: a chunk is several
// consecutive lines with exact columns, so an extra line inserted in the
// middle of a quoted Decision block still fails.
func assertGoldenFragment(t *testing.T, name, actual string) {
	t.Helper()
	got := normalise(actual)
	rest := got
	for _, chunk := range strings.Split(strings.TrimRight(readGolden(t, name), "\n"), "\n\n") {
		chunk = strings.TrimRight(chunk, "\n")
		if chunk == "" {
			continue
		}
		idx := strings.Index(rest, chunk)
		if idx < 0 {
			t.Errorf("output does not contain this block from %s\n--- want ---\n%s\n--- full output ---\n%s",
				name, chunk, got)
			return
		}
		rest = rest[idx+len(chunk):]
	}
}

func firstDifference(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) && i < len(g); i++ {
		if w[i] != g[i] {
			return "first differing line " + itoa(i+1) + ":\n  want: " + visible(w[i]) + "\n  got:  " + visible(g[i])
		}
	}
	return "line counts differ: want " + itoa(len(w)) + ", got " + itoa(len(g))
}

// visible makes a column mismatch legible: a difference of one space is
// invisible in a diff and is exactly the kind this suite exists to catch.
func visible(s string) string { return strings.ReplaceAll(s, " ", "·") }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
